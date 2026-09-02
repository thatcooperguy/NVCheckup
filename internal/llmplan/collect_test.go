package llmplan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// gb10Meminfo mirrors the spec 10 fixture: MemTotal 125513944 kB = 119.7 GiB.
const gb10Meminfo = `MemTotal:       125513944 kB
MemFree:        90000000 kB
MemAvailable:   115000000 kB
Buffers:          500000 kB
Cached:         20000000 kB
SwapCached:            0 kB
SwapTotal:       8000000 kB
SwapFree:        7000000 kB
HugePages_Total:       0
HugePages_Free:        0
Hugepagesize:       2048 kB
`

func TestParseMeminfo(t *testing.T) {
	p := parseMeminfo(gb10Meminfo)
	near(t, "MemTotal", GiBf(p.TotalBytes), 119.7, 0.05)
	if p.AvailableBytes != 115000000*1024 || p.CachedBytes != 20000000*1024 || !p.SwapKnown {
		t.Errorf("parsed pool = %+v", p)
	}
	near(t, "swap used", GiBf(p.SwapUsedBytes()), GiBf(1000000*1024), 0.001)
	// spec 3.3: allocatable = MemAvailable + SwapFree.
	if p.AllocatableBytes != (115000000+7000000)*1024 {
		t.Errorf("allocatable = %v", p.AllocatableBytes)
	}
	// HugePages override: allocatable = HugePages_Free x Hugepagesize, swap counts 0.
	hp := parseMeminfo(strings.Replace(strings.Replace(gb10Meminfo, "HugePages_Total:       0", "HugePages_Total:    1000", 1), "HugePages_Free:        0", "HugePages_Free:      900", 1))
	if hp.Allocatable() != 900*2048*1024 {
		t.Errorf("hugepages allocatable = %v", hp.Allocatable())
	}
}

func TestSimRoot_ReadMeminfoAndPorts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc", "net"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "meminfo"), []byte(gb10Meminfo), 0644); err != nil {
		t.Fatal(err)
	}
	tcp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1 0000000000000000 100 0 0 10 0\n" + // 8000 LISTEN
		"   1: 0100007F:2CAA 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1 0000000000000000 100 0 0 10 0\n" + // 11434 LISTEN
		"   2: 0100007F:1F90 0100007F:D3A2 01 00000000:00000000 00:00000000 00000000  1000        0 1 0000000000000000 100 0 0 10 0\n" // 8080 ESTABLISHED
	if err := os.WriteFile(filepath.Join(root, "proc", "net", "tcp"), []byte(tcp), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVC_SIM_ROOT", root)

	if got := simPath("/proc/meminfo"); got != filepath.Join(root, "proc", "meminfo") {
		t.Errorf("simPath = %s", got)
	}
	p, err := readMeminfo()
	if err != nil {
		t.Fatal(err)
	}
	near(t, "sim MemTotal", GiBf(p.TotalBytes), 119.7, 0.05)
	if !strings.Contains(p.Source, "/proc/meminfo") {
		t.Errorf("source = %s", p.Source)
	}

	ports, known := ListeningPorts(&types.Report{}, "windows") // sim root makes the files readable on any OS
	if !known || fmtInts(ports) != "8000,11434" {
		t.Errorf("listening ports = %v known=%v, want [8000 11434]", ports, known)
	}

	// DerivePool without a unified-memory struct falls back to the sim meminfo.
	r := &types.Report{GPUs: []types.GPUInfo{{Name: "NVIDIA GB10", IsNVIDIA: true, MemoryReporting: "not-supported"}}}
	pool, _ := DerivePool(r, "linux", 5, 0, false)
	if !pool.Unified || !strings.Contains(pool.Source, "/proc/meminfo") {
		t.Errorf("pool from sim meminfo = %+v", pool)
	}
}

