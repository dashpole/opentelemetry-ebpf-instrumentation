// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfconvenience // import "go.opentelemetry.io/obi/pkg/internal/ebpf/convenience"

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

// This file contains convenience functions around the cilium/ebpf
// CollectionSpec.Variables API.
// This wrapper has been deprecated in the main cilium/ebpf codebase.

const PinInternal = ebpf.PinType(100)

func roundToNearestMultiple(x, n uint32) uint32 {
	if x < n {
		return n
	}

	if x%n == 0 {
		return x
	}

	return (x + n/2) / n * n
}

// RingBuf and UserRingbuf max_entries must be page-aligned: both go through the kernel's ringbuf_map_alloc
func alignMaxEntriesIfRingBuf(m *ebpf.MapSpec) {
	if m.Type == ebpf.RingBuf || m.Type == ebpf.UserRingbuf {
		m.MaxEntries = roundToNearestMultiple(m.MaxEntries, uint32(os.Getpagesize()))
	}
}

// Maps that are identically defined in generictracer and gotracer (via shared
// protocol_handler.c / common headers) without OBI_PIN_INTERNAL, causing
// redundant kernel allocations when both tracers load on a mixed node.
// All listed PerCPUArray scratch maps are used only in kprobe/uprobe task
// context (never from softirq/sock_ops/socket_filter).
var implicitSharedMaps = map[string]bool{
	"protocol_cache":               true,
	"kafka_state":                  true,
	"kafka_ongoing_requests":       true,
	"mysql_state":                  true,
	"aerospike_state":              true,
	"listening_ports":              true,
	"tcp_large_buffers_storage":    true,
	"h2_joined_storage":            true,
	"exception_record_t_storage":   true,
	"grpc_frames_ctx_mem":          true,
	"http1_header_buf_storage":     true,
	"http1_header_scan_storage":    true,
	"http2_info_storage":           true,
	"http_info_mem":                true,
	"tcp_req_mem":                  true,
	"h2_cut_frame_storage":         true,
	"protocol_args_mem":            true,
	"large_buf_emit_state_storage": true,
	"h2_tp_huff_win_storage":       true,
	"h2_tp_huff_out_storage":       true,
	"connection_meta_mem":          true,
}

// Userspace-populated (BPF read-only) or strictly bounded metadata maps where
// BPF_MAP_TYPE_HASH | BPF_F_NO_PREALLOC is safe on Linux 5.8+ (zero BPF-side
// dynamic allocation in kprobe/uprobe hot paths) and avoids multi-MiB upfront
// preallocation.
type rightSizeRule struct {
	maxEntries uint32
	toHash     bool
	noPrealloc bool
}

var semanticRightSizeRules = map[string]rightSizeRule{
	// One entry per unique Go executable (dev, ino) on the node; userspace-written only.
	"go_offsets_map": {maxEntries: 1024, toHash: true, noPrealloc: true},
	// One entry per instrumented Go process with runtime metrics; userspace-written only.
	"go_runtime_metric_targets": {maxEntries: 3001, toHash: true, noPrealloc: true},
	// Serialized transient TID entry held only during uprobe_register.
	"go_executable_identity_requests": {maxEntries: 64, toHash: true, noPrealloc: true},
	// Up to 3 activation attempts per Go Auto SDK process.
	"go_auto_activation_attempts": {maxEntries: 1024, toHash: true, noPrealloc: true},
	// Userspace-written per-PID config / target tables (BPF read-only).
	"pid_samplers":                    {maxEntries: 0, toHash: true, noPrealloc: true},
	"go_auto_targets":                 {maxEntries: 0, toHash: true, noPrealloc: true},
	"go_exception_pids":               {maxEntries: 0, toHash: true, noPrealloc: true},
	"python_exception_procs":          {maxEntries: 0, toHash: true, noPrealloc: true},
	"python_runtime_metric_targets":   {maxEntries: 0, toHash: true, noPrealloc: true},
	"python_runtime_metric_snapshots": {maxEntries: 0, toHash: true, noPrealloc: true},
}

