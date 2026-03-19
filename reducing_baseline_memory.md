# Approaches for Reducing Baseline Memory Usage

This document summarizes potential architectural and configuration changes capable of drastically reducing the baseline (idle) system memory footprint of the OBI pod. It addresses both userspace memory (Go Heap/OS cache) and kernel-space memory (non-evictable eBPF pinned maps).

The approaches are ordered from **simplest and most impactful** to **most complex**.

---

## 1. Dynamically Adjust / Reduce BPF Map Sizes
**Target:** Kernel-Space (Non-Evictable Memory) | **Complexity:** Very Low | **Impact:** Very High

OBI currently hardcodes maximum capacities for its `LRU_HASH` maps (e.g., `MAX_CONCURRENT_REQUESTS = 10000`, `MAX_CONCURRENT_SHARED_REQUESTS = 30000`) out of the box to prevent traffic drops in worst-case scenarios. The Linux kernel unconditionally pre-allocates this RAM when the maps load.
* **Approach:** Instead of using fixed compile-time constants for every user, we can intercept the map definitions inside the Go layer (`tracer_linux.go` via the `ebpf.CollectionSpec`) right before they are loaded into the Linux kernel and override the `MaxEntries`. 
* **Outcome:** Implementing an `.env` variable (like `OTEL_EBPF_BPF_CAPACITY_FACTOR`) would let us down-scale these maps by 50-90% for typical web applications, immediately freeing dozens of megabytes of non-evictable memory.

## 2. Conditionally Load BPF Protocol Tracers 
**Target:** Kernel-Space (Non-Evictable Memory) | **Complexity:** Low | **Impact:** High

Currently, the `generictracer` module frequently loads maps and programs for all supported telemetry (HTTP, MySQL, PostgreSQL, Redis, Kafka) regardless of whether the user actually has those workloads running.
* **Approach:** Refactor the initialization in Go (`loadTracers()`) to only inject the BPF modules corresponding to the explicitly enabled protocols (`OTEL_EBPF_TRACES_INSTRUMENTATIONS`). 
* **Outcome:** If a cluster only needs to trace HTTP traffic, preventing the massive Kafka and MySQL state-tracking maps from ever entering the kernel securely strips megabytes of unused RAM.

## 3. Deduplicate and Reuse Idle Process Polling (`fetchProcessPorts`)
**Target:** Userspace (Go Heap / GC Overhead) | **Complexity:** Low | **Impact:** Medium

Analysis of the `heap.pprof` and `allocs.pprof` showed that OBI unconditionally polls all node PIDs every 5 seconds—even when zero applications are actively instrumented. This allocates a brand new `map[app.PID]ProcessAttrs` and lists hundreds of connections to generate the `openPorts` slices.
* **Approach:** Refactor `fetchProcessPorts` to cap connections at a tight limit (e.g. 50), implement deduplication, and importantly, **reuse an existing pre-allocated Go map** instead of destroying and re-allocating a new one every 5 seconds to reduce memory fragmentation.
* **Outcome:** Lower CPU spikes and less garbage-collection bloat contributing to `working_set_bytes`. 

## 4. Tune BPF Ring Buffer Capacity
**Target:** Kernel-Space (Non-Evictable Memory) | **Complexity:** Low | **Impact:** Medium

OBI pipes raw telemetry from kernel-to-userspace using BPF Ring Buffers. These ring buffers require contiguous pages of `mmap`'ed (locked) memory.
* **Approach:** We can reduce the default size of the ring buffer (or make it configurable via config maps).
* **Outcome:** Since we proved earlier that OBI's Go userspace can read traces extremely quickly using batching, a deep, memory-heavy ring buffer queue is less necessary, dropping another solid chunk of memory.

## 5. Enable Elastic Map Allocation (`BPF_F_NO_PREALLOC`)
**Target:** Kernel-Space (Non-Evictable Memory) | **Complexity:** Medium/High | **Impact:** Medium

By default, the Linux kernel pre-allocates 100% of a Hash Map's memory upfront to guarantee lockless performance. 
* **Approach:** For maps that do *not* require an LRU eviction policy (standard `BPF_MAP_TYPE_HASH`), we can enable `BPF_F_NO_PREALLOC`. This flag forces the kernel to start the map at 0 bytes and dynamically allocate/free kernel memory exactly when entries are added/removed.
* **Outcome:** Memory scales beautifully with workload/traffic. **Note:** This cannot be used on LRU maps (like `incoming_trace_map`), so we'd have to rewrite maps and implement userspace periodic eviction logic to prevent full-map data drops.

## 6. Swap Caching for Per-CPU Scratchpads
**Target:** Kernel-Space (Non-Evictable Memory) | **Complexity:** High | **Impact:** Medium

Some maps are just used to buffer data temporarily within a single BPF execution context before writing to the Ringbuffer.
* **Approach:** Swap huge global hash maps for `BPF_MAP_TYPE_PERCPU_ARRAY`.
* **Outcome:** Instead of allocating memory for 10,000 potential requests, memory is allocated exclusively per CPU core (e.g., 4 cores = 4 tiny scratchpads). Memory usage drops to kilobytes, but it requires rewriting BPF C payload extraction logic.
