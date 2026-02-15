package analyzer

import (
	"testing"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func TestAnalyzeGPUPresence_NoGPU(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{},
	}
	findings := analyzeGPUPresence(report)
	if len(findings) == 0 {
		t.Error("expected findings when no GPU detected")
	}
	if findings[0].Severity != types.SeverityCrit {
		t.Errorf("expected CRIT severity, got %s", findings[0].Severity)
	}
}

func TestAnalyzeGPUPresence_NVIDIAPresent(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{
			{Name: "RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true},
		},
	}
	findings := analyzeGPUPresence(report)
	// Should have no critical findings for missing GPU
	for _, f := range findings {
		if f.Title == "No NVIDIA GPU Detected" {
			t.Error("should not flag missing GPU when NVIDIA GPU is present")
		}
	}
}

func TestAnalyzeGPUPresence_HybridDetected(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{
			{Name: "RTX 4070", Vendor: "NVIDIA", IsNVIDIA: true},
			{Name: "Intel UHD 770", Vendor: "Intel"},
		},
	}
	findings := analyzeGPUPresence(report)
	found := false
	for _, f := range findings {
		if f.Title == "Hybrid GPU Configuration Detected" {
			found = true
			if f.Severity != types.SeverityInfo {
				t.Errorf("expected INFO severity for hybrid, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected hybrid GPU finding")
	}
}

func TestAnalyzeDriverBasics_NoDriver(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{},
	}
	findings := analyzeDriverBasics(report)
	if len(findings) == 0 {
		t.Error("expected findings when driver not detected")
	}
	hasCrit := false
	for _, f := range findings {
		if f.Severity == types.SeverityCrit {
			hasCrit = true
		}
	}
	if !hasCrit {
		t.Error("expected CRIT finding for missing driver")
	}
}

func TestAnalyzeDriverBasics_DriverPresent(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{
			Version:       "566.36",
			NvidiaSmiPath: "nvidia-smi",
		},
	}
	findings := analyzeDriverBasics(report)
	for _, f := range findings {
		if f.Severity == types.SeverityCrit {
			t.Errorf("unexpected CRIT finding when driver is present: %s", f.Title)
		}
	}
}

