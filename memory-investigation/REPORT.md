# OBI Baseline Memory Investigation & Reduction Prototypes

- **Target Repo**: `open-telemetry/opentelemetry-ebpf-instrumentation` (`main` @ `52dd614`, "Capture Python exceptions as exception logs (#4230)")
- **Prototype Workspace**: `this repository`, branch `proto/memory-reductions` (`HEAD` @ `5dd2dab`, 6 local commits, NOT pushed)
- **Test Environment**: Linux `7.1.6` (`amd64`), 24 CPUs, Docker containers with dedicated cgroup v2 (`--memory=2g`) sharing target `--pid` and `--net` namespaces, Prometheus `/metrics` verification on every run, Go `pprof` heap profiles (`idle`, `idle_gc`, `active`, `active_gc`), and exact kernel `memlock` via `/proc/<pid>/fdinfo/*` + `ebpf.NewMapFromID`.
- **Canonical Data**: JSON snapshots in ``memory-investigation/results/{baseline,proto1,proto2_v2,proto3_hybrid_v2,proto3_full,proto3_capped}/``. Note: `results_proto1` captures standalone Layer 1 (`dd4875c`); `results_proto2_v2` and `results_proto3_hybrid_v2` capture `8ec7a8c` (with `5dd2dab` adding non-sizing safety guards verified on `mixed_default` in `results_proto2_v2_smoke`); `results_proto3_full` and `results_proto3_capped` (`092378a`) show the all-`NO_PREALLOC` lower bound.

## 1. Fresh Baseline Measurements (`main` @ `52dd614`)

Under default settings (Prometheus metrics exporter, 1 Go HTTP server + 1 Python HTTP server on the same host), OBI uses **449.45 MiB** of container cgroup memory (`453.96 MiB` peak) during active HTTP traffic, **454.98 MiB** (`462.20 MiB` peak) with OTLP traces also enabled, and **471.72 MiB** (`479.11 MiB` peak) with network flows and TCP stats enabled.

| Scenario (active unless noted) | Cgroup Cur (MiB) | Peak (MiB) | Anon (MiB) | Kernel (MiB) | BPF Maps (MiB) | BPF Progs (MiB) | #Maps | #Progs | LRUHash (MiB) | Hash (MiB) | PerCPU (MiB) | RingBuf (MiB) | Array (MiB) | Go Heap post-GC (MiB) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `idle` (no targets) | 46.45 | 46.46 | 41.75 | 2.03 | 0.06 | 0.00 | 2 | 1 | 0.00 | 0.00 | 0.00 | 0.06 | 0.00 | 9.46 |
| `generic_only` (Python HTTP) | 295.86 | 300.59 | 43.31 | 207.19 | 203.08 | 1.65 | 122 | 96 | 194.69 | 3.54 | 1.88 | 1.34 | 1.62 | 15.10 |
| `go_only` (Go HTTP) | 387.84 | 397.40 | 64.55 | 289.95 | 285.20 | 1.92 | 164 | 158 | 267.67 | 12.58 | 2.24 | 1.09 | 1.62 | 20.20 |
| `mixed_default` (Go + Python) | 449.45 | 453.96 | 67.81 | 340.14 | 333.87 | 3.49 | 236 | 253 | 313.33 | 13.45 | 4.12 | 1.35 | 1.62 | 20.98 |
| `mixed_traces` (+ OTLP traces) | 454.98 | 462.20 | 73.82 | 340.11 | 333.87 | 3.44 | 236 | 253 | 313.33 | 13.45 | 4.12 | 1.35 | 1.62 | 27.87 |
| `mixed_scale_neg3` (`scale=-3`) | 156.92 | 165.45 | 64.23 | 54.44 | 48.33 | 3.52 | 236 | 253 | 40.59 | 1.78 | 4.12 | 0.23 | 1.62 | 24.35 |
| `mixed_cp_all` (`cp: all`) | 453.31 | 464.95 | 59.16 | 356.09 | 348.51 | 3.52 | 256 | 271 | 325.96 | 13.45 | 4.13 | 1.35 | 2.62 | 20.43 |
| `mixed_all_features` (app+net+stats) | 471.72 | 479.11 | 66.65 | 367.64 | 365.71 | 3.21 | 247 | 266 | 322.79 | 13.45 | 9.32* | 18.53 | 1.62 | 25.80 |

