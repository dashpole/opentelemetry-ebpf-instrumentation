package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cilium/ebpf"
)

type mapRow struct {
	Spec       string
	Name       string
	Type       string
	KeySize    uint32
	ValSize    uint32
	MaxEntries uint32
	Flags      uint32
	Pinning    uint32
	EstBytes24 uint64
	EstBytes96 uint64
	EstBytes1  uint64
	Resizable  bool
}

func isResizable(t ebpf.MapType) bool {
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

func roundUp8(x uint64) uint64 {
	return (x + 7) &^ 7
}

func nextPow2(x uint64) uint64 {
	if x <= 1 {
		return 1
	}
	p := uint64(1)
	for p < x {
		p <<= 1
	}
	return p
}

// Conservative analytical estimate matching Linux kernel alloc formulas
// (actual kernel memlock will be measured directly from live /proc/PID/fdinfo!).
func estBytes(m *ebpf.MapSpec, cpus uint64) uint64 {
	entries := uint64(m.MaxEntries)
	ks := roundUp8(uint64(m.KeySize))
	vs := roundUp8(uint64(m.ValueSize))
	switch m.Type {
	case ebpf.RingBuf, ebpf.UserRingbuf:
		// kernel allocates 2x data pages + metadata pages for mmap ring wraparound,
		// actual backing pages = entries + 2-3 pages
		return entries + 12288
	case ebpf.PerCPUArray:
		return entries * vs * cpus
	case ebpf.Array:
		return entries * vs
	case ebpf.Hash:
		// htab: bucket table (nextPow2(entries)*16) + preallocated elements (entries * (sizeof(htab_elem)~48 + ks + vs))
		buckets := nextPow2(entries) * 16
		if m.Flags&0x1 != 0 { // BPF_F_NO_PREALLOC
			return buckets
		}
		return buckets + entries*(48+ks+vs)
	case ebpf.LRUHash:
		// lru_htab: buckets + entries * (sizeof(htab_elem)+lru_node ~ 80 + ks + vs)
		buckets := nextPow2(entries) * 16
		return buckets + entries*(80+ks+vs)
	case ebpf.PerCPUHash:
		buckets := nextPow2(entries) * 16
		if m.Flags&0x1 != 0 {
			return buckets
		}
		return buckets + entries*(48+ks+vs*cpus)
	case ebpf.LRUCPUHash:
		buckets := nextPow2(entries) * 16
		return buckets + entries*(80+ks+vs*cpus)
	case ebpf.SockHash:
		buckets := nextPow2(entries) * 16
		return buckets
	case ebpf.ProgramArray:
		return entries * 8
	case ebpf.SkStorage, ebpf.TaskStorage, ebpf.InodeStorage:
		return 0 // allocated dynamically per socket/task
	default:
		return entries * (ks + vs)
	}
}

func main() {
	root := "/tmp/prototypes/obi-memory"
	objs := []struct {
		name string
		path string
	}{
		{"generictracer", "pkg/internal/ebpf/generictracer/bpf_x86_bpfel.o"},
		{"gotracer", "pkg/internal/ebpf/gotracer/bpf_x86_bpfel.o"},
		{"tpinjector", "pkg/internal/ebpf/tpinjector/bpf_x86_bpfel.o"},
		{"tpinjector_iter", "pkg/internal/ebpf/tpinjector/bpfiter_x86_bpfel.o"},
		{"tpinjector_fionread", "pkg/internal/ebpf/tpinjector/bpffionreadfixup_x86_bpfel.o"},
		{"logenricher", "pkg/internal/ebpf/logenricher/bpf_x86_bpfel.o"},
		{"gpuevent", "pkg/internal/ebpf/gpuevent/bpf_x86_bpfel.o"},
		{"logger", "pkg/internal/ebpf/logger/bpf_x86_bpfel.o"},
		{"watcher", "pkg/internal/ebpf/watcher/bpf_x86_bpfel.o"},
		{"netolly_tc", "pkg/internal/netolly/ebpf/net_x86_bpfel.o"},
		{"netolly_sock", "pkg/internal/netolly/ebpf/netsk_x86_bpfel.o"},
		{"statsolly", "pkg/internal/statsolly/ebpf/stats_x86_bpfel.o"},
		{"rdns", "pkg/internal/rdns/ebpf/xdp/bpf_x86_bpfel.o"},
	}

	var rows []mapRow
	byMapName := map[string][]mapRow{}

	fmt.Println("=== PER-SPEC SUMMARY (analytical est, 24 CPUs / 96 CPUs) ===")
	for _, o := range objs {
		spec, err := ebpf.LoadCollectionSpec(filepath.Join(root, o.path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR loading %s: %v\n", o.name, err)
			continue
		}
		var tot24, tot96, nonRes24 uint64
		var progInsns int
		for _, p := range spec.Programs {
			progInsns += len(p.Instructions)
		}
		for name, m := range spec.Maps {
			if name == ".rodata" || name == ".bss" || name == ".data" || name == ".rodata.str1.1" || name == ".rodata.cst16" {
				continue
			}
			e1 := estBytes(m, 1)
			e24 := estBytes(m, 24)
			e96 := estBytes(m, 96)
			res := isResizable(m.Type) && m.MaxEntries >= 64 && m.Pinning != ebpf.PinByName
			r := mapRow{
				Spec: o.name, Name: name, Type: m.Type.String(),
				KeySize: m.KeySize, ValSize: m.ValueSize, MaxEntries: m.MaxEntries,
				Flags: m.Flags, Pinning: uint32(m.Pinning),
				EstBytes1: e1, EstBytes24: e24, EstBytes96: e96,
				Resizable: res,
			}
			rows = append(rows, r)
			byMapName[name] = append(byMapName[name], r)
			tot24 += e24
			tot96 += e96
			if !res {
				nonRes24 += e24
			}
		}
		fmt.Printf("%-20s maps=%3d progs=%3d insns=%6d est24=%7.2f MiB (non-resizable=%6.2f MiB) est96=%7.2f MiB\n",
			o.name, len(spec.Maps), len(spec.Programs), progInsns,
			float64(tot24)/1048576, float64(nonRes24)/1048576, float64(tot96)/1048576)
	}

	fmt.Println("\n=== DUPLICATED MAPS (same name in >1 spec WITHOUT pinning!) ===")
	var dupNames []string
	for name, list := range byMapName {
		if len(list) > 1 {
			dupNames = append(dupNames, name)
		}
	}
	sort.Strings(dupNames)
	var dupWaste24, dupWaste96 uint64
	for _, name := range dupNames {
		list := byMapName[name]
		unpinned := 0
		for _, r := range list {
			if r.Pinning == 0 {
				unpinned++
			}
		}
		if unpinned > 0 {
			specs := []string{}
			for _, r := range list {
				specs = append(specs, fmt.Sprintf("%s(pin=%d,max=%d,v=%d)", r.Spec, r.Pinning, r.MaxEntries, r.ValSize))
			}
			fmt.Printf("  %-35s type=%-15s est24=%6.2f MiB est96=%6.2f MiB  %v\n",
				name, list[0].Type, float64(list[0].EstBytes24)/1048576, float64(list[0].EstBytes96)/1048576, specs)
			dupWaste24 += list[0].EstBytes24 * uint64(len(list)-1)
			dupWaste96 += list[0].EstBytes96 * uint64(len(list)-1)
		}
	}
	fmt.Printf("TOTAL unpinned duplicate waste (if all loaded): est24=%.2f MiB, est96=%.2f MiB\n",
		float64(dupWaste24)/1048576, float64(dupWaste96)/1048576)

	fmt.Println("\n=== TOP 45 LARGEST MAPS (sorted by est24) ===")
	sort.Slice(rows, func(i, j int) bool { return rows[i].EstBytes24 > rows[j].EstBytes24 })
	for i := 0; i < len(rows) && i < 45; i++ {
		r := rows[i]
		fmt.Printf("%2d. %-16s %-34s %-16s k=%3d v=%5d max=%8d pin=%3d flg=%d res=%-5v est24=%6.2f MiB est96=%6.2f MiB\n",
			i+1, r.Spec, r.Name, r.Type, r.KeySize, r.ValSize, r.MaxEntries, r.Pinning, r.Flags, r.Resizable,
			float64(r.EstBytes24)/1048576, float64(r.EstBytes96)/1048576)
	}

	fmt.Println("\n=== ALL PERCPU_ARRAY / PERCPU_HASH / LRU_PERCPU_HASH MAPS ===")
	sort.Slice(rows, func(i, j int) bool { return rows[i].EstBytes96 > rows[j].EstBytes96 })
	var pcpu24, pcpu96 uint64
	for _, r := range rows {
		if r.Type == "PerCPUArray" || r.Type == "PerCPUHash" || r.Type == "LRUCPUHash" {
			fmt.Printf("  %-16s %-34s %-14s v=%6d max=%6d pin=%3d est24=%6.2f MiB est96=%6.2f MiB\n",
				r.Spec, r.Name, r.Type, r.ValSize, r.MaxEntries, r.Pinning,
				float64(r.EstBytes24)/1048576, float64(r.EstBytes96)/1048576)
			pcpu24 += r.EstBytes24
			pcpu96 += r.EstBytes96
		}
	}
	fmt.Printf("TOTAL PerCPU maps: est24=%.2f MiB, est96=%.2f MiB\n", float64(pcpu24)/1048576, float64(pcpu96)/1048576)
}
