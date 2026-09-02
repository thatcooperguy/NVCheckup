package analyzer

// DGX Spark / RTX Spark / unified-memory rules.
//
// Every rule here implements one row of the catalog in
// docs/roadmap/spark-support.md section 5 (machine-readable copy:
// docs/roadmap/spark-rules.json, mirrored in knowledge/rules.json and in the
// generated sparkRules table). Titles, next steps, impact and confidence come
// from that table via sparkFinding; this file only decides WHEN a rule fires
// and what measured values go into its evidence. Thresholds and strings taken
// from the spec cite their section next to the constant. Nothing here reads
// the system: the analyzer works on the collected types only.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Platform class names (spec section 3.1, the closed platforms[] set).
const (
	classDGXSpark    = "dgx-spark"
	classRTXSpark    = "rtx-spark"
	classJetson      = "jetson"
	classGraceHopper = "grace-hopper"
	classArm64DGPU   = "arm64-dgpu"
)

// memoryReportingNotSupported is GPUInfo.MemoryReporting when nvidia-smi
// prints [N/A] / "Not Supported" for the memory fields (spec 3.1 flag rule B).
const memoryReportingNotSupported = "not-supported"

const kbPerGiB = 1024 * 1024

// ── Unified-memory thresholds (spec section 5) ────────────────────────

const (
	// unified-memory-pressure: WARN below 8 GiB MemAvailable, CRIT below 4 GiB
	// with a GPU process present. Both are inferences aligned with the
	// llm-plan OS floor F (spec 5 / 7.4), not NVIDIA figures.
	umPressureWarnKB = 8 * kbPerGiB
	umPressureCritKB = 4 * kbPerGiB
	// unified-memory-pressure PSI thresholds (/proc/pressure/memory full avg10;
	// spec 5, inference).
	umPSIFullWarn = 0.1
	umPSIFullCrit = 1.0
	// unified-memory-swap-in-use: >= 1 GiB swapped or > 2 % of MemTotal (spec 5).
	umSwapUsedKB     = 1 * kbPerGiB
	umSwapUsedPctX10 = 20 // 2.0 % expressed in tenths of a percent
	// unified-memory-page-cache-hold: MemFree < 4 GiB and > 20 GiB reclaimable (spec 5).
	umPageCacheFreeKB = 4 * kbPerGiB
	umPageCacheHoldKB = 20 * kbPerGiB
)

// ── DGX OS / driver thresholds (spec sections 2.1 and 5) ─────────────

const (
	// dgx-spark-driver-too-old: GB10 needs the R580 open modules (spec 5).
	gb10MinDriverMajor = 580
	// dgx-spark-ota-outdated: current FE stack per the Aug 2026 table in
	// spec 2.1 / rule row: DGX OS 7.5.0, driver 580.159.03, kernel 6.17.
	otaCurrentVersion  = "7.5.0"
	otaCurrentDriver   = "580.159.03"
	otaCurrentKernel   = "6.17"
	otaCurrentDisplay  = "DGX OS 7.5.0 / 580.159.03 / CUDA 13.0.2 / kernel 6.17"
	dashboardPort      = 11000 // DGX Dashboard http://localhost:11000 (spec 2.1)
	cublasFixedDriver  = "580.173.02"
	gb10MaxSMClockMHz  = 3003 // nvidia-smi -q "Max Clocks Graphics 3003 MHz" (spec 2.1)
	gb10ThermalHotMC   = 93000
	uncleanBootWindow  = 14 // gb10-logless-hard-poweroff default window in days (spec 5)
	uncleanBootsToWarn = 2  // WARN needs >= 2 unclean boots; one is INFO (spec 5)
)

// Driver branches NVIDIA staff called unsupported on DGX Spark (spec 5,
// dgx-spark-driver-branch-unsupported, S40/S125).
var unsupportedDriverMajors = map[int]bool{590: true, 595: true}

// nvidiaKernelRe is the Canonical linux-nvidia flavour test of spec 3.1 row 11.
var nvidiaKernelRe = regexp.MustCompile(`^\d+\.\d+\.\d+-\d+-nvidia(-64k|-lowlatency)?$`)

// gspFailureNeedles are the GSP / SEC2 kernel lines of spec 3.2.
var gspFailureNeedles = []string{
	"Timeout after 6s of waiting for RPC response from GPU0 GSP",
	"ksec2PrepareBootCommands_GB20B: SEC2 secure boot partition timed out",
	"RmInitAdapter: Cannot initialize GSP firmware RM",
	"RmInitAdapter failed!",
}

// noDevicesNeedle is the nvidia-smi text of spec 3.2.
const noDevicesNeedle = "No devices were found"

// gb10NotSupportedNeedle is the nvidia-smi text listed in spec 6 and the
// dgx-spark-foreign-driver-packages trigger.
const gb10NotSupportedNeedle = "NVIDIA-GB10 not supported"

// cublasBatchNeedles are the log strings of the dgx-spark-cublas-batch-bug row.
var cublasBatchNeedles = []string{
	"CUDNN_FE failure 11: CUDNN_BACKEND_API_FAILED",
	"CUBLAS failure 14: CUBLAS_STATUS_INTERNAL_ERROR",
}

// cx7SlotPowerNeedle is the benign mlx5 message of spec 3.2.
const cx7SlotPowerNeedle = "Detected insufficient power on the PCIe slot"

// suspendWarningNeedle: dgx-spark-suspend-failure matches the function name
// only (the line number changes with every driver build, spec 5).
const suspendWarningNeedle = "nv_set_system_power_state"

// Foreign driver package patterns of the dgx-spark-foreign-driver-packages
// row (spec 3.2 "Wrong:" list). nvidia-dkms-NNN-open is excluded on purpose.
var foreignPackageRes = []*regexp.Regexp{
	regexp.MustCompile(`^nvidia-driver-\d+-server`),
	regexp.MustCompile(`^nvidia-dkms-\d+(-open)?-server`),
	regexp.MustCompile(`^nvidia-fabricmanager-`),
	regexp.MustCompile(`^nvidia-nvswitch-`),
	regexp.MustCompile(`^nvidia-system-station`),
	regexp.MustCompile(`^nvidia-driver-\d+$`), // non-'-open' driver metapackage
}

// ── Firmware thresholds (spec 5, dgx-spark-firmware-behind; S4 July 2026) ──

const (
	fwFEECMin  = "3.5.8"    // Embedded controller, Founders Edition
	fwFESoCMin = "2.155.11" // UEFI / SoC firmware, Founders Edition
	fwFEPDMin  = "0.5.22"   // USB power delivery controller, Founders Edition
	// feVendor: DMI sys_vendor of a Founders Edition unit (spec 3.1 row 10);
	// every other vendor with a GB10 is an OEM unit on its own firmware track.
	feVendor = "NVIDIA"
)

// fwComponentNames map fwupdmgr device names onto the three FE thresholds.
// ASSUMPTION: the verbatim fwupdmgr get-devices names on GB10 are not
// captured yet (spec 12 open question); matching is on these lower-cased
// tokens and must be revisited with a real capture.
var (
	fwNameEC  = []string{"embedded controller", " ec", "ec "}
	fwNameSoC = []string{"soc", "uefi", "system firmware", "sbios"}
	fwNamePD  = []string{"power delivery", "usb-pd", "usb pd", " pd"}
	// fwNameCX7: the ConnectX-7 NIC firmware row (nvidia-spark-mlnx-firmware-
	// manager / fwupd), quoted by cx7-link-speed-degraded; no FE threshold.
	fwNameCX7 = []string{"connectx", "cx-7", "cx7", "mellanox", "mt2910"}
)

// ── PD power wedge signature (spec 5, gb10-pd-power-wedge; S120) ──────

const (
	wedgeUtilMin     = 90   // utilization.gpu >= 90 %
	wedgeClockMaxMHz = 1400 // clocks.sm < 1400 MHz (typically 513/611/650/721)
	wedgePowerMaxW   = 40.0 // power.draw < 40 W
)

// ── generic helpers ──────────────────────────────────────────────────

