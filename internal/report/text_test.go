package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestGenerateText_BasicStructure(t *testing.T) {
	report := createTestReport()
	output := GenerateText(report)

	// Check header elements
	if !strings.Contains(output, "NVCheckup v0.1.0") {
		t.Error("missing version in header")
	}
	if !strings.Contains(output, types.Disclaimer) {
		t.Error("missing disclaimer")
	}
	if !strings.Contains(output, "SUMMARY") {
		t.Error("missing summary section")
	}
	if !strings.Contains(output, "SYSTEM INFO") {
		t.Error("missing system info section")
	}
	if !strings.Contains(output, "GPU INVENTORY") {
		t.Error("missing GPU section")
	}
	if !strings.Contains(output, "FINDINGS") {
		t.Error("missing findings section")
	}
	if !strings.Contains(output, "PRIVACY") {
		t.Error("missing privacy section")
	}
}

func TestGenerateText_FindingsPresent(t *testing.T) {
	report := createTestReport()
	report.Findings = []types.Finding{
		{
			ID:           "test-critical",
			Severity:     types.SeverityCrit,
			Title:        "Test Critical Finding",
			Evidence:     "Test evidence",
			WhyItMatters: "Test reason",
			NextSteps:    []string{"Step 1", "Step 2"},
			Remediation:  &types.RemediationAction{ID: "do-thing"},
		},
	}
	output := GenerateText(report)

	if !strings.Contains(output, "[CRIT]") {
		t.Error("missing CRIT severity marker")
	}
	if !strings.Contains(output, "Test Critical Finding (test-critical)") {
		t.Error("missing finding title with id")
	}
	if !strings.Contains(output, "Step 1") {
		t.Error("missing next step")
	}
	if !strings.Contains(output, "nvcheckup fix --id do-thing") {
		t.Error("missing remediation hint")
	}
}

func TestGenerateText_RedactionNote(t *testing.T) {
	report := createTestReport()
	report.Metadata.RedactionEnabled = true
	output := GenerateText(report)

	if !strings.Contains(output, "Redaction: ENABLED") {
		t.Error("missing redaction status")
	}
}

func TestFooter_WithoutNetworkProbes(t *testing.T) {
	report := createTestReport()
	report.Metadata.NetworkProbes = false
	for name, out := range map[string]string{"text": GenerateText(report), "markdown": GenerateMarkdown(report)} {
		if !strings.Contains(out, footerLocal) {
			t.Errorf("%s: missing local-generation sentence", name)
		}
		if !strings.Contains(out, footerReadOnly) {
			t.Errorf("%s: missing read-only sentence", name)
		}
		if strings.Contains(out, footerProbes) {
			t.Errorf("%s: network probe sentence present although no probes ran", name)
		}
		if strings.Contains(out, "does not modify your system, drivers, or settings") {
			t.Errorf("%s: stale footer wording present", name)
		}
	}
}

func TestFooter_WithNetworkProbes(t *testing.T) {
	report := createTestReport()
	report.Metadata.NetworkProbes = true
	for name, out := range map[string]string{"text": GenerateText(report), "markdown": GenerateMarkdown(report)} {
		if !strings.Contains(out, footerProbes) {
			t.Errorf("%s: missing network probe sentence", name)
		}
	}
}

func TestGenerateText_PCIeIdleLine(t *testing.T) {
	report := createTestReport()
	report.PCIe = &types.PCIeInfo{
		CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16",
		Downshifted: false, IdleLikely: true, PowerState: "P8",
	}
	out := GenerateText(report)
	if !strings.Contains(out, "PCIe:          Gen1 x16 (idle, max Gen4)") {
		t.Errorf("idle PCIe line missing:\n%s", out)
	}

	// DOWNSHIFTED follows the analyzer's WARN, not the collector flag.
	report.PCIe.Downshifted = true
	report.PCIe.IdleLikely = false
	report.Findings = []types.Finding{{ID: "pcie-downshift", Severity: types.SeverityWarn, Title: "PCIe Link Speed Downshifted Under Load"}}
	out = GenerateText(report)
	if !strings.Contains(out, "Gen1 x16 (DOWNSHIFTED, max Gen4 x16)") {
		t.Errorf("downshift PCIe line missing:\n%s", out)
	}
	md := GenerateMarkdown(report)
	if !strings.Contains(md, "**PCIe:** Gen1 x16 (DOWNSHIFTED, max Gen4 x16)") {
		t.Errorf("markdown downshift PCIe line missing:\n%s", md)
	}
}

