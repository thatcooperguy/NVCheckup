package analyzer

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// findByID returns the first finding with the given ID, or nil.
func findByID(findings []types.Finding, id string) *types.Finding {
	for i := range findings {
		if findings[i].ID == id {
			return &findings[i]
		}
	}
	return nil
}

func ids(findings []types.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

// ── C1: PCIe ──────────────────────────────────────────────────────────

func TestAnalyzePCIe_Table(t *testing.T) {
	tests := []struct {
		name     string
		pcie     types.PCIeInfo
		wantID   string
		wantSev  types.Severity
		wantNone bool
	}{
		{
			name:    "P8 Gen1 idle is expected power saving",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 0, IdleLikely: true},
			wantID:  "pcie-idle-power-saving",
			wantSev: types.SeverityInfo,
		},
		{
			name:    "P0 Gen1 at 95% utilization is a real downshift",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 95, IdleLikely: false},
			wantID:  "pcie-downshift",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "x8 of x16 is a width problem regardless of idle",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x8", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 0, IdleLikely: true},
			wantID:  "pcie-width-reduced",
			wantSev: types.SeverityWarn,
		},
		{
			name:     "full link produces nothing",
			pcie:     types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 80},
			wantNone: true,
		},
		{
			name:     "old Gen2 slot at its own maximum is not a fault",
			pcie:     types.PCIeInfo{CurrentSpeed: "Gen2", MaxSpeed: "Gen2", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 80},
			wantNone: true,
		},
		{
			name:    "P8 without IdleLikely still counts as idle",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 2},
			wantID:  "pcie-idle-power-saving",
			wantSev: types.SeverityInfo,
		},
		{
			// Older collectors capture neither P-state nor utilization: with no
			// evidence of load, Gen1 must not be reported as a fault.
			name:    "no P-state and no utilization falls back to the idle INFO",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16"},
			wantID:  "pcie-idle-power-saving",
			wantSev: types.SeverityInfo,
		},
		{
			name:    "no P-state but clear utilization is a downshift",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", UtilizationPct: 55},
			wantID:  "pcie-downshift",
			wantSev: types.SeverityWarn,
		},
		{
			// Some mobile parts and video-decode workloads sit in P3/P4 while
			// busy; a reduced generation there is a real downshift.
			name:    "P3 at 70% utilization with IdleLikely=false is a downshift",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P3", UtilizationPct: 70, IdleLikely: false},
			wantID:  "pcie-downshift",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "P4 with modest utilization and IdleLikely=false is a downshift",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen2", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P4", UtilizationPct: 10, IdleLikely: false},
			wantID:  "pcie-downshift",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "P8 with clear utilization is still a downshift",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 45, IdleLikely: false},
			wantID:  "pcie-downshift",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "IdleLikely overrides a P0 reading",
			pcie:    types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 1, IdleLikely: true},
			wantID:  "pcie-idle-power-saving",
			wantSev: types.SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.pcie
			findings := analyzePCIe(&types.Report{PCIe: &p})
			if tt.wantNone {
				if len(findings) != 0 {
					t.Fatalf("expected no findings, got %v", ids(findings))
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("expected exactly one finding, got %v", ids(findings))
			}
			f := findings[0]
			if f.ID != tt.wantID || f.Severity != tt.wantSev {
				t.Errorf("got %s/%s, want %s/%s", f.ID, f.Severity, tt.wantID, tt.wantSev)
			}
			wantPState := tt.pcie.PowerState
			if wantPState == "" {
				wantPState = "unknown"
			}
			if !strings.Contains(f.Evidence, "P-state: "+wantPState) {
				t.Errorf("evidence should include the P-state, got %q", f.Evidence)
			}
			if !strings.Contains(f.Evidence, "utilization") {
				t.Errorf("evidence should include utilization, got %q", f.Evidence)
			}
		})
	}
}

func TestSummaryPCIeLine(t *testing.T) {
	idle := &types.Report{
		GPUs:   []types.GPUInfo{{Name: "RTX 3090", IsNVIDIA: true, DriverVersion: "591.86"}},
		Driver: types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
		PCIe:   &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", IdleLikely: true},
	}
	Analyze(idle, types.ModeFull)
	if !strings.Contains(idle.SummaryBlock, "PCIe: Gen1 x16 (idle, max Gen4)") {
		t.Errorf("idle summary line wrong:\n%s", idle.SummaryBlock)
	}
	if strings.Contains(idle.SummaryBlock, "DOWNSHIFTED") {
		t.Errorf("idle link must not be labelled DOWNSHIFTED:\n%s", idle.SummaryBlock)
	}

	load := &types.Report{
		GPUs:   idle.GPUs,
		Driver: idle.Driver,
		PCIe:   &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99},
	}
	Analyze(load, types.ModeFull)
	if !strings.Contains(load.SummaryBlock, "PCIe: Gen1 x16 (DOWNSHIFTED") {
		t.Errorf("loaded downshift should be labelled DOWNSHIFTED:\n%s", load.SummaryBlock)
	}
}

// ── C2: WHEA ──────────────────────────────────────────────────────────

func wheaEvents(n, id int, level, msg string) []types.EventLogEntry {
	out := make([]types.EventLogEntry, n)
	for i := range out {
		out[i] = types.EventLogEntry{EventID: id, Source: "Microsoft-Windows-WHEA-Logger", Level: level, Message: msg, Time: time.Now()}
	}
	return out
}

const nicWHEAMessage = "A corrected hardware error has occurred.\nComponent: PCI Express Root Port\nError Source: Advanced Error Reporting (PCI Express)\nBus:Device:Function: 0x0:0x1C:0x0\nVendor ID:Device ID: 0x8086:0xA33C"