func TestAnalyzeWindowsGaming_DriverResets(t *testing.T) {
	report := &types.Report{
		Windows: &types.WindowsInfo{
			DriverResetEvents: make([]types.EventLogEntry, 5),
		},
	}
	findings := analyzeWindowsGaming(report)
	found := false
	for _, f := range findings {
		if f.Title == "Display Driver Resets Detected (Event ID 4101)" {
			found = true
			if f.Severity != types.SeverityCrit {
				t.Errorf("expected CRIT for 5 resets, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected driver reset finding")
	}
}

func TestAnalyzeWindowsGaming_FewResets(t *testing.T) {
	report := &types.Report{
		Windows: &types.WindowsInfo{
			DriverResetEvents: make([]types.EventLogEntry, 2),
		},
	}
	findings := analyzeWindowsGaming(report)
	for _, f := range findings {
		if f.Title == "Display Driver Resets Detected (Event ID 4101)" {
			if f.Severity != types.SeverityWarn {
				t.Errorf("expected WARN for 2 resets, got %s", f.Severity)
			}
		}
	}
}

func TestAnalyzePyTorch_CPUOnly(t *testing.T) {
	report := &types.Report{
		AI: &types.AIInfo{
			PyTorchInfo: &types.PyTorchInfo{
				Version:       "2.2.0",
				CUDAVersion:   "",
				CUDAAvailable: false,
			},
		},
	}
	findings := analyzePyTorch(report)
	found := false
	for _, f := range findings {
		if f.Title == "PyTorch Installed Without CUDA Support" {
			found = true
			if f.Severity != types.SeverityWarn {
				t.Errorf("expected WARN, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected CPU-only PyTorch finding")
	}
}

func TestAnalyzePyTorch_Working(t *testing.T) {
	report := &types.Report{
		AI: &types.AIInfo{
			PyTorchInfo: &types.PyTorchInfo{
				Version:       "2.2.0",
				CUDAVersion:   "12.1",
				CUDAAvailable: true,
				DeviceName:    "RTX 4090",
			},
		},
	}
	findings := analyzePyTorch(report)
	found := false
	for _, f := range findings {
		if f.Title == "PyTorch CUDA is Working" {
			found = true
			if f.Severity != types.SeverityInfo {
				t.Errorf("expected INFO, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected working PyTorch finding")
	}
}

func TestAnalyzeLinuxModules_Nouveau(t *testing.T) {
	report := &types.Report{
		Linux: &types.LinuxInfo{
			LoadedModules: map[string]bool{
				"nouveau": true,
			},
		},
	}
	findings := analyzeLinuxModules(report)
	found := false
	for _, f := range findings {
		if f.Title == "Nouveau Driver is Active (Instead of NVIDIA)" {
			found = true
			if f.Severity != types.SeverityCrit {
				t.Errorf("expected CRIT, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected nouveau finding")
	}
}

func TestAnalyzeSecureBoot_EnabledBlocking(t *testing.T) {
	report := &types.Report{
		Linux: &types.LinuxInfo{
			SecureBootState: "Enabled",
			LoadedModules: map[string]bool{
				"nvidia": false,
			},
		},
	}
	findings := analyzeSecureBoot(report)
	found := false
	for _, f := range findings {
		if f.Title == "Secure Boot Enabled — NVIDIA Module May Be Blocked" {
			found = true
			if f.Severity != types.SeverityCrit {
				t.Errorf("expected CRIT, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected secure boot blocking finding")
	}
}

func TestBuildTopIssues(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityCrit, Title: "Critical Issue"},
		{Severity: types.SeverityWarn, Title: "Warning Issue"},
		{Severity: types.SeverityInfo, Title: "Info Issue"},
	}
	issues := buildTopIssues(findings)
	if len(issues) != 2 {
		t.Errorf("expected 2 top issues (CRIT + WARN), got %d", len(issues))
	}
}

func TestBuildTopIssues_NoIssues(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityInfo, Title: "Info Only"},
	}
	issues := buildTopIssues(findings)
	if len(issues) != 1 {
		t.Errorf("expected 1 issue (no significant), got %d", len(issues))
	}
	if issues[0] != "No significant issues detected." {
		t.Errorf("unexpected message: %s", issues[0])
	}
}

func TestSortFindings(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityInfo, Title: "Info"},
		{Severity: types.SeverityCrit, Title: "Critical"},
		{Severity: types.SeverityWarn, Title: "Warning"},
	}
	sortFindings(findings)
	if findings[0].Severity != types.SeverityCrit {
		t.Errorf("expected CRIT first, got %s", findings[0].Severity)
	}
	if findings[1].Severity != types.SeverityWarn {
		t.Errorf("expected WARN second, got %s", findings[1].Severity)
	}
	if findings[2].Severity != types.SeverityInfo {
		t.Errorf("expected INFO third, got %s", findings[2].Severity)
	}
}

func TestMajorVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"12.2.1", "12"},
		{"11", "11"},
		{"", ""},
	}
	for _, tt := range tests {
		got := majorVersion(tt.input)
		if got != tt.want {
			t.Errorf("majorVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAnalyzeFullPipeline(t *testing.T) {
	report := &types.Report{
		GPUs: []types.GPUInfo{
			{Name: "RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "566.36", VRAMTotalMB: 24576},
		},
		Driver: types.DriverInfo{
			Version:       "566.36",
			CUDAVersion:   "12.7",
			NvidiaSmiPath: "nvidia-smi",
		},
	}

	Analyze(report, types.ModeFull)

	if report.SummaryBlock == "" {
		t.Error("expected non-empty summary block")
	}
	if len(report.TopIssues) == 0 {
		t.Error("expected at least one top issue entry")
	}
	if len(report.NextSteps) == 0 {
		t.Error("expected at least one next step")
	}
}