// Core hot-path request/connection maps that keep their full MaxEntries bucket
// table in OBI_PROTO_NO_PREALLOC=capped mode while secondary/cold maps cap
// buckets at 2048 to lower the fixed hash bucket array floor.
var hotRequestMaps = map[string]bool{
	"ongoing_http":                     true,
	"ongoing_http2_grpc":               true,
	"ongoing_tcp_req":                  true,
	"trace_map":                        true,
	"server_traces":                    true,
	"server_traces_aux":                true,
	"http_server_trace_index":          true,
	"cp_support_connect_info":          true,
	"ongoing_http_server_requests":     true,
	"ongoing_http_client_requests_data": true,
	"ongoing_server_connections":       true,
	"ongoing_goroutines":               true,
	"go_trace_map":                     true,
	"fd_map":                           true,
	"fd_to_connection":                 true,
	"connection_tracker":               true,
	"pid_tid_to_conn":                  true,
	"ssl_to_conn":                      true,
}

// Maps that empirically rely on LRU eviction (not explicitly deleted on every
// close/exit path) or act as long-lived caches; hybrid mode keeps these as
// LRU_HASH to eliminate -E2BIG exhaustion risk while converting all strictly
// paired/bounded/idle-language/idle-protocol maps to HASH | BPF_F_NO_PREALLOC.
var lruEvictionDependentMaps = map[string]bool{
	"pid_tid_to_conn":                true,
	"fd_map":                         true,
	"server_traces_aux":              true,
	"connection_tracker":             true,
	"ongoing_goroutines":             true,
	"clone_map":                      true,
	"ssl_to_conn":                    true,
	"protocol_cache":                 true,
	"listening_ports":                true,
	"filter_ports":                   true,
	"unreadable_buffer_ports":        true,
	"handled_by_go_conn":             true,
	"go_grpc_client_conns":           true,
	"cached_grpc_client_connections": true,
	"active_ssl_connections":         true,
	"bio_to_ssl":                     true,
	"ssl_to_bios":                    true,
	"tls_prefix_to_ssl":              true,
	"sock_pids":                      true,
	"active_unix_socks":              true,
}

var (
	bpfMemAllocOnce sync.Once
	bpfMemAllocOK   bool
)

// supportsBPFMemAlloc reports whether the running kernel is >= 6.1, where
// BPF_F_NO_PREALLOC hash maps use lockless per-CPU bpf_mem_alloc instead of
// kmalloc(GFP_ATOMIC), making them safe in kprobe/uprobe/tracing contexts.
func supportsBPFMemAlloc() bool {
	bpfMemAllocOnce.Do(func() {
		var u unix.Utsname
		if err := unix.Uname(&u); err != nil {
			return
		}
		rel := unix.ByteSliceToString(u.Release[:])
		parts := strings.SplitN(rel, ".", 3)
		if len(parts) < 2 {
			return
		}
		major, err1 := strconv.Atoi(parts[0])
		minorStr := parts[1]
		for i, r := range minorStr {
			if r < '0' || r > '9' {
				minorStr = minorStr[:i]
				break
			}
		}
		minor, err2 := strconv.Atoi(minorStr)
		if err1 == nil && err2 == nil {
			bpfMemAllocOK = major > 6 || (major == 6 && minor >= 1)
		}
	})
	return bpfMemAllocOK
}

func referencedMaps(spec *ebpf.CollectionSpec) map[string]bool {
	refs := make(map[string]bool, len(spec.Maps))
	for _, prog := range spec.Programs {
		for _, ins := range prog.Instructions {
			if ref := ins.Reference(); ref != "" {
				if _, ok := spec.Maps[ref]; ok {
					refs[ref] = true
				}
			}
		}
	}
	return refs
}