func TestAnalyzeWHEA_Table(t *testing.T) {
	tests := []struct {
		name    string
		events  []types.EventLogEntry
		gpus    []types.GPUInfo
		wantID  string
		wantSev types.Severity
	}{
		{
			name:    "16 corrected NIC errors are INFO",
			events:  wheaEvents(16, 17, "Warning", nicWHEAMessage),
			wantID:  "whea-corrected",
			wantSev: types.SeverityInfo,
		},
		{
			name:    "one fatal machine check is WARN",
			events:  wheaEvents(1, 18, "Error", "A fatal hardware error has occurred.\nComponent: Processor Core"),
			wantID:  "whea-errors",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "three fatal machine checks are CRIT",
			events:  wheaEvents(3, 18, "Error", "A fatal hardware error has occurred."),
			wantID:  "whea-errors",
			wantSev: types.SeverityCrit,
		},
		{
			name:    "corrected error naming VEN_10DE escalates to WARN",
			events:  wheaEvents(1, 17, "Warning", "A corrected hardware error has occurred.\nDevice: PCI\\VEN_10DE&DEV_2204"),
			wantID:  "whea-corrected",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "corrected error naming the GPU bus id escalates to WARN",
			events:  wheaEvents(1, 17, "Warning", "Corrected error on 01:00.0"),
			gpus:    []types.GPUInfo{{IsNVIDIA: true, PCIBusID: "00000000:01:00.0"}},
			wantID:  "whea-corrected",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "50 corrected errors in 30 days escalate to WARN",
			events:  wheaEvents(50, 19, "Warning", nicWHEAMessage),
			wantID:  "whea-corrected",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "domain-less GPU bus id matches the same address",
			events:  wheaEvents(1, 17, "Warning", "Corrected error on 01:00.0"),
			gpus:    []types.GPUInfo{{IsNVIDIA: true, PCIBusID: "01:00.0"}},
			wantID:  "whea-corrected",
			wantSev: types.SeverityWarn,
		},
		{
			// A domain-less "01:00.0" must not be shortened to "00.0", which
			// would match unrelated text such as firmware version strings.
			name:    "domain-less GPU bus id does not match firmware version text",
			events:  wheaEvents(1, 17, "Warning", "Corrected error. Firmware version 1.00.0 updated."),
			gpus:    []types.GPUInfo{{IsNVIDIA: true, PCIBusID: "01:00.0"}},
			wantID:  "whea-corrected",
			wantSev: types.SeverityInfo,
		},
		{
			name:    "degenerate GPU bus id never escalates",
			events:  wheaEvents(1, 17, "Warning", "Corrected error 0 on some device"),
			gpus:    []types.GPUInfo{{IsNVIDIA: true, PCIBusID: "0"}},
			wantID:  "whea-corrected",
			wantSev: types.SeverityInfo,
		},
		{
			name:    "unknown id with Error level is uncorrected",
			events:  wheaEvents(1, 99, "Error", "something bad"),
			wantID:  "whea-errors",
			wantSev: types.SeverityWarn,
		},
		{
			name:    "legacy entries without id or level stay WARN",
			events:  make([]types.EventLogEntry, 1),
			wantID:  "whea-errors",
			wantSev: types.SeverityWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &types.Report{GPUs: tt.gpus}
			findings := analyzeWHEA(report, tt.events)
			if len(findings) != 1 {
				t.Fatalf("expected exactly one finding, got %v", ids(findings))
			}
			f := findings[0]
			if f.ID != tt.wantID || f.Severity != tt.wantSev {
				t.Errorf("got %s/%s, want %s/%s", f.ID, f.Severity, tt.wantID, tt.wantSev)
			}
		})
	}
}

func TestAnalyzeWHEA_CorrectedEvidenceHasComponent(t *testing.T) {
	findings := analyzeWHEA(&types.Report{}, wheaEvents(2, 17, "Warning", nicWHEAMessage))
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %v", ids(findings))
	}
	if !strings.Contains(findings[0].Evidence, "PCI Express Root Port") {
		t.Errorf("evidence should quote the component, got %q", findings[0].Evidence)
	}
	if !strings.Contains(strings.ToLower(findings[0].WhyItMatters), "benign") {
		t.Errorf("corrected errors should be described as usually benign, got %q", findings[0].WhyItMatters)
	}
}

func TestAnalyzeWHEA_MixedSplitsIntoTwoFindings(t *testing.T) {
	events := append(wheaEvents(5, 17, "Warning", nicWHEAMessage), wheaEvents(1, 20, "Error", "fatal PCIe")...)
	findings := analyzeWHEA(&types.Report{}, events)
	if findByID(findings, "whea-corrected") == nil || findByID(findings, "whea-errors") == nil {
		t.Fatalf("expected both corrected and uncorrected findings, got %v", ids(findings))
	}
}

// ── C3: Thermal ───────────────────────────────────────────────────────

