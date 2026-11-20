package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
)

type bpfMapInfo struct {
	ID         uint32 `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	KeySize    uint32 `json:"key_size"`
	ValueSize  uint32 `json:"value_size"`
	MaxEntries uint32 `json:"max_entries"`
	Flags      uint32 `json:"flags"`
	Memlock    uint64 `json:"memlock"`
	FDs        int    `json:"fds"`
}

type bpfProgInfo struct {
	ID      uint32 `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Memlock uint64 `json:"memlock"`
}

type report struct {
	PID             int               `json:"pid"`
	CgroupCurrent   uint64            `json:"cgroup_current_bytes"`
	CgroupPeak      uint64            `json:"cgroup_peak_bytes"`
	CgroupStat      map[string]uint64 `json:"cgroup_stat"`
	ProcStatus      map[string]string `json:"proc_status"`
	SmapsRollup     map[string]string `json:"smaps_rollup"`
	BPFMapCount     int               `json:"bpf_map_count"`
	BPFMapMemlock   uint64            `json:"bpf_map_memlock_bytes"`
	BPFProgCount    int               `json:"bpf_prog_count"`
	BPFProgMemlock  uint64            `json:"bpf_prog_memlock_bytes"`
	BPFTotalMemlock uint64            `json:"bpf_total_memlock_bytes"`
	MapsByType      map[string]uint64 `json:"maps_memlock_by_type"`
	TopMaps         []bpfMapInfo      `json:"top_maps"`
}

func readUintFile(p string) uint64 {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v
}