// OptimizeSpecMaps applies zero-tradeoff map hygiene before ResolveMaps:
// 1. Replaces unreferenced non-special maps with a minimal private 1-entry Array
//    so header-leaked maps are not allocated or inserted into sharedMaps.
// 2. Marks known duplicated protocol/scratch maps as PinInternal.
// 3. Right-sizes non-request metadata maps and enables BPF_F_NO_PREALLOC on
//    userspace-populated Hash maps.
func OptimizeSpecMaps(spec *ebpf.CollectionSpec) {
	if len(spec.Programs) == 0 {
		return
	}
	refs := referencedMaps(spec)
	for name, m := range spec.Maps {
		if strings.HasPrefix(name, ".") {
			continue
		}
		if m.Type == ebpf.ProgramArray || m.Type == ebpf.RingBuf || m.Type == ebpf.UserRingbuf {
			continue
		}
		if !refs[name] {
			// Detach from PinInternal/PinByName so we never poison sharedMaps
			// for another tracer that genuinely references this map.
			m.Pinning = ebpf.PinNone
			m.Type = ebpf.Array
			m.KeySize = 4
			m.ValueSize = 4
			m.MaxEntries = 1
			m.Flags = 0
			m.Key = nil
			m.Value = nil
			continue
		}
		if m.Pinning == ebpf.PinNone && implicitSharedMaps[name] {
			m.Pinning = PinInternal
		}
		if rule, ok := semanticRightSizeRules[name]; ok {
			if rule.maxEntries > 0 && m.MaxEntries > rule.maxEntries {
				m.MaxEntries = rule.maxEntries
			}
			if rule.toHash {
				m.Type = ebpf.Hash
			}
			if rule.noPrealloc && m.Type == ebpf.Hash {
				m.Flags |= unix.BPF_F_NO_PREALLOC
			}
		}

		// Layer 3 prototype: on Linux >= 6.1 (bpf_mem_alloc), convert
		// preallocated LRUHash/Hash maps to demand-allocated HASH | BPF_F_NO_PREALLOC.
		// Controlled by OBI_PROTO_NO_PREALLOC=full|capped|hybrid (off by default).
		noPreallocMode := os.Getenv("OBI_PROTO_NO_PREALLOC")
		if (noPreallocMode == "full" || noPreallocMode == "capped" || noPreallocMode == "hybrid") &&
			supportsBPFMemAlloc() &&
			m.Pinning != ebpf.PinByName &&
			m.MaxEntries > 1 &&
			(m.Type == ebpf.LRUHash || m.Type == ebpf.Hash) {
			if noPreallocMode == "hybrid" && lruEvictionDependentMaps[name] {
				// Keep eviction-dependent / cache maps as LRU_HASH.
				continue
			}
			m.Type = ebpf.Hash
			m.Flags |= unix.BPF_F_NO_PREALLOC
			if (noPreallocMode == "capped" || noPreallocMode == "hybrid") && !hotRequestMaps[name] && m.MaxEntries > 2048 {
				m.MaxEntries = 2048
			}
		}
	}
}

// ResolveMaps sets up internal maps and ensures sane max entries values
func ResolveMaps(spec *ebpf.CollectionSpec, sharedMaps map[string]*ebpf.Map, mu *sync.Mutex) (*ebpf.CollectionOptions, error) {
	OptimizeSpecMaps(spec)
	collOpts := ebpf.CollectionOptions{MapReplacements: map[string]*ebpf.Map{}}

	mu.Lock()
	defer mu.Unlock()

	for k, v := range spec.Maps {
		alignMaxEntriesIfRingBuf(v)

		if v.Pinning != PinInternal {
			continue
		}

		v.Pinning = ebpf.PinNone
		internalMap := sharedMaps[k]

		var err error

		if internalMap == nil {
			internalMap, err = ebpf.NewMap(v)
			if err != nil {
				return nil, fmt.Errorf("failed to load shared map: %w", err)
			}

			sharedMaps[k] = internalMap
			runtime.SetFinalizer(internalMap, (*ebpf.Map).Close)
		}

		collOpts.MapReplacements[k] = internalMap
	}

	return &collOpts, nil
}

// LoadSpec loads a BPF collection spec into the provided objects, handling
// constant rewriting, PinInternal map resolution, and bpffs pin path setup.
// Notes about some parameters:
// - constants: optional map of BPF constants to rewrite (may be nil)
// - sharedMaps: map store for PinInternal maps, shared across specs within the same agent
// - pinPath: bpffs pin path for PinByName maps (empty string to skip)
// - cache: optional kernel BTF cache shared across loads (may be nil)
func LoadSpec(spec *ebpf.CollectionSpec, objects any, constants map[string]any, sharedMaps map[string]*ebpf.Map, mu *sync.Mutex, pinPath string, cache *btf.Cache) error {
	if constants != nil {
		if err := RewriteConstants(spec, constants); err != nil {
			return fmt.Errorf("rewriting BPF constants: %w", err)
		}
	}

	collOpts, err := ResolveMaps(spec, sharedMaps, mu)
	if err != nil {
		return fmt.Errorf("resolving maps: %w", err)
	}

	collOpts.Programs = ebpf.ProgramOptions{LogSizeStart: 640 * 1024}
	collOpts.Maps = ebpf.MapOptions{PinPath: pinPath}
	collOpts.Cache = cache

	if err := spec.LoadAndAssign(objects, collOpts); err != nil {
		return fmt.Errorf("loading and assigning BPF objects: %w", err)
	}

	return nil
}

