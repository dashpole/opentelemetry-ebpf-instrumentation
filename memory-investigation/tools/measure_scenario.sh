#!/usr/bin/env bash
set -euo pipefail

SCENARIO="${1:?scenario name}"
OBI_BIN="${2:?path to obi binary}"
WORKLOAD="${3:?idle|generic|go|mixed}"
CFG_FILE="${4:?path to obi config yaml}"
OUT_DIR="${5:-/tmp/obi-bench/results}"

mkdir -p "$OUT_DIR"
TARGET_NAME="obi-bench-target"
AGENT_NAME="obi-bench-agent"

cleanup() {
  docker rm -f "$AGENT_NAME" "$TARGET_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

# Start isolated target container with workload(s)
case "$WORKLOAD" in
  idle)
    TARGET_CMD="sleep 300"
    ;;
  generic)
    TARGET_CMD="python3 -u -c '
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.send_header(\"Content-Length\",\"3\"); self.end_headers(); self.wfile.write(b\"ok\\n\")
    def log_message(self, *a): pass
print(\"py-http-srv ready\", flush=True)
ThreadingHTTPServer((\"0.0.0.0\", 18081), H).serve_forever()
'"
    ;;
  go)
    TARGET_CMD="/bench/go-http-srv :18080"
    ;;
  mixed)
    TARGET_CMD="/bench/go-http-srv :18080 &
python3 -u -c '
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.send_header(\"Content-Length\",\"3\"); self.end_headers(); self.wfile.write(b\"ok\\n\")
    def log_message(self, *a): pass
print(\"py-http-srv ready\", flush=True)
ThreadingHTTPServer((\"0.0.0.0\", 18081), H).serve_forever()
'"
    ;;
  *)
    echo "unknown workload: $WORKLOAD" >&2
    exit 1
    ;;
esac

docker run -d --name "$TARGET_NAME" \
  -v /tmp/obi-bench:/bench:ro \
  python:3-alpine sh -c "$TARGET_CMD" >/dev/null

# Wait briefly for workload servers to open ports
sleep 2

# Start OBI in its own isolated container sharing target's PID + NET namespaces
# so OBI has its own dedicated cgroup v2 for exact kernel+userspace memory accounting.
docker run -d --name "$AGENT_NAME" \
  --privileged \
  --memory=2g --memory-swap=2g \
  --pid="container:$TARGET_NAME" \
  --net="container:$TARGET_NAME" \
  -e "OBI_PROTO_NO_PREALLOC=${OBI_PROTO_NO_PREALLOC:-}" \
  -v "$OBI_BIN:/obi:ro" \
  -v /tmp/obi-bench:/bench:ro \
  -v "$CFG_FILE:/config.yaml:ro" \
  alpine:latest sh -c 'mkdir -p /sys/fs/bpf /sys/kernel/tracing && mount -t bpf bpffs /sys/fs/bpf 2>/dev/null || true; mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || true; exec /obi --config /config.yaml' >/dev/null

# Wait for OBI to discover & instrument workload (min_process_age defaults to 5s + BPF verify)
sleep 18

# Verify OBI is alive and find its PID inside the shared PID namespace
OBI_PID=$(docker exec "$AGENT_NAME" sh -c 'for p in /proc/[0-9]*; do [ -r "$p/comm" ] && [ "$(cat "$p/comm")" = "obi" ] && basename "$p" && break; done')
if [ -z "$OBI_PID" ]; then
  echo "ERROR: obi exited early!" >&2
  docker logs "$AGENT_NAME" | tail -n 50 >&2
  exit 1
fi

# Phase A: Capture clean cgroup stats BEFORE running probe or traffic
echo "=== SCENARIO: $SCENARIO (workload=$WORKLOAD, obi_pid=$OBI_PID) ==="
echo "--- [POST-ATTACH IDLE] RAW CGROUP (before probe) ---"
docker exec "$AGENT_NAME" sh -c 'echo "memory.current=$(cat /sys/fs/cgroup/memory.current) memory.peak=$(cat /sys/fs/cgroup/memory.peak)"'
docker exec "$AGENT_NAME" /bench/bpf-mem-probe "$OBI_PID" > "$OUT_DIR/${SCENARIO}_idle.txt"
docker exec "$AGENT_NAME" /bench/bpf-mem-probe "$OBI_PID" --json > "$OUT_DIR/${SCENARIO}_idle.json"
cat "$OUT_DIR/${SCENARIO}_idle.txt" | head -n 12

# Capture Go pprof heap (idle, both live and post-GC)
docker exec "$TARGET_NAME" python3 -c "
import urllib.request
for name, url in [('heap_idle.pb.gz', 'http://127.0.0.1:6060/debug/pprof/heap'), ('heap_idle_gc.pb.gz', 'http://127.0.0.1:6060/debug/pprof/heap?gc=1')]:
    urllib.request.urlretrieve(url, '/tmp/' + name)
"
docker cp "$TARGET_NAME:/tmp/heap_idle.pb.gz" "$OUT_DIR/${SCENARIO}_heap_idle.pb.gz"
docker cp "$TARGET_NAME:/tmp/heap_idle_gc.pb.gz" "$OUT_DIR/${SCENARIO}_heap_idle_gc.pb.gz"

# Phase B: Drive active HTTP traffic if workload != idle
if [ "$WORKLOAD" != "idle" ]; then
  if [ "$WORKLOAD" = "go" ] || [ "$WORKLOAD" = "mixed" ]; then
    docker exec "$TARGET_NAME" /bench/http-loadgen http://127.0.0.1:18080/hello 8 8
  fi
  if [ "$WORKLOAD" = "generic" ] || [ "$WORKLOAD" = "mixed" ]; then
    docker exec "$TARGET_NAME" /bench/http-loadgen http://127.0.0.1:18081/hello 8 8
  fi
  sleep 3

  # Verify real telemetry was captured on Prometheus /metrics
  echo "--- TELEMETRY VERIFICATION (/metrics) ---"
  docker exec "$TARGET_NAME" python3 -c "
import urllib.request
body = urllib.request.urlopen('http://127.0.0.1:9090/metrics', timeout=5).read().decode()
lines = [l for l in body.splitlines() if l.startswith('http_server_request_duration_seconds_count') or l.startswith('obi_network_flow_bytes_total') or l.startswith('obi_tcp_')]
print(f'matching metric series: {len(lines)}')
for l in lines[:10]:
    print('  ' + l)
"

  echo "--- [POST-TRAFFIC ACTIVE] RAW CGROUP (before probe) ---"
  docker exec "$AGENT_NAME" sh -c 'echo "memory.current=$(cat /sys/fs/cgroup/memory.current) memory.peak=$(cat /sys/fs/cgroup/memory.peak)"'
  docker exec "$AGENT_NAME" /bench/bpf-mem-probe "$OBI_PID" > "$OUT_DIR/${SCENARIO}_active.txt"
  docker exec "$AGENT_NAME" /bench/bpf-mem-probe "$OBI_PID" --json > "$OUT_DIR/${SCENARIO}_active.json"
  cat "$OUT_DIR/${SCENARIO}_active.txt" | head -n 12

  docker exec "$TARGET_NAME" python3 -c "
import urllib.request
for name, url in [('heap_active.pb.gz', 'http://127.0.0.1:6060/debug/pprof/heap'), ('heap_active_gc.pb.gz', 'http://127.0.0.1:6060/debug/pprof/heap?gc=1')]:
    urllib.request.urlretrieve(url, '/tmp/' + name)
"
  docker cp "$TARGET_NAME:/tmp/heap_active.pb.gz" "$OUT_DIR/${SCENARIO}_heap_active.pb.gz"
  docker cp "$TARGET_NAME:/tmp/heap_active_gc.pb.gz" "$OUT_DIR/${SCENARIO}_heap_active_gc.pb.gz"
  if [ -x /tmp/obi-bench/bpf-entry-audit ]; then
    docker exec "$AGENT_NAME" /bench/bpf-entry-audit "$OBI_PID" > "$OUT_DIR/${SCENARIO}_entries.txt" || true
  fi
fi

docker logs "$AGENT_NAME" > "$OUT_DIR/${SCENARIO}_obi.log" 2>&1 || true
echo "--- OBI LOG TAIL ---"
tail -n 10 "$OUT_DIR/${SCENARIO}_obi.log"