func TestAnalyzeThermal_Table(t *testing.T) {
	tests := []struct {
		name    string
		thermal types.ThermalInfo
		wantIDs map[string]types.Severity
		forbid  []string
	}{
		{
			name: "gpu_idle decoded by the collector is not a slowdown",
			thermal: types.ThermalInfo{TemperatureC: 38, PowerState: "P8", SlowdownActive: false,
				SlowdownReason: "0x0000000000000001", ThrottleReasons: []string{"gpu_idle"}, FanSupported: true, FanSpeedPct: 0},
			wantIDs: map[string]types.Severity{},
			forbid:  []string{"thermal-throttling", "gpu-clock-slowdown", "gpu-power-state-stuck", "fan-not-spinning"},
		},
		{
			name: "hw_thermal_slowdown bit is CRIT",
			thermal: types.ThermalInfo{TemperatureC: 91, PowerState: "P0", UtilizationPct: 99, ThermalThrottle: true, SlowdownActive: true,
				SlowdownReason: "0x0000000000000040", ThrottleReasons: []string{"hw_thermal_slowdown"}, FanSupported: true, FanSpeedPct: 80},
			wantIDs: map[string]types.Severity{"thermal-throttling": types.SeverityCrit},
			forbid:  []string{"gpu-clock-slowdown", "gpu-running-hot"},
		},
		{
			name: "sw_power_cap alone is the INFO power-cap note, not a slowdown WARN",
			thermal: types.ThermalInfo{TemperatureC: 70, PowerState: "P0", UtilizationPct: 99, SlowdownActive: true,
				SlowdownReason: "0x0000000000000004", ThrottleReasons: []string{"sw_power_cap"}, FanSupported: true, FanSpeedPct: 60},
			wantIDs: map[string]types.Severity{"gpu-power-cap": types.SeverityInfo},
			forbid:  []string{"thermal-throttling", "gpu-clock-slowdown"},
		},
		{
			name: "hw_slowdown is a WARN even when the power cap is also active",
			thermal: types.ThermalInfo{TemperatureC: 70, PowerState: "P0", UtilizationPct: 99, SlowdownActive: true,
				SlowdownReason: "0x000000000000000c", ThrottleReasons: []string{"sw_power_cap", "hw_slowdown"}, FanSupported: true, FanSpeedPct: 60},
			wantIDs: map[string]types.Severity{"gpu-clock-slowdown": types.SeverityWarn},
			forbid:  []string{"thermal-throttling", "gpu-power-cap"},
		},
		{
			name: "old collector ThermalThrottle at 85C without thermal bits is only running hot",
			thermal: types.ThermalInfo{TemperatureC: 86, PowerState: "P0", UtilizationPct: 99, ThermalThrottle: true,
				SlowdownReason: "0x0000000000000000", FanSupported: true, FanSpeedPct: 70},
			wantIDs: map[string]types.Severity{"gpu-running-hot": types.SeverityWarn},
			forbid:  []string{"thermal-throttling"},
		},
		{
			name:    "93C is CRIT running hot",
			thermal: types.ThermalInfo{TemperatureC: 94, PowerState: "P0", UtilizationPct: 99, FanSupported: true, FanSpeedPct: 100},
			wantIDs: map[string]types.Severity{"gpu-running-hot": types.SeverityCrit},
		},
		{
			name:    "84C produces no temperature finding",
			thermal: types.ThermalInfo{TemperatureC: 84, PowerState: "P0", UtilizationPct: 99, FanSupported: true, FanSpeedPct: 100},
			wantIDs: map[string]types.Severity{},
			forbid:  []string{"gpu-running-hot", "thermal-throttling"},
		},
		{
			name:    "P8 at idle is not stuck",
			thermal: types.ThermalInfo{TemperatureC: 40, PowerState: "P8", UtilizationPct: 3, CurrentClockMHz: 210, MaxClockMHz: 2100},
			wantIDs: map[string]types.Severity{},
			forbid:  []string{"gpu-power-state-stuck"},
		},
		{
			name:    "P8 at 80% utilization is stuck",
			thermal: types.ThermalInfo{TemperatureC: 55, PowerState: "P8", UtilizationPct: 80, CurrentClockMHz: 210, MaxClockMHz: 2100},
			wantIDs: map[string]types.Severity{"gpu-power-state-stuck": types.SeverityWarn},
		},
		{
			name:    "P2 under load is fine",
			thermal: types.ThermalInfo{TemperatureC: 55, PowerState: "P2", UtilizationPct: 100},
			wantIDs: map[string]types.Severity{},
			forbid:  []string{"gpu-power-state-stuck"},
		},
		{
			name:    "fan 0% at 70C on a passive card is not a finding",
			thermal: types.ThermalInfo{TemperatureC: 70, FanSupported: false, FanSpeedPct: 0},
			wantIDs: map[string]types.Severity{},
			forbid:  []string{"fan-not-spinning"},
		},
		{
			name:    "fan 0% at 70C on an active card is a WARN",
			thermal: types.ThermalInfo{TemperatureC: 70, FanSupported: true, FanSpeedPct: 0},
			wantIDs: map[string]types.Severity{"fan-not-spinning": types.SeverityWarn},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := tt.thermal
			findings := analyzeThermal(&types.Report{Thermal: &th})
			for id, sev := range tt.wantIDs {
				f := findByID(findings, id)
				if f == nil {
					t.Errorf("expected finding %s, got %v", id, ids(findings))
					continue
				}
				if f.Severity != sev {
					t.Errorf("%s: got severity %s, want %s", id, f.Severity, sev)
				}
			}
			for _, id := range tt.forbid {
				if findByID(findings, id) != nil {
					t.Errorf("finding %s must not fire, got %v", id, ids(findings))
				}
			}
			if len(tt.wantIDs) == 0 && len(tt.forbid) > 0 && len(findings) != 0 {
				t.Errorf("expected no findings at all, got %v", ids(findings))
			}
		})
	}
}

func TestAnalyzeThermal_SlowdownNamesReasons(t *testing.T) {
	// The collector decodes the mask; the analyzer only reports its names.
	th := &types.ThermalInfo{TemperatureC: 70, SlowdownActive: true, SlowdownReason: "0x0000000000000084",
		ThrottleReasons: []string{"sw_power_cap", "hw_power_brake_slowdown"}}
	findings := analyzeThermal(&types.Report{Thermal: th})
	f := findByID(findings, "gpu-clock-slowdown")
	if f == nil {
		t.Fatalf("expected gpu-clock-slowdown, got %v", ids(findings))
	}
	if !strings.Contains(f.Evidence, "sw_power_cap") || !strings.Contains(f.Evidence, "hw_power_brake_slowdown") {
		t.Errorf("evidence should name the collector-decoded reasons, got %q", f.Evidence)
	}
}

func TestAnalyzeThermal_TrustsCollectorNotRawMask(t *testing.T) {
	// A decimal raw mask ("4") used to be re-parsed here as hex and disagree
	// with the collector. The analyzer must now follow the collector's fields
	// regardless of how the raw string is spelled.
	th := &types.ThermalInfo{TemperatureC: 70, PowerState: "P0", UtilizationPct: 99,
		SlowdownActive: true, SlowdownReason: "4", ThrottleReasons: []string{"sw_power_cap"}}
	findings := analyzeThermal(&types.Report{Thermal: th})
	if findByID(findings, "gpu-power-cap") == nil || findByID(findings, "gpu-clock-slowdown") != nil || findByID(findings, "thermal-throttling") != nil {
		t.Errorf("expected only the collector's sw_power_cap note, got %v", ids(findings))
	}
	quiet := &types.ThermalInfo{TemperatureC: 40, PowerState: "P8", SlowdownReason: "0x0000000000000020"}
	if got := analyzeThermal(&types.Report{Thermal: quiet}); len(got) != 0 {
		t.Errorf("raw mask alone must not be re-decoded by the analyzer, got %v", ids(got))
	}
}

// ── C4: CUDA / PyTorch version direction ──────────────────────────────

