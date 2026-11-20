package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cilium/ebpf"
)

func roundUp8(x uint64) uint64 { return (x + 7) &^ 7 }
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

func estBytes(m *ebpf.MapSpec, cpus uint64) uint64 {
	entries := uint64(m.MaxEntries)
	ks := roundUp8(uint64(m.KeySize))
	vs := roundUp8(uint64(m.ValueSize))
	switch m.Type {
	case ebpf.RingBuf, ebpf.UserRingbuf:
		return entries + 12288
	case ebpf.PerCPUArray:
		return entries * vs * cpus
	case ebpf.Array:
		return entries * vs
	case ebpf.Hash:
		buckets := nextPow2(entries) * 16
		if m.Flags&0x1 != 0 {
			return buckets
		}
		return buckets + entries*(48+ks+vs)
	case ebpf.LRUHash:
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
		return nextPow2(entries) * 16
	case ebpf.ProgramArray:
		return entries * 8
	case ebpf.SkStorage, ebpf.TaskStorage, ebpf.InodeStorage:
		return 0
	default:
		return entries * (ks + vs)
	}
}

func isInternalSec(name string) bool {
	return name == ".rodata" || name == ".bss" || name == ".data" ||
		name == ".rodata.str1.1" || name == ".rodata.cst16" || name == ".rodata.cst8" ||
		name == ".rodata.cst32"
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
		{"watcher", "pkg/internal/ebpf/watcher/bpf_x86_bpfel.o"},
		{"netolly_sock", "pkg/internal/netolly/ebpf/netsk_x86_bpfel.o"},
		{"statsolly", "pkg/internal/statsolly/ebpf/stats_x86_bpfel.o"},
	}

	specs := map[string]*ebpf.CollectionSpec{}
	refs := map[string]map[string][]string{} // spec -> mapName -> progNames

	for _, o := range objs {
		spec, err := ebpf.LoadCollectionSpec(filepath.Join(root, o.path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR %s: %v\n", o.name, err)
			continue
		}
		specs[o.name] = spec
		refs[o.name] = map[string][]string{}
		for pname, prog := range spec.Programs {
			seen := map[string]bool{}
			for _, ins := range prog.Instructions {
				if ins.IsLoadFromMap() || ins.Reference() != "" {
					ref := ins.Reference()
					if _, ok := spec.Maps[ref]; ok && !seen[ref] {
						seen[ref] = true
						refs[o.name][ref] = append(refs[o.name][ref], pname)
					}
				}
			}
		}
	}

	fmt.Println("=== UNREFERENCED MAPS PER SPEC (emitted in .maps but referenced by 0 programs in that spec) ===")
	for _, o := range objs {
		spec := specs[o.name]
		var unref []string
		var unrefBytes uint64
		for mname, m := range spec.Maps {
			if isInternalSec(mname) {
				continue
			}
			if len(refs[o.name][mname]) == 0 {
				unref = append(unref, mname)
				unrefBytes += estBytes(m, 24)
			}
		}
		sort.Strings(unref)
		fmt.Printf("\n[%s] unreferenced=%d/%d maps, est24=%.2f MiB\n", o.name, len(unref), len(spec.Maps), float64(unrefBytes)/1048576)
		for _, mname := range unref {
			m := spec.Maps[mname]
			b := estBytes(m, 24)
			if b >= 100*1024 {
				fmt.Printf("  %-36s %-14s k=%3d v=%5d max=%6d pin=%3d est24=%6.2f MiB\n",
					mname, m.Type.String(), m.KeySize, m.ValueSize, m.MaxEntries, m.Pinning, float64(b)/1048576)
			}
		}
	}

	// Deduplicated combination totals (respecting PinInternal = 100 and PinByName = 1)
	combos := []struct {
		label string
		names []string
	}{
		{"generictracer only", []string{"generictracer"}},
		{"gotracer only", []string{"gotracer"}},
		{"generic + go", []string{"generictracer", "gotracer"}},
		{"generic + go + tpinjector", []string{"generictracer", "gotracer", "tpinjector", "tpinjector_iter", "tpinjector_fionread"}},
		{"generic + go + tpinjector + logenricher", []string{"generictracer", "gotracer", "tpinjector", "tpinjector_iter", "tpinjector_fionread", "logenricher"}},
		{"all (app + net + stats)", []string{"generictracer", "gotracer", "tpinjector", "tpinjector_iter", "tpinjector_fionread", "logenricher", "netolly_sock", "statsolly", "watcher"}},
	}

	fmt.Println("\n=== DEDUPLICATED COMBINATION TOTALS (PinInternal/PinByName shared once) ===")
	for _, c := range combos {
		sharedSeen := map[string]bool{}
		var tot24, tot96 uint64
		var hashCount, lruCount, otherCount int
		var hashBytes, lruBytes uint64
		for _, sname := range c.names {
			spec := specs[sname]
			for mname, m := range spec.Maps {
				if isInternalSec(mname) {
					continue
				}
				if m.Pinning == 100 || m.Pinning == ebpf.PinByName {
					if sharedSeen[mname] {
						continue
					}
					sharedSeen[mname] = true
				}
				b24 := estBytes(m, 24)
				b96 := estBytes(m, 96)
				tot24 += b24
				tot96 += b96
				switch m.Type {
				case ebpf.Hash:
					hashCount++
					hashBytes += b24
				case ebpf.LRUHash:
					lruCount++
					lruBytes += b24
				default:
					otherCount++
				}
			}
		}
		fmt.Printf("%-42s total24=%6.2f MiB (LRU=%6.2f[%d], Hash=%5.2f[%d], Other=%d) total96=%6.2f MiB\n",
			c.label, float64(tot24)/1048576, float64(lruBytes)/1048576, lruCount, float64(hashBytes)/1048576, hashCount, otherCount, float64(tot96)/1048576)
	}

	// Print ALL maps in generictracer + gotracer deduplicated, sorted by est24
	fmt.Println("\n=== FULL DEDUPLICATED MAP INVENTORY: generictracer + gotracer (sorted by est24) ===")
	type row struct {
		name      string
		specs     string
		typ       string
		k, v, max uint32
		pin       uint32
		flags     uint32
		b24       uint64
		progs     int
	}
	var allRows []row
	seenShared := map[string]bool{}
	for _, sname := range []string{"generictracer", "gotracer"} {
		spec := specs[sname]
		for mname, m := range spec.Maps {
			if isInternalSec(mname) {
				continue
			}
			if m.Pinning == 100 || m.Pinning == ebpf.PinByName {
				if seenShared[mname] {
					continue
				}
				seenShared[mname] = true
			}
			progs := len(refs[sname][mname])
			if m.Pinning == 100 || m.Pinning == ebpf.PinByName {
				progs = len(refs["generictracer"][mname]) + len(refs["gotracer"][mname])
			}
			allRows = append(allRows, row{
				name: mname, specs: sname, typ: m.Type.String(),
				k: m.KeySize, v: m.ValueSize, max: m.MaxEntries,
				pin: uint32(m.Pinning), flags: m.Flags, b24: estBytes(m, 24), progs: progs,
			})
		}
	}
	sort.Slice(allRows, func(i, j int) bool { return allRows[i].b24 > allRows[j].b24 })
	var cum uint64
	for i, r := range allRows {
		cum += r.b24
		if r.b24 >= 200*1024 || i < 80 {
			fmt.Printf("%3d. %-36s %-13s %-12s k=%3d v=%5d max=%6d pin=%3d flg=%d progs=%3d est24=%6.2f MiB cum=%6.2f MiB\n",
				i+1, r.name, r.specs, r.typ, r.k, r.v, r.max, r.pin, r.flags, r.progs, float64(r.b24)/1048576, float64(cum)/1048576)
		}
	}
}
