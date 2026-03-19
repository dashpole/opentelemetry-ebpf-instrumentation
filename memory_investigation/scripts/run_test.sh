#!/bin/bash

# Usage: run_test.sh <PROTOCOL> <CONFIG> <QPS>
PROTOCOL=${1:-http}
CONFIG=${2:-baseline}
QPS=${3:-10}

CLUSTER_NAME="obi-memory-test"
NAMESPACE="default"

echo "=== Running Metric Test: Protocol=$PROTOCOL Config=$CONFIG QPS=$QPS ==="

echo "Building custom workload image..."
pushd memory_investigation/workloads/generator > /dev/null
docker build -t memory-investigation-workload:latest .
popd > /dev/null

echo "Building OBI image..."
docker build -t otel/ebpf-instrument:latest .

echo "Checking for existing kind cluster..."
if ! ./.tools/kind get clusters | grep -q "$CLUSTER_NAME"; then
  echo "Creating kind cluster..."
  ./.tools/kind create cluster --name "$CLUSTER_NAME"
fi

echo "Loading docker images into kind..."
./.tools/kind load docker-image memory-investigation-workload:latest --name "$CLUSTER_NAME"
./.tools/kind load docker-image otel/ebpf-instrument:latest --name "$CLUSTER_NAME"

echo "Deploying OTEL Collector..."
kubectl apply -f memory_investigation/manifests/otelcol.yaml
kubectl rollout status deployment/otelcol -n $NAMESPACE --timeout=120s || true

echo "Depoying Test Target Server for $PROTOCOL..."
if [ -f "memory_investigation/manifests/servers/${PROTOCOL}-server.yaml" ]; then
    kubectl apply -f memory_investigation/manifests/servers/${PROTOCOL}-server.yaml
    # We don't strictly wait here, the generator deployment will wait for it to be ready via ping loops
else
    echo "No specific server manifest found for $PROTOCOL, assuming it is handled via standard workload."
fi

echo "Deploying Workload Generator ($PROTOCOL)..."
export WORKLOAD_PROTOCOL=$PROTOCOL
export WORKLOAD_QPS=$QPS

case $PROTOCOL in
  http)  export WORKLOAD_TARGET_URL="http-server.default.svc.cluster.local:8080" ;;
  https) export WORKLOAD_TARGET_URL="https-server.default.svc.cluster.local:8443" ;;
  grpc)  export WORKLOAD_TARGET_URL="grpc-server.default.svc.cluster.local:9090" ;;
  redis) export WORKLOAD_TARGET_URL="redis-service.default.svc.cluster.local:6379" ;;
  mysql) export WORKLOAD_TARGET_URL="test:test@tcp(mysql-service.default.svc.cluster.local:3306)/testdb" ;;
  postgres) export WORKLOAD_TARGET_URL="postgres://test:test@postgres-service.default.svc.cluster.local:5432/testdb?sslmode=disable" ;;
  kafka) export WORKLOAD_TARGET_URL="kafka-service.default.svc.cluster.local:9092" ;;
esac

envsubst < memory_investigation/manifests/workload-template.yaml | kubectl apply -f -
kubectl rollout status deployment/workload-client -n $NAMESPACE --timeout=120s || true

echo "Deploying OBI ($CONFIG)..."

export OBI_ENV_VARS=""
case $CONFIG in
  baseline)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "false"
EOF
)
    ;;
  k8s_attributes)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_KUBE_METADATA_ENABLE
              value: "true"
EOF
)
    ;;
  go_tracers)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_TRACES_INSTRUMENTATIONS
              value: "go"
EOF
)
    ;;
  service_graph)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_METRICS_FEATURES
              value: "application,service_graph"
EOF
)
    ;;
  golden_signals)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
EOF
)
    ;;
  traces)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
EOF
)
    ;;
  context_disabled)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "disabled"
EOF
)
    ;;
  context_headers)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "headers"
EOF
)
    ;;
  context_tcp)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_BPF_CONTEXT_PROPAGATION
              value: "tcp"
EOF
)
    ;;
  large_batching)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_BPF_WAKEUP_LEN
              value: "1000"
            - name: OTEL_EBPF_BPF_BATCH_LENGTH
              value: "10000"
EOF
)
    ;;
  large_caches)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "none"
            - name: OTEL_EBPF_BPF_MYSQL_PREPARED_STATEMENTS_CACHE_SIZE
              value: "10000"
            - name: OTEL_KAFKA_TOPIC_UUID_CACHE_SIZE
              value: "10000"