func TestAnalyzeCUDA_Table(t *testing.T) {
	tests := []struct {
		name    string
		toolkit string
		driver  string
		torch   *types.PyTorchInfo
		// want maps every expected finding id to its severity; an empty map
		// means no findings at all.
		want map[string]types.Severity
	}{
		{
			name: "toolkit 12.4 with driver 13.1 is the normal supported case", toolkit: "12.4", driver: "13.1",
		},
		{
			name: "toolkit 13.0 with driver 12.4 is a mismatch", toolkit: "13.0", driver: "12.4",
			want: map[string]types.Severity{"cuda-mismatch": types.SeverityWarn},
		},
		{
			// CUDA minor-version compatibility: a 12.6 toolkit runs on any
			// 12.x driver, so this is informational rather than a WARN.
			name: "toolkit 12.6 with driver 12.4 is minor-version compatible (INFO)", toolkit: "12.6", driver: "12.4",
			want: map[string]types.Severity{"cuda-toolkit-minor-newer": types.SeverityInfo},
		},
		{
			name: "equal versions produce nothing", toolkit: "12.4", driver: "12.4",
		},
		{
			name: "torch cu128 with driver 12.4 and CUDA unavailable is newer than driver", driver: "12.4",
			torch: &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8", CUDAAvailable: false},
			want:  map[string]types.Severity{"pytorch-cuda-newer-than-driver": types.SeverityWarn},
		},
		{
			// The WARN is gated on the symptom: a cu128 wheel that reports
			// CUDA available on a 12.4 driver is a normal working setup.
			name: "torch 12.8 on driver 12.4 with CUDA available produces nothing", driver: "12.4",
			torch: &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8", CUDAAvailable: true},
		},
		{
			name: "torch cu130 on driver 12.4 with CUDA available is a low-confidence INFO", driver: "12.4",
			torch: &types.PyTorchInfo{Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: true},
			want:  map[string]types.Severity{"pytorch-cuda-newer-but-working": types.SeverityInfo},
		},
		{
			name: "torch cu130 on driver 12.4 with CUDA unavailable is a WARN", driver: "12.4",
			torch: &types.PyTorchInfo{Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: false},
			want:  map[string]types.Severity{"pytorch-cuda-newer-than-driver": types.SeverityWarn},
		},
		{
			name: "torch cu118 on a 13.x driver is fine", driver: "13.1",
			torch: &types.PyTorchInfo{Version: "2.5.1+cu118", CUDAVersion: "11.8", CUDAAvailable: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &types.Report{
				Driver: types.DriverInfo{Version: "591.86", CUDAVersion: tt.driver},
				AI:     &types.AIInfo{CUDAToolkitVersion: tt.toolkit, PyTorchInfo: tt.torch},
			}
			findings := analyzeCUDA(report)
			for id, sev := range tt.want {
				f := findByID(findings, id)
				if f == nil {
					t.Errorf("expected %s, got %v", id, ids(findings))
					continue
				}
				if f.Severity != sev {
					t.Errorf("%s: expected %s, got %s", id, sev, f.Severity)
				}
			}
			if len(findings) != len(tt.want) {
				t.Errorf("unexpected findings: got %v, want %d finding(s)", ids(findings), len(tt.want))
			}
		})
	}
}

func TestAnalyzeCUDA_TorchAvailableNeverContradictsItself(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{CUDAVersion: "12.4"},
		AI:     &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8", CUDAAvailable: true, DeviceName: "RTX 3090"}},
	}
	all := append(analyzeCUDA(report), analyzePyTorch(report)...)
	if findByID(all, "pytorch-cuda-ok") == nil {
		t.Errorf("expected pytorch-cuda-ok, got %v", ids(all))
	}
	if findByID(all, "pytorch-cuda-newer-than-driver") != nil {
		t.Errorf("a working torch install must not also be reported as broken: %v", ids(all))
	}
	for _, f := range all {
		if f.Severity != types.SeverityInfo {
			t.Errorf("healthy torch setup produced non-INFO finding %s (%s)", f.ID, f.Severity)
		}
	}
}

func TestAnalyzeCUDA_MinorNewerToolkitIsInfoWithConfidence50(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{CUDAVersion: "12.4"},
		AI:     &types.AIInfo{CUDAToolkitVersion: "12.6"},
	}
	f := findByID(analyzeCUDA(report), "cuda-toolkit-minor-newer")
	if f == nil {
		t.Fatal("expected cuda-toolkit-minor-newer")
	}
	if f.Confidence != 50 {
		t.Errorf("confidence = %d, want 50", f.Confidence)
	}
	if !strings.Contains(strings.ToLower(f.WhyItMatters), "minor-version compatibility") {
		t.Errorf("WhyItMatters should explain minor-version compatibility, got %q", f.WhyItMatters)
	}
}

func TestAnalyzeCUDA_TorchNewerHasPipHint(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{CUDAVersion: "12.4"},
		AI:     &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8"}},
	}
	f := findByID(analyzeCUDA(report), "pytorch-cuda-newer-than-driver")
	if f == nil {
		t.Fatal("expected pytorch-cuda-newer-than-driver")
	}
	if f.Confidence != 80 {
		t.Errorf("confidence = %d, want 80", f.Confidence)
	}
	joined := strings.Join(f.NextSteps, "\n")
	if !strings.Contains(joined, "pip install") || !strings.Contains(joined, "whl/cu124") {
		t.Errorf("next steps should contain a pip reinstall hint for cu124, got %q", joined)
	}
	// The generic "GPU not accessible" finding must be suppressed since the cause is known.
	if findByID(analyzePyTorch(report), "pytorch-cuda-no-gpu") != nil {
		t.Error("pytorch-cuda-no-gpu should be suppressed when the CUDA build is newer than the driver")
	}
}

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"12.4", 12, 4, true},
		{"12.4.1", 12, 4, true},
		{"13", 13, 0, true},
		{"11.8-rc1", 11, 8, true},
		{"", 0, 0, false},
		{"cu118", 0, 0, false},
	}
	for _, tt := range tests {
		major, minor, ok := parseMajorMinor(tt.in)
		if major != tt.major || minor != tt.minor || ok != tt.ok {
			t.Errorf("parseMajorMinor(%q) = (%d, %d, %v), want (%d, %d, %v)", tt.in, major, minor, ok, tt.major, tt.minor, tt.ok)
		}
	}
}

