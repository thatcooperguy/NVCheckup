package llmplan

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/core"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// MemoryPool is the memory picture the plan is sized against. Zero
// AvailableBytes means "unknown".
type MemoryPool struct {
	TotalBytes        float64
	AvailableBytes    float64
	FreeBytes         float64
	CachedBytes       float64
	BuffersBytes      float64
	SwapTotalBytes    float64
	SwapFreeBytes     float64
	HugePagesTotal    int64
	HugePagesFree     int64
	HugepagesizeBytes float64
	AllocatableBytes  float64 // spec 3.3 arithmetic
	SwapKnown         bool

	Source   string // where TotalBytes came from, printed in the plan
	Unified  bool   // one CPU/GPU pool (GB10/N1X)
	Discrete bool   // dedicated VRAM of a discrete GPU
}

// Allocatable is spec 3.3: allocatable = MemAvailable + SwapFree; if
// HugePages_Total != 0 then allocatable = HugePages_Free x Hugepagesize and
// swap counts 0.
func (p MemoryPool) Allocatable() float64 {
	if p.HugePagesTotal != 0 {
		return float64(p.HugePagesFree) * p.HugepagesizeBytes
	}
	return p.AvailableBytes + p.SwapFreeBytes
}

// SwapUsedBytes is SwapTotal - SwapFree when swap is known.
func (p MemoryPool) SwapUsedBytes() float64 {
	if !p.SwapKnown || p.SwapTotalBytes < p.SwapFreeBytes {
		return 0
	}
	return p.SwapTotalBytes - p.SwapFreeBytes
}

// parseMeminfo reads the /proc/meminfo keys the wizard needs (values in kB
// except HugePages counts).
func parseMeminfo(text string) MemoryPool {
	var p MemoryPool
	kb := func(v string) float64 {
		f := strings.Fields(v)
		if len(f) == 0 {
			return 0
		}
		n, _ := strconv.ParseFloat(f[0], 64)
		return n * 1024
	}
	count := func(v string) int64 {
		f := strings.Fields(v)
		if len(f) == 0 {
			return 0
		}
		n, _ := strconv.ParseInt(f[0], 10, 64)
		return n
	}
	for _, line := range strings.Split(text, "\n") {
		k, v := util.ParseKeyValue(line, ":")
		switch k {
		case "MemTotal":
			p.TotalBytes = kb(v)
		case "MemFree":
			p.FreeBytes = kb(v)
		case "MemAvailable":
			p.AvailableBytes = kb(v)
		case "Buffers":
			p.BuffersBytes = kb(v)
		case "Cached":
			p.CachedBytes = kb(v)
		case "SwapTotal":
			p.SwapTotalBytes = kb(v)
			p.SwapKnown = true
		case "SwapFree":
			p.SwapFreeBytes = kb(v)
		case "HugePages_Total":
			p.HugePagesTotal = count(v)
		case "HugePages_Free":
			p.HugePagesFree = count(v)
		case "Hugepagesize":
			p.HugepagesizeBytes = kb(v)
		}
	}
	p.AllocatableBytes = p.Allocatable()
	return p
}

// readMeminfo reads /proc/meminfo (under NVC_SIM_ROOT when set).
func readMeminfo() (MemoryPool, error) {
	path := common.SimPath("/proc/meminfo")
	data, err := os.ReadFile(path)
	if err != nil {
		return MemoryPool{}, err
	}
	p := parseMeminfo(string(data))
	if p.TotalBytes <= 0 {
		return p, fmt.Errorf("%s: no MemTotal", path)
	}
	p.Source = "/proc/meminfo MemTotal (measured)"
	return p, nil
}

// poolFromUnifiedMemory converts the phase-4 collector result.
func poolFromUnifiedMemory(u *types.UnifiedMemoryInfo) MemoryPool {
	p := MemoryPool{
		TotalBytes:        float64(u.MemTotalKB) * 1024,
		AvailableBytes:    float64(u.MemAvailableKB) * 1024,
		FreeBytes:         float64(u.MemFreeKB) * 1024,
		CachedBytes:       float64(u.CachedKB) * 1024,
		BuffersBytes:      float64(u.BuffersKB) * 1024,
		SwapTotalBytes:    float64(u.SwapTotalKB) * 1024,
		SwapFreeBytes:     float64(u.SwapFreeKB) * 1024,
		HugePagesTotal:    u.HugePagesTotal,
		HugePagesFree:     u.HugePagesFree,
		HugepagesizeBytes: float64(u.HugepagesizeKB) * 1024,
		SwapKnown:         true,
		Source:            "report.unified_memory (/proc/meminfo MemTotal, measured)",
		Unified:           true,
	}
	p.AllocatableBytes = float64(u.AllocatableKB) * 1024
	if p.AllocatableBytes == 0 {
		p.AllocatableBytes = p.Allocatable()
	}
	return p
}

