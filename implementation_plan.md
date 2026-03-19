# OBI Memory Investigation Plan

## Goal Description
Investigate the baseline memory usage of OpenTelemetry eBPF Instrumentation (OBI) based on its current state. The goal is to profile and characterize how resource consumption scales with different features and load sizes, aiming to understand the current memory footprint and identify areas for optimization. This is strictly an analysis of OBI's current state, without comparing historical commits or regressions to Beyla.

## Local Testing Matrix

### Workload Protocols
We will create a multi-protocol mock Go server/client application that can generate and receive traffic for:
1. HTTP/1.1 (Plaintext)
2. HTTPS/1.1 (TLS)
3. gRPC (HTTP/2)
4. MySQL
5. Postgres
6. Redis
7. Kafka

### Feature Dimensions (OBI Configuration)
To drastically reduce testing combinations, we will evaluate memory/CPU scaling against the following feature toggles exhaustively **only** on a primary target protocol (e.g., HTTP/1.1). For all other protocols, we will only test the **Baseline** and a **Typical** configuration (Baseline + Golden Signals + Span Generation).

The specific environment variable configurations being tested are:
1. **Baseline**: All telemetry disabled. (`OTEL_METRICS_EXPORTER=none`, `OTEL_TRACES_EXPORTER=none`)
2. **Service Graph metrics**: Toggle via `OTEL_EBPF_METRICS_FEATURES=application,service_graph` vs `OTEL_EBPF_METRICS_FEATURES=application`.
3. **Golden Signals (Metrics)**: Toggle request/error/duration metrics via `OTEL_METRICS_EXPORTER=otlp` vs `none`.
4. **Span/Trace Generation**: Toggle trace generation via `OTEL_TRACES_EXPORTER=otlp` vs `none`.
5. **Context Propagation**: Toggle via `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=disabled`, `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=headers`, and `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=tcp`.
6. **eBPF Batching & Wakeup**: Tune buffering between kernel and userspace via `OTEL_EBPF_BPF_WAKEUP_LEN` and `OTEL_EBPF_BPF_BATCH_LENGTH`. We will test default vs large values.
7. **Userspace LRU Caches**: Tune protocol-specific cache limits (e.g. `OTEL_EBPF_BPF_MYSQL_PREPARED_STATEMENTS_CACHE_SIZE`, `OTEL_KAFKA_TOPIC_UUID_CACHE_SIZE`, etc.) which could pre-allocate or grow unbounded. We will test default vs large sizes.
8. **Payload Extraction**: Toggle HTTP/GraphQL/AWS payload extraction (`OTEL_EBPF_HTTP_GRAPHQL_ENABLED`, etc.) which might retain larger strings in memory.
9. **Kubernetes Attributes**: Toggle Kubernetes metadata enrichment via `OTEL_EBPF_KUBE_METADATA_ENABLE=true` vs `false`.

### Scaling Parameters (Load Testing)
Rather than a full Cartesian product, we will run the following targeted scenarios for the chosen features to capture pure connection overhead, max scale load, and multi-connection map contention:

1. **Idle**: 0 QPS, 0 clients (pure baseline resource consumption).
2. **High-Single**: 1000 QPS (total) targeting 1 server via 1 client.
3. **High-Multi**: 1000 QPS (total) distributed across 5 clients targeting 5 servers.

## Verification Plan
### Automated Data Collection
1. **Infrastructure (GKE)**: Provision a GKE cluster with explicitly defined node sizes (e.g., `e2-standard-4`). Use Node Affinity/Tolerations to run the OBI + Server workload on one Node, and the Load Generator (Client) on a separate Node to guarantee CPU isolation.
2. **eBPF Compatibility**: Ensure the OBI DaemonSet is configured with the necessary privileges (`CAP_SYS_ADMIN` or `CAP_BPF`) and host volume mounts compatible with GKE's Container-Optimized OS (COS).
3. **Data Destination & Extraction**: Deploy an OTEL Collector and an automated scraping job **inside** the GKE cluster. This avoids the latency and unreliability of running `kubectl port-forward` over the internet.
4. **Profiling (In-Cluster)**: The in-cluster scraper will connect to OBI's internal profiling port (`profile_port` config) during steady-state load to collect:
   * `heap` and `allocs` profiles (memory consumption details)
   * `profile` (CPU profile)
5. **Telemetry Volume & High-Frequency Metrics Collection**:
   * **High-Frequency Sampling**: Configure the internal OTLP metrics exporter to push data at a high frequency (e.g., every 1 second) to capture a dense time-series of Go runtime metrics and overall heap behavior.
   * **Statistical Error Bounds**: Maintain a steady-state load for a minimum duration (e.g., 3-5 minutes) per scenario. Use the dense 1-second metric samples to calculate the mean, p99, and standard deviation (error bounds) for both CPU and memory consumption.
   * **Total Telemetry Data**: Collect overall telemetry quantity (Data points / Spans per second) produced under each configuration by querying the OTEL collector.
6. **Artifact Retention (GCS)**: The in-cluster automated job will package and upload all `.pprof` profiles and exported metrics directly to a Google Cloud Storage (GCS) bucket for permanent storage and later analysis.