// ── C5: Network ───────────────────────────────────────────────────────

func TestAnalyzeNetwork_NoSamplesProducesNothing(t *testing.T) {
	report := &types.Report{Network: &types.NetworkInfo{InterfaceName: "Ethernet", InterfaceType: "ethernet"}}
	if findings := analyzeNetwork(report); len(findings) != 0 {
		t.Errorf("expected no findings for an all-zero network sample, got %v", ids(findings))
	}
	if findings := analyzeNetwork(&types.Report{}); len(findings) != 0 {
		t.Errorf("expected no findings when Network is nil, got %v", ids(findings))
	}
}

func TestAnalyzeNetwork_HealthyWithSamples(t *testing.T) {
	report := &types.Report{Network: &types.NetworkInfo{
		InterfaceType: "ethernet", LatencyMs: 12.3, JitterMs: 1.2, DNSTimeMs: 20,
		Hops: []types.HopInfo{{Number: 1, LatencyMs: 1}},
	}}
	findings := analyzeNetwork(report)
	if len(findings) != 1 || findings[0].ID != "network-healthy" {
		t.Errorf("expected only network-healthy, got %v", ids(findings))
	}
	if strings.Contains(findings[0].Evidence, "Latency: 0.0 ms") {
		t.Errorf("healthy evidence must carry the measured latency, got %q", findings[0].Evidence)
	}
}

func TestAnalyzeNetwork_HopsWithoutPingIsNotHealthy(t *testing.T) {
	// traceroute worked, ping failed: no latency/jitter/loss verdict is possible.
	report := &types.Report{Network: &types.NetworkInfo{
		InterfaceType: "ethernet", JitterMs: 40, DNSTimeMs: 20,
		Hops: []types.HopInfo{{Number: 1, LatencyMs: 1}, {Number: 2, LatencyMs: 9}},
	}}
	findings := analyzeNetwork(report)
	if findByID(findings, "network-healthy") != nil {
		t.Errorf("hops alone must not produce network-healthy, got %v", ids(findings))
	}
	if findByID(findings, "high-jitter") != nil {
		t.Errorf("jitter rule must not fire without ping samples, got %v", ids(findings))
	}
	f := findByID(findings, "network-ping-unavailable")
	if f == nil {
		t.Fatalf("expected network-ping-unavailable, got %v", ids(findings))
	}
	if f.Severity != types.SeverityInfo || !strings.Contains(f.Evidence, "Ping produced no samples; latency, jitter and loss could not be measured") {
		t.Errorf("unexpected ping-unavailable finding: %+v", *f)
	}

	// Loss with zero latency is still ping evidence (every probe was lost).
	// With DNS also failing, that really is packet loss.
	lossOnly := &types.Report{Network: &types.NetworkInfo{PacketLossPct: 100, Hops: []types.HopInfo{{Number: 1}}}}
	got := analyzeNetwork(lossOnly)
	if findByID(got, "packet-loss") == nil || findByID(got, "network-ping-unavailable") != nil {
		t.Errorf("100%% loss is ping evidence, got %v", ids(got))
	}
}

// Every probe lost while DNS worked is ICMP filtering (GitHub runners, most
// corporate VPNs), not packet loss. This was observed live on ubuntu-24.04 and
// ubuntu-24.04-arm runners, where the analyzer used to raise a WARN.
func TestAnalyzeNetwork_AllLostWithWorkingDNSIsICMPFiltered(t *testing.T) {
	filtered := &types.Report{Network: &types.NetworkInfo{InterfaceName: "eth0", InterfaceType: "ethernet", PacketLossPct: 100, DNSTimeMs: 22.33}}
	got := analyzeNetwork(filtered)
	f := findByID(got, "icmp-filtered")
	if f == nil {
		t.Fatalf("expected icmp-filtered, got %v", ids(got))
	}
	if f.Severity != types.SeverityInfo {
		t.Errorf("icmp-filtered must be INFO, got %s", f.Severity)
	}
	if findByID(got, "packet-loss") != nil || findByID(got, "network-healthy") != nil {
		t.Errorf("filtered ICMP must not be reported as packet loss or as healthy, got %v", ids(got))
	}

	// Partial loss with working DNS is still real packet loss.
	partial := &types.Report{Network: &types.NetworkInfo{InterfaceName: "eth0", InterfaceType: "ethernet", LatencyMs: 12, PacketLossPct: 30, DNSTimeMs: 20}}
	got = analyzeNetwork(partial)
	if findByID(got, "packet-loss") == nil || findByID(got, "icmp-filtered") != nil {
		t.Errorf("30%% loss is packet loss, got %v", ids(got))
	}
}

func TestAnalyzeNetwork_WifiCongestionNeedsSignal(t *testing.T) {
	noSignal := &types.Report{Network: &types.NetworkInfo{InterfaceType: "wifi", WifiBand: "2.4GHz", WifiSignalDBM: 0, LatencyMs: 20}}
	if findByID(analyzeNetwork(noSignal), "wifi-congestion") != nil {
		t.Error("wifi-congestion must not fire when the signal strength is unknown (0 dBm)")
	}
	ethernet := &types.Report{Network: &types.NetworkInfo{InterfaceType: "ethernet", WifiBand: "2.4GHz", WifiSignalDBM: -80, LatencyMs: 20}}
	if findByID(analyzeNetwork(ethernet), "wifi-congestion") != nil {
		t.Error("wifi-congestion must not fire on ethernet")
	}
	weak := &types.Report{Network: &types.NetworkInfo{InterfaceType: "wifi", WifiBand: "2.4GHz", WifiSignalDBM: -80, LatencyMs: 20}}
	f := findByID(analyzeNetwork(weak), "wifi-congestion")
	if f == nil || f.Confidence != 75 {
		t.Errorf("expected wifi-congestion with confidence 75 for weak 2.4GHz, got %+v", f)
	}
}

// ── C6: IDs ───────────────────────────────────────────────────────────