*In `mixed_all_features`, PerCPU = `4.12 MiB` `PerCPUArray` + `5.20 MiB` `LRUCPUHash` (`aggregated_flows`).

### Key Root Causes Found

1. **Preallocated `BPF_MAP_TYPE_LRU_HASH` dominates**:
   - In `mixed_default`, BPF maps (`333.87 MiB`, `74.3%` of cgroup) plus BPF progs (`3.49 MiB`) account for `75.1%` of container memory. Preallocated `LRUHash` alone is `313.33 MiB` (`69.7%` of cgroup, `93.8%` of BPF map bytes) across 142 maps sized to `10,000` or `30,000` entries.
   - Under active Go + Python HTTP traffic, 130 of those 142 `LRUHash` maps hold 0 entries at runtime.
2. **Unreferenced header-leaked maps and unpinned duplicate state maps**:
   - Shared C headers emit maps into `.maps` with 0 instruction references in that ELF: `generictracer` emits 8 unreferenced data maps totaling `25.85 MiB` real kernel `memlock` (including `go_offsets_map` at `12.38 MiB` on non-Go hosts; the 9th unreferenced map is a `448 B` `ProgramArray`), `gotracer` emits 11 unreferenced maps (including `ongoing_grpc_header_writes`, referenced by 0 programs anywhere), and `tpinjector` emits 11 unreferenced maps.
   - Six shared state hash maps (`protocol_cache`, `kafka_state`, `kafka_ongoing_requests`, `mysql_state`, `aerospike_state`, `listening_ports`) omit `OBI_PIN_INTERNAL` and are allocated twice across `generictracer` and `gotracer`.
   - Note on `PerCPUArray` scratch maps: 15 scratch maps are also duplicated between `generictracer` and `gotracer`, but pinning them across kprobes and uprobes is unsafe on Linux 6.0-6.9 because Linux 6.0 dropped `preempt_disable` for uprobes while `bpf_preempt_disable` kfuncs did not arrive until Linux 6.10 (`bpf/common/preempt_guard.h:9-29`).
