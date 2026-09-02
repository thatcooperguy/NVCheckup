package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const jetsonRelease = "# R36 (release), REVISION: 4.3, GCID: 38968081, BOARD: generic, EABI: aarch64, DATE: Wed Jan 8 01:49:37 UTC 2025"

// A healthy Jetson has no nvidia-smi, no driver version and no PCIe GPU. That
// must not read as "no GPU, no driver"; it is reported once as the platform.
func TestAnalyze_JetsonSuppressesDesktopFindings(t *testing.T) {
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeAI, types.ModeCreator, types.ModeStreaming, types.ModeFull} {
		// Linux is populated the way a real L4T R32 board (Nano, Xavier NX,
		// TX2) looks: the GPU module is 'nvgpu', not 'nvidia', the driver
		// version is unknown because there is no nvidia-smi, and libcuda lives
		// under the tegra directory the common-path search does not know.
		report := &types.Report{
			System: types.SystemInfo{OSName: "Ubuntu", OSVersion: "22.04", Architecture: "arm64", IsJetson: true, JetsonRelease: jetsonRelease},
			Linux:  &types.LinuxInfo{LoadedModules: map[string]bool{"nvgpu": true}},
		}
		Analyze(report, mode)
		for _, forbidden := range []string{"no-nvidia-gpu", "driver-not-detected", "nvidia-smi-missing", "nvidia-module-not-loaded", "libcuda-not-found"} {
			if f := findByID(report.Findings, forbidden); f != nil {
				t.Errorf("mode %s: %s must be suppressed on Jetson: %+v", mode, forbidden, f)
			}
		}
		f := findByID(report.Findings, "jetson-detected")
		if f == nil {
			t.Fatalf("mode %s: jetson-detected missing: %v", mode, ids(report.Findings))
		}
		if f.Severity != types.SeverityInfo || f.Confidence != 90 || f.Category != "gpu" {
			t.Errorf("mode %s: severity/confidence/category = %s/%d/%s", mode, f.Severity, f.Confidence, f.Category)
		}
		// Spec 5.1: Jetson Thor ships nvidia-smi, so the wording must not
		// claim it is absent on Tegra in general.
		if !strings.Contains(f.Evidence, jetsonRelease) || !strings.Contains(f.Evidence, "Jetson Thor / JetPack 7 ships it") || strings.Contains(f.Evidence, "nvidia-smi is not available on Tegra") {
			t.Errorf("mode %s: evidence should quote the release and explain nvidia-smi without claiming Tegra never has it: %q", mode, f.Evidence)
		}
		steps := strings.Join(f.NextSteps, "\n")
		if !strings.Contains(steps, "sudo tegrastats") || !strings.Contains(steps, "jetson_release -v") {
			t.Errorf("mode %s: next steps should point at tegrastats and jetson_release: %v", mode, f.NextSteps)
		}
	}
}

func TestAnalyzeJetson_UnknownReleaseAndNonJetson(t *testing.T) {
	report := &types.Report{System: types.SystemInfo{IsJetson: true}}
	findings := analyzeJetson(report)
	if len(findings) != 1 || !strings.Contains(findings[0].Evidence, "L4T release unknown") {
		t.Errorf("Jetson without release file: %+v", findings)
	}
	if got := analyzeJetson(&types.Report{}); len(got) != 0 {
		t.Errorf("non-Jetson must not produce jetson-detected: %+v", got)
	}
	// The desktop rules still fire on a non-Jetson with nothing detected,
	// including the Linux module check that is skipped on Jetson.
	desktop := &types.Report{Linux: &types.LinuxInfo{LoadedModules: map[string]bool{"nvgpu": true}}}
	Analyze(desktop, types.ModeFull)
	for _, want := range []string{"no-nvidia-gpu", "driver-not-detected", "nvidia-smi-missing", "nvidia-module-not-loaded", "libcuda-not-found"} {
		if findByID(desktop.Findings, want) == nil {
			t.Errorf("non-Jetson empty report should still raise %s: %v", want, ids(desktop.Findings))
		}
	}
	if findByID(desktop.Findings, "jetson-detected") != nil {
		t.Error("jetson-detected must not fire on a desktop")
	}
}