// syntheticBusyReport triggers as many rules as possible at once.
func syntheticBusyReport() *types.Report {
	now := time.Now()
	return &types.Report{
		Metadata: types.ReportMetadata{ToolVersion: "0.2.1", Timestamp: now},
		System:   types.SystemInfo{OSName: "Microsoft Windows 11 Enterprise Edition Long Name", OSVersion: "10.0.26200", KernelVersion: "6.8.0-45-generic-with-long-suffix", Architecture: "amd64"},
		GPUs: []types.GPUInfo{
			{Name: "NVIDIA GeForce GTX 1050", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 2048, PCIBusID: "00000000:01:00.0"},
			{Name: "Intel UHD 770", Vendor: "Intel"},
		},
		Driver: types.DriverInfo{Version: "591.86", CUDAVersion: "12.4"},
		Windows: &types.WindowsInfo{
			HAGSEnabled:       "Enabled",
			GameMode:          "Enabled",
			PowerPlan:         "Balanced",
			DriverResetEvents: wheaEvents(3, 4101, "Warning", "Display driver nvlddmkm stopped responding"),
			NvlddmkmErrors:    wheaEvents(1, 14, "Error", "nvlddmkm error"),
			WHEAErrors:        append(wheaEvents(2, 17, "Warning", nicWHEAMessage), wheaEvents(1, 18, "Error", "fatal")...),
			RecentKBs:         []types.WindowsUpdate{{KBID: "KB5000001", InstalledOn: now}},
			NVIDIAAppVersion:  "11.0.1",
			OverlaySoftware:   []string{"Discord", "MSI Afterburner"},
		},
		Linux: &types.LinuxInfo{
			LoadedModules:    map[string]bool{"nouveau": true, "nvidia": false},
			DKMSErrors:       "Error! Build of nvidia.ko failed",
			SecureBootState:  "Enabled",
			SessionType:      "wayland",
			XidErrors:        []types.XidError{{Code: 79, Message: "GPU has fallen off the bus", Count: 2}},
			LlvmpipeFallback: true,
		},
		WSL: &types.WSLInfo{IsWSL: true, DevDxgExists: false},
		AI: &types.AIInfo{
			CUDAToolkitVersion: "13.0",
			PyTorchInfo:        &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8", CUDAAvailable: false},
			TensorFlowInfo:     &types.TFInfo{Version: "2.16.1"},
		},
		Thermal: &types.ThermalInfo{TemperatureC: 86, PowerState: "P8", UtilizationPct: 80, SlowdownActive: true, SlowdownReason: "0x0000000000000004", ThrottleReasons: []string{"sw_power_cap"}, FanSupported: true, FanSpeedPct: 0},
		PCIe:    &types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x8", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 80},
		Displays: []types.DisplayInfo{
			{Name: "A", RefreshHz: 144}, {Name: "B", RefreshHz: 60}, {Name: "C", RefreshHz: 60},
		},
		Network: &types.NetworkInfo{InterfaceType: "wifi", WifiBand: "2.4GHz", WifiSignalDBM: -75, LatencyMs: 40, JitterMs: 22, PacketLossPct: 2, DNSTimeMs: 150},
	}
}

var kebabCase = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestAnalyze_AllFindingsHaveUniqueKebabIDs(t *testing.T) {
	report := syntheticBusyReport()
	Analyze(report, types.ModeFull)

	if len(report.Findings) < 25 {
		t.Fatalf("synthetic report should trigger many rules, got only %d: %v", len(report.Findings), ids(report.Findings))
	}
	seen := map[string]string{}
	for _, f := range report.Findings {
		if f.ID == "" {
			t.Errorf("finding %q has an empty ID", f.Title)
			continue
		}
		if !kebabCase.MatchString(f.ID) {
			t.Errorf("finding ID %q is not kebab-case", f.ID)
		}
		if prev, dup := seen[f.ID]; dup {
			t.Errorf("duplicate finding ID %q (%q and %q)", f.ID, prev, f.Title)
		}
		seen[f.ID] = f.Title
	}
}

