// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon // import "go.opentelemetry.io/obi/pkg/ebpf/common"

import (
	"os"

	"github.com/cilium/ebpf"

	"go.opentelemetry.io/obi/pkg/export/instrumentations"
)

// MapPruneOptions controls which BPF maps can be shrunk to minimal stubs
// because the controlling feature, exporter signal, or protocol is disabled
// in configuration. Both generictracer and gotracer must pass identical
// options so shared PinInternal maps agree on dimensions.
type MapPruneOptions struct {
	ContextPropagationEnabled bool
	HeaderPropagationEnabled  bool
	TrackRequestHeaders       bool
	PopulateTraceContext      bool
	NodeJSManualSpans         bool
	NodeJSEnabled             bool
	JavaEnabled               bool
	LargeBuffersEnabled       bool
	AppRuntimeMetricsEnabled  bool
	ExceptionsEnabled         bool
	GoChannelLinksEnabled     bool
	GoManualSDKEnabled        bool
	Protocols                 instrumentations.InstrumentationSelection
}

func shrinkMap(spec *ebpf.CollectionSpec, name string) {
	m := spec.Maps[name]
	if m == nil {
		return
	}
	switch m.Type {
	case ebpf.RingBuf, ebpf.UserRingbuf:
		m.Pinning = ebpf.PinNone
		m.MaxEntries = uint32(os.Getpagesize())
	case ebpf.PerCPUArray:
		// Convert disabled PerCPU scratch arrays to a regular Array preserving
		// ValueSize and MaxEntries (e.g. span_mem has MaxEntries=2) so BPF
		// verifier load-time bounds checks pass while eliminating N_CPUs scaling.
		// Callers MUST ensure all BPF programs touching this map are either
		// unattached or dead-code-eliminated via .rodata constants.
		m.Type = ebpf.Array
	default:
		if m.Pinning == ebpf.PinByName {
			m.Pinning = ebpf.PinNone
		}
		m.MaxEntries = 1
	}
}

func shrinkMaps(spec *ebpf.CollectionSpec, names []string) {
	for _, name := range names {
		shrinkMap(spec, name)
	}
}