// threeGPURig is an RTX 3090 plus two RTX 4090s. GPU 0 is idle at Gen1 (fine),
// GPU 1 is at full speed, GPU 2 is busy at Gen1 (a real downshift) and hot.
func threeGPURig() *types.Report {
	return &types.Report{
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "NVIDIA GeForce RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24576},
			{Index: 1, Name: "NVIDIA GeForce RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24564},
			{Index: 2, Name: "NVIDIA GeForce RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24564},
		},
		Driver: types.DriverInfo{Version: "591.86", CUDAVersion: "13.1", NvidiaSmiPath: "nvidia-smi"},
		GPUThermal: []types.ThermalInfo{
			{GPUIndex: 0, TemperatureC: 43, PowerState: "P8", CurrentClockMHz: 210, MaxClockMHz: 2100, FanSupported: true, UtilizationPct: 2},
			{GPUIndex: 1, TemperatureC: 66, PowerState: "P0", CurrentClockMHz: 2730, MaxClockMHz: 2805, FanSupported: true, FanSpeedPct: 70, UtilizationPct: 98},
			{GPUIndex: 2, TemperatureC: 88, PowerState: "P0", CurrentClockMHz: 2700, MaxClockMHz: 2805, FanSupported: true, FanSpeedPct: 85, UtilizationPct: 99},
		},
		GPUPCIe: []types.PCIeInfo{
			{GPUIndex: 0, CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 2, IdleLikely: true},
			{GPUIndex: 1, CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 98},
			{GPUIndex: 2, CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99, Downshifted: true},
		},
	}
}

func TestAnalyze_ThreeGPURig_PerGPUFindings(t *testing.T) {
	report := threeGPURig()
	report.Thermal = &report.GPUThermal[0]
	report.PCIe = &report.GPUPCIe[0]
	Analyze(report, types.ModeFull)

	down := findByID(report.Findings, "pcie-downshift")
	if down == nil {
		t.Fatalf("GPU 2 busy at Gen1 must be flagged: %v", ids(report.Findings))
	}
	if !strings.HasPrefix(down.Evidence, "GPU 2 (NVIDIA GeForce RTX 4090): ") {
		t.Errorf("downshift evidence should name GPU 2: %q", down.Evidence)
	}
	if !reflect.DeepEqual(down.GPUIndexes, []int{2}) {
		t.Errorf("downshift GPUIndexes = %v, want [2]", down.GPUIndexes)
	}
	if strings.Contains(down.Evidence, "GPU 0") {
		t.Errorf("GPU 0 idle at Gen1 must not be part of the downshift finding: %q", down.Evidence)
	}

	idle := findByID(report.Findings, "pcie-idle-power-saving")
	if idle == nil || !strings.HasPrefix(idle.Evidence, "GPU 0 (NVIDIA GeForce RTX 3090): ") || !reflect.DeepEqual(idle.GPUIndexes, []int{0}) {
		t.Errorf("GPU 0 should get the idle INFO: %+v", idle)
	}

	hot := findByID(report.Findings, "gpu-running-hot")
	if hot == nil || hot.Severity != types.SeverityWarn || !strings.HasPrefix(hot.Evidence, "GPU 2 (") {
		t.Errorf("GPU 2 at 88C should be a WARN naming GPU 2: %+v", hot)
	}

	// Each id appears once even though three GPUs were analyzed.
	seen := map[string]int{}
	for _, f := range report.Findings {
		seen[f.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("finding %s emitted %d times; per-GPU findings must be merged", id, n)
		}
	}

	// Summary: GPU 0's lines are unchanged (and not labelled DOWNSHIFTED by
	// GPU 2's warning), plus exactly one multi-GPU line.
	for _, want := range []string{"Temp: 43°C | P-State: P8 | Util: 2%", "PCIe: Gen1 x16 (idle, max Gen4)", "GPUs: 3 NVIDIA (worst temp 88°C on GPU 2)"} {
		if !strings.Contains(report.SummaryBlock, want) {
			t.Errorf("summary missing %q:\n%s", want, report.SummaryBlock)
		}
	}
	if strings.Contains(report.SummaryBlock, "DOWNSHIFTED") {
		t.Errorf("GPU 0's PCIe line must not inherit GPU 2's downshift:\n%s", report.SummaryBlock)
	}
	if strings.Count(report.SummaryBlock, "GPUs: ") != 1 {
		t.Errorf("exactly one GPUs: line expected:\n%s", report.SummaryBlock)
	}
	for _, line := range strings.Split(strings.TrimRight(report.SummaryBlock, "\n"), "\n") {
		if n := len([]rune(line)); n > 72 {
			t.Errorf("summary line is %d runes (> 72): %q", n, line)
		}
	}
}

// Identical findings on several GPUs collapse into one, keeping the worst
// severity and listing every affected GPU.
func TestAnalyzeThermal_MergesIdenticalFindingsAcrossGPUs(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "NVIDIA A100-SXM4-80GB", IsNVIDIA: true},
			{Index: 1, Name: "NVIDIA A100-SXM4-80GB", IsNVIDIA: true},
			{Index: 2, Name: "NVIDIA A100-SXM4-80GB", IsNVIDIA: true},
		},
		GPUThermal: []types.ThermalInfo{
			{GPUIndex: 0, TemperatureC: 86, PowerState: "P0", UtilizationPct: 100},
			{GPUIndex: 1, TemperatureC: 70, PowerState: "P0", UtilizationPct: 100},
			{GPUIndex: 2, TemperatureC: 94, PowerState: "P0", UtilizationPct: 100},
		},
	}
	findings := analyzeThermal(report)
	if len(findings) != 1 {
		t.Fatalf("expected one merged finding, got %v", ids(findings))
	}
	f := findings[0]
	if f.ID != "gpu-running-hot" || f.Severity != types.SeverityCrit {
		t.Errorf("merged finding should keep the CRIT instance: %s %s", f.ID, f.Severity)
	}
	if !reflect.DeepEqual(f.GPUIndexes, []int{0, 2}) {
		t.Errorf("GPUIndexes = %v, want [0 2]", f.GPUIndexes)
	}
	for _, want := range []string{"GPU 0 (NVIDIA A100-SXM4-80GB): GPU temperature: 86°C", "GPU 2 (NVIDIA A100-SXM4-80GB): GPU temperature: 94°C", " | ", "Affected GPUs: 0, 2."} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("merged evidence missing %q: %q", want, f.Evidence)
		}
	}
	if strings.Contains(f.Evidence, "GPU 1") {
		t.Errorf("healthy GPU 1 must not appear in the evidence: %q", f.Evidence)
	}
}

