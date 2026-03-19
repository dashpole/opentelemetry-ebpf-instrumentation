import os
import subprocess
import re

PROTOCOLS = ["http", "https", "grpc", "mysql", "postgres", "redis", "kafka"]
CONFIGS = ["baseline", "k8s_attributes", "go_tracers", "service_graph", "golden_signals", "traces", "context_disabled", "context_headers", "context_tcp", "large_batching", "large_caches", "payloads"]
QPS_LEVELS = ["0", "10", "100", "1000"]

def parse_cpu(path):
    if not os.path.exists(path):
        return 0.0
    try:
        out = subprocess.check_output(["go", "tool", "pprof", "-top", path], stderr=subprocess.STDOUT).decode("utf-8")
        match = re.search(r'Total samples = ([\d\.]+)(ms|s|m|h)?', out)
        if match:
            v = float(match.group(1))
            unit = match.group(2)
            if unit == 'ms': return v
            elif unit == 's': return v * 1000
            elif unit == 'm': return v * 60000
            elif unit == 'h': return v * 3600000
            return v
        return 0.0
    except Exception as e:
        return 0.0

def parse_mem(path, pprof_type):
    if not os.path.exists(path):
        return 0.0
    try:
        out = subprocess.check_output(["go", "tool", "pprof", "-top", "-" + pprof_type, path], stderr=subprocess.STDOUT).decode("utf-8")
        match = re.search(r'Showing nodes accounting for .*?, 100% of ([\d\.]+\w+)', out)
        if match:
            s = match.group(1)
            if s.endswith('MB'): return float(s[:-2])
            elif s.endswith('kB'): return float(s[:-2]) / 1024.0
            elif s.endswith('B'): return float(s[:-1]) / (1024.0 * 1024.0)
            elif s.endswith('GB'): return float(s[:-2]) * 1024.0
        return 0.0
    except:
        return 0.0

def get_metrics_for_run(p, c, q):
    dir_path = f"memory_investigation/artifacts/{p}/{c}/{q}qps"
    heap = os.path.join(dir_path, "heap.pprof")
    allocs = os.path.join(dir_path, "allocs.pprof")
    cpu = os.path.join(dir_path, "cpu.pprof")
    
    inuse = parse_mem(heap, "inuse_space")
    top_file = os.path.join(dir_path, "top.txt")
    if os.path.exists(top_file):
        with open(top_file, "r") as f:
            m = re.search(r'\s+(\d+)Mi', f.read())
            if m:
                inuse = float(m.group(1))

    alloc = parse_mem(allocs, "alloc_space")
    cpu_ms = parse_cpu(cpu)
    
    return inuse, alloc, cpu_ms

lines = []
lines.append("# Feature Cost Estimation")
lines.append("The following table illustrates the *estimated cost delta* for each feature against the `baseline` configuration for the same protocol and QPS. Values are expressed as (Feature - Baseline). CPU time represents the increase in CPU execution milliseconds during the 10-second profile window.")
lines.append("")
lines.append("| Protocol | QPS | Feature | Added CPU Time (per 10s) | Added In-Use Memory | Added Total Alloc |")
lines.append("| :--- | :--- | :--- | :--- | :--- | :--- |")

for p in PROTOCOLS:
    for q in QPS_LEVELS:
        # Get baseline
        bl_inuse, bl_alloc, bl_cpu = get_metrics_for_run(p, "baseline", q)
        
        for c in CONFIGS:
            if c == "baseline":
                continue
            
            in_use, alloc, cpu = get_metrics_for_run(p, c, q)
            
            # calculate delta
            delta_in_use = in_use - bl_inuse
            delta_alloc = alloc - bl_alloc
            delta_cpu = cpu - bl_cpu
            
            lines.append(f"| {p.upper()} | {q} | {c} | {delta_cpu:.2f} ms | {delta_in_use:.2f} MB | {delta_alloc:.2f} MB |")

with open("feature_cost_analysis.md", "w") as f:
    f.write("\n".join(lines) + "\n")