func TestGenerateText_PCIeDownshiftedFlagWithoutFindingIsIdle(t *testing.T) {
	// The collector's Downshifted flag can contradict the analyzer, which
	// emits the idle INFO when pstate/utilization are unknown. The renderers
	// must follow the findings, exactly like the summary block.
	report := createTestReport()
	report.PCIe = &types.PCIeInfo{
		CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16",
		Downshifted: true,
	}
	report.Findings = []types.Finding{{ID: "pcie-idle-power-saving", Severity: types.SeverityInfo, Title: "PCIe Link Power-Saving at Idle (expected)"}}
	for name, out := range map[string]string{"text": GenerateText(report), "markdown": GenerateMarkdown(report)} {
		if strings.Contains(out, "DOWNSHIFTED") {
			t.Errorf("%s: Downshifted flag without a PCIe WARN must not print DOWNSHIFTED:\n%s", name, out)
		}
		if !strings.Contains(out, "Gen1 x16 (idle, max Gen4)") {
			t.Errorf("%s: idle PCIe line missing:\n%s", name, out)
		}
	}

	// At maximum generation there is nothing to annotate.
	report.PCIe = &types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", Downshifted: true}
	report.Findings = nil
	if out := GenerateText(report); !strings.Contains(out, "PCIe:          Gen4 x16\n") || strings.Contains(out, "DOWNSHIFTED") {
		t.Errorf("at-max PCIe line should be bare:\n%s", out)
	}
}

