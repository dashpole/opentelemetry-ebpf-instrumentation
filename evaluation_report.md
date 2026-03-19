# OBI Scaling Evaluation Report

Based on the performance metrics collected across 60 scenarios (including different protocols, scaling parameters, and feature dimensions), this report evaluates how the OpenTelemetry eBPF Instrumentation (OBI) scales with respect to QPS, workloads, and features.

## 1. Scaling with Workloads (Protocols)

OBI demonstrates varying levels of resource consumption depending on the protocol being instrumented. The following observations compare the `baseline` configuration (all default features enabled) at **Idle** (0 QPS) vs **High-Single** (1000 QPS with 1 client).

| Protocol | Idle CPU | High-Single CPU | Idle Memory | High-Single Memory |
| :--- | :--- | :--- | :--- | :--- |
| **Kafka** | 0.008 cores | 0.345 cores | 171.74 MB | 89.64 MB |
| **gRPC** | 0.007 cores | 0.312 cores | 128.76 MB | 83.55 MB |
| **HTTP** | 0.012 cores | 0.276 cores | 161.50 MB | 77.79 MB |
| **Redis** | 0.007 cores | 0.202 cores | 122.52 MB | 91.53 MB |
| **MySQL** | 0.008 cores | 0.161 cores | 119.94 MB | 141.59 MB |
| **PostgreSQL**| 0.005 cores | 0.130 cores | 116.33 MB | 70.66 MB |
| **HTTPS** | 0.006 cores | 0.128 cores | 137.42 MB | 64.51 MB |

**Key Findings:**
* **CPU Scalability by Protocol:** Kafka and gRPC are the most CPU-intensive to parse/instrument at 1000 QPS, consuming ~0.3. cores. Simple request/response protocols (PostgreSQL, HTTPS) are significantly cheaper (~0.13 cores).
* **High Idle Memory "Ballooning":** An interesting pattern is that the Heap memory usage is often *higher* during idle periods than under load. This is a common artifact of the Go garbage collector: large initial buffers are allocated during startup, but without memory pressure or frequent GC cycles, they remain uncollected. Under High QPS, the GC runs far more aggressively, settling the Heap memory footprint into a tight 70-90 MB band for most protocols. MySQL is the exception, actively growing to 140+ MB under load.

## 2. Scaling with QPS and Concurrency

We evaluated HTTP baseline scenarios to understand how load volume (QPS) and concurrency (number of clients) impact the agent's performance.

* **Idle (0 QPS, 0 Clients):** CPU 0.012 cores , Memory 161.50 MB
* **High-Single (1000 QPS, 1 Client):** CPU 0.276 cores, Memory 77.79 MB
* **High-Multi (1000 QPS, 5 Clients):** CPU 0.207 cores, Memory 89.27 MB

**Key Findings:**
* **Traffic CPU Overhead:** CPU usage scales strongly with QPS. Going from 0 QPS to 1000 QPS introduces ~0.26 cores of active overhead to process eBPF ring buffers, parse connections, and emit telemetry.
* **Concurrency Efficiency:** When maintaining 1000 QPS, shifting the load from 1 active connection to 5 parallel active connections (`High-Multi`) actually *decreased* CPU usage from 0.276 cores to 0.207 cores! This suggests OBI's internal processing pipelines or BPF map reads might be more efficient when traffic is interleaved across multiple sockets rather than serialized tightly on one. 
* **Connection Memory Footprint:** Concurrency incurs a minor memory penalty. Tracking state for 5 simultaneous active clients consumed roughly ~11 MB more Heap space than 1 client.

## 3. Scaling with Features (HTTP)

By far the most dramatic insight from the testing matrix is the overhead of individual tracing/metric features. The baseline config enables *all* default features, while the specific feature scenarios toggle dimensions on/off to isolate costs.

The table below shows CPU and Memory for HTTP traffic at 1000 QPS (`High-Single`) when isolating features:

| Feature Setting | CPU Profile | Heap Inuse | Impact Note |
| :--- | :--- | :--- | :--- |
| **baseline (All Features ON)** | **0.276 cores** | **77.79 MB** | The compounding cost of all instrumentations. |
| context_disabled | 0.006 cores | 37.12 MB | Disabling context drops overhead massively. |
| service_graph | 0.005 cores | 33.83 MB | Extremely lightweight when running in isolation. |
| golden_signals | 0.006 cores | 22.32 MB | RED metrics generation is incredibly cheap. |
| payloads / context_headers | 0.007 cores | ~36 MB | Header inspection adds minimal single-feature cost. |
| go_tracers | 0.022 cores | 29.89 MB | Active tracing pushes CPU up slightly from baseline isolated metrics. |
| k8s_attributes | 0.023 cores | 31.36 MB | Attribute decoration adds minor overhead. |

**Key Findings:**
* **Significant Compounding Overhead:** No single feature is uniquely responsible for high CPU consumption. `service_graph`, `golden_signals`, or even `traces` individually consume well under 0.03 cores. However, when combined into the `baseline` configuration, the overhead explodes by 10x to 0.276 cores. This implies that there are significant multiplicative costs or unoptimized lock-contention points when multiple observability pipelines (Go tracers, K8s decorators, context interceptors) are active simultaneously.
* **Minimal Memory Impact of Features:** Memory scales beautifully across features. Most isolated feature pipelines consume between 22-45 MB of Heap space, keeping the overall agent very lean.
