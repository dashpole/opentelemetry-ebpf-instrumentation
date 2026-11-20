//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
)

type entryInfo struct {
	id         uint32
	name       string
	typ        string
	maxEntries uint32
	flags      uint32
	entries    int
	memlock    int64
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <pid>\n", os.Args[0])
		os.Exit(1)
	}
	pid := os.Args[1]
	fdinfoDir := fmt.Sprintf("/proc/%s/fdinfo", pid)
	dirEntries, err := os.ReadDir(fdinfoDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readdir %s: %v\n", fdinfoDir, err)
		os.Exit(1)
	}

	seen := map[uint32]int64{}
	for _, de := range dirEntries {
		f, err := os.Open(filepath.Join(fdinfoDir, de.Name()))
		if err != nil {
			continue
		}
		var mapID uint32
		var memlock int64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "map_id:") {
				v, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "map_id:")), 10, 32)
				mapID = uint32(v)
			} else if strings.HasPrefix(line, "memlock:") {
				v, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "memlock:")), 10, 64)
				memlock = v
			}
		}
		f.Close()
		if mapID != 0 {
			seen[mapID] = memlock
		}
	}

	var nonEmpty []entryInfo
	totalHashMaps := 0
	totalEntries := 0
	for id, mem := range seen {
		m, err := ebpf.NewMapFromID(ebpf.MapID(id))
		if err != nil {
			continue
		}
		info, err := m.Info()
		if err != nil {
			m.Close()
			continue
		}
		if info.Type != ebpf.Hash && info.Type != ebpf.LRUHash {
			m.Close()
			continue
		}
		totalHashMaps++
		key := make([]byte, info.KeySize)
		val := make([]byte, info.ValueSize)
		count := 0
		iter := m.Iterate()
		for iter.Next(&key, &val) {
			count++
		}
		m.Close()
		totalEntries += count
		if count > 0 {
			nonEmpty = append(nonEmpty, entryInfo{
				id:         id,
				name:       info.Name,
				typ:        info.Type.String(),
				maxEntries: info.MaxEntries,
				flags:      info.Flags,
				entries:    count,
				memlock:    mem,
			})
		}
	}

	sort.Slice(nonEmpty, func(i, j int) bool {
		if nonEmpty[i].entries != nonEmpty[j].entries {
			return nonEmpty[i].entries > nonEmpty[j].entries
		}
		return nonEmpty[i].name < nonEmpty[j].name
	})

	fmt.Printf("Hash/LRUHash maps audited=%d, non-empty=%d, total_live_entries=%d\n", totalHashMaps, len(nonEmpty), totalEntries)
	for _, e := range nonEmpty {
		fmt.Printf("  id=%-5d %-16s %-8s entries=%-6d max=%-6d flg=%d memlock=%6.2f KiB\n",
			e.id, e.name, e.typ, e.entries, e.maxEntries, e.flags, float64(e.memlock)/1024)
	}
}
