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
For each protocol, we evaluate memory/CPU scaling against the following feature toggles. The specific environment variable configurations being tested are explicitly defined below:
1. **Baseline**: All telemetry disabled. (`OTEL_METRICS_EXPORTER=none`, `OTEL_TRACES_EXPORTER=none`)
2. **Go-Specific Tracers**: Toggle Go uretprobes vs generic probes on/off. (Evaluate by toggling target language if applicable or via `OTEL_EBPF_TRACES_INSTRUMENTATIONS`).
3. **Service Graph metrics**: Toggle via `OTEL_EBPF_METRICS_FEATURES=application,service_graph` vs `OTEL_EBPF_METRICS_FEATURES=application`.
4. **Golden Signals (Metrics)**: Toggle request/error/duration metrics via `OTEL_METRICS_EXPORTER=otlp` vs `none`.
5. **Span/Trace Generation**: Toggle trace generation via `OTEL_TRACES_EXPORTER=otlp` vs `none`.
6. **Context Propagation**: Toggle via `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=disabled`, `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=headers`, and `OTEL_EBPF_BPF_CONTEXT_PROPAGATION=tcp`.
7. **eBPF Batching & Wakeup**: Tune buffering between kernel and userspace via `OTEL_EBPF_BPF_WAKEUP_LEN` and `OTEL_EBPF_BPF_BATCH_LENGTH`. We will test default vs large values.
8. **Userspace LRU Caches**: Tune protocol-specific cache limits (e.g. `OTEL_EBPF_BPF_MYSQL_PREPARED_STATEMENTS_CACHE_SIZE`, `OTEL_KAFKA_TOPIC_UUID_CACHE_SIZE`, etc.) which could pre-allocate or grow unbounded. We will test default vs large sizes.
9. **Payload Extraction**: Toggle HTTP/GraphQL/AWS payload extraction (`OTEL_EBPF_HTTP_GRAPHQL_ENABLED`, etc.) which might retain larger strings in memory.
10. **Kubernetes Attributes**: Toggle Kubernetes metadata enrichment via `OTEL_EBPF_KUBE_METADATA_ENABLE=true` vs `false`.

### Scaling Parameters (Load Testing)
For each enabled feature set, run scenarios at:
1. **Idle** (0 QPS, measuring pure overhead)
2. **Low** (10 QPS)
3. **Medium** (100 QPS)
4. **High** (1000 QPS)

## Verification Plan
### Automated Data Collection
1. **Infrastructure**: Deploy OBI alongside a generic workload Pod within a local Kubernetes cluster.
2. **Data Destination**: Deploy OTEL Collector with the `debug` exporter (no external cloud connections).
3. **Profiling**: For each step in the Test Matrix, we will use `go tool pprof` connected to OBI's internal profiling port (`profile_port` config) to collect:
   * `heap` profiles (memory consumption details)
   * `allocs` profiles
   * `profile` (CPU profile)
4. **Telemetry Volume & Internal Metrics**:
   * Scrape OBI's internal Prometheus endpoint (`OTEL_EBPF_INTERNAL_METRICS_PROMETHEUS_PORT`) to collect Go runtime metrics (`go_memstats_*`, `go_gc_duration_seconds`) to monitor Garbage Collection thrashing and overall heap behavior.
   * Collect total telemetry quantity (Data points / Spans per second) produced under each configuration. We will extract this by querying the OTEL collector debug exporter metrics or the OBI internal prometheus exporter.
5. **Artifact Retention**: Save all `.pprof` files and scraped metrics locally in the user's workspace so follow-up analysis can be performed.
