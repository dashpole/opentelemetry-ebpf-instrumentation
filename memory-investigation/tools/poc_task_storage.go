//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

func fdMemlock(fd int) int64 {
	f, err := os.Open(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "memlock:") {
			v, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "memlock:")), 10, 64)
			return v
		}
	}
	return -1
}

func main() {
	fmt.Println("=== Kernel Map Allocation Micro-PoCs (Real Kernel memlock via fdinfo) ===")

	// 1. Compare pid_tgid stash map (k=8, v=48, matching pid_tid_to_conn) across map types:
	//    a) LRU_HASH max=30000 (OBI baseline)
	//    b) HASH | NO_PREALLOC max=30000
	//    c) HASH | NO_PREALLOC max=1024
	//    d) TASK_STORAGE (v=48, NO_PREALLOC, verified inside a loaded kprobe on Linux 6.2+)
	specs := []struct {
		label string
		spec  ebpf.MapSpec
	}{
		{
			label: "LRU_HASH (k=8, v=48, max=30000) [OBI pid_tid_to_conn baseline]",
			spec:  ebpf.MapSpec{Name: "poc_lru30k", Type: ebpf.LRUHash, KeySize: 8, ValueSize: 48, MaxEntries: 30000},
		},
		{
			label: "HASH | NO_PREALLOC (k=8, v=48, max=30000) [empty, bucket table only]",
			spec:  ebpf.MapSpec{Name: "poc_hnp30k", Type: ebpf.Hash, KeySize: 8, ValueSize: 48, MaxEntries: 30000, Flags: unix.BPF_F_NO_PREALLOC},
		},
		{
			label: "HASH | NO_PREALLOC (k=8, v=48, max=1024) [empty, capped buckets]",
			spec:  ebpf.MapSpec{Name: "poc_hnp1k", Type: ebpf.Hash, KeySize: 8, ValueSize: 48, MaxEntries: 1024, Flags: unix.BPF_F_NO_PREALLOC},
		},
		{
			label: "LRU_HASH (k=2, v=1, max=10000) [OBI filter_ports baseline]",
			spec:  ebpf.MapSpec{Name: "poc_ports_lru", Type: ebpf.LRUHash, KeySize: 2, ValueSize: 1, MaxEntries: 10000},
		},
		{
			label: "ARRAY bitmap (k=4, v=8, max=1024) [65536-port bitmap replacement]",
			spec:  ebpf.MapSpec{Name: "poc_ports_bmp", Type: ebpf.Array, KeySize: 4, ValueSize: 8, MaxEntries: 1024},
		},
	}

	for _, tc := range specs {
		m, err := ebpf.NewMap(&tc.spec)
		if err != nil {
			fmt.Printf("  FAIL %-68s err=%v\n", tc.label, err)
			continue
		}
		mem := fdMemlock(m.FD())
		fmt.Printf("  %-68s memlock=%8d B (%6.2f KiB / %5.2f MiB)\n", tc.label, mem, float64(mem)/1024, float64(mem)/(1024*1024))
		m.Close()
	}

	// 2. TASK_STORAGE with BTF key/value + real kprobe verifier check using
	//    bpf_get_current_task_btf() + bpf_task_storage_get().
	u32Type := &btf.Int{Name: "u32", Size: 4, Encoding: btf.Unsigned}
	valStruct := &btf.Struct{
		Name: "task_scratch",
		Size: 48,
		Members: []btf.Member{
			{Name: "a", Type: u32Type, Offset: 0},
		},
	}
	tsSpec := &ebpf.MapSpec{
		Name:       "poc_task_st",
		Type:       ebpf.TaskStorage,
		KeySize:    4,
		ValueSize:  48,
		MaxEntries: 0,
		Flags:      unix.BPF_F_NO_PREALLOC,
		Key:        u32Type,
		Value:      valStruct,
	}
	tsMap, err := ebpf.NewMap(tsSpec)
	if err != nil {
		fmt.Printf("  FAIL TASK_STORAGE create: %v\n", err)
		return
	}
	defer tsMap.Close()
	tsMem := fdMemlock(tsMap.FD())
	fmt.Printf("  %-68s memlock=%8d B (%6.2f KiB / %5.2f MiB)\n", "TASK_STORAGE (v=48, NO_PREALLOC, empty)", tsMem, float64(tsMem)/1024, float64(tsMem)/(1024*1024))

	// Load a real Kprobe program that calls bpf_get_current_task_btf() and bpf_task_storage_get()
	progSpec := &ebpf.ProgramSpec{
		Name:    "poc_kprobe_task_storage",
		Type:    ebpf.Kprobe,
		License: "GPL",
		Instructions: asm.Instructions{
			// r0 = bpf_get_current_task_btf()
			asm.FnGetCurrentTaskBtf.Call(),
			// r2 = r0 (task)
			asm.Mov.Reg(asm.R2, asm.R0),
			// r1 = map_fd
			asm.LoadMapPtr(asm.R1, tsMap.FD()),
			// r3 = NULL (init value)
			asm.Mov.Imm(asm.R3, 0),
			// r4 = BPF_LOCAL_STORAGE_GET_F_CREATE (1)
			asm.Mov.Imm(asm.R4, 1),
			// r0 = bpf_task_storage_get(map, task, NULL, CREATE)
			asm.FnTaskStorageGet.Call(),
			// return 0
			asm.Mov.Imm(asm.R0, 0),
			asm.Return(),
		},
	}
	prog, err := ebpf.NewProgram(progSpec)
	if err != nil {
		fmt.Printf("  FAIL Kprobe verifier check with TASK_STORAGE: %v\n", err)
		return
	}
	defer prog.Close()
	fmt.Printf("  PASS Kprobe verifier accepted bpf_get_current_task_btf + bpf_task_storage_get (prog_fd=%d, map_memlock=%d B)\n", prog.FD(), fdMemlock(tsMap.FD()))

	// Also verify HASH | NO_PREALLOC growth when inserting 100 active entries vs 30000 max_entries
	hnp, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "poc_hnp_live",
		Type:       ebpf.Hash,
		KeySize:    8,
		ValueSize:  48,
		MaxEntries: 30000,
		Flags:      unix.BPF_F_NO_PREALLOC,
	})
	if err == nil {
		defer hnp.Close()
		mem0 := fdMemlock(hnp.FD())
		val := make([]byte, 48)
		for i := uint64(1); i <= 100; i++ {
			_ = hnp.Put(i, val)
		}
		mem100 := fdMemlock(hnp.FD())
		for i := uint64(1); i <= 100; i++ {
			_ = hnp.Delete(i)
		}
		memDel := fdMemlock(hnp.FD())
		fmt.Printf("  HASH|NO_PREALLOC(max=30000) dynamic memlock: empty=%d B (%.1f KiB), 100_entries=%d B, after_delete=%d B\n",
			mem0, float64(mem0)/1024, mem100, memDel)
	}
}