// One GPU: evidence and findings are byte-for-byte what they were before the
// per-GPU work, whether the data arrives in the slice or the legacy pointer.
func TestAnalyzeThermalPCIe_SingleGPUUnchanged(t *testing.T) {
	th := types.ThermalInfo{GPUIndex: 0, TemperatureC: 88, PowerState: "P0", UtilizationPct: 95, FanSupported: true, FanSpeedPct: 60}
	pc := types.PCIeInfo{GPUIndex: 0, CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 95}
	gpus := []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 3090", IsNVIDIA: true}}

	viaSlice := &types.Report{GPUs: gpus, GPUThermal: []types.ThermalInfo{th}, GPUPCIe: []types.PCIeInfo{pc}}
	viaPointer := &types.Report{GPUs: gpus, Thermal: &th, PCIe: &pc}

	a := append(analyzeThermal(viaSlice), analyzePCIe(viaSlice)...)
	b := append(analyzeThermal(viaPointer), analyzePCIe(viaPointer)...)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("slice and pointer inputs disagree:\n%+v\n%+v", a, b)
	}
	if len(a) != 2 {
		t.Fatalf("expected gpu-running-hot and pcie-downshift, got %v", ids(a))
	}
	for _, f := range a {
		if strings.HasPrefix(f.Evidence, "GPU 0") {
			t.Errorf("single-GPU evidence must not carry a GPU prefix: %q", f.Evidence)
		}
		if !reflect.DeepEqual(f.GPUIndexes, []int{0}) {
			t.Errorf("single-GPU finding should still be attributed to GPU 0: %v", f.GPUIndexes)
		}
	}
	if a[1].Evidence != "Current: Gen1 x16. Maximum: Gen4 x16. P-state: P0. GPU utilization: 95%." {
		t.Errorf("PCIe evidence changed: %q", a[1].Evidence)
	}
}

func TestSummaryBlock_NoMultiGPULineForSingleGPU(t *testing.T) {
	report := &types.Report{
		GPUs:       []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 3090", IsNVIDIA: true, DriverVersion: "591.86"}},
		Driver:     types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
		GPUThermal: []types.ThermalInfo{{GPUIndex: 0, TemperatureC: 44, PowerState: "P8", UtilizationPct: 25}},
	}
	report.Thermal = &report.GPUThermal[0]
	Analyze(report, types.ModeFull)
	if strings.Contains(report.SummaryBlock, "GPUs: ") {
		t.Errorf("single GPU must not get the GPUs: line:\n%s", report.SummaryBlock)
	}
	if !strings.Contains(report.SummaryBlock, "Temp: 44°C | P-State: P8 | Util: 25%") {
		t.Errorf("GPU 0 temp line changed:\n%s", report.SummaryBlock)
	}
}