func TestAnalyze_EveryAnalyzerAssignsIDs(t *testing.T) {
	// Run the busy report through each mode so that every analyzer executes at least once.
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeAI, types.ModeCreator, types.ModeFull} {
		report := syntheticBusyReport()
		Analyze(report, mode)
		for _, f := range report.Findings {
			if f.ID == "" {
				t.Errorf("mode %s: finding %q has an empty ID", mode, f.Title)
			}
		}
	}
	// And the rules that the busy report cannot trigger at the same time.
	extra := [][]types.Finding{
		analyzeGPUPresence(&types.Report{}),
		analyzeDriverBasics(&types.Report{}),
		analyzeStreaming(&types.Report{}),
		analyzeSecureBoot(&types.Report{Linux: &types.LinuxInfo{SecureBootState: "Enabled", LoadedModules: map[string]bool{"nvidia": true}}}),
		analyzeLinuxModules(&types.Report{Linux: &types.LinuxInfo{LoadedModules: map[string]bool{"nvidia": true}, LibCudaPath: "/usr/lib/libcuda.so"}}),
		analyzePyTorch(&types.Report{AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1", CUDAVersion: "12.1", CUDAAvailable: true}}}),
		analyzePyTorch(&types.Report{AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1"}}}),
		analyzePyTorch(&types.Report{AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Error: "boom"}}}),
		analyzePyTorch(&types.Report{Driver: types.DriverInfo{CUDAVersion: "12.8"}, AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1", CUDAVersion: "12.1"}}}),
		analyzeTensorFlow(&types.Report{AI: &types.AIInfo{TensorFlowInfo: &types.TFInfo{Version: "2.16", GPUs: []string{"/GPU:0"}}}}),
		analyzeTensorFlow(&types.Report{AI: &types.AIInfo{TensorFlowInfo: &types.TFInfo{Error: "boom"}}}),
		analyzeWSL(&types.Report{WSL: &types.WSLInfo{IsWSL: true, DevDxgExists: true, NvidiaSmiOK: false}}),
		analyzeThermal(&types.Report{Thermal: &types.ThermalInfo{TemperatureC: 95, ThermalThrottle: true, SlowdownActive: true, SlowdownReason: "0x40", ThrottleReasons: []string{"hw_thermal_slowdown"}}}),
		analyzeThermal(&types.Report{Thermal: &types.ThermalInfo{TemperatureC: 95}}),
		analyzePCIe(&types.Report{PCIe: &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", IdleLikely: true}}),
		analyzePCIe(&types.Report{PCIe: &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99}}),
		analyzeNetwork(&types.Report{Network: &types.NetworkInfo{LatencyMs: 10}}),
		analyzeNetwork(&types.Report{Network: &types.NetworkInfo{Hops: []types.HopInfo{{Number: 1}}}}),
		analyzeOverlays(&types.Report{Windows: &types.WindowsInfo{GFEVersion: "3.28"}}),
	}
	for _, findings := range extra {
		for _, f := range findings {
			if f.ID == "" || !kebabCase.MatchString(f.ID) {
				t.Errorf("finding %q has bad ID %q", f.Title, f.ID)
			}
		}
	}
}

// ── C7: ordering and next steps ───────────────────────────────────────

func TestSortFindings_StableByConfidenceAndTitle(t *testing.T) {
	findings := []types.Finding{
		{ID: "a", Severity: types.SeverityInfo, Confidence: 90, Title: "Zeta"},
		{ID: "b", Severity: types.SeverityWarn, Confidence: 50, Title: "Beta"},
		{ID: "c", Severity: types.SeverityCrit, Confidence: 60, Title: "Gamma"},
		{ID: "d", Severity: types.SeverityWarn, Confidence: 90, Title: "Delta"},
		{ID: "e", Severity: types.SeverityWarn, Confidence: 50, Title: "Alpha"},
		{ID: "f", Severity: types.SeverityCrit, Confidence: 95, Title: "Omega"},
	}
	sortFindings(findings)
	want := []string{"f", "c", "d", "e", "b", "a"}
	got := ids(findings)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sort order = %v, want %v", got, want)
	}

	// Stability: identical keys keep insertion order across repeated sorts.
	same := []types.Finding{
		{ID: "first", Severity: types.SeverityWarn, Confidence: 70, Title: "Same"},
		{ID: "second", Severity: types.SeverityWarn, Confidence: 70, Title: "Same"},
	}
	for i := 0; i < 3; i++ {
		sortFindings(same)
	}
	if same[0].ID != "first" || same[1].ID != "second" {
		t.Errorf("sort is not stable: %v", ids(same))
	}
}

func TestBuildNextSteps_RoundRobin(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityCrit, NextSteps: []string{"A1", "A2", "A3"}},
		{Severity: types.SeverityWarn, NextSteps: []string{"B1", "B2", "B3"}},
		{Severity: types.SeverityInfo, NextSteps: []string{"I1", "I2"}},
	}
	got := buildNextSteps(findings)
	want := []string{"A1", "B1", "A2", "B2", "A3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("next steps = %v, want %v", got, want)
	}
	for _, s := range got {
		if strings.HasPrefix(s, "I") {
			t.Errorf("INFO steps must not appear when CRIT/WARN exist: %v", got)
		}
	}
}

func TestBuildNextSteps_DedupesCaseInsensitively(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityWarn, NextSteps: []string{"Reseat the GPU.", "Update BIOS."}},
		{Severity: types.SeverityWarn, NextSteps: []string{"reseat the gpu.", "Check cables."}},
	}
	got := buildNextSteps(findings)
	want := []string{"Reseat the GPU.", "Update BIOS.", "Check cables."}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("next steps = %v, want %v", got, want)
	}
}

func TestBuildNextSteps_InfoOnlyFallback(t *testing.T) {
	findings := []types.Finding{
		{Severity: types.SeverityInfo, NextSteps: []string{"I1", "I2"}},
		{Severity: types.SeverityInfo, NextSteps: []string{"J1"}},
	}
	got := buildNextSteps(findings)
	want := []string{"I1", "J1", "I2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("next steps = %v, want %v", got, want)
	}

	if got := buildNextSteps(nil); len(got) != 1 || !strings.Contains(got[0], "No immediate action") {
		t.Errorf("empty findings should yield the healthy message, got %v", got)
	}

	// Placeholder steps from healthy INFO findings must not displace real ones.
	placeholders := []types.Finding{
		{Severity: types.SeverityInfo, NextSteps: []string{"No action needed."}},
		{Severity: types.SeverityInfo, NextSteps: []string{"No network action needed. Issue may be external to your network."}},
		{Severity: types.SeverityInfo, NextSteps: []string{"Re-run under GPU load to verify the link reaches Gen4."}},
	}
	got = buildNextSteps(placeholders)
	if len(got) != 1 || !strings.HasPrefix(got[0], "Re-run under GPU load") {
		t.Errorf("placeholder steps should be skipped, got %v", got)
	}
	onlyPlaceholders := []types.Finding{{Severity: types.SeverityInfo, NextSteps: []string{"No action needed."}}}
	if got := buildNextSteps(onlyPlaceholders); len(got) != 1 || !strings.Contains(got[0], "No immediate action") {
		t.Errorf("only placeholders should yield the healthy message, got %v", got)
	}
}

// ── C8: HAGS / Game Mode / power plan ─────────────────────────────────

func TestWindowsPerfSettings_DefaultsProduceNothing(t *testing.T) {
	report := &types.Report{Windows: &types.WindowsInfo{
		HAGSEnabled: "Default (not configured)",
		GameMode:    "Default (not configured)",
		PowerPlan:   "High performance",
	}}
	if findings := analyzeWindowsPerfSettings(report); len(findings) != 0 {
		t.Errorf("defaults should not produce findings, got %v", ids(findings))
	}
	report.Windows.GameMode = "Enabled"
	report.Windows.HAGSEnabled = "Disabled"
	if findings := analyzeWindowsPerfSettings(report); len(findings) != 0 {
		t.Errorf("Game Mode enabled is the Win11 default and must not produce findings, got %v", ids(findings))
	}
}

func TestPowerPlanBalanced_OnlyInGamingStreamingFull(t *testing.T) {
	base := func() *types.Report {
		return &types.Report{
			GPUs:    []types.GPUInfo{{Name: "RTX 3090", IsNVIDIA: true}},
			Driver:  types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
			Windows: &types.WindowsInfo{PowerPlan: "Balanced", HAGSEnabled: "Enabled"},
		}
	}
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeFull} {
		r := base()
		Analyze(r, mode)
		f := findByID(r.Findings, "power-plan-suboptimal")
		if f == nil || f.Severity != types.SeverityInfo {
			t.Errorf("mode %s: expected INFO power-plan-suboptimal, got %v", mode, ids(r.Findings))
		}
		if findByID(r.Findings, "hags-enabled") == nil {
			t.Errorf("mode %s: expected hags-enabled, got %v", mode, ids(r.Findings))
		}
	}
	for _, mode := range []types.RunMode{types.ModeCreator, types.ModeAI} {
		r := base()
		Analyze(r, mode)
		if findByID(r.Findings, "power-plan-suboptimal") != nil || findByID(r.Findings, "hags-enabled") != nil {
			t.Errorf("mode %s: power plan / HAGS rules must not run, got %v", mode, ids(r.Findings))
		}
	}
}

