import os
import subprocess
import re

PROTOCOLS = ["http", "https", "grpc", "mysql", "postgres", "redis", "kafka"]
CONFIGS = ["baseline", "k8s_attributes", "go_tracers", "service_graph", "golden_signals", "traces", "context_disabled", "context_headers", "context_tcp", "large_batching", "large_caches", "payloads"]
QPS_LEVELS = ["0", "10", "100", "1000"]

def format_bytes(b):
    mb = b / (1024*1024.0)
    return f"{mb:.2f} MB"

def parse_pprof_total(path, pprof_type):
    if not os.path.exists(path):
        return "N/A"
    try:
        out = subprocess.check_output(["go", "tool", "pprof", "-top", "-" + pprof_type, path], stderr=subprocess.STDOUT).decode("utf-8")
        match = re.search(r'Showing nodes accounting for .*?, 100% of ([\d\.]+\w+)', out)
        if match:
            s = match.group(1)
            if s.endswith('MB'):
                return s.replace('MB', ' MB')
            elif s.endswith('kB'):
                return f"{float(s[:-2])/1024:.2f} MB"
            elif s.endswith('B'):
                return f"{float(s[:-1])/(1024*1024):.2f} MB"
            elif s.endswith('GB'):
                return f"{float(s[:-2])*1024:.2f} MB"
            return s
        return "0 MB"
    except Exception as e:
        return "Error"

def get_metric_info(config, dir_path):
    spans = 0
    datapoints = 0
    cardinality = 0
    
    col_int_path = os.path.join(dir_path, "collector-internal-metrics.txt")
    if os.path.exists(col_int_path):
        with open(col_int_path, "r") as f:
            content = f.read()
            # metrics points (if receiver is OTLP)
            for line in content.split('\n'):
                if line.startswith('otelcol_receiver_accepted_spans{receiver="otlp"'):
                    m = re.search(r'\} (\d+)', line)
                    if m: spans = int(m.group(1))
                elif line.startswith('otelcol_receiver_accepted_metric_points{receiver="otlp"'):
                    m = re.search(r'\} (\d+)', line)
                    if m: datapoints = int(m.group(1))

    # Cardinality
    obi_work = os.path.join(dir_path, "obi-workload-metrics.txt")
    col_work = os.path.join(dir_path, "collector-workload-metrics.txt")
    
    if os.path.exists(obi_work):
        with open(obi_work, "r") as f:
            lines = [l for l in f.readlines() if l.strip() and not l.startswith('#')]
            cardinality += len(lines)
            if datapoints == 0 and len(lines) > 0:
                datapoints = len(lines) # fallback if prometheus

    if os.path.exists(col_work):
        with open(col_work, "r") as f:
            lines = [l for l in f.readlines() if l.strip() and not l.startswith('#')]
            cardinality += len(lines)

    return spans, datapoints, cardinality

lines = []
for p in PROTOCOLS:
    for c in CONFIGS:
        for q in QPS_LEVELS:
            dir_path = f"memory_investigation/artifacts/{p}/{c}/{q}qps"
            if not os.path.isdir(dir_path):
                continue
            
            heap = os.path.join(dir_path, "heap.pprof")
            allocs = os.path.join(dir_path, "allocs.pprof")
            
            inuse = parse_pprof_total(heap, "inuse_space")
            alloc = parse_pprof_total(allocs, "alloc_space")
            
            spans, dt, card = get_metric_info(c, dir_path)
            
            link = f"[directory](./{dir_path}/)"
            
            if q == "0":
                lines.append(f"| {p.upper()} | {c} | {q} | {inuse} | {alloc} | 0 | 0 / 0 | {link} |")
            else:
                lines.append(f"| {p.upper()} | {c} | {q} | {inuse} | {alloc} | {spans} | {dt} / {card} | {link} |")

with open("temp_results.md", "w") as f:
    f.write("\n".join(lines) + "\n")
