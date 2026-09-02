package common

import (
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Linux files read by the unified-memory collector (all through SimPath).
const (
	swapsPath      = "/proc/swaps"
	swappinessPath = "/proc/sys/vm/swappiness"
	psiMemoryPath  = "/proc/pressure/memory"
	vmstatPath     = "/proc/vmstat"
)

// Kernel-log signatures of the unified-memory-oom-events rule
// (docs/roadmap/spark-rules.json): the OOM killer line and the NVRM
// allocation failure "NVRM: nvCheckOkFailedNoLog: Check failed: Out of memory
// [NV_ERR_NO_MEMORY]".
const (
	oomKilledText    = "Out of memory: Killed process"
	nvrmNoMemoryText = "NV_ERR_NO_MEMORY"
)

// ComputeAppsQuery is the nvidia-smi query that counts processes holding a GPU
// context (count only; pids and names are never stored). NVML may answer
// "Not Supported" on some platforms, which the collector tolerates.
const ComputeAppsQuery = "--query-compute-apps=pid"

// windowsMemoryScript prints the Windows pool figures of spec section 8
// (Win32_OperatingSystem.TotalVisibleMemorySize / FreePhysicalMemory, both in
// kB) as key=value lines.
const windowsMemoryScript = `$ErrorActionPreference = 'SilentlyContinue'; $os = Get-CimInstance Win32_OperatingSystem; ` +
	`"total_kb=$($os.TotalVisibleMemorySize)"; "free_kb=$($os.FreePhysicalMemory)"; exit 0`

// CollectUnifiedMemory gathers the system-memory picture of a unified-memory
// platform (spec section 3.3 and the UnifiedMemoryInfo type of section 4).
// On Linux it reads /proc/meminfo, /proc/swaps, vm.swappiness,
// /proc/pressure/memory and /proc/vmstat, counts GPU processes via nvidia-smi
// and OOM / NVRM no-memory lines in the kernel log. On Windows it reads the
// WMI pool figures. Every source is optional: a missing file or a rejected
// command yields a non-fatal CollectorError and partial data, never a failure.
func CollectUnifiedMemory(timeout int) (types.UnifiedMemoryInfo, []types.CollectorError) {
	var info types.UnifiedMemoryInfo
	var errs []types.CollectorError

	switch {
	case util.IsLinux():
		collectUnifiedMemoryLinux(&info, &errs, timeout)
	case util.IsWindows():
		collectUnifiedMemoryWindows(&info, &errs, timeout)
	default:
		errs = append(errs, types.CollectorError{Collector: "unified_memory", Error: "unsupported platform"})
		return info, errs
	}

	info.GPUProcesses = countGPUProcesses(timeout, &errs)
	return info, errs
}

func collectUnifiedMemoryLinux(info *types.UnifiedMemoryInfo, errs *[]types.CollectorError, timeout int) {
	note := func(collector, msg string) {
		*errs = append(*errs, types.CollectorError{Collector: collector, Error: msg})
	}

	// First pswpin sample; the second is taken after the other reads and the
	// nvidia-smi / dmesg calls so the delta reflects swap-in activity during
	// the collector's own run time.
	pswpinBefore, havePswpin := readVmstatField("pswpin")

	if data, err := ReadSimFile(meminfoPath); err == nil {
		applyMeminfo(info, ParseMeminfo(string(data)))
	} else {
		note("unified_memory.meminfo", meminfoPath+": "+err.Error())
	}

	if data, err := ReadSimFile(swapsPath); err == nil {
		info.SwapDevices = parseSwaps(string(data))
	} else {
		note("unified_memory.swaps", swapsPath+": "+err.Error())
	}

	if v := readSimString(swappinessPath); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.Swappiness = n
		} else {
			note("unified_memory.swappiness", "unparsable value "+strconv.Quote(v))
		}
	} else {
		note("unified_memory.swappiness", swappinessPath+" unreadable")
	}

	if data, err := ReadSimFile(psiMemoryPath); err == nil {
		info.PSISomeAvg10, info.PSIFullAvg10 = parsePSI(string(data))
	} else {
		note("unified_memory.psi", psiMemoryPath+": "+err.Error()+" (PSI needs CONFIG_PSI; pressure unknown)")
	}

	info.OOMKills, info.NVRMNoMemory = countKernelMemoryEvents(timeout, errs)

	if after, ok := readVmstatField("pswpin"); ok {
		info.Pswpin = after
		if havePswpin && after >= pswpinBefore {
			info.PswpinDelta = after - pswpinBefore
		}
	} else {
		note("unified_memory.vmstat", vmstatPath+" pswpin unreadable")
	}
}

func collectUnifiedMemoryWindows(info *types.UnifiedMemoryInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", windowsMemoryScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "unified_memory.wmi", Error: "Win32_OperatingSystem query failed: " + commandFailureDetail(r)})
		return
	}
	total, free := parseWindowsMemory(r.Stdout)
	info.MemTotalKB = total
	info.MemFreeKB = free
	// ASSUMPTION: Windows exposes no MemAvailable; FreePhysicalMemory is the
	// closest figure spec section 8 names, so it doubles as MemAvailable and
	// the allocatable estimate (no swap term; the page file is not counted).
	info.MemAvailableKB = free
	info.AllocatableKB = free
}

// parseWindowsMemory reads the total_kb / free_kb lines of windowsMemoryScript.
func parseWindowsMemory(out string) (totalKB, freeKB int64) {
	for _, line := range strings.Split(out, "\n") {
		k, v := util.ParseKeyValue(line, "=")
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "total_kb":
			totalKB = n
		case "free_kb":
			freeKB = n
		}
	}
	return totalKB, freeKB
}