func TestGenerateText_EventLogNotReadable(t *testing.T) {
	report := createTestReport()
	report.Windows = &types.WindowsInfo{WHEAErrors: []types.EventLogEntry{{EventID: 17}}}
	report.CollectorErrors = []types.CollectorError{
		{Collector: "windows.event4101", Error: "requires Administrator to read the System log"},
		{Collector: "windows.nvlddmkm", Error: "could not read the System log: timed out"},
	}
	out := GenerateText(report)
	for _, want := range []string{
		"Driver Resets (4101):  not readable (see Collector Notes)",
		"nvlddmkm Errors:       not readable (see Collector Notes)",
		"WHEA Errors:           1 event(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Driver Resets (4101):  0 event(s)") {
		t.Error("failed query must not be reported as 0 events")
	}
	md := GenerateMarkdown(report)
	for _, want := range []string{
		"| Driver resets (4101) | not readable (see Collector Notes) |",
		"| nvlddmkm errors | not readable (see Collector Notes) |",
		"| WHEA errors | 1 in last 30 days |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestGenerateText_MonitorsWithoutResolutionSkipped(t *testing.T) {
	report := createTestReport()
	report.Windows = &types.WindowsInfo{
		Monitors: []types.MonitorInfo{
			{Name: "Generic PnP", Resolution: "", RefreshRate: ""},
			{Name: "DELL U2723QE", Resolution: "3840x2160", RefreshRate: "60 Hz"},
		},
	}
	out := GenerateText(report)
	if strings.Contains(out, "Generic PnP") {
		t.Error("monitor without resolution should be skipped")
	}
	if !strings.Contains(out, "DELL U2723QE: 3840x2160 @ 60 Hz") {
		t.Error("real monitor missing")
	}
	md := GenerateMarkdown(report)
	if strings.Contains(md, "Generic PnP") {
		t.Error("markdown: monitor without resolution should be skipped")
	}
}

func TestGenerateJSON(t *testing.T) {
	report := createTestReport()
	report.Metadata.SchemaVersion = ""
	jsonStr, err := GenerateJSON(report)
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if !strings.Contains(jsonStr, `"tool_version"`) {
		t.Error("missing tool_version in JSON")
	}
	if !strings.Contains(jsonStr, `"gpus"`) {
		t.Error("missing gpus in JSON")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	meta := decoded["metadata"].(map[string]interface{})
	if meta["schema_version"] != types.SchemaVersion {
		t.Errorf("schema_version = %v, want %q", meta["schema_version"], types.SchemaVersion)
	}
	if _, ok := meta["network_probes"]; !ok {
		t.Error("missing network_probes in metadata")
	}
}

func TestGenerateMarkdown_Structure(t *testing.T) {
	report := createTestReport()
	report.Windows = &types.WindowsInfo{HAGSEnabled: "Enabled", GameMode: "On", PowerPlan: "Balanced", OverlaySoftware: []string{"Discord"}}
	report.AI = &types.AIInfo{CUDAToolkitVersion: "12.4", CuDNNVersion: "9.1",
		PyTorchInfo: &types.PyTorchInfo{Version: "2.4.0", CUDAVersion: "12.4", CUDAAvailable: true, DeviceName: "RTX 4090"},
		KeyPackages: []types.PackageInfo{{Name: "numpy", Version: "2.0"}}}
	report.PCIe = &types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16"}
	report.Network = &types.NetworkInfo{InterfaceName: "Ethernet", InterfaceType: "ethernet", LatencyMs: 8.2}
	report.CollectorErrors = []types.CollectorError{{Collector: "dxdiag", Error: "timed out"}}
	output := GenerateMarkdown(report)

	for _, want := range []string{
		"# NVCheckup Diagnostic Report", "## Summary", "## GPUs", "## Findings",
		"## Windows", "| HAGS | Enabled |", "| Overlays | Discord |",
		"## AI / CUDA Environment", "| PyTorch | 2.4.0 (CUDA 12.4, available=true, device=RTX 4090) |", "| numpy | 2.0 |",
		"**PCIe:** Gen4 x16\n",
		"## Network", "| Interface | Ethernet (ethernet) |",
		"## Collector Notes", "- **dxdiag:** timed out",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestMdCell(t *testing.T) {
	got := mdCell("a | b\nc\r\nd")
	if got != `a \| b c d` {
		t.Errorf("mdCell = %q", got)
	}
}

func TestValueOrNA(t *testing.T) {
	if valueOrNA("") != "N/A" {
		t.Error("empty string should return N/A")
	}
	if valueOrNA("hello") != "hello" {
		t.Error("non-empty string should return as-is")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 100) != "short" {
		t.Error("short string should not be truncated")
	}
	result := truncate("this is a very long string", 10)
	if utf8.RuneCountInString(result) > 10 {
		t.Errorf("truncated string too long: %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("truncated string should end with ...")
	}
}

func TestTruncate_RuneSafe(t *testing.T) {
	// Every character is multi-byte; a byte-based slice would cut mid-rune.
	in := strings.Repeat("°", 20)
	got := truncate(in, 10)
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if utf8.RuneCountInString(got) != 10 {
		t.Errorf("rune count = %d, want 10", utf8.RuneCountInString(got))
	}
	if got != strings.Repeat("°", 7)+"..." {
		t.Errorf("unexpected result %q", got)
	}
}

func createTestReport() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{
			ToolVersion:      "0.1.0",
			Timestamp:        time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			Mode:             types.ModeFull,
			RuntimeSeconds:   2.5,
			RedactionEnabled: true,
			Platform:         "windows",
			SchemaVersion:    types.SchemaVersion,
		},
		System: types.SystemInfo{
			OSName:       "Windows 11",
			OSVersion:    "23H2",
			Architecture: "amd64",
			CPUModel:     "AMD Ryzen 9 7950X",
			RAMTotalMB:   32768,
			Uptime:       "3d 5h 20m",
			BootMode:     "UEFI",
			SecureBoot:   "Enabled",
		},
		GPUs: []types.GPUInfo{
			{
				Index:         0,
				Name:          "NVIDIA GeForce RTX 4090",
				Vendor:        "NVIDIA",
				IsNVIDIA:      true,
				DriverVersion: "566.36",
				VRAMTotalMB:   24576,
				VRAMFreeMB:    20000,
				VRAMUsedMB:    4576,
				Temperature:   42,
			},
		},
		Driver: types.DriverInfo{
			Version:     "566.36",
			CUDAVersion: "12.7",
		},
		Findings:     []types.Finding{},
		TopIssues:    []string{"No significant issues detected."},
		NextSteps:    []string{"No action required."},
		SummaryBlock: "NVCheckup v0.1.0 | 2025-01-15 14:30:00\nGPU: RTX 4090 | Driver: 566.36\n",
	}
}