// windowsMemory is the Win32_OperatingSystem projection (kB values).
type windowsMemory struct {
	TotalVisibleMemorySize float64 `json:"TotalVisibleMemorySize"`
	FreePhysicalMemory     float64 `json:"FreePhysicalMemory"`
}

// windowsPool reads TotalVisibleMemorySize/FreePhysicalMemory (spec 7.1,
// spec 8) with one read-only CIM query.
func windowsPool(timeout int) (MemoryPool, error) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize,FreePhysicalMemory | ConvertTo-Json -Compress")
	if r.Err != nil && r.Stdout == "" {
		return MemoryPool{}, fmt.Errorf("Win32_OperatingSystem query failed: %v", r.Err)
	}
	var w windowsMemory
	if err := json.Unmarshal([]byte(r.Stdout), &w); err != nil {
		return MemoryPool{}, fmt.Errorf("Win32_OperatingSystem query: %w", err)
	}
	if w.TotalVisibleMemorySize <= 0 {
		return MemoryPool{}, fmt.Errorf("Win32_OperatingSystem returned no TotalVisibleMemorySize")
	}
	p := MemoryPool{
		TotalBytes:     w.TotalVisibleMemorySize * 1024,
		AvailableBytes: w.FreePhysicalMemory * 1024,
		FreeBytes:      w.FreePhysicalMemory * 1024,
		Source:         "Win32_OperatingSystem.TotalVisibleMemorySize (measured)",
	}
	p.AllocatableBytes = p.AvailableBytes
	return p, nil
}

// sparkGPUNames are the nvidia-smi / INF names of spec 3.2 / 2.1 / 2.2 used
// as a fallback when the platform collector has not classified the machine.
var sparkGPUNames = []struct{ substr, soc string }{
	{"GB10", "GB10"},         // spec 2.1: nvidia-smi Name "NVIDIA GB10"
	{"RTX Spark N1X", "N1X"}, // spec 2.2: INF names "NVIDIA RTX Spark N1X (...)"
	{"RTX Spark", "N1X"},     // spec 2.2 marketing name
}

// SparkSoC returns "GB10", "N1X" or "" for the report. Platform.GPUSoC is
// trusted only for those two values: the detector also writes "GH200" (spec
// 3.1 row 7, a discrete-HBM Grace Hopper part) and "unknown-cc12.1" (row 9),
// neither of which is a Spark SoC the wizard knows a bandwidth for.
func SparkSoC(r *types.Report) string {
	if r == nil {
		return ""
	}
	switch r.Platform.GPUSoC {
	case "GB10", "N1X":
		return r.Platform.GPUSoC
	}
	if r.Platform.Class == "grace-hopper" {
		return "" // spec 3.1 row 7: GH200/GB200/GB300 are never a Spark
	}
	switch r.Platform.Class {
	case "dgx-spark":
		return "GB10"
	case "rtx-spark":
		return "N1X"
	}
	for _, g := range r.GPUs {
		for _, s := range sparkGPUNames {
			if strings.Contains(g.Name, s.substr) {
				return s.soc
			}
		}
	}
	return ""
}

// IsUnified reports whether the machine shares one memory pool between CPU
// and GPU (spec 2.1: nvidia-smi memory fields are [N/A] on GB10).
func IsUnified(r *types.Report) bool {
	if r == nil {
		return false
	}
	if r.Platform.Class == "grace-hopper" {
		// spec 3.1 flag rule C: Class=grace-hopper (numeric nvidia-smi memory)
		// forces UnifiedMemory=false; the GPU is discrete HBM (spec 2.3 / S29).
		return false
	}
	if r.Platform.UnifiedMemory || r.UnifiedMemory != nil {
		return true
	}
	for _, g := range r.GPUs {
		if g.MemoryReporting == "not-supported" {
			return true
		}
	}
	return SparkSoC(r) != ""
}

// Bandwidth returns the memory bandwidth used for the decode ceilings and a
// label saying where it came from. Zero when the spec gives no figure.
func Bandwidth(r *types.Report) (float64, string) {
	switch SparkSoC(r) {
	case "GB10":
		return GB10BandwidthBytesPerSec, "273 GB/s (GB10 LPDDR5X, spec 2.1)"
	case "N1X":
		return GB10BandwidthBytesPerSec, "273 GB/s assumed (N1X press figure ~300 GB/s is unconfirmed; spec 2.2 says use 273)"
	}
	return 0, "unknown for this GPU (no figure in the spec)"
}

