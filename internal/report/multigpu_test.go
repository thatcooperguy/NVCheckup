package report

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// twoGPUReport is an RTX 3090 (idle, Gen1) next to an RTX 4090 that is busy
// but stuck at Gen1, with the analyzer's per-GPU WARN attached to GPU 1.
func twoGPUReport() *types.Report {
	r := createTestReport()
	r.GPUs = []types.GPUInfo{
		{Index: 0, Name: "NVIDIA GeForce RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24576, VRAMUsedMB: 2970, VRAMFreeMB: 21357, Temperature: 43},
		{Index: 1, Name: "NVIDIA GeForce RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24564, VRAMUsedMB: 20000, VRAMFreeMB: 4564, Temperature: 71},
	}
	r.Driver = types.DriverInfo{Version: "591.86", CUDAVersion: "13.1", NvidiaSmiPath: "nvidia-smi"}
	r.GPUThermal = []types.ThermalInfo{
		{GPUIndex: 0, TemperatureC: 43, PowerState: "P8", FanSupported: true, FanSpeedPct: 0, PowerDrawW: "33.62", PowerLimitW: "350.00", UtilizationPct: 2},
		{GPUIndex: 1, TemperatureC: 71, PowerState: "P0", FanSupported: true, FanSpeedPct: 70, PowerDrawW: "431.20", PowerLimitW: "450.00", UtilizationPct: 99},
	}
	r.GPUPCIe = []types.PCIeInfo{
		{GPUIndex: 0, CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 2, IdleLikely: true},
		{GPUIndex: 1, CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99, Downshifted: true},
	}
	r.Thermal = &r.GPUThermal[0]
	r.PCIe = &r.GPUPCIe[0]
	r.Findings = []types.Finding{
		{ID: "pcie-downshift", Severity: types.SeverityWarn, Title: "PCIe Link Speed Downshifted Under Load", Evidence: "GPU 1 (NVIDIA GeForce RTX 4090): Current: Gen1 x16.", GPUIndexes: []int{1}},
		{ID: "pcie-idle-power-saving", Severity: types.SeverityInfo, Title: "PCIe Link Power-Saving at Idle (expected)", Evidence: "GPU 0 (NVIDIA GeForce RTX 3090): Current: Gen1 x16.", GPUIndexes: []int{0}},
	}
	return r
}

func TestGenerateText_TwoGPUsPrintPerGPULines(t *testing.T) {
	out := GenerateText(twoGPUReport())

	gpu0 := "  [GPU 0] NVIDIA GeForce RTX 3090\n" +
		"    Vendor:    NVIDIA\n" +
		"    Driver:    591.86\n" +
		"    VRAM:      24576 MB total, 2970 MB used, 21357 MB free\n" +
		"    Temp:      43°C\n" +
		"    PCIe:      Gen1 x16 (idle, max Gen4)\n" +
		"    Thermal:   43°C, P8, fan 0%, 33.62 / 350.00 W\n\n"
	if !strings.Contains(out, gpu0) {
		t.Errorf("GPU 0 block with per-GPU PCIe/Thermal lines missing:\n%s", out)
	}
	gpu1 := "  [GPU 1] NVIDIA GeForce RTX 4090\n" +
		"    Vendor:    NVIDIA\n" +
		"    Driver:    591.86\n" +
		"    VRAM:      24564 MB total, 20000 MB used, 4564 MB free\n" +
		"    Temp:      71°C\n" +
		"    PCIe:      Gen1 x16 (DOWNSHIFTED, max Gen4 x16)\n" +
		"    Thermal:   71°C, P0, fan 70%, 431.20 / 450.00 W\n\n"
	if !strings.Contains(out, gpu1) {
		t.Errorf("GPU 1 block should be labelled DOWNSHIFTED from its own finding:\n%s", out)
	}
	// The single GPU-0-only lines are replaced, not duplicated.
	if strings.Contains(out, "  PCIe:          ") || strings.Contains(out, "  Thermal:       ") {
		t.Errorf("top-level PCIe/Thermal lines must not be printed for a multi-GPU report:\n%s", out)
	}
	if !strings.Contains(out, "  NVIDIA Driver: 591.86\n  CUDA (driver): 13.1\n") {
		t.Errorf("driver lines missing:\n%s", out)
	}
}

func TestGenerateMarkdown_TwoGPUsPrintPerGPURows(t *testing.T) {
	md := GenerateMarkdown(twoGPUReport())
	for _, want := range []string{
		"### GPU 0: NVIDIA GeForce RTX 3090\n",
		"| PCIe | Gen1 x16 (idle, max Gen4) |\n",
		"| Thermal | 43°C, P8, fan 0%, 33.62 / 350.00 W |\n",
		"### GPU 1: NVIDIA GeForce RTX 4090\n",
		"| PCIe | Gen1 x16 (DOWNSHIFTED, max Gen4 x16) |\n",
		"| Thermal | 71°C, P0, fan 70%, 431.20 / 450.00 W |\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "**PCIe:**") || strings.Contains(md, "**Thermal:**") {
		t.Errorf("bold single-GPU lines must not appear in a multi-GPU report:\n%s", md)
	}
}

// One GPU whose data arrives in the new slices renders exactly like the same
// report with only the legacy pointers: the single "PCIe:" and "Thermal:"
// lines keep their place and format byte for byte.
func TestGenerateText_SingleGPUSlicesRenderIdentically(t *testing.T) {
	legacy := createTestReport()
	legacy.PCIe = &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 22, IdleLikely: true}
	legacy.Thermal = &types.ThermalInfo{TemperatureC: 44, PowerState: "P8", FanSupported: true, PowerDrawW: "31.49", PowerLimitW: "350.00", UtilizationPct: 25}
	legacy.Findings = []types.Finding{{ID: "pcie-idle-power-saving", Severity: types.SeverityInfo, Title: "PCIe Link Power-Saving at Idle (expected)", Evidence: "x"}}

	modern := createTestReport()
	modern.GPUPCIe = []types.PCIeInfo{*legacy.PCIe}
	modern.GPUThermal = []types.ThermalInfo{*legacy.Thermal}
	modern.PCIe = &modern.GPUPCIe[0]
	modern.Thermal = &modern.GPUThermal[0]
	modern.Findings = []types.Finding{{ID: "pcie-idle-power-saving", Severity: types.SeverityInfo, Title: "PCIe Link Power-Saving at Idle (expected)", Evidence: "x", GPUIndexes: []int{0}}}

	a, b := GenerateText(legacy), GenerateText(modern)
	if a != b {
		t.Errorf("single-GPU text output differs between legacy pointers and slices:\n%s\n----\n%s", a, b)
	}
	for _, want := range []string{"  PCIe:          Gen1 x16 (idle, max Gen4)\n", "  Thermal:       44°C, P8, fan 0%, 31.49 / 350.00 W\n"} {
		if !strings.Contains(b, want) {
			t.Errorf("single-GPU line missing %q:\n%s", want, b)
		}
	}
	if strings.Contains(b, "    PCIe:      ") {
		t.Errorf("per-GPU lines must not appear for a single GPU:\n%s", b)
	}
	if ma, mb := GenerateMarkdown(legacy), GenerateMarkdown(modern); ma != mb {
		t.Errorf("single-GPU markdown differs between legacy pointers and slices")
	}
}

func TestPcieWarnedFor(t *testing.T) {
	findings := []types.Finding{
		{ID: "pcie-downshift", Severity: types.SeverityWarn, GPUIndexes: []int{2}},
		{ID: "pcie-idle-power-saving", Severity: types.SeverityInfo, GPUIndexes: []int{0}},
	}
	if pcieWarnedFor(findings, 0) || !pcieWarnedFor(findings, 2) {
		t.Error("warn must attach only to GPU 2")
	}
	// A WARN without attribution applies to every GPU (older reports).
	if !pcieWarnedFor([]types.Finding{{ID: "pcie-width-reduced", Severity: types.SeverityWarn}}, 3) {
		t.Error("unattributed WARN should apply to any GPU")
	}
	if pcieWarnedFor(nil, 0) {
		t.Error("no findings, no warning")
	}
}

// nvidia-smi returned two thermal/PCIe rows but the inventory knows only GPU 0
// (or nothing at all, when 'nvidia-smi -L' could not be parsed). The unmatched
// samples are still printed rather than silently dropped from text/markdown.
func TestGenerateText_UnmatchedSamplesStillPrinted(t *testing.T) {
	r := twoGPUReport()
	r.GPUs = r.GPUs[:1] // GPU 1 vanished from the inventory
	out := GenerateText(r)
	want := "  [GPU 1] (not in inventory)\n" +
		"    PCIe:      Gen1 x16 (DOWNSHIFTED, max Gen4 x16)\n" +
		"    Thermal:   71°C, P0, fan 70%, 431.20 / 450.00 W\n\n"
	if !strings.Contains(out, want) {
		t.Errorf("GPU 1 samples must be printed even without an inventory entry:\n%s", out)
	}
	if !strings.Contains(out, "  [GPU 0] NVIDIA GeForce RTX 3090\n") || strings.Contains(out, "[GPU 0] (not in inventory)") {
		t.Errorf("GPU 0 keeps its normal inventory block:\n%s", out)
	}

	md := GenerateMarkdown(r)
	for _, w := range []string{"### GPU 1: (not in inventory)\n", "| PCIe | Gen1 x16 (DOWNSHIFTED, max Gen4 x16) |\n", "| Thermal | 71°C, P0, fan 70%, 431.20 / 450.00 W |\n"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown missing %q:\n%s", w, md)
		}
	}

	// No inventory at all: every sample is shown, in index order.
	r.GPUs = nil
	out = GenerateText(r)
	i0, i1 := strings.Index(out, "  [GPU 0] (not in inventory)\n    PCIe:      Gen1 x16 (idle, max Gen4)\n"), strings.Index(out, "  [GPU 1] (not in inventory)\n")
	if i0 < 0 || i1 < 0 || i0 > i1 {
		t.Errorf("both unmatched GPUs should be printed in order (%d, %d):\n%s", i0, i1, out)
	}
	if !strings.Contains(out, "  No GPUs detected.\n") {
		t.Errorf("empty inventory notice still expected:\n%s", out)
	}
}

func TestUnmatchedSampleIndexes(t *testing.T) {
	r := &types.Report{
		GPUs:       []types.GPUInfo{{Index: 0, IsNVIDIA: true}, {Index: 1, IsNVIDIA: false}},
		GPUPCIe:    []types.PCIeInfo{{GPUIndex: 2}, {GPUIndex: 0}},
		GPUThermal: []types.ThermalInfo{{GPUIndex: 1}, {GPUIndex: 2}},
	}
	got := unmatchedSampleIndexes(r)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("unmatchedSampleIndexes = %v, want [1 2] (index 1 is a non-NVIDIA entry, index 0 matches)", got)
	}
	if got := unmatchedSampleIndexes(twoGPUReport()); len(got) != 0 {
		t.Errorf("fully matched report should have no unmatched indexes, got %v", got)
	}
}