3. **Maps preallocated for features that are OFF under default config**:
   - Header context propagation off (`context_propagation: disabled`): `go_h2_owned_streams`, `grpc_h2_owned_streams`, `ongoing_write_subsets`, `java_http1_{connections,requests}`, `http2_owned_stream_by_{framer,request}`, `http2_header_observations`, `grpc_h2_header_observations`, `grpc_app_owned_writes`, `grpc_owned_{writer,stream}_by_request`, `grpc_pending_header_by_request`, `incoming_trace_map`, `msg_buffers`, `msg_buffer_mem`.
   - Header scanning off (`!ContextPropagation.IsEnabled() && !TrackRequestHeaders`): `http1_header_scans` (`5.54 MiB`), `go_http1_header_scans` (`1.85 MiB`), `http1_header_scan_storage`. (Note: `http1_header_buf_storage` remains active in `gotracer` because `gotracer.go:539` hardcodes `g_bpf_traceparent_enabled: true` and attaches `ObiUprobeReadMimeHeader` on kernels >= 5.17.)
   - Trace context export off (`!PopulateTraceContext()` and `!NodeJS.ManualSpans`): `obi_ctx_stacks` (`3.68 MiB`), `traces_ctx_v1` (`1.50 MiB`), `node_manual_ctx_shadow` (`1.50 MiB`). Before detaching `PinByName` on `traces_ctx_v1`, a non-populating run must still open and drain any pre-existing `/sys/fs/bpf/otel/traces_ctx_v1` bpffs pin (`DrainPinnedTraceContextMap`) and close the FD so external OTEP #4855 readers do not see stale entries from a prior run.
   - Large buffers, runtime metrics, and exceptions off: `ongoing_large_buffers` (`1.70 MiB`), `tcp_large_buffers_storage` (`0.75 MiB` @ 24 CPUs, `3.00 MiB` @ 96 CPUs; requires adding the missing `if (tcp_max_captured_bytes == 0 || bytes_len == 0) return;` guard in `bpf/generictracer/protocol_tcp.h:unknown_send_large_buffer` so `tcp_large_buffers_mem()` is `.rodata`-DCE'd), `large_buf_emit_state_storage`, `jvm_mem_pool_samples`, `jvm_gc_calls`, `go_runtime_metric_targets`, `python_runtime_metric_*`, `exception_events` (shrunk in `gotracer` but missed in `generictracer`), `go_exception_pids`, `exception_record_t_storage`.
   - Metrics-only mode (`!Traces.Enabled() && !TracePrinter.Enabled()`): 5 Go channel-link maps (`7.28 MiB`; open PR #4299 skips attaching the probes but still allocates the maps) and 5 Go manual/auto SDK maps (`5.24 MiB` total, including `active_spans` `2.92 MiB`, `span_names` `1.62 MiB`, `go_auto_span_event_storage`, `span_mem`).
4. **Network overflow RingBuf (`direct_flows`)**:
   - In `pkg/internal/netolly/ebpf/{sock_tracer.go,tracer.go}`, `direct_flows` is an overflow fallback when `aggregated_flows` fills up, yet unconditionally allocates `16 MiB` (`1 << 24`). Resizing it to `1 MiB` when `cacheMaxSize > 0` saves `15 MiB`.
5. **Per-CPU scratch array scaling (`PerCPUArray`)**:
   - `4.12 MiB` on 24 CPUs projects to `16.46 MiB` on 96 CPUs and `32.93 MiB` on 192 CPUs, unaffected by `global_scale_factor`.
6. **Userspace Go heap (`20.98 MiB` post-GC active, `67.81 MiB` anon)**:
   - `pkg/internal/java/java_inject.go:NewJavaInjector()` calls `ensureEmbeddedAgent()` (copying the `4.86 MiB` / `5,092,711 B` embedded `obi-java-agent.jar` `[]byte` into a Go `string`) and eagerly calls `agentVersionFromJar()` to open and parse it as a ZIP on startup even with 0 Java workloads (`-7.00 MiB` idle cgroup when fixed).
   - `pkg/appolly/parent_settle.go` allocates a 65,536-entry LRU (`6.10 MiB` = `29.1%` of active Go heap) even in metrics-only mode, although only OTLP traces and `trace_printer` read `ParentSpanID`.
7. **Incidental finding on `tpinjector` (`mixed_cp_all`)**:
   - On Linux 7.1.6, `tpinjector` disables context propagation due to kernel commit `929e30f93125` (`ioctl(FIONREAD)` sockhash compensation attach failure), but leaves `+14.64 MiB` of loaded maps open (`333.87 -> 348.51 MiB`).
8. **Limits of `global_scale_factor: -3` (#1318)**:
   - Reduces `mixed_default` from `449.45 -> 156.92 MiB` (`48.33 MiB` BPF maps), but requires manual opt-in, cuts hot-path capacity 8x (`events` `1 MiB -> 128 KiB`, `ongoing_http` `30k -> 3.75k`), and ignores `Array`, `PerCPUArray`, `SockHash`, and `PinByName` maps.

## 2. Prototype Layers & Measured Results

Stacked on local branch `proto/memory-reductions` in `this repository` (`52dd614..5dd2dab`):

| Layer | Commit(s) | Min Kernel | Key Change |
|---|---|---|---|
| **Layer 1** | `dd4875c` + `8ec7a8c` | Linux 5.8+ | Prune unreferenced maps; pin 6 duplicate state hash maps; `NO_PREALLOC` strictly on 8 userspace-written metadata maps (right-size `go_auto_activation_attempts` to 1024 without `NO_PREALLOC` on < 6.1); lazy Java JAR; `1 MiB` `direct_flows`. |
| **Layer 2A** | `1c90343` + `8ec7a8c` + `5dd2dab` | Linux 5.8+ | Shrink maps for disabled config features; add `unknown_send_large_buffer` early-return guard (`protocol_tcp.h`) so `tcp_large_buffers_storage` is `.rodata`-DCE'd; drain any pre-existing `traces_ctx_v1` bpffs pin before detaching (`DrainPinnedTraceContextMap`); gate Go manual/auto/embedded SDK uprobes & `AllowPID` activation and NodeJS `uv_fs_access` probes on config; bypass `parent_settle` in metrics-only mode. |
| **Layer 2B** | `1c90343` + `8ec7a8c` + `5dd2dab` | Linux 5.8+ | Protocol-selective map shrinking from union of exporter `instrumentations` (#2159; HTTP/2 maps gated on `!HTTP && !gRPC && !GenAI`, keeping HTTP/2 & TCP `PerCPUArray` scratch maps intact until `protocol_handler.c` gates tail calls via `.rodata` and disabled Go protocol uprobes are skipped). |
| **Layer 3c** | `6ce6488` + `8ec7a8c` | Linux 6.1+ | `OBI_PROTO_NO_PREALLOC=hybrid`: keep 21 allowlisted eviction-dependent/cache maps (20 loaded in `mixed_default`, all 21 in `mixed_all_features`, covering all 6 measured leaking maps) as `LRU_HASH`; convert 92 paired/bounded/idle maps (`mixed_default`) to `HASH \| BPF_F_NO_PREALLOC`, capping non-hot non-PID-table buckets at `2048` while preserving `k_max_concurrent_pids` (`3001`). |
| **Layer 3a/3b** | `092378a` | Linux 6.1+ | Lower-bound experiments converting all eligible maps to `HASH \| NO_PREALLOC` at full (`3a`) or capped (`3b`) buckets (requires explicit cleanup/sweeping of the 6 eviction-dependent maps). |
| **Layer 4** | Standalone PoC | 5.8+ / 6.2+ | Kernel micro-PoC (``memory-investigation/tools/poc_task_storage.go``) measuring real `fdinfo` `memlock` for `BPF_MAP_TYPE_TASK_STORAGE` in a verified Kprobe (6.2+) and 65,536-port `Array` bitmap (5.8+). |

### End-to-End Measured Comparison (`mixed_default`, Active Traffic)

| Variant (Dataset) | Cgroup Cur (MiB) | Delta | Peak (MiB) | Kernel (MiB) | BPF Maps (MiB) | Delta BPF | PerCPUArray (24 / 96 CPUs) | Hot Capacity |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| Baseline (`results_baseline`) | 449.45 | 0.0% | 453.96 | 340.14 | 333.87 | 0.0% | 4.12 / 16.46 | Full |
| `global_scale_factor: -3` | 156.92 | -65.1% | 165.45 | 54.44 | 48.33 | -85.5% | 4.12 / 16.46 | 8x smaller |
| Layer 1 (`results_proto1`, pre-fix*) | 411.63 | -8.4% | 415.62 | 306.07 | 298.10 | -10.7% | 2.47 / 9.89 | Full |
| **Layer 1 + 2A (`results_proto2_v2`)** | **329.45** | **-26.7%** | **340.52** | **256.25** | **246.93** | **-26.0%** | **1.10 / 4.42** | **Full** |
| **Layer 1 + 2A + 2B `[http]` (`results_proto2_v2`)** | **257.82** | **-42.6%** | **281.53** | **179.65** | **170.43** | **-49.0%** | **1.10 / 4.42** | **Full (HTTP/1.1)** |
| **Layer 1 + 2A + 3c Hybrid (`results_proto3_hybrid_v2`)** | **140.02** | **-68.8%** | **149.82** | **65.04** | **51.68** | **-84.5%** | **1.10 / 4.42** | **Full** |
| Layer 1 + 2A + 3a Full (`results_proto3_full`) | 127.44 | -71.6% | 140.31 | 52.05 | 36.28 | -89.1% | 0.80 / 3.20 | Needs sweep** |
| Layer 1 + 2A + 3b Capped (`results_proto3_capped`) | 116.12 | -74.2% | 122.43 | 31.47 | 15.67 | -95.3% | 0.80 / 3.20 | Needs sweep** |

*\*`results_proto1` (`dd4875c`) preceded `8ec7a8c` (which removed `PerCPUArray` scratch pinning across kprobes/uprobes and removed 5.8 `NO_PREALLOC` from 2 BPF-written maps); all `v2` rows include those fixes.*
*\*\*Requires explicit deletion or periodic sweeping on the 6 eviction-dependent maps.*

Go post-GC live heap in `mixed_default` dropped from **20.98 MiB** (baseline) to **13.83 MiB** (`results_proto2_v2`) and **11.39 MiB** (`results_proto3_hybrid_v2`).

### Other Scenarios Summary (Active Traffic, Hardened `v2`)

| Scenario | Baseline (Cgrp / BPF) | Layer 1 + 2A `v2` (Cgrp / BPF) | Layer 1 + 2A + 3c Hybrid `v2` (Cgrp / BPF) |
|---|---|---|---|
| `generic_only` | 295.86 / 203.08 MiB | 237.27 / 160.12 MiB (-19.8% / -21.2%) | n/a |
| `go_only` | 387.84 / 285.20 MiB | 299.75 / 213.58 MiB (-22.7% / -25.1%) | n/a |
| `mixed_traces` (apples-to-apples) | 454.98 / 333.87 MiB | 392.00 / 259.35 MiB (-13.8% / -22.3%) | n/a |
| `mixed_all_features` | 471.72 / 365.71 MiB | 345.70 / 263.70 MiB (-26.7% / -27.9%) | 123.39 / 61.13 MiB (-73.8% / -83.3%) |

## 3. Measured Trade-Offs & Production Guardrails

### A. A/B/A Throughput & CPU Control (`LRU_HASH` vs `HASH | BPF_F_NO_PREALLOC`)
Back-to-back runs (`8` concurrent workers, `8s`, `cgroup/cpu.stat` `usage_usec`):
- **Go HTTP**: `proto2` LRU Run 1 = `537.4 us/req` target / `29.0 us/req` OBI (`103,459` reqs); `proto3` `NO_PREALLOC` Run 2 = `537.4 us/req` target / `28.8 us/req` OBI (`113,443/113,443` captured); `proto2` LRU Run 3 = `522.7 us/req` target / `28.4 us/req` OBI (`124,688` reqs).
- **Python HTTP**: `proto2` LRU = `2,685 us/req` target / `55.5 us/req` OBI (`7,013` reqs); `proto3` `NO_PREALLOC` = `2,580 us/req` target / `54.1 us/req` OBI (`7,167` reqs).
- No measurable CPU regression from Linux 6.1+ `bpf_mem_alloc`.

### B. Stale-Entry Audit After Traffic Stops (6 Eviction-Dependent Maps Found)
Iterating all 150 Hash/LRUHash maps 3 seconds after `mixed_all_features` traffic stops (`results_proto3_full/mixed_all_features_entries.txt`) found **6 maps** that accumulate stale entries because they rely on LRU eviction:
1. `tcp_io_accum` (`statsolly`, `6,404` entries): hit `2048/2048` (`-E2BIG`) in initial `proto3_hybrid` before being added to `lruEvictionDependentMaps` in `8ec7a8c`.
2. `pid_tid_to_conn` (`generictracer`, `5,318` entries): never deleted on thread exit (`ThreadingHTTPServer` spawns a thread per request).
3. `fd_map` (`generictracer`, `4,784` entries): not deleted on every socket close path.
4. `server_traces_aux` (`generictracer`, `4,784` entries): `delete_server_trace()` in `bpf/common/trace_lifecycle.h` deletes `server_traces` but not `server_traces_aux`.
5. `connection_tracker` (`generictracer`, `4,620` entries): relies on LRU expiry.
6. `ongoing_goroutines` (`gotracer`, `595` entries in `mixed_all_features`, `653` in `go_only`).

In `results_proto3_hybrid_v2`, keeping those 6 maps plus 15 long-lived cache maps (`21` total) as `LRU_HASH` left **zero `-E2BIG` caps** (`tcp_io_accum` at `6,753/16,384` `LRUHash`, `go_auto_targets` preserved at `max=3001`, and only 23/13/4 live entries in `server_traces`/`fd_to_connection`/`trace_map` from active server sockets/threads). Production adoption of `NO_PREALLOC` on request-scoped maps should still pair with a periodic userspace sweeper to guard against abnormal process exits.

### C. Fixed Bucket-Table Floor
Even empty `NO_PREALLOC` maps allocate `roundup_pow_of_two(max_entries) * 16 B` upfront (`512 KiB` at 30k, `256 KiB` at 10k), leaving a `~30-35 MiB` floor across ~100 maps (`proto3_full`) unless non-hot non-PID-table maps use smaller bucket counts (`2048` = `32 KiB`, cutting the floor to `~10-15 MiB`).

### D. Sibling-Path & Embedder Constraints (for Upstream PRs)
- **`OverrideAppExportQueue` embedders (e.g. Beyla)**: `pkg/appolly/instrumenter.go` guards `SettleConditionalParents` bypass on `ctxInfo.OverrideAppExportQueue == nil`. Production `MapPruneOptions()` must mirror PR #4299's run-local config resolution in `pkg/instrumenter/` so embedders with custom span queues keep trace, channel-link, SDK, and protocol maps enabled.
- **OTel Collector receiver**: `collector/config_linux.go:78-82` (`setReceiverConsumers`) installs placeholder `TracesExport`/`MetricsExport`/`LogsExport` closures even when a signal is absent (also noted in PR #4299), which blocks metrics-only and `!Logs.Enabled()` map shrinking in Collector mode until placeholder consumers are distinguished.
- **Layer 2B protocol-selective pruning**: Shrinking protocol maps (`ongoing_tcp_req`, DB/MQ/gRPC maps) in production must be paired with `.rodata` protocol gating in `bpf/generictracer/protocol_handler.c` and skipping disabled Go protocol uprobe attachment so non-selected traffic does not write into shrunk maps.

## 4. Layer 4 Kernel Micro-PoCs (Real `fdinfo` `memlock`)

Executed via ``memory-investigation/tools/poc_task_storage.go`` on Linux `7.1.6`:

| Replacement | Baseline Real `memlock` | Replacement Real `memlock` | Reduction | Min Kernel |
|---|---:|---:|---:|---|
| `pid_tid_to_conn` (`k=8, v=48, max=30000`) -> `BPF_MAP_TYPE_TASK_STORAGE` (verified in live `Kprobe` with `bpf_get_current_task_btf` + `bpf_task_storage_get`) | `3.48 MiB` (`3,645,184 B`) | `0.80 KiB` (`816 B`) | **4,467x** (auto-freed on exit) | Linux 6.2+ |
| `filter_ports` / `unreadable_buffer_ports` (`LRU_HASH k=2, v=1, max=10000`) -> 65,536-port `Array` bitmap (`k=4, v=8, max=1024`) | `0.86 MiB` (`903,040 B`) each (`1.72 MiB` pair) | `8.30 KiB` (`8,496 B`) each (`16.6 KiB` pair) | **106x** (all 65,536 ports, O(1)) | Linux 5.8+ |
| `HASH \| NO_PREALLOC` (`k=8, v=48, max=30000`) vs `max=1024` | `3.48 MiB` (`LRU_HASH`) | `512.9 KiB` (`30000`) / `16.9 KiB` (`1024`) | **7x / 211x** | Linux 6.1+ |

Additional Linux 5.8+ structural cleanups: merging mutually exclusive syscall/SSL `pid_tgid` stash maps (`active_{send,send_sock,recv,accept,connect}_args`, `active_ssl_{read,write}_args`) and consolidating small-value `connection_info_t`/`go_addr_key_t` maps.

## 5. Reproduction & Verification Commands

- Unit tests (all passing on `proto/memory-reductions` @ `5dd2dab`):
  ```bash
  cd this repository
  go test -short ./pkg/ebpf/... ./pkg/obi/... ./pkg/internal/ebpf/convenience/... ./pkg/internal/ebpf/generictracer/... ./pkg/internal/ebpf/gotracer/... ./pkg/internal/java/... ./pkg/appolly/...
  ```
- Inspect local prototype commits:
  ```bash
  git -C this repository log --oneline 52dd614..HEAD
  ```
- Re-run a Docker benchmark scenario:
  ```bash
  ./memory-investigation/tools/measure_scenario.sh mixed_default this repository/bin/obi mixed ./memory-investigation/configs/default.yaml /tmp/obi-bench/repro_out
  ```