// Two NVIDIA GPUs in the inventory but no thermal samples (nvidia-smi query
// failed): the count line still appears, without a temperature.
func TestSummaryBlock_MultiGPUWithoutThermal(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "Tesla T4", IsNVIDIA: true, DriverVersion: "535.183.01"},
			{Index: 1, Name: "Tesla T4", IsNVIDIA: true, DriverVersion: "535.183.01"},
		},
		Driver: types.DriverInfo{Version: "535.183.01", NvidiaSmiPath: "nvidia-smi"},
	}
	Analyze(report, types.ModeAI)
	if !strings.Contains(report.SummaryBlock, "\nGPUs: 2 NVIDIA\n") {
		t.Errorf("expected bare count line:\n%s", report.SummaryBlock)
	}
}

func TestMergePerGPUFindings_KeepsDistinctIDs(t *testing.T) {
	in := []types.Finding{
		{ID: "a", Severity: types.SeverityWarn, Evidence: "GPU 0: x", GPUIndexes: []int{0}, Confidence: 60},
		{ID: "b", Severity: types.SeverityInfo, Evidence: "GPU 0: y", GPUIndexes: []int{0}},
		{ID: "a", Severity: types.SeverityWarn, Evidence: "GPU 1: x", GPUIndexes: []int{1}, Confidence: 80},
	}
	out := mergePerGPUFindings(in)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("merge order/ids wrong: %v", ids(out))
	}
	if out[0].Confidence != 80 || !reflect.DeepEqual(out[0].GPUIndexes, []int{0, 1}) || out[0].Evidence != "GPU 0: x | GPU 1: x Affected GPUs: 0, 1." {
		t.Errorf("merged a = %+v", out[0])
	}
	if out[1].Evidence != "GPU 0: y" {
		t.Errorf("singleton b must be untouched: %+v", out[1])
	}
	if got := mergePerGPUFindings(nil); got != nil {
		t.Errorf("nil in, nil out; got %v", got)
	}
}

func TestGPULabel(t *testing.T) {
	report := &types.Report{GPUs: []types.GPUInfo{{Index: 0, Name: "Intel UHD 770"}, {Index: 1, Name: "NVIDIA GeForce RTX 4070", IsNVIDIA: true}}}
	if got := gpuLabel(report, 1); got != "GPU 1 (NVIDIA GeForce RTX 4070)" {
		t.Errorf("gpuLabel(1) = %q", got)
	}
	if got := gpuLabel(report, 0); got != "GPU 0" {
		t.Errorf("non-NVIDIA index 0 must not borrow the iGPU's name: %q", got)
	}
	if got := gpuLabel(report, 5); got != "GPU 5" {
		t.Errorf("gpuLabel(5) = %q", got)
	}
}

// A row whose temperature failed to parse carries 0 and must never be named
// the hottest GPU; when no row parsed the parenthetical is dropped entirely.
func TestMultiGPUSummary_SkipsUnparsedTemperatures(t *testing.T) {
	gpus := []types.GPUInfo{
		{Index: 0, Name: "NVIDIA A100-SXM4-80GB", IsNVIDIA: true},
		{Index: 1, Name: "NVIDIA A100-SXM4-80GB", IsNVIDIA: true},
	}
	mixed := &types.Report{GPUs: gpus, GPUThermal: []types.ThermalInfo{
		{GPUIndex: 0, TemperatureC: 0},
		{GPUIndex: 1, TemperatureC: 61},
	}}
	if got, want := multiGPUSummary(mixed), "GPUs: 2 NVIDIA (worst temp 61°C on GPU 1)"; got != want {
		t.Errorf("mixed rows: got %q, want %q", got, want)
	}
	allBad := &types.Report{GPUs: gpus, GPUThermal: []types.ThermalInfo{
		{GPUIndex: 0, TemperatureC: 0},
		{GPUIndex: 1, TemperatureC: 0},
	}}
	if got, want := multiGPUSummary(allBad), "GPUs: 2 NVIDIA"; got != want {
		t.Errorf("all rows unparsed: got %q, want %q", got, want)
	}
}