func fmtInts(xs []int) string {
	var s []string
	for _, x := range xs {
		s = append(s, itoa(x))
	}
	return strings.Join(s, ",")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestSimRoot_Unset_LeavesPaths(t *testing.T) {
	t.Setenv("NVC_SIM_ROOT", "")
	if simPath("/proc/meminfo") != "/proc/meminfo" {
		t.Error("simPath must be the identity without NVC_SIM_ROOT")
	}
}

func TestDerivePool_Sources(t *testing.T) {
	t.Setenv("NVC_SIM_ROOT", "")
	// 1. report.UnifiedMemory wins.
	r := gb10Report()
	pool, _ := DerivePool(r, "linux", 5, 0, false)
	if !pool.Unified || !strings.Contains(pool.Source, "report.unified_memory") {
		t.Errorf("unified source = %+v", pool.Source)
	}
	near(t, "unified total", GiBf(pool.TotalBytes), 119.7, 0.05)

	// 2. Discrete GPU: VRAM, never system RAM; F note attached.
	d := &types.Report{
		System: types.SystemInfo{RAMTotalMB: 65536},
		GPUs:   []types.GPUInfo{{Name: "NVIDIA GeForce RTX 4090", IsNVIDIA: true, VRAMTotalMB: 24564, VRAMFreeMB: 23000, MemoryReporting: "dedicated"}},
	}
	pool, notes := DerivePool(d, "windows", 5, 0, false)
	if !pool.Discrete || pool.Unified || !strings.Contains(pool.Source, "RTX 4090") || pool.TotalBytes != 24564*1024*1024 {
		t.Errorf("discrete pool = %+v", pool)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "--headroom-gib") {
		t.Errorf("discrete pool must note the F caveat, got %v", notes)
	}
	// F is a unified-memory host-OS reservation: on dedicated VRAM it defaults
	// to 0 (labelled an assumption) and --headroom-gib still overrides it.
	if f, why := PoolFloorBytes(pool, d, "windows", -1); f != 0 || !strings.Contains(why, "assumption") {
		t.Errorf("discrete F = %v GiB (%s), want 0", GiBf(f), why)
	}
	if f, _ := PoolFloorBytes(pool, d, "windows", 4); f != 4*GiB {
		t.Error("--headroom-gib must override the discrete default")
	}
	if f, _ := PoolFloorBytes(gb10Pool(), gb10Report(), "linux", -1); f != 8*GiB {
		t.Error("unified pools keep the spec 7.4 F")
	}

	// 2b. Grace Hopper: Class grace-hopper with GPUSoC "GH200" is a discrete
	// HBM GPU (spec 3.1 flag rule C forces UnifiedMemory=false), so the pool is
	// the 97871 MiB VRAM, never system RAM, and nothing Spark-specific applies.
	gh := gh200Report()
	if IsUnified(gh) {
		t.Error("grace-hopper must not be unified (spec 3.1 flag rule C)")
	}
	if soc := SparkSoC(gh); soc != "" {
		t.Errorf("SparkSoC(GH200) = %q, want empty (GPUSoC GH200 is not a Spark SoC)", soc)
	}
	if bw, _ := Bandwidth(gh); bw != 0 {
		t.Errorf("GH200 bandwidth = %v, want 0 (no figure in the spec)", bw)
	}
	pool, _ = DerivePool(gh, "linux", 5, 0, true)
	if !pool.Discrete || pool.Unified || pool.TotalBytes != float64(gh200MiB)*1024*1024 || !strings.Contains(pool.Source, "GH200") {
		t.Errorf("grace-hopper pool = %+v, want discrete %d MiB from VRAMTotalMB", pool, gh200MiB)
	}
	near(t, "GH200 pool", GiBf(pool.TotalBytes), 95.6, 0.05)
	// The same with the row-9 fallback SoC string: not a Spark SoC either.
	unk := gb10Report()
	unk.Platform.Class, unk.Platform.GPUSoC = "", "unknown-cc12.1"
	unk.GPUs[0].Name = "NVIDIA Graphics Device"
	if soc := SparkSoC(unk); soc != "" {
		t.Errorf("SparkSoC(unknown-cc12.1) = %q, want empty", soc)
	}
	if !IsUnified(unk) {
		t.Error("row-9 fallback keeps unified memory (flag rule B / memory [N/A])")
	}

	// 3. Unified platform without the struct and without /proc/meminfo (Windows N1X, no CIM here): system RAM fallback.
	w := &types.Report{
		System: types.SystemInfo{RAMTotalMB: 130000},
		GPUs:   []types.GPUInfo{{Name: "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)", IsNVIDIA: true}},
	}
	pool, _ = DerivePool(w, "darwin", 5, 0, false) // darwin: neither /proc/meminfo nor CIM is tried
	if !pool.Unified || !strings.Contains(pool.Source, "ram_total_mb") {
		t.Errorf("fallback pool = %+v", pool)
	}
	if SparkSoC(w) != "N1X" {
		t.Errorf("N1X not recognised from the adapter name")
	}

	// 4. --memory-gib overrides the total and labels the source.
	pool, _ = DerivePool(r, "linux", 5, 64, false)
	if GiBf(pool.TotalBytes) != 64 || !strings.Contains(pool.Source, "--memory-gib") {
		t.Errorf("override pool = %+v", pool)
	}

	// 5. Offline (--report) with no memory figure: this host is never queried,
	// whatever OS it runs, and the note asks for --memory-gib.
	pool, notes = DerivePool(&types.Report{}, runtime.GOOS, 5, 0, true)
	if pool.TotalBytes != 0 || pool.Source != "" {
		t.Errorf("offline pool must stay empty, got %+v", pool)
	}
	if joined := strings.Join(notes, " "); !strings.Contains(joined, "--memory-gib") || !strings.Contains(joined, "saved report") {
		t.Errorf("offline notes = %v", notes)
	}
	// The saved report's own RAM figure is still used offline.
	pool, _ = DerivePool(&types.Report{System: types.SystemInfo{RAMTotalMB: 65536}}, runtime.GOOS, 5, 0, true)
	if !strings.Contains(pool.Source, "ram_total_mb") {
		t.Errorf("offline pool must use the report's ram_total_mb, got %+v", pool)
	}
}

func TestPlatformLabel_Empty(t *testing.T) {
	if l := PlatformLabel(&types.Report{}); l != "unknown" {
		t.Errorf("empty report label = %q, want unknown", l)
	}
	if l := PlatformLabel(nil); l != "unknown" {
		t.Errorf("nil report label = %q, want unknown", l)
	}
}

func TestOSFloorAndBandwidth(t *testing.T) {
	r := gb10Report()
	if f, _ := OSFloorBytes(r, "linux", -1); f != 8*GiB {
		t.Errorf("headless Linux F = %v GiB, want 8", GiBf(f))
	}
	r.Linux.SessionType = "wayland"
	if f, why := OSFloorBytes(r, "linux", -1); f != 10*GiB || !strings.Contains(why, "desktop") {
		t.Errorf("desktop Linux F = %v GiB (%s), want 10", GiBf(f), why)
	}
	if f, _ := OSFloorBytes(r, "windows", -1); f != 10*GiB {
		t.Errorf("Windows F = %v GiB, want 10", GiBf(f))
	}
	if f, _ := OSFloorBytes(r, "linux", 0); f != 0 {
		t.Error("explicit --headroom-gib 0 must disable the floor")
	}
	if f, _ := OSFloorBytes(r, "linux", 16); f != 16*GiB {
		t.Error("explicit --headroom-gib 16")
	}
	bw, note := Bandwidth(r)
	if bw != 273e9 || !strings.Contains(note, "GB10") {
		t.Errorf("GB10 bandwidth = %v (%s)", bw, note)
	}
	n1x := &types.Report{Platform: types.PlatformInfo{Class: "rtx-spark"}}
	if bw, note = Bandwidth(n1x); bw != 273e9 || !strings.Contains(note, "unconfirmed") {
		t.Errorf("N1X bandwidth must be 273 and labelled unconfirmed: %v (%s)", bw, note)
	}
	if bw, _ = Bandwidth(&types.Report{GPUs: []types.GPUInfo{{Name: "NVIDIA GeForce RTX 4090", IsNVIDIA: true}}}); bw != 0 {
		t.Error("no bandwidth figure for GPUs the spec does not cover")
	}
}

func TestPlatformLabel(t *testing.T) {
	if l := PlatformLabel(gb10Report()); l != "DGX Spark (GB10)" {
		t.Errorf("label = %s", l)
	}
	inferred := &types.Report{GPUs: []types.GPUInfo{{Name: "NVIDIA GB10", IsNVIDIA: true}}}
	if l := PlatformLabel(inferred); !strings.Contains(l, "inferred") {
		t.Errorf("label without platform class = %s", l)
	}
}

// A multi-GPU discrete machine is sized against the largest single GPU (by
// VRAM total, then by free VRAM): the weights must fit one device because the
// wizard plans no tensor parallelism, and the plan says so in a note.
func TestDerivePool_MultiGPU(t *testing.T) {
	t.Setenv("NVC_SIM_ROOT", "")
	// Two identical 3090s, the first nearly full: the free twin is the pool.
	twins := &types.Report{
		System: types.SystemInfo{RAMTotalMB: 131072},
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "NVIDIA GeForce RTX 3090", IsNVIDIA: true, VRAMTotalMB: 24576, VRAMFreeMB: 100, MemoryReporting: "dedicated"},
			{Index: 1, Name: "NVIDIA GeForce RTX 3090", IsNVIDIA: true, VRAMTotalMB: 24576, VRAMFreeMB: 24000, MemoryReporting: "dedicated"},
		},
	}
	pool, notes := DerivePool(twins, "linux", 5, 0, true)
	if !pool.Discrete || pool.Unified || pool.TotalBytes != 24576*1024*1024 || pool.AvailableBytes != 24000*1024*1024 {
		t.Errorf("twin pool = %+v, want 24576 MiB total / 24000 MiB free", pool)
	}
	want := "2 NVIDIA GPUs detected; the plan sizes against the largest single GPU (NVIDIA GeForce RTX 3090, 24.0 GiB); multi-GPU tensor parallelism is not planned by this wizard."
	if joined := strings.Join(notes, " "); !strings.Contains(joined, want) {
		t.Errorf("twin notes = %q, want %q", joined, want)
	}

	// Different sizes: the largest total wins even when a smaller GPU has more
	// free VRAM, and a non-NVIDIA adapter is not counted.
	mixed := &types.Report{
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "Intel(R) UHD Graphics 770", Vendor: "Intel"},
			{Index: 1, Name: "NVIDIA GeForce RTX 3060", IsNVIDIA: true, VRAMTotalMB: 12288, VRAMFreeMB: 12000, MemoryReporting: "dedicated"},
			{Index: 2, Name: "NVIDIA GeForce RTX 4090", IsNVIDIA: true, VRAMTotalMB: 24564, VRAMFreeMB: 100, MemoryReporting: "dedicated"},
		},
	}
	pool, notes = DerivePool(mixed, "windows", 5, 0, true)
	if pool.TotalBytes != 24564*1024*1024 || pool.AvailableBytes != 100*1024*1024 || !strings.Contains(pool.Source, "RTX 4090") {
		t.Errorf("mixed pool = %+v, want the RTX 4090", pool)
	}
	if joined := strings.Join(notes, " "); !strings.Contains(joined, "2 NVIDIA GPUs detected; the plan sizes against the largest single GPU (NVIDIA GeForce RTX 4090, 24.0 GiB)") {
		t.Errorf("mixed notes = %q", joined)
	}

	// One GPU: no multi-GPU note (the win_rtx3090 golden is unchanged).
	pool, notes = DerivePool(rtx3090Report(), "windows", 5, 0, true)
	if !strings.Contains(pool.Source, "RTX 3090") || strings.Contains(strings.Join(notes, " "), "NVIDIA GPUs detected") {
		t.Errorf("single GPU pool = %+v notes = %v", pool, notes)
	}
}