// ShrinkDisabledFeatureMaps shrinks BPF maps whose controlling feature or
// protocol is statically disabled in OBI configuration. This preserves load-time
// verifier compatibility across Linux 5.8+ while eliminating preallocated memory
// for inactive features and protocols.
func ShrinkDisabledFeatureMaps(spec *ebpf.CollectionSpec, opts MapPruneOptions) {
	if !opts.HeaderPropagationEnabled {
		shrinkMaps(spec, []string{
			"java_http1_connections",
			"java_http1_requests",
			"ongoing_write_subsets",
			"go_h2_owned_streams",
			"http2_owned_stream_by_framer",
			"http2_owned_stream_by_request",
			"http2_header_observations",
			"grpc_h2_owned_streams",
			"grpc_h2_header_observations",
			"grpc_app_owned_writes",
			"grpc_owned_writer_by_request",
			"grpc_owned_stream_by_request",
			"grpc_pending_header_by_request",
		})
	}

	if !opts.ContextPropagationEnabled {
		shrinkMaps(spec, []string{
			"incoming_trace_map",
			"msg_buffers",
			"msg_buffer_mem",
		})
	}

	if !opts.ContextPropagationEnabled && !opts.TrackRequestHeaders {
		// Note: http1_header_buf_storage is intentionally NOT converted to Array
		// because gotracer hardcodes g_bpf_traceparent_enabled=true and attaches
		// ObiUprobeReadMimeHeader on kernels >= 5.17, actively using it per-CPU.
		shrinkMaps(spec, []string{
			"http1_header_scans",
			"go_http1_header_scans",
			"http1_header_scan_storage",
		})
	}

	if !opts.PopulateTraceContext {
		shrinkMap(spec, "obi_ctx_stacks")
	}

	if !(opts.NodeJSEnabled && opts.NodeJSManualSpans) {
		shrinkMap(spec, "node_manual_ctx_shadow")
	}

	if !opts.PopulateTraceContext && !(opts.NodeJSEnabled && opts.NodeJSManualSpans) {
		shrinkMap(spec, "traces_ctx_v1")
	}

	if !opts.LargeBuffersEnabled {
		shrinkMaps(spec, []string{
			"ongoing_large_buffers",
			"tcp_large_buffers_storage",
			"large_buf_emit_state_storage",
		})
	}

	if !opts.AppRuntimeMetricsEnabled {
		shrinkMaps(spec, []string{
			"jvm_mem_pool_samples",
			"jvm_gc_calls",
			"go_runtime_metric_targets",
			"python_runtime_metric_targets",
			"python_runtime_metric_snapshots",
			"nodejs_rt_payload_storage",
			"nodejs_v8_payload_storage",
		})
	}

	if !opts.ExceptionsEnabled {
		shrinkMaps(spec, []string{
			"python_exception_procs",
			"python_exception_pending",
			"go_exception_pids",
			"exception_events",
			"exception_record_t_storage",
		})
	}

	if !opts.GoChannelLinksEnabled {
		shrinkMaps(spec, []string{
			"chanrecv_invocations",
			"chansend_invocations",
			"buffered_channel_senders",
			"direct_channel_senders",
			"direct_channel_receivers",
		})
	}

	if !opts.GoManualSDKEnabled {
		shrinkMaps(spec, []string{
			"active_spans",
			"span_names",
			"active_go_auto_spans",
			"go_auto_span_event_storage",
			"span_mem",
		})
	}

	if !opts.JavaEnabled {
		shrinkMaps(spec, []string{
			"java_tasks",
			"java_vt_threads",
			"java_http1_connections",
			"java_http1_requests",
		})
	}

	if !opts.NodeJSEnabled {
		shrinkMaps(spec, []string{
			"nodejs_fd_map",
			"node_manual_ctx_shadow",
			"nodejs_rt_payload_storage",
			"nodejs_v8_payload_storage",
		})
	}

	// Protocol-selective map shrinking (addresses #2159).
	if opts.Protocols != 0 {
		sel := opts.Protocols
		if !sel.MongoEnabled() {
			shrinkMap(spec, "ongoing_mongo_requests")
		}
		if !sel.RedisEnabled() {
			shrinkMaps(spec, []string{
				"ongoing_redis_requests",
				"redis_writes",
			})
		}
		if !sel.KafkaEnabled() {
			shrinkMaps(spec, []string{
				"kafka_requests",
				"fetch_requests",
				"ongoing_produce_messages",
				"ongoing_produce_topics",
				"produce_traceparents_by_goroutine",
				"produce_traceparents",
				"produce_requests",
				"ongoing_kafka_requests",
				"kafka_state",
				"kafka_ongoing_requests",
			})
		}
		if !sel.GRPCEnabled() {
			shrinkMaps(spec, []string{
				"go_grpc_client_conns",
				"grpc_h2_owned_streams",
				"transport_new_client_invocations",
				"pending_h2_invocations",
				"ongoing_streams",
				"ongoing_grpc_client_requests",
				"grpc_framer_invocation_map",
				"ongoing_grpc_transports",
				"ongoing_grpc_server_requests",
				"ongoing_grpc_server_stream_tps",
				"grpc_conn_ptr_to_conn",
				"cached_grpc_client_connections",
				"grpc_h2_header_observations",
				"grpc_owned_stream_by_request",
				"grpc_owned_writer_by_request",
				"ongoing_grpc_request_status",
				"ongoing_grpc_operate_headers",
				"grpc_app_owned_writes",
				"grpc_pending_header_by_request",
			})
		}
		if !sel.SQLEnabled() {
			shrinkMaps(spec, []string{
				"ongoing_sql_queries",
				"pq_hostnames",
				"mysql_state",
			})
		}
		if !sel.DNSEnabled() {
			shrinkMaps(spec, []string{
				"unconn_dns_socks",
				"unconn_dns_pending",
			})
		}
		if !sel.AerospikeEnabled() {
			shrinkMap(spec, "aerospike_state")
		}
		if !sel.DBEnabled() && !sel.MQEnabled() && !sel.SunRPCEnabled() {
			// Note: tcp_req_mem stays PerCPUArray until protocol_handler.c gates
			// k_tail_protocol_tcp via a .rodata protocol bitmask.
			shrinkMap(spec, "ongoing_tcp_req")
		}
		if !sel.HTTPEnabled() && !sel.GRPCEnabled() {
			shrinkMaps(spec, []string{
				"ongoing_http2_grpc",
				"ongoing_http2_connections",
				"h2_cut_frames",
				"h2_joined_storage",
				"h2_cut_frame_storage",
				"http2_info_storage",
				"grpc_frames_ctx_mem",
			})
		}
	}
}