// PlatformLabel is the header line's platform description.
func PlatformLabel(r *types.Report) string {
	if r == nil {
		return "unknown"
	}
	switch r.Platform.Class {
	case "dgx-spark":
		return "DGX Spark (GB10)"
	case "rtx-spark":
		return "RTX Spark (N1X)"
	case "":
	default:
		return r.Platform.Class
	}
	switch SparkSoC(r) {
	case "GB10":
		return "DGX Spark / GB10 (inferred from the nvidia-smi name)"
	case "N1X":
		return "RTX Spark / N1X (inferred from the adapter name)"
	}
	for _, g := range r.GPUs {
		if g.IsNVIDIA && g.Name != "" {
			return g.Name
		}
	}
	if r.System.OSName == "" {
		return "unknown"
	}
	return r.System.OSName
}

// DerivePool picks the memory pool (spec 7.2): report.UnifiedMemory first,
// then dedicated VRAM on discrete GPUs, then /proc/meminfo (Linux or
// NVC_SIM_ROOT), then TotalVisibleMemorySize on Windows, then the system
// collector's RAM total. It never reads nvidia-smi memory on unified
// platforms (spec 7.9). memoryGiB > 0 overrides the total (--memory-gib).
// offline (--report) means the plan is for the machine the report was taken
// on: the live /proc/meminfo and CIM fallbacks of this host are skipped.
func DerivePool(r *types.Report, goos string, timeout int, memoryGiB float64, offline bool) (MemoryPool, []string) {
	var notes []string
	unified := IsUnified(r)
	var pool MemoryPool
	found := false

	if r != nil && r.UnifiedMemory != nil && r.UnifiedMemory.MemTotalKB > 0 {
		pool = poolFromUnifiedMemory(r.UnifiedMemory)
		found = true
	}
	if !found && !unified && r != nil {
		// Discrete GPUs: the pool is the largest single device (by VRAM total,
		// then by free VRAM). The weights must fit one GPU because the wizard
		// plans no tensor parallelism, so on a multi-GPU machine the other
		// devices only get a note.
		best := -1
		count := 0
		for i, g := range r.GPUs {
			if !g.IsNVIDIA || g.VRAMTotalMB <= 0 || g.MemoryReporting == "not-supported" {
				continue
			}
			count++
			if best < 0 || g.VRAMTotalMB > r.GPUs[best].VRAMTotalMB ||
				(g.VRAMTotalMB == r.GPUs[best].VRAMTotalMB && g.VRAMFreeMB > r.GPUs[best].VRAMFreeMB) {
				best = i
			}
		}
		if best >= 0 {
			g := r.GPUs[best]
			pool = MemoryPool{
				TotalBytes:     float64(g.VRAMTotalMB) * 1024 * 1024,
				AvailableBytes: float64(g.VRAMFreeMB) * 1024 * 1024,
				Source:         fmt.Sprintf("nvidia-smi memory.total of %s (dedicated VRAM, discrete GPU)", g.Name),
				Discrete:       true,
			}
			pool.AllocatableBytes = pool.AvailableBytes
			found = true
			notes = append(notes, "Discrete GPU: the pool is dedicated VRAM, not the shared pool spec 7.4 sizes; the OS floor F defaults to 0 here (F is a host-OS reservation out of unified memory) unless --headroom-gib is given, and the swap/page-cache checks are skipped.")
			if count > 1 {
				notes = append(notes, fmt.Sprintf("%d NVIDIA GPUs detected; the plan sizes against the largest single GPU (%s, %s); multi-GPU tensor parallelism is not planned by this wizard.", count, g.Name, fmtGiB(pool.TotalBytes)))
			}
		}
	}
	if !found && !offline && (goos == "linux" || common.SimRoot() != "") {
		if p, err := readMeminfo(); err == nil {
			pool = p
			found = true
		} else {
			notes = append(notes, "could not read /proc/meminfo: "+err.Error())
		}
	}
	if !found && !offline && goos == "windows" {
		if p, err := windowsPool(timeout); err == nil {
			pool = p
			found = true
		} else {
			notes = append(notes, err.Error())
		}
	}
	if !found && r != nil && r.System.RAMTotalMB > 0 {
		pool = MemoryPool{TotalBytes: float64(r.System.RAMTotalMB) * 1024 * 1024, Source: "report.system.ram_total_mb (MemAvailable unknown)"}
		found = true
	}
	if unified {
		pool.Unified = true
	}
	if memoryGiB > 0 {
		pool.TotalBytes = memoryGiB * GiB
		pool.Source = fmt.Sprintf("--memory-gib %.1f (user override)", memoryGiB)
		if !found {
			notes = append(notes, "MemAvailable unknown: only the design fit (W + KV + R + F <= pool) is evaluated.")
		}
	} else if !found && offline {
		notes = append(notes, "the report has no memory figure and the live host is not queried for a saved report; pass --memory-gib N to size against a pool.")
	} else if !found {
		notes = append(notes, "no memory source available; pass --memory-gib N to size against a pool.")
	}
	return pool, notes
}