const (
	MaxMapEntries       uint32 = 1 << 24
	MinMapEntries       uint32 = 64
	MinResizableMapSize uint32 = 64
)

// isResizableMapType returns true for map types where scaling MaxEntries
// is meaningful. Excludes special map types whose MaxEntries has fixed
// semantics (e.g. ProgramArray entries are tail-call slots, not data), and
// array types, whose MaxEntries is a valid index space rather than a capacity
// (e.g. valid_pids is indexed using a constant shared with userspace).
func isResizableMapType(t ebpf.MapType) bool {
	switch t {
	case ebpf.Array, ebpf.PerCPUArray,
		ebpf.ProgramArray, ebpf.PerfEventArray, ebpf.CGroupArray,
		ebpf.ArrayOfMaps, ebpf.HashOfMaps,
		ebpf.DevMap, ebpf.SockMap, ebpf.CPUMap, ebpf.XSKMap, ebpf.SockHash,
		ebpf.DevMapHash, ebpf.ReusePortSockArray:
		return false
	default:
		return true
	}
}

// SetupMapSizes scales all resizable maps in the spec by globalScaleFactor.
// If globalScaleFactor > 0, sizes are doubled that many times (left shift).
// If globalScaleFactor < 0, sizes are halved that many times (right shift).
// Maps with PinByName are skipped regardless of scale factor.
func SetupMapSizes(spec *ebpf.CollectionSpec, globalScaleFactor int) {
	if globalScaleFactor == 0 {
		return
	}

	for _, mSpec := range spec.Maps {
		mSpec.MaxEntries = ScaledMaxEntries(mSpec, globalScaleFactor)
	}
}

func ScaledMaxEntries(mSpec *ebpf.MapSpec, globalScaleFactor int) uint32 {
	oldEntries := mSpec.MaxEntries

	if globalScaleFactor == 0 ||
		!isResizableMapType(mSpec.Type) ||
		oldEntries < MinResizableMapSize ||
		mSpec.Pinning == ebpf.PinByName {
		return oldEntries
	}

	var newEntries uint32

	if globalScaleFactor > 0 {
		newEntries = oldEntries << uint32(globalScaleFactor)
		if newEntries < oldEntries {
			newEntries = MaxMapEntries
		}
	} else {
		newEntries = oldEntries >> uint32(-globalScaleFactor)
	}

	if newEntries < MinMapEntries && oldEntries >= MinMapEntries {
		newEntries = MinMapEntries
	}
	if newEntries > MaxMapEntries {
		newEntries = MaxMapEntries
	}

	return newEntries
}

// MissingConstantsError is returned by [ebpf.CollectionSpec.RewriteConstants].
type MissingConstantsError struct {
	// The constants missing from .rodata.
	Constants []string
}

func (m *MissingConstantsError) Error() string {
	return "some constants are missing from .rodata: " + strings.Join(m.Constants, ", ")
}

// RewriteConstants replaces the value of multiple constants.
//
// The constant must be defined like so in the C program:
//
//	volatile const type foobar;
//	volatile const type foobar = default;
//
// Replacement values must be of the same length as the C sizeof(type).
// If necessary, they are marshaled according to the same rules as
// map values.
//
// From Linux 5.5 the verifier will use constants to eliminate dead code.
//
// Returns an error wrapping [MissingConstantsError] if a constant doesn't exist.
func RewriteConstants(cs *ebpf.CollectionSpec, consts map[string]any) error {
	var missing []string
	for n, c := range consts {
		v, ok := cs.Variables[n]
		if !ok {
			missing = append(missing, n)
			continue
		}

		if !v.Constant() {
			return fmt.Errorf("variable %s is not a constant", n)
		}

		if err := v.Set(c); err != nil {
			return fmt.Errorf("rewriting constant %s: %w", n, err)
		}
	}

	if len(missing) != 0 {
		return fmt.Errorf("rewrite constants: %w", &MissingConstantsError{Constants: missing})
	}

	return nil
}