// sparkFinding builds a finding from the catalog row id with the given
// evidence. Callers may override Severity/Confidence for documented variants.
func sparkFinding(id, evidence string) types.Finding {
	rule, ok := sparkRules[id]
	if !ok {
		// Unknown ids are a programming error caught by the knowledge tests;
		// still return something usable rather than panic in production.
		return types.Finding{ID: id, Severity: types.SeverityInfo, Title: id, Evidence: evidence, Impact: "none"}
	}
	steps := make([]string, len(rule.Steps))
	copy(steps, rule.Steps)
	return types.Finding{
		ID:           id,
		Severity:     rule.Severity,
		Title:        rule.Title,
		Evidence:     evidence,
		WhyItMatters: rule.Why,
		NextSteps:    steps,
		Category:     rule.Category,
		Confidence:   rule.Confidence,
		Impact:       rule.Impact,
	}
}

func isDGXSpark(r *types.Report) bool   { return r.Platform.Class == classDGXSpark }
func isRTXSpark(r *types.Report) bool   { return r.Platform.Class == classRTXSpark }
func isUnifiedMem(r *types.Report) bool { return r.Platform.UnifiedMemory }

// isWindowsReport reports whether the data came from a Windows host.
func isWindowsReport(r *types.Report) bool {
	return r.Platform.IsWindowsOnArm || r.Windows != nil || r.Metadata.Platform == "windows" ||
		strings.Contains(strings.ToLower(r.System.OSName), "windows")
}

// gb10Hardware: GB10 silicon is present whatever nvidia-smi says (spec 3.1
// row 5: lspci [10de:2e12] or the GB10 name).
func gb10Hardware(r *types.Report) bool {
	if isDGXSpark(r) || strings.EqualFold(r.Platform.GPUSoC, "GB10") {
		return true
	}
	for _, g := range r.GPUs {
		if strings.EqualFold(strings.TrimPrefix(strings.ToLower(g.PCIDeviceID), "0x"), "2e12") {
			return true
		}
	}
	return false
}

// firstNVIDIAGPU returns the first NVIDIA inventory entry, or nil.
func firstNVIDIAGPU(r *types.Report) *types.GPUInfo {
	for i := range r.GPUs {
		if r.GPUs[i].IsNVIDIA {
			return &r.GPUs[i]
		}
	}
	return nil
}

// nvidiaGPUIndexes lists the nvidia-smi indexes of every NVIDIA GPU.
func nvidiaGPUIndexes(r *types.Report) []int {
	var out []int
	for _, g := range r.GPUs {
		if g.IsNVIDIA {
			out = append(out, g.Index)
		}
	}
	return out
}

// computeCap returns the platform or first-GPU compute capability.
func computeCap(r *types.Report) string {
	if r.Platform.ComputeCap != "" {
		return r.Platform.ComputeCap
	}
	for _, g := range r.GPUs {
		if g.IsNVIDIA && g.ComputeCap != "" {
			return g.ComputeCap
		}
	}
	return ""
}

// logText joins every free-text log source the report carries so string
// rules can scan them: dmesg / journal snippets (--include-logs), the raw
// nvidia-smi table, collector error texts and the framework probe output.
func logText(r *types.Report) string {
	var parts []string
	if r.Linux != nil {
		parts = append(parts, r.Linux.DmesgSnippets, r.Linux.JournalSnippets)
		// GSP / SEC2 lines kept by linux.CollectNVRMMessages without
		// --include-logs (LinuxInfo.GSPFailureLines, spec 3.2).
		parts = append(parts, r.Linux.GSPFailureLines...)
	}
	parts = append(parts, r.Driver.NvidiaSmiOutput)
	for _, e := range r.CollectorErrors {
		parts = append(parts, e.Error)
	}
	if r.AI != nil && r.AI.PyTorchInfo != nil {
		parts = append(parts, r.AI.PyTorchInfo.Error)
		parts = append(parts, r.AI.PyTorchInfo.Warnings...)
	}
	if r.Ecosystem != nil {
		parts = append(parts, r.Ecosystem.TorchWarnings...)
	}
	return strings.Join(parts, "\n")
}

// firstLineContaining returns the first line of text containing any needle
// (case-sensitive, trimmed), or "".
func firstLineContaining(text string, needles ...string) string {
	for _, l := range strings.Split(text, "\n") {
		for _, n := range needles {
			if n != "" && strings.Contains(l, n) {
				return strings.TrimSpace(l)
			}
		}
	}
	return ""
}

func containsAny(text string, needles ...string) bool {
	return firstLineContaining(text, needles...) != ""
}