// discreteFloorGiB is F for a dedicated-VRAM pool. Assumption, not from the
// spec: spec 7.4 defines F (8/10 GiB) as the host OS's share of unified
// memory, which does not come out of a discrete GPU's VRAM; the spec gives no
// figure for discrete GPUs, so nothing is reserved unless --headroom-gib says so.
const discreteFloorGiB = 0

// PoolFloorBytes is OSFloorBytes with the pool kind taken into account: a
// discrete-GPU pool gets discreteFloorGiB unless --headroom-gib overrides it.
func PoolFloorBytes(pool MemoryPool, r *types.Report, goos string, headroomGiB float64) (float64, string) {
	if pool.Discrete && headroomGiB < 0 {
		return discreteFloorGiB * GiB, fmt.Sprintf("%d GiB: dedicated VRAM of a discrete GPU (assumption; spec 7.4 F is a unified-memory reservation; set --headroom-gib to reserve VRAM)", discreteFloorGiB)
	}
	return OSFloorBytes(r, goos, headroomGiB)
}

// OSFloorBytes is spec 7.4 F: 8 GiB headless DGX OS, 10 GiB with GNOME or on
// Windows. headroomGiB >= 0 overrides it (--headroom-gib).
func OSFloorBytes(r *types.Report, goos string, headroomGiB float64) (float64, string) {
	if headroomGiB >= 0 {
		return headroomGiB * GiB, fmt.Sprintf("--headroom-gib %.1f (user override)", headroomGiB)
	}
	if goos == "windows" {
		return OSFloorDesktopGiB * GiB, "10 GiB: Windows (spec 7.4)"
	}
	if r != nil && r.Linux != nil {
		switch strings.ToLower(r.Linux.SessionType) {
		case "x11", "wayland":
			return OSFloorDesktopGiB * GiB, fmt.Sprintf("10 GiB: desktop session (%s) detected (spec 7.4: GNOME)", r.Linux.SessionType)
		}
	}
	return OSFloorHeadlessGiB * GiB, "8 GiB: headless Linux (spec 7.4)"
}

// watchedPorts are the inference ports of spec 7.7.
var watchedPorts = []int{8000, 30000, 11434, 8355}

// ListeningPorts returns the TCP ports in LISTEN state, from the ecosystem
// collector when present, else from /proc/net/tcp{,6} (read-only, under
// NVC_SIM_ROOT when set). known is false when neither source was readable.
func ListeningPorts(r *types.Report, goos string) (ports []int, known bool) {
	if r != nil && r.Ecosystem != nil && len(r.Ecosystem.ListeningPorts) > 0 {
		return append([]int(nil), r.Ecosystem.ListeningPorts...), true
	}
	if goos != "linux" && common.SimRoot() == "" {
		return nil, false
	}
	set := map[int]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(common.SimPath(f))
		if err != nil {
			continue
		}
		known = true
		for _, p := range parseProcNetTCP(string(data)) {
			set[p] = true
		}
	}
	for p := range set {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, known
}

// parseProcNetTCP extracts local ports of sockets in state 0A (LISTEN).
func parseProcNetTCP(text string) []int {
	var ports []int
	for i, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 4 || f[3] != "0A" {
			continue
		}
		idx := strings.LastIndex(f[1], ":")
		if idx < 0 {
			continue
		}
		n, err := strconv.ParseInt(f[1][idx+1:], 16, 32)
		if err == nil {
			ports = append(ports, int(n))
		}
	}
	return ports
}

// CollectReport runs the existing read-only pipeline in AI mode with network
// probes off (spec 7.2 reuses the collectors; spec 7.9 never contacts the
// network).
func CollectReport(timeout int, printFn func(string)) (*types.Report, error) {
	cfg := types.RunConfig{
		Mode:        types.ModeAI,
		OutDir:      ".",
		Timeout:     timeout,
		Redact:      true,
		NetworkTest: false,
	}
	return core.Run(cfg, false, printFn)
}