EOF
)
    ;;
  payloads)
    OBI_ENV_VARS=$(cat <<'EOF'
            - name: OTEL_METRICS_EXPORTER
              value: "prometheus"
            - name: OTEL_EBPF_PROMETHEUS_PORT
              value: "8888"
            - name: OTEL_TRACES_EXPORTER
              value: "otlp"
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otelcol.default.svc.cluster.local:4318"
            - name: OTEL_EBPF_HTTP_GRAPHQL_ENABLED
              value: "true"
EOF
)
    ;;
esac

export OBI_ENV_VARS
envsubst < memory_investigation/manifests/obi-template.yaml | kubectl apply -f -
kubectl rollout status daemonset/obi -n $NAMESPACE --timeout=120s || true

echo "All components deployed successfully. Sleeping for 60 seconds to allow stable accumulation..."
sleep 60

echo "Collecting profiles..."
OBI_POD=$(kubectl get pod -l app=obi -n $NAMESPACE -o jsonpath='{.items[0].metadata.name}')

PROFILE_DIR="memory_investigation/artifacts/${PROTOCOL}/${CONFIG}/${QPS}qps"
mkdir -p "$PROFILE_DIR"

# Local port forwarding since pprof is inside the cluster
kubectl port-forward pod/$OBI_POD 8082:8082 &
PORT_FORWARD_PID=$!
sleep 2 # wait for port forward to establish

echo "Downloading heap profile..."
curl -s http://localhost:8082/debug/pprof/heap > "${PROFILE_DIR}/heap.pprof"

echo "Downloading allocs profile..."
curl -s http://localhost:8082/debug/pprof/allocs > "${PROFILE_DIR}/allocs.pprof"

echo "Downloading CPU profile (10s)..."
curl -s "http://localhost:8082/debug/pprof/profile?seconds=10" > "${PROFILE_DIR}/cpu.pprof"

kill $PORT_FORWARD_PID || true

echo "Collecting internal metrics (GC/Memstats)..."
kubectl port-forward pod/$OBI_POD 8081:8081 &
PORT_FORWARD_PID_METRICS=$!
sleep 2
curl -s http://localhost:8081/internal/metrics > "${PROFILE_DIR}/obi-internal-metrics.txt"
kill $PORT_FORWARD_PID_METRICS || true

echo "Collecting Workload Telemetry..."
# 1. Scrape OBI's Prometheus metric exporter directly (for configs using OTEL_METRICS_EXPORTER=prometheus)
kubectl port-forward pod/$OBI_POD 8890:8888 &
PORT_FORWARD_PID_OBI_8888=$!

# 2. Scrape OTEL Collector's internal telemetry (to see otlp receiver stats)
OTELCOL_POD=$(kubectl get pod -l app=otelcol -n $NAMESPACE -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward pod/$OTELCOL_POD 8888:8888 &
PORT_FORWARD_PID_OTEL_8888=$!

# 3. Scrape OTEL Collector's Prometheus metric exporter (for configs using OTEL_METRICS_EXPORTER=otlp)
kubectl port-forward pod/$OTELCOL_POD 8889:8889 &
PORT_FORWARD_PID_OTEL_8889=$!

sleep 2

curl -s http://localhost:8890/metrics > "${PROFILE_DIR}/obi-workload-metrics.txt" || true
curl -s http://localhost:8888/metrics > "${PROFILE_DIR}/collector-internal-metrics.txt" || true
curl -s http://localhost:8889/metrics > "${PROFILE_DIR}/collector-workload-metrics.txt" || true

kill $PORT_FORWARD_PID_OBI_8888 || true
kill $PORT_FORWARD_PID_OTEL_8888 || true
kill $PORT_FORWARD_PID_OTEL_8889 || true


echo "Cleaning up current deployment to reset for next test run..."
kubectl delete daemonset/obi -n $NAMESPACE --ignore-not-found=true
kubectl delete deployment/workload-client deployment/workload-server -n $NAMESPACE --ignore-not-found=true
if [ -f "memory_investigation/manifests/servers/${PROTOCOL}-server.yaml" ]; then
    kubectl delete -f memory_investigation/manifests/servers/${PROTOCOL}-server.yaml --ignore-not-found=true
fi
sleep 5

echo "Test complete! Artifacts saved to $PROFILE_DIR"