// applyMeminfo copies the /proc/meminfo fields into info and derives
// AllocatableKB per spec 3.3.
func applyMeminfo(info *types.UnifiedMemoryInfo, m map[string]int64) {
	info.MemTotalKB = m["MemTotal"]
	info.MemFreeKB = m["MemFree"]
	info.MemAvailableKB = m["MemAvailable"]
	info.BuffersKB = m["Buffers"]
	info.CachedKB = m["Cached"]
	info.SwapTotalKB = m["SwapTotal"]
	info.SwapFreeKB = m["SwapFree"]
	info.HugePagesTotal = m["HugePages_Total"]
	info.HugePagesFree = m["HugePages_Free"]
	info.HugepagesizeKB = m["Hugepagesize"]
	info.AllocatableKB = AllocatableKB(info.MemAvailableKB, info.SwapFreeKB, info.HugePagesTotal, info.HugePagesFree, info.HugepagesizeKB)
}

// AllocatableKB is the unified-memory arithmetic of spec section 3.3 (NVIDIA
// guidance): allocatable = MemAvailable + SwapFree; when HugePages_Total != 0,
// allocatable = HugePages_Free * Hugepagesize and swap counts 0. nvidia-smi
// memory, cudaMemGetInfo and MemFree are never used as headroom.
func AllocatableKB(memAvailableKB, swapFreeKB, hugePagesTotal, hugePagesFree, hugepagesizeKB int64) int64 {
	if hugePagesTotal != 0 {
		return hugePagesFree * hugepagesizeKB
	}
	return memAvailableKB + swapFreeKB
}

// parseSwaps returns the device names of /proc/swaps (first column of every
// line after the header), e.g. "/swapfile" or "/dev/zram0".
func parseSwaps(content string) []string {
	var devs []string
	for i, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || (i == 0 && fields[0] == "Filename") {
			continue
		}
		devs = append(devs, fields[0])
	}
	return devs
}

// parsePSI extracts "some avg10" and "full avg10" from /proc/pressure/memory:
//
//	some avg10=0.00 avg60=0.00 avg300=0.00 total=12345
//	full avg10=0.00 avg60=0.00 avg300=0.00 total=6789
func parsePSI(content string) (someAvg10, fullAvg10 float64) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var v float64
		found := false
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "avg10=") {
				if x, err := strconv.ParseFloat(strings.TrimPrefix(f, "avg10="), 64); err == nil {
					v, found = x, true
				}
			}
		}
		if !found {
			continue
		}
		switch fields[0] {
		case "some":
			someAvg10 = v
		case "full":
			fullAvg10 = v
		}
	}
	return someAvg10, fullAvg10
}

// readVmstatField returns one counter of /proc/vmstat.
func readVmstatField(name string) (int64, bool) {
	data, err := ReadSimFile(vmstatPath)
	if err != nil {
		return 0, false
	}
	return parseVmstatField(string(data), name)
}

// parseVmstatField finds "name N" in /proc/vmstat content.
func parseVmstatField(content, name string) (int64, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == name {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// countGPUProcesses counts the rows of nvidia-smi --query-compute-apps=pid.
// A missing nvidia-smi, a rejected query or a "Not Supported" answer all
// produce a note and a count of 0.
func countGPUProcesses(timeout int, errs *[]types.CollectorError) int {
	if !util.CommandExists("nvidia-smi") {
		return 0
	}
	r := util.RunCommand(timeout, "nvidia-smi", ComputeAppsQuery, "--format=csv,noheader")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "unified_memory.gpu_processes", Error: "nvidia-smi compute-apps query failed (GPU process count unknown): " + commandFailureDetail(r)})
		return 0
	}
	return countComputeAppRows(r.Stdout)
}

// countComputeAppRows counts numeric pid lines, ignoring placeholders and
// failure text.
func countComputeAppRows(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || isNotAvailable(t) {
			continue
		}
		if _, ok := parseSmallInt(strings.Split(t, ",")[0]); ok {
			n++
		}
	}
	return n
}

// countKernelMemoryEvents counts OOM-killer and NVRM NV_ERR_NO_MEMORY lines in
// dmesg, falling back to journalctl -k when dmesg is restricted
// (kernel.dmesg_restrict). Counts only; no process names are kept.
func countKernelMemoryEvents(timeout int, errs *[]types.CollectorError) (oom, nvrm int) {
	var log string
	got := false
	if util.CommandExists("dmesg") {
		r := util.RunCommand(timeout, "dmesg")
		if r.Err == nil {
			log, got = r.Stdout, true
		}
	}
	if !got && util.CommandExists("journalctl") {
		r := util.RunCommand(timeout, "journalctl", "-k", "-b", "--no-pager", "-q")
		if r.Err == nil {
			log, got = r.Stdout, true
		}
	}
	if !got {
		*errs = append(*errs, types.CollectorError{Collector: "unified_memory.kernel_log", Error: "kernel log unreadable (dmesg restricted and journalctl unavailable); OOM counts unknown"})
		return 0, 0
	}
	return CountMemoryEvents(log)
}

// CountMemoryEvents counts the OOM-killer and NVRM no-memory signatures in a
// kernel log.
func CountMemoryEvents(log string) (oom, nvrm int) {
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, oomKilledText) {
			oom++
		}
		if strings.Contains(line, "NVRM") && strings.Contains(line, nvrmNoMemoryText) {
			nvrm++
		}
	}
	return oom, nvrm
}