// ── C9: mode gating ───────────────────────────────────────────────────

func TestAnalyze_ModeGating(t *testing.T) {
	report := func() *types.Report {
		r := syntheticBusyReport()
		return r
	}

	ai := report()
	Analyze(ai, types.ModeAI)
	if findByID(ai.Findings, "wsl-no-dxg") == nil {
		t.Errorf("ai mode should analyze WSL, got %v", ids(ai.Findings))
	}
	if findByID(ai.Findings, "driver-resets-4101") != nil {
		t.Errorf("ai mode does not collect Windows info and must not analyze it")
	}

	streaming := report()
	Analyze(streaming, types.ModeStreaming)
	if findByID(streaming.Findings, "mixed-refresh-rate") == nil {
		t.Errorf("streaming mode collects displays and should analyze them, got %v", ids(streaming.Findings))
	}

	creator := report()
	Analyze(creator, types.ModeCreator)
	if findByID(creator.Findings, "mixed-refresh-rate") != nil {
		t.Errorf("creator mode does not collect displays and must not analyze them")
	}
	if findByID(creator.Findings, "cuda-mismatch") == nil || findByID(creator.Findings, "driver-resets-4101") == nil {
		t.Errorf("creator mode should analyze AI and Windows info, got %v", ids(creator.Findings))
	}

	// Network analysis is independent of mode: it runs whenever probes ran.
	for _, mode := range []types.RunMode{types.ModeAI, types.ModeCreator} {
		r := report()
		Analyze(r, mode)
		if findByID(r.Findings, "high-jitter") == nil {
			t.Errorf("mode %s: network findings should appear when probe data exists", mode)
		}
	}
}

// ── C10: summary block ────────────────────────────────────────────────

func TestSummaryBlock_TopLineAndWidth(t *testing.T) {
	report := syntheticBusyReport()
	report.AI.PyTorchInfo = &types.PyTorchInfo{Version: "2.5.1+cu118", CUDAVersion: "11.8", CUDAAvailable: true}
	Analyze(report, types.ModeFull)

	if !strings.Contains(report.SummaryBlock, "PyTorch: 2.5.1+cu118 (CUDA available)") {
		t.Errorf("summary should include the PyTorch line:\n%s", report.SummaryBlock)
	}
	if !strings.Contains(report.SummaryBlock, "\nTop: ") {
		t.Errorf("summary should include a Top: line:\n%s", report.SummaryBlock)
	}
	for _, line := range strings.Split(strings.TrimRight(report.SummaryBlock, "\n"), "\n") {
		if n := len([]rune(line)); n > 72 {
			t.Errorf("summary line is %d runes (> 72): %q", n, line)
		}
	}
	for _, want := range []string{"NVCheckup v", "OS: ", "GPU: ", "CUDA (driver): ", "Temp: ", "PCIe: ", "Findings: "} {
		if !strings.Contains(report.SummaryBlock, want) {
			t.Errorf("summary missing %q:\n%s", want, report.SummaryBlock)
		}
	}
}

func TestSummaryBlock_TopListsTwoActionableTitles(t *testing.T) {
	report := &types.Report{
		GPUs:   []types.GPUInfo{{Name: "RTX 3090", IsNVIDIA: true}},
		Driver: types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
		PCIe:   &types.PCIeInfo{CurrentSpeed: "Gen4", MaxSpeed: "Gen4", CurrentWidth: "x8", MaxWidth: "x16"},
	}
	Analyze(report, types.ModeFull)
	if !strings.Contains(report.SummaryBlock, "Top: PCIe Link Width Reduced") {
		t.Errorf("summary Top line should name the WARN finding:\n%s", report.SummaryBlock)
	}

	healthy := &types.Report{
		GPUs:   []types.GPUInfo{{Name: "RTX 3090", IsNVIDIA: true}},
		Driver: types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
	}
	Analyze(healthy, types.ModeFull)
	if strings.Contains(healthy.SummaryBlock, "Top:") {
		t.Errorf("no Top line expected when nothing is actionable:\n%s", healthy.SummaryBlock)
	}
}

// ── C8: power plan name classification ────────────────────────────────

func TestPowerPlanSuboptimal_Table(t *testing.T) {
	tests := []struct {
		plan string
		want bool
	}{
		{"Balanced", true},
		{"Power saver", true},
		{"Balanced (recommended)", true},
		{"381b4222-f694-41f0-9685-ff5bb260df2e", true}, // Balanced GUID
		{"a1841308-3541-4fab-bc81-f71556f20b4a", true}, // Power saver GUID
		{"Equilibrado", true},                          // es/pt Balanced
		{"Ausbalanciert", true},                        // de Balanced
		{"High performance", false},
		{"Ultimate Performance", false},
		{"Höchstleistung", false},   // de High performance
		{"Alto rendimiento", false}, // es High performance
		{"N/A", false},
		{"n/a", false},
		{"Not available", false},
		{"", false},
		{"Unknown", false},
		{"Default (not configured)", false},
		{"ASUS Turbo Mode", false}, // unrecognised OEM plan
	}
	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			if got := powerPlanSuboptimal(tt.plan); got != tt.want {
				t.Errorf("powerPlanSuboptimal(%q) = %v, want %v", tt.plan, got, tt.want)
			}
		})
	}
}

func TestPowerPlanNA_NoFinding(t *testing.T) {
	report := &types.Report{Windows: &types.WindowsInfo{PowerPlan: "N/A", HAGSEnabled: "Disabled"}}
	if f := findByID(analyzeWindowsPerfSettings(report), "power-plan-suboptimal"); f != nil {
		t.Errorf("an unreadable power plan must not be reported as suboptimal: %+v", f)
	}
}