func readKVFile(p string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(p)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if k, v, ok := strings.Cut(line, ":"); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func readSpaceKVUint(p string) map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open(p)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 {
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			out[fields[0]] = v
		}
	}
	return out
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <pid> [--json]\n", os.Args[0])
		os.Exit(1)
	}
	pid, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad pid: %v\n", err)
		os.Exit(1)
	}
	asJSON := len(os.Args) > 2 && os.Args[2] == "--json"

	rep := report{
		PID:           pid,
		CgroupCurrent: readUintFile("/sys/fs/cgroup/memory.current"),
		CgroupPeak:    readUintFile("/sys/fs/cgroup/memory.peak"),
		CgroupStat:    readSpaceKVUint("/sys/fs/cgroup/memory.stat"),
		ProcStatus:    map[string]string{},
		SmapsRollup:   readKVFile(fmt.Sprintf("/proc/%d/smaps_rollup", pid)),
		MapsByType:    map[string]uint64{},
	}

	fullStatus := readKVFile(fmt.Sprintf("/proc/%d/status", pid))
	for _, k := range []string{"VmPeak", "VmSize", "VmHWM", "VmRSS", "RssAnon", "RssFile", "RssShmem", "VmData", "VmStk", "VmExe", "Threads"} {
		if v, ok := fullStatus[k]; ok {
			rep.ProcStatus[k] = v
		}
	}

	mapsByID := map[uint32]*bpfMapInfo{}
	progsByID := map[uint32]*bpfProgInfo{}

	fdinfoDir := fmt.Sprintf("/proc/%d/fdinfo", pid)
	entries, err := os.ReadDir(fdinfoDir)
	if err == nil {
		for _, e := range entries {
			kv := readKVFile(filepath.Join(fdinfoDir, e.Name()))
			if idStr, ok := kv["map_id"]; ok {
				id64, _ := strconv.ParseUint(idStr, 10, 32)
				id := uint32(id64)
				if existing, ok := mapsByID[id]; ok {
					existing.FDs++
					continue
				}
				memlock, _ := strconv.ParseUint(kv["memlock"], 10, 64)
				ks, _ := strconv.ParseUint(kv["key_size"], 10, 32)
				vs, _ := strconv.ParseUint(kv["value_size"], 10, 32)
				me, _ := strconv.ParseUint(kv["max_entries"], 10, 32)
				fl, _ := strconv.ParseUint(kv["map_flags"], 0, 32)
				mt, _ := strconv.ParseUint(kv["map_type"], 10, 32)
				name := fmt.Sprintf("map_%d", id)
				typeStr := ebpf.MapType(mt).String()
				if m, err := ebpf.NewMapFromID(ebpf.MapID(id)); err == nil {
					if info, err := m.Info(); err == nil {
						if info.Name != "" {
							name = info.Name
						}
						typeStr = info.Type.String()
					}
					_ = m.Close()
				}
				mapsByID[id] = &bpfMapInfo{
					ID: id, Name: name, Type: typeStr,
					KeySize: uint32(ks), ValueSize: uint32(vs),
					MaxEntries: uint32(me), Flags: uint32(fl),
					Memlock: memlock, FDs: 1,
				}
			} else if idStr, ok := kv["prog_id"]; ok {
				id64, _ := strconv.ParseUint(idStr, 10, 32)
				id := uint32(id64)
				if _, ok := progsByID[id]; ok {
					continue
				}
				memlock, _ := strconv.ParseUint(kv["memlock"], 10, 64)
				name := fmt.Sprintf("prog_%d", id)
				typeStr := kv["prog_type"]
				if p, err := ebpf.NewProgramFromID(ebpf.ProgramID(id)); err == nil {
					if info, err := p.Info(); err == nil {
						if info.Name != "" {
							name = info.Name
						}
						typeStr = info.Type.String()
					}
					_ = p.Close()
				}
				progsByID[id] = &bpfProgInfo{
					ID: id, Name: name, Type: typeStr, Memlock: memlock,
				}
			}
		}
	}

	var mapList []bpfMapInfo
	for _, m := range mapsByID {
		mapList = append(mapList, *m)
		rep.BPFMapMemlock += m.Memlock
		rep.MapsByType[m.Type] += m.Memlock
	}
	sort.Slice(mapList, func(i, j int) bool { return mapList[i].Memlock > mapList[j].Memlock })
	rep.BPFMapCount = len(mapList)
	rep.TopMaps = mapList

	for _, p := range progsByID {
		rep.BPFProgMemlock += p.Memlock
	}
	rep.BPFProgCount = len(progsByID)
	rep.BPFTotalMemlock = rep.BPFMapMemlock + rep.BPFProgMemlock

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}

	mib := func(b uint64) float64 { return float64(b) / (1024 * 1024) }
	fmt.Printf("PID=%d  cgroup.current=%.2f MiB  cgroup.peak=%.2f MiB\n", pid, mib(rep.CgroupCurrent), mib(rep.CgroupPeak))
	fmt.Printf("  cgroup.stat: anon=%.2f file=%.2f kernel=%.2f slab=%.2f(unrecl=%.2f) percpu=%.2f vmalloc=%.2f pagetables=%.2f\n",
		mib(rep.CgroupStat["anon"]), mib(rep.CgroupStat["file"]), mib(rep.CgroupStat["kernel"]),
		mib(rep.CgroupStat["slab"]), mib(rep.CgroupStat["slab_unreclaimable"]),
		mib(rep.CgroupStat["percpu"]), mib(rep.CgroupStat["vmalloc"]), mib(rep.CgroupStat["pagetables"]))
	fmt.Printf("  proc.status: VmRSS=%s VmHWM=%s RssAnon=%s RssFile=%s Threads=%s\n",
		rep.ProcStatus["VmRSS"], rep.ProcStatus["VmHWM"], rep.ProcStatus["RssAnon"], rep.ProcStatus["RssFile"], rep.ProcStatus["Threads"])
	fmt.Printf("  BPF maps=%d memlock=%.2f MiB | BPF progs=%d memlock=%.2f MiB | BPF total=%.2f MiB\n",
		rep.BPFMapCount, mib(rep.BPFMapMemlock), rep.BPFProgCount, mib(rep.BPFProgMemlock), mib(rep.BPFTotalMemlock))
	fmt.Printf("  BPF maps by type:")
	var types []string
	for t := range rep.MapsByType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Printf(" %s=%.2fMiB", t, mib(rep.MapsByType[t]))
	}
	fmt.Println()
	fmt.Println("  Top 25 BPF maps by real kernel memlock:")
	for i := 0; i < len(mapList) && i < 25; i++ {
		m := mapList[i]
		fmt.Printf("    %2d. id=%-5d %-16s %-13s k=%3d v=%5d max=%7d flg=%d memlock=%6.2f MiB\n",
			i+1, m.ID, m.Name, m.Type, m.KeySize, m.ValueSize, m.MaxEntries, m.Flags, mib(m.Memlock))
	}
}