// versionInts parses the leading dotted numeric part of a version string
// ("580.159.03-0ubuntu0.24.04.1" -> [580 159 3]); nil when it has none.
func versionInts(s string) []int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.') {
		end++
	}
	if end == 0 {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s[:end], ".") {
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// versionLess reports whether a < b component-wise; missing components are 0.
// Unparseable input yields false so a rule never fires on garbage.
func versionLess(a, b string) bool {
	av, bv := versionInts(a), versionInts(b)
	if av == nil || bv == nil {
		return false
	}
	for i := 0; i < len(av) || i < len(bv); i++ {
		x, y := 0, 0
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// versionMajor returns the leading integer of a version, 0 when absent.
func versionMajor(s string) int {
	v := versionInts(s)
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

// fmtGiB renders a kB count as "119.7".
func fmtGiB(kb int64) string {
	return fmt.Sprintf("%.1f", float64(kb)/kbPerGiB)
}

func orNA(s string) string {
	if strings.TrimSpace(s) == "" {
		return "n/a"
	}
	return s
}

// ── dispatch ─────────────────────────────────────────────────────────

// analyzePlatform runs the platform, unified-memory and DGX OS rules. They
// run in every mode (spec 5 "Modes").
func analyzePlatform(report *types.Report) []types.Finding {
	var findings []types.Finding
	findings = append(findings, analyzePlatformDetected(report)...)
	findings = append(findings, analyzeUnifiedMemory(report)...)
	findings = append(findings, analyzeDGXOS(report)...)
	findings = append(findings, analyzeGB10Power(report)...)
	return findings
}

// analyzeEcosystem runs the sm121-* / arm64-* / docker-* / onnxruntime-* /
// gb10-k8s-* rules (ai, creator and full modes, spec 5 "Modes").
func analyzeEcosystem(report *types.Report) []types.Finding {
	var findings []types.Finding
	findings = append(findings, analyzeCUDA12Wheel(report)...)
	findings = append(findings, analyzeSM121(report)...)
	findings = append(findings, analyzeArm64Containers(report)...)
	findings = append(findings, analyzeDockerGPU(report)...)
	findings = append(findings, analyzeONNXRuntime(report)...)
	findings = append(findings, analyzeK8sDevicePlugin(report)...)
	return findings
}

// ── platform detected ────────────────────────────────────────────────

// platformLabel is the human name of a platform class for evidence and the
// summary block ("Platform: DGX Spark").
func platformLabel(class string) string {
	switch class {
	case classDGXSpark:
		return "DGX Spark"
	case classRTXSpark:
		return "RTX Spark"
	case classJetson:
		return "Jetson"
	case classGraceHopper:
		return "Grace Hopper"
	case classArm64DGPU:
		return "arm64 + discrete GPU"
	}
	return class
}

// feOrOEM labels a GB10 unit (spec 3.1 row 10).
func feOrOEM(r *types.Report) string {
	if strings.EqualFold(strings.TrimSpace(r.Platform.Vendor), feVendor) {
		return "Founders Edition"
	}
	if strings.TrimSpace(r.Platform.Vendor) == "" {
		return "vendor unknown"
	}
	return "OEM"
}

func analyzePlatformDetected(r *types.Report) []types.Finding {
	var findings []types.Finding
	gpu := firstNVIDIAGPU(r)
	gpuName := "GPU not enumerated"
	if gpu != nil {
		gpuName = gpu.Name
	}
	switch r.Platform.Class {
	case classDGXSpark:
		// Rule row dgx-spark-detected (spec 5). Evidence template "DGX OS
		// {swbuild} / OTA {ota}": swbuild is DGX_SWBUILD_VERSION (the factory
		// image), ota is DGX_OTA_VERSION with the nvidia-spark-ota-check name
		// in parentheses; the summary line and the DGX OS block use the same
		// reading.
		swbuild, ota := "n/a", "n/a"
		if r.DGXOS != nil {
			swbuild = orNA(r.DGXOS.SWBuildVersion)
			ota = dgxOTALabel(r.DGXOS)
		}
		f := sparkFinding("dgx-spark-detected", fmt.Sprintf("GB10 platform: %s %s (%s); GPU %s CC %s; kernel %s; DGX OS %s / OTA %s.",
			orNA(r.Platform.Vendor), orNA(r.Platform.Model), feOrOEM(r), gpuName, orNA(computeCap(r)), orNA(r.System.KernelVersion), swbuild, ota))
		f.GPUIndexes = nvidiaGPUIndexes(r)
		findings = append(findings, f)
	case classRTXSpark:
		if isWindowsReport(r) {
			// Rule row rtx-spark-detected (spec 5): WoA + PNP DEV_2E03/2E06,
			// from the nvidia-smi inventory or the WMI adapter row
			// (PlatformInfo.WoA) when nvidia-smi.exe is absent.
			cores, devid := rtxSparkAdapterFacts(r)
			f := sparkFinding("rtx-spark-detected", fmt.Sprintf("RTX Spark N1X (%s-core, DEV_%s), Windows build %s ARM64, %s, pool %.1f GiB (Win32_OperatingSystem.TotalVisibleMemorySize).",
				cores, devid, orNA(r.System.OSBuild), orNA(strings.TrimSpace(r.Platform.Vendor+" "+r.Platform.Model)), float64(r.System.RAMTotalMB)/1024))
			f.GPUIndexes = nvidiaGPUIndexes(r)
			findings = append(findings, f)
		}
	case classGraceHopper:
		// Rule row grace-hopper-detected (spec 5).
		vram := int64(0)
		if gpu != nil {
			vram = gpu.VRAMTotalMB
		}
		f := sparkFinding("grace-hopper-detected", fmt.Sprintf("%s, %d MiB HBM, CC %s, NUMA nodes not collected.", gpuName, vram, orNA(computeCap(r))))
		f.GPUIndexes = nvidiaGPUIndexes(r)
		findings = append(findings, f)
	}
	return findings
}

// dgxOTALabel renders DGX_OTA_VERSION with the OTA name in parentheses
// ("7.5.0 (OTA2607)"), the name alone when the version is unknown.
func dgxOTALabel(d *types.DGXOSInfo) string {
	switch {
	case d.OTAVersion != "" && d.OTAName != "":
		return d.OTAVersion + " (" + d.OTAName + ")"
	case d.OTAVersion != "":
		return d.OTAVersion
	}
	return orNA(d.OTAName)
}

// ── unified memory ───────────────────────────────────────────────────

func analyzeUnifiedMemory(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !isUnifiedMem(r) {
		return findings
	}
	um := r.UnifiedMemory

	// Rule row unified-memory-nvsmi-expected (spec 5): memory [N/A] / Not
	// Supported on a unified-memory GPU is by design and suppresses the
	// VRAM / fan / limit / PCIe rules (5.1).
	// platforms: dgx-spark, rtx-spark; the class-less row-9 fallback of spec
	// 3.1 (CC 12.1 + [N/A], unknown PCI id) is included because flag rule B
	// still applies the suppressions there and the user deserves the reason.
	switch r.Platform.Class {
	case classDGXSpark, classRTXSpark, "":
	default:
		return findings
	}
	for _, g := range r.GPUs {
		if !g.IsNVIDIA {
			continue
		}
		if g.MemoryReporting != memoryReportingNotSupported && !(g.VRAMTotalMB == 0 && (isDGXSpark(r) || isRTXSpark(r))) {
			continue
		}
		value := "[N/A]"
		if g.MemoryReporting == "" {
			value = "not reported"
		}
		// The pool source is /proc/meminfo on Linux (spec 3.3) and
		// Win32_OperatingSystem on Windows on Arm (spec 8).
		source := "/proc/meminfo"
		if isWindowsReport(r) {
			source = "Win32_OperatingSystem TotalVisibleMemorySize"
		}
		pool := "Pool: " + source + " not collected."
		if um != nil {
			pool = fmt.Sprintf("Pool: MemTotal %s GiB measured from %s", fmtGiB(um.MemTotalKB), source)
			if isDGXSpark(r) {
				// Spec 2.1: 128 GiB LPDDR5X, ~8.3 GiB reserved on 2025 units.
				pool += "; the remainder of the 128 GiB LPDDR5X (marketed as 128 GB) is reserved for display/firmware (~8.3 GiB on 2025 units; the 2 GB / 4 GB display reserve is a BIOS toggle since July 2026, S4)"
			}
			pool += fmt.Sprintf(". MemAvailable %s GiB, swap %s GiB.", fmtGiB(um.MemAvailableKB), fmtGiB(um.SwapTotalKB-um.SwapFreeKB))
		}
		f := sparkFinding("unified-memory-nvsmi-expected", fmt.Sprintf("nvidia-smi memory '%s' on %s is expected on unified-memory iGPUs. %s Fan, power limit, memory clock and PCIe gen/width are also [N/A] or misreported ('GEN 1@ 1x', S7).",
			value, gpuLabel(r, g.Index), pool))
		f.GPUIndexes = []int{g.Index}
		findings = append(findings, f)
		break // one finding per report; the pool is shared
	}

	if um == nil {
		return findings
	}

	// Rule row unified-memory-pressure (spec 5).
	lowAvail := um.GPUProcesses > 0 && um.MemAvailableKB > 0 && um.MemAvailableKB < umPressureWarnKB
	if lowAvail || um.PSIFullAvg10 > umPSIFullWarn {
		f := sparkFinding("unified-memory-pressure", fmt.Sprintf("MemAvailable %s of %s GiB; PSI full avg10 %.2f; GPU processes %d. (A vLLM/SGLang/TRT-LLM server pre-allocates its fraction of the pool at start-up: low MemAvailable with PSI ~0 is expected while it serves.)",
			fmtGiB(um.MemAvailableKB), fmtGiB(um.MemTotalKB), um.PSIFullAvg10, um.GPUProcesses))
		if (lowAvail && um.MemAvailableKB < umPressureCritKB) || um.PSIFullAvg10 > umPSIFullCrit {
			f.Severity = types.SeverityCrit
		}
		findings = append(findings, f)
	}

	if !isDGXSpark(r) {
		return findings
	}

	// Rule row unified-memory-swap-in-use (spec 5): INFO while a GPU process
	// holds memory, WARN only when MemAvailable < 8 GiB or PSI full > 0.1.
	// UnifiedMemoryInfo.PswpinDelta is /proc/vmstat pswpin sampled at the
	// start and the end of the collector: a positive delta is swap-in
	// activity and fires on its own; zero proves nothing (the window is
	// milliseconds), so the swap-used clauses stay the primary trigger.
	swapUsed := um.SwapTotalKB - um.SwapFreeKB
	swapSized := swapUsed >= umSwapUsedKB || (um.MemTotalKB > 0 && swapUsed*1000 > um.MemTotalKB*umSwapUsedPctX10)
	if um.GPUProcesses > 0 && ((swapUsed > 0 && swapSized) || um.PswpinDelta > 0) {
		f := sparkFinding("unified-memory-swap-in-use", fmt.Sprintf("Swap in use: %s of %s GiB (%s); MemAvailable %s GiB; vm.swappiness=%d; pswpin delta %d pages during the run. Single user measurement on GB10 (S36): 6.7 GiB swapped cost 43%% tok/s, 13 GiB cost 95%%.",
			fmtGiB(swapUsed), fmtGiB(um.SwapTotalKB), orNA(strings.Join(um.SwapDevices, ", ")), fmtGiB(um.MemAvailableKB), um.Swappiness, um.PswpinDelta))
		if (um.MemAvailableKB > 0 && um.MemAvailableKB < umPressureWarnKB) || um.PSIFullAvg10 > umPSIFullWarn {
			f.Severity = types.SeverityWarn
		}
		findings = append(findings, f)
	}

	// Rule row unified-memory-page-cache-hold (spec 5).
	if um.MemFreeKB > 0 && um.MemFreeKB < umPageCacheFreeKB && um.MemAvailableKB-um.MemFreeKB > umPageCacheHoldKB {
		findings = append(findings, sparkFinding("unified-memory-page-cache-hold", fmt.Sprintf("MemFree %s GiB but MemAvailable %s GiB: %s GiB of reclaimable page cache (expected after loading a model with mmap; the kernel reclaims it on demand).",
			fmtGiB(um.MemFreeKB), fmtGiB(um.MemAvailableKB), fmtGiB(um.MemAvailableKB-um.MemFreeKB))))
	}

	// Rule row unified-memory-oom-events (spec 5): WARN on OOM-killer
	// events, CRIT on NVRM NV_ERR_NO_MEMORY.
	if um.OOMKills > 0 || um.NVRMNoMemory > 0 {
		f := sparkFinding("unified-memory-oom-events", fmt.Sprintf("%d OOM-killer events (process names not recorded), %d NVRM NV_ERR_NO_MEMORY events; MemAvailable %s GiB, swap used %s GiB.",
			um.OOMKills, um.NVRMNoMemory, fmtGiB(um.MemAvailableKB), fmtGiB(swapUsed)))
		if um.NVRMNoMemory > 0 {
			f.Severity = types.SeverityCrit
		}
		findings = append(findings, f)
	}
	return findings
}

// ── GSP failure and DGX OS ───────────────────────────────────────────

// noDevicesFound reports whether nvidia-smi answered "No devices were found"
// (spec 3.2), either in its captured table or in a collector error.
func noDevicesFound(r *types.Report) bool {
	if strings.Contains(r.Driver.NvidiaSmiOutput, noDevicesNeedle) {
		return true
	}
	for _, e := range r.CollectorErrors {
		if strings.Contains(e.Error, noDevicesNeedle) {
			return true
		}
	}
	return false
}

// hasXid reports whether the Linux Xid list contains code.
func hasXid(r *types.Report, code int) bool {
	if r.Linux == nil {
		return false
	}
	for _, x := range r.Linux.XidErrors {
		if x.Code == code {
			return true
		}
	}
	return false
}

// gspInitFailure is the trigger of dgx-spark-gsp-init-failure (spec 5): GB10
// silicon is present (lspci 10de:2e12 / class dgx-spark) but nvidia-smi
// reports "No devices were found". The GSP / SEC2 dmesg lines and Xid 119
// raise the confidence when captured; they are not required because dmesg
// is only read with --include-logs and the failure signature is otherwise
// identical (S12). Spec 5.1: no-nvidia-gpu and driver-not-detected are not
// emitted when this fires.
func gspInitFailure(r *types.Report) bool {
	return gb10Hardware(r) && noDevicesFound(r)
}

// cx7RegressionKernel reports whether the running kernel is one of the two
// 6.17 builds with the ConnectX-7 hotplug regression (spec 5, cx7-not-enumerated).
func cx7RegressionKernel(kernel string) bool {
	k := strings.TrimSpace(kernel)
	return k == "6.17.0-1021-nvidia" || k == "6.17.0-1029-nvidia"
}

func analyzeDGXOS(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !gb10Hardware(r) {
		return findings
	}
	logs := logText(r)
	dgx := r.DGXOS

	// Rule row dgx-spark-gsp-init-failure (spec 5), CRIT.
	if gspInitFailure(r) {
		line := firstLineContaining(logs, gspFailureNeedles...)
		conf := 95
		switch {
		case line != "":
		case hasXid(r, 119) || hasXid(r, 120):
			line = "Xid 119/120 recorded (GSP RPC timeout / GSP task exception)"
		default:
			line = "no GSP/SEC2 kernel line captured (re-run with --include-logs)"
			conf = 80
		}
		bdf := "000f:01:00.0" // GB10 BDF captured in S23/S12 (spec 3.1 row 5)
		if g := firstNVIDIAGPU(r); g != nil && g.PCIBusID != "" {
			bdf = g.PCIBusID
		}
		drv, fw, ota := "n/a", "n/a", "n/a"
		if dgx != nil {
			drv, fw, ota = orNA(dgx.DriverPkgVersion), orNA(dgx.FirmwarePkgVersion), orNA(dgx.OTAName)
		}
		f := sparkFinding("dgx-spark-gsp-init-failure", fmt.Sprintf("GPU at %s but nvidia-smi 'No devices were found'; dmesg '%s'; nvidia-driver-580-open %s, nvidia-firmware-580 %s, OTA %s, kernel %s.",
			bdf, line, drv, fw, ota, orNA(r.System.KernelVersion)))
		f.Confidence = conf
		findings = append(findings, f)
	}

	// Rule row dgx-spark-driver-too-old (spec 5), CRIT: driver < 580 or CUDA 12.x.
	if major := versionMajor(r.Driver.Version); major > 0 && (major < gb10MinDriverMajor || versionMajor(r.Driver.CUDAVersion) == 12) {
		findings = append(findings, sparkFinding("dgx-spark-driver-too-old", fmt.Sprintf("Driver %s / CUDA %s; GB10 requires R580 open modules and CUDA 13 (signature on the wrong driver: ~5 W and 0%% utilization under load).",
			r.Driver.Version, orNA(r.Driver.CUDAVersion))))
	} else if unsupportedDriverMajors[major] {
		// Rule row dgx-spark-driver-branch-unsupported (spec 5), WARN.
		findings = append(findings, sparkFinding("dgx-spark-driver-branch-unsupported", fmt.Sprintf("Driver %s: NVIDIA staff (aniculescu, 2026-06-08, S40) wrote 'Driver 595 is not yet supported on DGX Spark'; 590.48.01 (CUDA 13.1, via noble-proposed) left ~93 GB of unified memory resident after a vLLM exit in one report (S125; S11 also notes the leak).",
			r.Driver.Version)))
	}

	// Rule row dgx-spark-foreign-driver-packages (spec 5), WARN.
	if r.Linux != nil {
		var foreign []string
		for _, pkg := range r.Linux.NVIDIAPackages {
			name := strings.Fields(pkg)
			if len(name) == 0 {
				continue
			}
			for _, re := range foreignPackageRes {
				if re.MatchString(name[0]) {
					foreign = append(foreign, name[0])
					break
				}
			}
		}
		smiErr := firstLineContaining(logs, gb10NotSupportedNeedle)
		if len(foreign) > 0 || smiErr != "" {
			findings = append(findings, sparkFinding("dgx-spark-foreign-driver-packages", fmt.Sprintf("Installed %s; expected nvidia-driver-580-open + nvidia-firmware-580-<ver> + linux-modules-nvidia-580-open-<kernel> (S11, S30). nvidia-smi '%s'.",
				orNA(strings.Join(foreign, ", ")), orNA(smiErr))))
		}
	}

	// Rule row dgx-spark-cublas-batch-bug (spec 5), WARN: log strings only.
	if line := firstLineContaining(logs, cublasBatchNeedles...); line != "" {
		findings = append(findings, sparkFinding("dgx-spark-cublas-batch-bug", fmt.Sprintf("Log '%s' on driver %s; reported fixed in %s (forum report S41, not yet paired by an OTA).",
			line, orNA(r.Driver.Version), cublasFixedDriver)))
	}

	// Rule row dgx-spark-non-nvidia-kernel (spec 5), WARN. The collector's
	// flag and the spec 3.1 row 11 regex must both say "not -nvidia".
	if k := strings.TrimSpace(r.System.KernelVersion); k != "" && !r.Platform.NvidiaKernelFlavour && !nvidiaKernelRe.MatchString(k) {
		findings = append(findings, sparkFinding("dgx-spark-non-nvidia-kernel", fmt.Sprintf("Kernel %s; DGX Spark expects Canonical's linux-nvidia flavour (6.14.0-NNNN-nvidia / 6.17.0-NNNN-nvidia).", k)))
	}

	// Rule row dgx-spark-cx7-slot-power-benign (spec 5), INFO (--include-logs).
	if line := firstLineContaining(logs, cx7SlotPowerNeedle); line != "" {
		findings = append(findings, sparkFinding("dgx-spark-cx7-slot-power-benign", fmt.Sprintf("dmesg '%s': benign per NVIDIA moderators (no slot power limit advertised).", line)))
	}

	// Rule row dgx-spark-suspend-failure (spec 5), WARN.
	suspendLine := firstLineContaining(logs, suspendWarningNeedle)
	policy := strings.TrimSpace(r.Platform.GDMSleepPolicy)
	headless := policy != "" && policy != "nothing"
	if r.Platform.SuspendFailed || (r.Platform.SuspendAttempts > 0 && suspendLine != "") || headless {
		what := "nvidia-suspend.service did not fail"
		switch {
		case suspendLine != "":
			what = "logged nv_set_system_power_state warning"
		case r.Platform.SuspendFailed:
			what = "nvidia-suspend.service failed"
		}
		findings = append(findings, sparkFinding("dgx-spark-suspend-failure", fmt.Sprintf("%d suspend attempts; driver %s %s; GDM greeter policy %s.",
			r.Platform.SuspendAttempts, orNA(r.Driver.Version), what, orNA(policy))))
	}

	if dgx == nil {
		return findings
	}

	// Rule row dgx-spark-ota-torn (spec 5), WARN.
	torn := 0
	if dgx.OTATorn != nil {
		torn = *dgx.OTATorn
	}
	drvBase := strings.Join(intsToStrings(versionInts(dgx.DriverPkgVersion)), ".")
	fwBase := strings.Join(intsToStrings(versionInts(dgx.FirmwarePkgVersion)), ".")
	pkgMismatch := drvBase != "" && fwBase != "" && drvBase != fwBase
	// ModulesForKernel is a measurement only when the DGX OS collector ran
	// its probes (DGXOSInfo.UnitsQueried, integration contract): a DGXOSInfo
	// filled from /etc/dgx-release alone carries zero-valued booleans.
	modulesMissing := dgx.UnitsQueried && dgx.DriverPkgVersion != "" && !dgx.ModulesForKernel
	if torn > 0 || len(dgx.OTAFailed) > 0 || pkgMismatch || modulesMissing {
		state := "installed"
		if modulesMissing {
			state = "missing"
		}
		findings = append(findings, sparkFinding("dgx-spark-ota-torn", fmt.Sprintf("OTA %s torn=%d, failed %s; driver pkg %s vs firmware pkg %s; modules for %s: %s.",
			orNA(dgx.OTAName), torn, orNA(strings.Join(dgx.OTAFailed, ", ")), orNA(dgx.DriverPkgVersion), orNA(dgx.FirmwarePkgVersion), orNA(r.System.KernelVersion), state)))
	}

	// Rule row dgx-spark-ota-outdated (spec 5), WARN. The kernel clause is
	// suppressed when the cx7-not-enumerated 6.17.0-1021/1029 regression
	// variant fires on this host (its stop-gap is an older kernel).
	otaOld := dgx.OTAVersion != "" && versionLess(dgx.OTAVersion, otaCurrentVersion)
	kernelOld := r.System.KernelVersion != "" && versionLess(r.System.KernelVersion, otaCurrentKernel) && !cx7RegressionKernel(r.System.KernelVersion)
	driverOld := r.Driver.Version != "" && versionLess(r.Driver.Version, otaCurrentDriver)
	if otaOld || kernelOld || driverOld {
		findings = append(findings, sparkFinding("dgx-spark-ota-outdated", fmt.Sprintf("OTA %s (%s), kernel %s, driver %s; current FE stack %s.",
			orNA(dgx.OTAVersion), orNA(dgx.OTADate), orNA(r.System.KernelVersion), orNA(r.Driver.Version), otaCurrentDisplay)))
	}

	// Rule row dgx-spark-dashboard-unhealthy (spec 5), WARN. Evaluated only
	// when systemctl answered (DGXOSInfo.UnitsQueried): otherwise the
	// *Active booleans are unknown rather than inactive and the rule stays
	// silent instead of reporting a healthy unit as unhealthy.
	if dgx.UnitsQueried && (!dgx.DashboardActive || !dgx.DashboardAdminActive || !dgx.DashboardPortOpen || !dgx.FwupdActive || dgx.FwupdError != "") {
		state := func(b bool) string {
			if b {
				return "active"
			}
			return "inactive"
		}
		port := "closed"
		if dgx.DashboardPortOpen {
			port = "open"
		}
		fw := state(dgx.FwupdActive)
		if dgx.FwupdError != "" {
			fw = "failed"
		}
		findings = append(findings, sparkFinding("dgx-spark-dashboard-unhealthy", fmt.Sprintf("dgx-dashboard %s, dgx-dashboard-admin %s, port %d %s, fwupd %s ('%s'), lingering check_ota_status.py not counted.",
			state(dgx.DashboardActive), state(dgx.DashboardAdminActive), dashboardPort, port, fw, dgx.FwupdError)))
	}

	findings = append(findings, analyzeFirmware(r)...)
	return findings
}

func intsToStrings(v []int) []string {
	out := make([]string, 0, len(v))
	for _, n := range v {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// ── firmware ─────────────────────────────────────────────────────────

// fwVersionTriplet parses a firmware version in dotted ("3.5.8") or hex
// ("0x03000508") form into major.minor.patch. ASSUMPTION (spec 5,
// dgx-spark-firmware-behind): the hex form decodes as major (8 bits), minor
// (16 bits), patch (8 bits), so 0x03000508 = 3.5.8, 0x02009b0b = 2.155.11 and
// 0x00000516 = 0.5.22; arithmetic only, unconfirmed against fwupdmgr output.
func fwVersionTriplet(v string) ([3]int, bool) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "0x") {
		n, err := strconv.ParseUint(v[2:], 16, 32)
		if err != nil {
			return [3]int{}, false
		}
		return [3]int{int(n >> 24 & 0xff), int(n >> 8 & 0xffff), int(n & 0xff)}, true
	}
	parts := versionInts(v)
	if len(parts) == 0 {
		return [3]int{}, false
	}
	var t [3]int
	copy(t[:], parts)
	return t, true
}

// fwTripletLess compares two triplets.
func fwTripletLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// fwTripletString renders a triplet as "3.5.8".
func fwTripletString(t [3]int) string {
	return fmt.Sprintf("%d.%d.%d", t[0], t[1], t[2])
}

// fwClass maps a fwupdmgr device name onto "ec", "soc", "pd", "cx7" or "".
func fwClass(name string) string {
	n := " " + strings.ToLower(strings.TrimSpace(name)) + " "
	for _, k := range fwNameCX7 {
		if strings.Contains(n, k) {
			return "cx7"
		}
	}
	for _, k := range fwNamePD {
		if strings.Contains(n, k) {
			return "pd"
		}
	}
	for _, k := range fwNameEC {
		if strings.Contains(n, k) {
			return "ec"
		}
	}
	for _, k := range fwNameSoC {
		if strings.Contains(n, k) {
			return "soc"
		}
	}
	return ""
}

// analyzeFirmware implements dgx-spark-firmware-behind (spec 5): FE units
// are compared against the July 2026 thresholds, OEM units only report
// pending capsules.
func analyzeFirmware(r *types.Report) []types.Finding {
	var findings []types.Finding
	if len(r.Platform.Firmware) == 0 {
		return findings
	}
	isFE := strings.EqualFold(strings.TrimSpace(r.Platform.Vendor), feVendor)
	seen := map[string]string{}
	var behind, pending []string
	for _, c := range r.Platform.Firmware {
		if c.Pending != "" {
			pending = append(pending, fmt.Sprintf("%s -> %s", c.Name, c.Pending))
		}
		class := fwClass(c.Name)
		if (class != "ec" && class != "soc" && class != "pd") || !isFE {
			continue // only the three FE-thresholded components are compared
		}
		cur, ok := fwVersionTriplet(c.Version)
		if !ok {
			continue
		}
		var minS string
		switch class {
		case "ec":
			minS = fwFEECMin
		case "soc":
			minS = fwFESoCMin
		case "pd":
			minS = fwFEPDMin
		}
		min, _ := fwVersionTriplet(minS)
		seen[class] = fwTripletString(cur)
		if fwTripletLess(cur, min) {
			behind = append(behind, fmt.Sprintf("%s %s < %s", class, fwTripletString(cur), minS))
		}
	}
	if len(behind) == 0 && (isFE || len(pending) == 0) {
		return findings
	}
	vendor := "OEM: " + orNA(r.Platform.Vendor)
	if isFE {
		vendor = "Founders Edition"
	}
	f := sparkFinding("dgx-spark-firmware-behind", fmt.Sprintf("EC %s, UEFI/SoC %s, USB-PD %s, SBIOS %s (%s); FE reference %s / %s / %s (July 2026, S4); pending %s; behind: %s.",
		orNA(seen["ec"]), orNA(seen["soc"]), orNA(seen["pd"]), orNA(r.Platform.BIOSVersion), vendor,
		fwFEECMin, fwFESoCMin, fwFEPDMin, orNA(strings.Join(pending, ", ")), orNA(strings.Join(behind, ", "))))
	findings = append(findings, f)
	return findings
}

// ── GB10 power, boot and thermal rules ───────────────────────────────

// firmwareVersionOf returns the current version of the first firmware
// component of the given class ("ec"), or "n/a".
func firmwareVersionOf(r *types.Report, class string) string {
	for _, c := range r.Platform.Firmware {
		if fwClass(c.Name) == class {
			if t, ok := fwVersionTriplet(c.Version); ok {
				return fwTripletString(t)
			}
			return c.Version
		}
	}
	return "n/a"
}

// wedgeSample reports whether one thermal sample carries the PD wedge
// signature (spec 5, gb10-pd-power-wedge): busy, slow, cold and no active
// clock event reason.
func wedgeSample(t *types.ThermalInfo) bool {
	power, err := strconv.ParseFloat(strings.TrimSpace(t.PowerDrawW), 64)
	if err != nil {
		return false
	}
	if t.UtilizationPct < wedgeUtilMin || t.CurrentClockMHz <= 0 || t.CurrentClockMHz >= wedgeClockMaxMHz || power >= wedgePowerMaxW {
		return false
	}
	if t.SlowdownActive {
		return false
	}
	for _, reason := range t.ThrottleReasons {
		switch strings.ToLower(strings.TrimSpace(reason)) {
		case "", "gpu_idle", "applications_clocks_setting", "display_clock_setting", "sync_boost":
		default:
			return false
		}
	}
	return true
}

// eventCounter returns the microsecond counter whose key mentions every
// token (case-insensitive), or 0.
func eventCounter(counters map[string]int64, tokens ...string) int64 {
	for k, v := range counters {
		lk := strings.ToLower(k)
		all := true
		for _, t := range tokens {
			if !strings.Contains(lk, t) {
				all = false
				break
			}
		}
		if all {
			return v
		}
	}
	return 0
}

func analyzeGB10Power(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !isDGXSpark(r) {
		return findings
	}
	samples := thermalEntries(r)

	// Rule row gb10-pd-power-wedge (spec 5): CRIT when every sample of the
	// run carries the signature (>= 2 samples), WARN on a single match.
	if len(samples) > 0 {
		byGPU := map[int][]*types.ThermalInfo{}
		var order []int
		for _, t := range samples {
			if _, ok := byGPU[t.GPUIndex]; !ok {
				order = append(order, t.GPUIndex)
			}
			byGPU[t.GPUIndex] = append(byGPU[t.GPUIndex], t)
		}
		for _, idx := range order {
			group := byGPU[idx]
			match := 0
			var last *types.ThermalInfo
			for _, t := range group {
				if wedgeSample(t) {
					match++
					last = t
				}
			}
			if match == 0 {
				continue
			}
			f := sparkFinding("gb10-pd-power-wedge", fmt.Sprintf("Under load: %d MHz (max %d), %d%%, %s W, %d C, %s; reasons Not Active in %d/%d samples; SW Power Capping counter %d us. Community-observed healthy range ~2200-2600 MHz at 80-100 W (S47, S108).",
				last.CurrentClockMHz, gb10MaxSMClockMHz, last.UtilizationPct, last.PowerDrawW, last.TemperatureC, orNA(last.PowerState), match, len(group), eventCounter(last.EventCounters, "power", "cap")))
			if match < len(group) || len(group) < 2 {
				f.Severity = types.SeverityWarn
			}
			f.GPUIndexes = []int{idx}
			findings = append(findings, f)
		}
	}

	// Rule row gb10-logless-hard-poweroff (spec 5): the previous boot ended
	// without a clean-shutdown marker and pstore is empty. WARN needs >= 2
	// such boots in the window (PlatformInfo.UncleanBoots); one is INFO.
	if r.Platform.PrevBootClean != nil && !*r.Platform.PrevBootClean && !cleanShutdownLine(r.Platform.PrevBootLastLine) && !crashLine(r.Platform.PrevBootLastLine) {
		pstoreEmpty := r.Platform.PstoreEmpty == nil || *r.Platform.PstoreEmpty
		if pstoreEmpty {
			n := r.Platform.UncleanBoots
			if n < 1 {
				n = 1
			}
			maxClock := "n/a"
			if len(samples) > 0 && samples[0].MaxClockMHz > 0 {
				maxClock = fmt.Sprintf("%d MHz", samples[0].MaxClockMHz)
			}
			f := sparkFinding("gb10-logless-hard-poweroff", fmt.Sprintf("%d boots without a clean-shutdown marker in %d days; last line of boot -1 '%s'; pstore empty; max SM clock %s; EC %s, SBIOS %s. Alternative explanations: mains loss, power-button hold.",
				n, uncleanBootWindow, orNA(r.Platform.PrevBootLastLine), maxClock, firmwareVersionOf(r, "ec"), orNA(r.Platform.BIOSVersion)))
			if n < uncleanBootsToWarn {
				f.Severity = types.SeverityInfo
			}
			findings = append(findings, f)
		}
	}

	// Rule row gb10-acpi-thermal-zone-hot (spec 5): any acpitz zone >= 93000
	// mC (inference from S49/S106/S109/S110) or thermal slowdown counters > 0.
	var hot []string
	maxMC := 0
	zones := make([]string, 0, len(r.Platform.ACPIThermalMC))
	for z := range r.Platform.ACPIThermalMC {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	for _, z := range zones {
		mc := r.Platform.ACPIThermalMC[z]
		hot = append(hot, fmt.Sprintf("%s=%d", z, mc))
		if mc > maxMC {
			maxMC = mc
		}
	}
	var swUS, hwUS int64
	gpuTemp := 0
	if len(samples) > 0 {
		gpuTemp = samples[0].TemperatureC
		swUS = eventCounter(samples[0].EventCounters, "sw", "thermal")
		hwUS = eventCounter(samples[0].EventCounters, "hw", "thermal")
	}
	if maxMC >= gb10ThermalHotMC || swUS > 0 || hwUS > 0 {
		findings = append(findings, sparkFinding("gb10-acpi-thermal-zone-hot", fmt.Sprintf("ACPI zones %s (max %d mC) vs GPU %d C; slowdown counters SW %d / HW %d us; fan telemetry unavailable (EC); EC %s.",
			orNA(strings.Join(hot, ", ")), maxMC, gpuTemp, swUS, hwUS, firmwareVersionOf(r, "ec"))))
	}

	// Rule row gb10-clock-cap-active (spec 5), INFO.
	source := ""
	minClock, maxClock := 0, 0
	switch {
	case r.Platform.ClockCapUnit != "":
		source = r.Platform.ClockCapUnit
	case len(samples) > 0:
		for _, reason := range samples[0].ThrottleReasons {
			if strings.EqualFold(strings.TrimSpace(reason), "applications_clocks_setting") {
				source = "Applications Clocks Setting: Active"
			}
		}
		if source == "" && samples[0].MaxClockMHz > 0 && samples[0].MaxClockMHz < gb10MaxSMClockMHz {
			source = "locked max SM clock"
		}
	}
	if source != "" {
		if len(samples) > 0 {
			maxClock = samples[0].MaxClockMHz
			minClock = samples[0].CurrentClockMHz
		}
		findings = append(findings, sparkFinding("gb10-clock-cap-active", fmt.Sprintf("Clocks locked to %d-%d MHz (hardware max %d) via %s; community estimates ~1-10%% throughput cost (S48).",
			minClock, maxClock, gb10MaxSMClockMHz, source)))
	}
	return findings
}

// cleanShutdownLine reports whether a journal line is one of the
// clean-shutdown markers of spec 5 (gb10-logless-hard-poweroff).
func cleanShutdownLine(line string) bool {
	for _, m := range []string{"Journal stopped", "systemd-shutdown", "Shutting down.", "Reached target Power-Off", "Reached target Reboot", "Reached target Halt"} {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// crashLine reports whether the previous boot's last line already explains
// the reset (panic / OOM / Xid / thermal), in which case the log-less rule
// stays silent (spec 5).
func crashLine(line string) bool {
	l := strings.ToLower(line)
	for _, m := range []string{"kernel panic", "out of memory", "xid", "thermal", "critical temperature"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// ── ecosystem rules ──────────────────────────────────────────────────

// cc121 reports whether the host GPU is compute capability 12.1 (GB10 / N1X).
func cc121(r *types.Report) bool {
	return computeCap(r) == "12.1" || isDGXSpark(r) || isRTXSpark(r)
}

// torchArchList returns the torch arch list from either home.
func torchArchList(r *types.Report) []string {
	if r.Ecosystem != nil && len(r.Ecosystem.TorchArchList) > 0 {
		return r.Ecosystem.TorchArchList
	}
	if r.AI != nil && r.AI.PyTorchInfo != nil {
		return r.AI.PyTorchInfo.ArchList
	}
	return nil
}

func hasArch(list []string, arch string) bool {
	for _, a := range list {
		if strings.EqualFold(strings.TrimSpace(a), arch) {
			return true
		}
	}
	return false
}

// analyzeCUDA12Wheel implements arm64-cuda12-wheel-on-cuda13 (spec 5).
func analyzeCUDA12Wheel(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !(isDGXSpark(r) || isRTXSpark(r) || r.Platform.Class == classArm64DGPU) {
		return findings
	}
	if versionMajor(r.Driver.CUDAVersion) < 13 || r.AI == nil || r.AI.PyTorchInfo == nil {
		return findings
	}
	pt := r.AI.PyTorchInfo
	importErr := firstLineContaining(logText(r), "libcudart.so.12: cannot open shared object file", "libnvrtc.so.12")
	cu12 := strings.Contains(pt.Version, "+cu12") || versionMajor(pt.CUDAVersion) == 12
	if !cu12 && importErr == "" {
		return findings
	}
	present := "absent"
	if r.Ecosystem != nil {
		for _, lib := range r.Ecosystem.LibcudartVersions {
			if strings.Contains(lib, "12") {
				present = "present"
			}
		}
	}
	pkgCUDA := pt.CUDAVersion
	if pkgCUDA == "" {
		pkgCUDA = "12.x"
	}
	f := sparkFinding("arm64-cuda12-wheel-on-cuda13", fmt.Sprintf("torch %s is built for CUDA %s; host has CUDA %s (libcudart.so.12 %s). Tag-only finding: may lack sm_121 kernels; verify torch.cuda.is_available() and a matmul.",
		orNA(pt.Version), pkgCUDA, r.Driver.CUDAVersion, present))
	if importErr != "" {
		f.Severity = types.SeverityCrit
		f.Evidence = fmt.Sprintf("torch %s is built for CUDA %s; host has CUDA %s (libcudart.so.12 %s). Import error '%s'.", orNA(pt.Version), pkgCUDA, r.Driver.CUDAVersion, present, importErr)
	}
	findings = append(findings, f)
	return findings
}

// analyzeSM121 implements sm121-torch-capability-warning-benign,
// sm121-kernel-missing and sm121-triton-ptxas-stale (spec 5).
func analyzeSM121(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !(isDGXSpark(r) || isRTXSpark(r)) {
		return findings
	}
	logs := logText(r)
	arch := torchArchList(r)
	torchVersion, torchCUDA := "n/a", "n/a"
	if r.AI != nil && r.AI.PyTorchInfo != nil {
		torchVersion, torchCUDA = orNA(r.AI.PyTorchInfo.Version), orNA(r.AI.PyTorchInfo.CUDAVersion)
	}

	// Rule row sm121-torch-capability-warning-benign (spec 3.2 string), INFO.
	capWarn := firstLineContaining(logs, "Minimum and Maximum cuda capability supported by this version of PyTorch is (8.0) - (12.0)")
	if capWarn != "" || (hasArch(arch, "sm_120") && !hasArch(arch, "sm_121")) {
		findings = append(findings, sparkFinding("sm121-torch-capability-warning-benign", fmt.Sprintf("PyTorch %s (CUDA %s) warns about capability 12.1; arch list %s. sm_120 SASS runs on sm_121.",
			torchVersion, torchCUDA, orNA(strings.Join(arch, ",")))))
	}

	// Rule row sm121-kernel-missing (spec 5), WARN.
	if cc121(r) {
		line := firstLineContaining(logs, "no kernel image is available for execution on the device", "kernel built for sm80-sm100, but running on sm121", "cudaErrorSymbolNotFound", "TRTLLMGenFusedMoE does not support SM120 and above")
		archLacks := len(arch) > 0 && !hasArch(arch, "sm_120") && !hasArch(arch, "sm_121") && !hasArch(arch, "compute_120")
		if line != "" || archLacks {
			component := "PyTorch " + torchVersion
			if line == "" {
				line = "torch.cuda.get_arch_list() = " + strings.Join(arch, ",")
			} else {
				component = "a CUDA component"
			}
			findings = append(findings, sparkFinding("sm121-kernel-missing", fmt.Sprintf("%s has no sm_120/sm_120f/sm_121a kernels: '%s'. GB10/N1X are CC 12.1; sm_100 and sm_90 binaries never run here.", component, line)))
		}
	}

	// Rule row sm121-triton-ptxas-stale (spec 5), WARN; dgx-spark only.
	if isDGXSpark(r) && r.Ecosystem != nil {
		eco := r.Ecosystem
		ptxasLine := firstLineContaining(logs, "ptxas fatal : Value 'sm_121a' is not defined for option 'gpu-name'")
		stale := eco.TritonPtxasVersion != "" && versionMajor(eco.TritonPtxasVersion) < 13 && eco.TritonPtxasPath == ""
		if stale || ptxasLine != "" {
			sys := "n/a"
			if r.AI != nil {
				sys = orNA(r.AI.CUDAToolkitVersion)
			}
			findings = append(findings, sparkFinding("sm121-triton-ptxas-stale", fmt.Sprintf("Triton bundles ptxas %s; TRITON_PTXAS_PATH=%s; system ptxas %s. Error '%s'.",
				orNA(eco.TritonPtxasVersion), orNA(eco.TritonPtxasPath), sys, orNA(ptxasLine))))
		}
	}
	return findings
}

// ngcImageTooOld decides the sm121-ngc-image-too-old row for one image ref.
func ngcImageTooOld(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name, tag := ref, ""
	if i := strings.LastIndex(ref, ":"); i > 0 && !strings.Contains(ref[i:], "/") {
		name, tag = ref[:i], ref[i+1:]
	}
	switch {
	case strings.HasSuffix(name, "nvcr.io/nvidia/pytorch") || name == "nvidia/pytorch":
		// pytorch <= 25.09 predates the first tag shipping compute_120 (S118).
		return tag != "" && !versionLess("25.09", tag)
	case strings.HasSuffix(name, "nvcr.io/nvidia/tensorflow") || name == "nvidia/tensorflow":
		return true // NVIDIA-optimized TensorFlow reached EOL after 25.02 (S113)
	case name == "lmsysorg/sglang":
		return tag == "latest" // cu12 build (S94)
	case strings.HasSuffix(name, "nvcr.io/nvidia/tensorrt-llm/release"):
		return tag != "" && versionLess(tag, "1.2")
	}
	return false
}

// analyzeArm64Containers implements arm64-flash-attn-no-wheel,
// arm64-container-amd64-image and sm121-ngc-image-too-old (spec 5).
func analyzeArm64Containers(r *types.Report) []types.Finding {
	var findings []types.Finding
	// platforms: dgx-spark, arm64-dgpu (catalog). Jetson and Grace Hopper are
	// aarch64 too but ship their own wheel ecosystems, so the class decides,
	// not the CPU architecture.
	arm64Host := isDGXSpark(r) || r.Platform.Class == classArm64DGPU
	if !arm64Host {
		return findings
	}
	logs := logText(r)
	eco := r.Ecosystem

	// Rule row arm64-flash-attn-no-wheel (spec 5), WARN.
	faVersion := ""
	if eco != nil {
		faVersion = eco.FlashAttnVersion
	}
	faLine := firstLineContaining(logs, "FlashAttention2 has been toggled on, but flash_attn is not installed")
	if faVersion != "" || faLine != "" {
		findings = append(findings, sparkFinding("arm64-flash-attn-no-wheel", fmt.Sprintf("flash_attn %s on aarch64: no official linux_aarch64+sm121 wheels exist (S55).", orNA(faVersion))))
	}

	// Rule row arm64-container-amd64-image (spec 5), WARN.
	var amd64 []string
	if eco != nil {
		for _, img := range eco.Images {
			if strings.EqualFold(strings.TrimSpace(img.Arch), "amd64") || strings.EqualFold(strings.TrimSpace(img.Arch), "x86_64") {
				amd64 = append(amd64, img.Ref)
			}
		}
	}
	execFormat := firstLineContaining(logs, "exec format error")
	if len(amd64) > 0 || execFormat != "" {
		image := strings.Join(amd64, ", ")
		if image == "" {
			image = "unknown ('" + execFormat + "')"
		}
		findings = append(findings, sparkFinding("arm64-container-amd64-image", fmt.Sprintf("Image %s is linux/amd64; host is linux/arm64.", image)))
	}

	// Rule row sm121-ngc-image-too-old (spec 5), WARN; dgx-spark only.
	if isDGXSpark(r) && eco != nil {
		var old []string
		for _, img := range eco.Images {
			if ngcImageTooOld(img.Ref) {
				old = append(old, img.Ref)
			}
		}
		if len(old) > 0 {
			findings = append(findings, sparkFinding("sm121-ngc-image-too-old", fmt.Sprintf("%s predates CUDA 13 / sm_121 (25.08 was the first CUDA 13 NGC PyTorch and the playbooks pin 25.11-py3, S114; 25.10 is the first tag observed shipping compute_120 for GB10, S118).", strings.Join(old, ", "))))
		}
	}
	return findings
}

// analyzeDockerGPU implements docker-snap-gpu-blocked and
// docker-cdi-spec-missing (spec 5).
func analyzeDockerGPU(r *types.Report) []types.Finding {
	var findings []types.Finding
	eco := r.Ecosystem
	logs := logText(r)

	// Rule row docker-snap-gpu-blocked (spec 5), WARN.
	if isDGXSpark(r) || r.Platform.Class == classArm64DGPU {
		snapLine := firstLineContaining(logs, "nvidia-container-cli: initialization error: load library failed: libnvidia-ml.so.1")
		if (eco != nil && eco.SnapDocker) || snapLine != "" {
			state := "unknown"
			if r.Linux != nil && r.Linux.ContainerRuntime != "" {
				state = r.Linux.ContainerRuntime
			}
			findings = append(findings, sparkFinding("docker-snap-gpu-blocked", fmt.Sprintf("Snap Docker %s installed; docker-ce %s; error '%s'.", boolWord(eco != nil && eco.SnapDocker, "is", "not detected as"), state, orNA(snapLine))))
		}
	}

	// Rule row docker-cdi-spec-missing (spec 5): WARN when CDI is on without
	// nvidia.yaml, INFO when daemon.json lacks runtimes.nvidia; dgx-spark only.
	if isDGXSpark(r) && eco != nil {
		cdiLine := firstLineContaining(logs, "no nvidia.com/gpu CDI spec is present on the host")
		runtimeLine := firstLineContaining(logs, "unknown or invalid runtime name: nvidia")
		hasNvidiaRuntime := false
		for _, rt := range eco.DockerRuntimes {
			if strings.EqualFold(strings.TrimSpace(rt), "nvidia") {
				hasNvidiaRuntime = true
			}
		}
		ctk := "n/a"
		if r.Linux != nil {
			ctk = orNA(r.Linux.NVContainerToolkit)
		}
		evidence := func(line string) string {
			return fmt.Sprintf("Docker CDI %s, nvidia.yaml %s, runtimes %s (toolkit %s); error '%s'.",
				boolWord(eco.DockerCDI, "enabled", "disabled"), boolWord(eco.CDISpecPresent, "present", "missing"), orNA(strings.Join(eco.DockerRuntimes, ", ")), ctk, orNA(line))
		}
		switch {
		case (eco.DockerCDI && !eco.CDISpecPresent) || cdiLine != "":
			findings = append(findings, sparkFinding("docker-cdi-spec-missing", evidence(cdiLine)))
		case (len(eco.DockerRuntimes) > 0 && !hasNvidiaRuntime) || runtimeLine != "":
			f := sparkFinding("docker-cdi-spec-missing", evidence(runtimeLine))
			f.Severity = types.SeverityInfo
			findings = append(findings, f)
		}
	}
	return findings
}

func boolWord(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// analyzeONNXRuntime implements onnxruntime-cuda-provider-missing (spec 5).
func analyzeONNXRuntime(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !(isDGXSpark(r) || r.Platform.Class == classArm64DGPU) || r.Ecosystem == nil {
		return findings
	}
	eco := r.Ecosystem
	if eco.ORTVersion == "" {
		return findings
	}
	hasCUDA := false
	for _, p := range eco.ORTProviders {
		if strings.TrimSpace(p) == "CUDAExecutionProvider" {
			hasCUDA = true
		}
	}
	if hasCUDA && !eco.ORTGPUShadowed {
		return findings
	}
	gpuPkg := "absent"
	if eco.ORTGPUShadowed {
		gpuPkg = "installed (shadowed by the CPU wheel)"
	}
	findings = append(findings, sparkFinding("onnxruntime-cuda-provider-missing", fmt.Sprintf("Providers %s; onnxruntime=%s, onnxruntime-gpu=%s. No prebuilt onnxruntime-gpu wheels for aarch64 Linux existed as of April 2026 (S61); a later pip install of the CPU wheel shadows a GPU build.",
		orNA(strings.Join(eco.ORTProviders, ", ")), eco.ORTVersion, gpuPkg)))
	return findings
}

// analyzeK8sDevicePlugin implements gb10-k8s-device-plugin-old (spec 5). The
// plugin version is not collected; the rule fires on the NVML error the old
// plugin logs on unified memory.
func analyzeK8sDevicePlugin(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !isDGXSpark(r) {
		return findings
	}
	line := firstLineContaining(logText(r), "error getting device memory: Not Supported")
	if line == "" {
		return findings
	}
	findings = append(findings, sparkFinding("gb10-k8s-device-plugin-old", fmt.Sprintf("k8s-device-plugin (version not collected) on GB10: '%s'; nvmlDeviceGetMemoryInfo returns NVML_ERROR_NOT_SUPPORTED on unified memory (S8; HAMi S116).", line)))
	return findings
}
