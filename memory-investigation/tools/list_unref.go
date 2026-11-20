package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cilium/ebpf"
)

func main() {
	root := "/tmp/prototypes/obi-memory"
	objs := []struct{ name, path string }{
		{"generictracer", "pkg/internal/ebpf/generictracer/bpf_x86_bpfel.o"},
		{"gotracer", "pkg/internal/ebpf/gotracer/bpf_x86_bpfel.o"},
		{"tpinjector", "pkg/internal/ebpf/tpinjector/bpf_x86_bpfel.o"},
		{"statsolly", "pkg/internal/statsolly/ebpf/stats_x86_bpfel.o"},
	}
	for _, o := range objs {
		spec, _ := ebpf.LoadCollectionSpec(filepath.Join(root, o.path))
		referenced := map[string]bool{}
		for _, prog := range spec.Programs {
			for _, ins := range prog.Instructions {
				if ref := ins.Reference(); ref != "" {
					if _, ok := spec.Maps[ref]; ok {
						referenced[ref] = true
					}
				}
			}
		}
		var unref []string
		for mname := range spec.Maps {
			if strings.HasPrefix(mname, ".") {
				continue
			}
			if !referenced[mname] {
				unref = append(unref, mname)
			}
		}
		sort.Strings(unref)
		fmt.Printf("[%s] ALL unreferenced (%d): %v\n", o.name, len(unref), unref)
	}
}
