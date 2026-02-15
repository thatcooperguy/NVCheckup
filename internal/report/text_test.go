package report

import (
	"strings"
	"testing"
	"time"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
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
			Severity:     types.SeverityCrit,
			Title:        "Test Critical Finding",
			Evidence:     "Test evidence",
			WhyItMatters: "Test reason",
			NextSteps:    []string{"Step 1", "Step 2"},
		},
	}
	output := GenerateText(report)

	if !strings.Contains(output, "[CRIT]") {
		t.Error("missing CRIT severity marker")
	}
	if !strings.Contains(output, "Test Critical Finding") {
		t.Error("missing finding title")
	}
	if !strings.Contains(output, "Step 1") {
		t.Error("missing next step")
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

func TestGenerateJSON(t *testing.T) {
	report := createTestReport()
	jsonStr, err := GenerateJSON(report)
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if jsonStr == "" {
		t.Error("expected non-empty JSON output")
	}
	if !strings.Contains(jsonStr, `"tool_version"`) {
		t.Error("missing tool_version in JSON")
	}
	if !strings.Contains(jsonStr, `"gpus"`) {
		t.Error("missing gpus in JSON")
	}
}

func TestGenerateMarkdown_Structure(t *testing.T) {
	report := createTestReport()
	output := GenerateMarkdown(report)

	if !strings.Contains(output, "# NVCheckup Diagnostic Report") {
		t.Error("missing markdown title")
	}
	if !strings.Contains(output, "## Summary") {
		t.Error("missing summary heading")
	}
	if !strings.Contains(output, "## GPUs") {
		t.Error("missing GPUs heading")
	}
	if !strings.Contains(output, "## Findings") {
		t.Error("missing findings heading")
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
	if len(result) > 10 {
		t.Errorf("truncated string too long: %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("truncated string should end with ...")
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
