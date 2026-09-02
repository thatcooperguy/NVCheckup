package common

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Captured from an RTX 3090 on driver 591.86 at idle (ThermalQueryFields order:
// index, temperature, pstate, current clock, max clock, power limit, power
// draw, fan, utilization).
const thermalSampleIdle = "0, 43, P8, 210, 2100, 350.00, 33.62, 0, 28"

func TestParseThermalCSV_RealSample(t *testing.T) {
	info, errs := parseThermalCSV(thermalSampleIdle)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := types.ThermalInfo{
		GPUIndex:        0,
		TemperatureC:    43,
		PowerState:      "P8",
		CurrentClockMHz: 210,
		MaxClockMHz:     2100,
		PowerLimitW:     "350.00",
		PowerDrawW:      "33.62",
		FanSpeedPct:     0,
		FanSupported:    true,
		UtilizationPct:  28,
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("parseThermalCSV(%q)\n got %+v\nwant %+v", thermalSampleIdle, info, want)
	}
}

// TestParseThermalCSV_GPUClasses runs realistic rows from very different
// NVIDIA products through the parser. None of them may produce an error, and
// the fan/utilization placeholders that data-center, passive and MIG parts
// report must not be mistaken for a stalled fan or an idle GPU.
func TestParseThermalCSV_GPUClasses(t *testing.T) {
	tests := []struct {
		name string
		line string
		want types.ThermalInfo
	}{
		{
			name: "GeForce RTX 5090 under load (Blackwell, 575 W)",
			line: "0, 71, P0, 2865, 3090, 575.00, 561.30, 78, 97",
			want: types.ThermalInfo{TemperatureC: 71, PowerState: "P0", CurrentClockMHz: 2865, MaxClockMHz: 3090, PowerLimitW: "575.00", PowerDrawW: "561.30", FanSpeedPct: 78, FanSupported: true, UtilizationPct: 97},
		},
		{
			name: "GeForce RTX 4060 Laptop GPU idle (no fan sensor exposed)",
			line: "0, 41, P8, 210, 2370, 115.00, 3.21, [N/A], 0",
			want: types.ThermalInfo{TemperatureC: 41, PowerState: "P8", CurrentClockMHz: 210, MaxClockMHz: 2370, PowerLimitW: "115.00", PowerDrawW: "3.21", FanSupported: false, UtilizationPct: 0},
		},
		{
			name: "GeForce GTX 1060 6GB on an R470 driver (Pascal video P2)",
			line: "0, 55, P2, 1708, 1911, 120.00, 60.15, 45, 73",
			want: types.ThermalInfo{TemperatureC: 55, PowerState: "P2", CurrentClockMHz: 1708, MaxClockMHz: 1911, PowerLimitW: "120.00", PowerDrawW: "60.15", FanSpeedPct: 45, FanSupported: true, UtilizationPct: 73},
		},
		{
			name: "NVIDIA A100-SXM4-80GB (passive, fan [N/A], 100% busy)",
			line: "0, 61, P0, 1410, 1410, 400.00, 312.55, [N/A], 100",
			want: types.ThermalInfo{TemperatureC: 61, PowerState: "P0", CurrentClockMHz: 1410, MaxClockMHz: 1410, PowerLimitW: "400.00", PowerDrawW: "312.55", FanSupported: false, UtilizationPct: 100},
		},
		{
			name: "NVIDIA H100 80GB HBM3 with MIG enabled (utilization [N/A])",
			line: "0, 38, P0, 1980, 1980, 700.00, 72.11, [N/A], [N/A]",
			want: types.ThermalInfo{TemperatureC: 38, PowerState: "P0", CurrentClockMHz: 1980, MaxClockMHz: 1980, PowerLimitW: "700.00", PowerDrawW: "72.11", FanSupported: false, UtilizationPct: 0},
		},
		{
			name: "Tesla T4 passive at idle",
			line: "0, 46, P8, 300, 1590, 70.00, 12.87, [N/A], 0",
			want: types.ThermalInfo{TemperatureC: 46, PowerState: "P8", CurrentClockMHz: 300, MaxClockMHz: 1590, PowerLimitW: "70.00", PowerDrawW: "12.87", FanSupported: false, UtilizationPct: 0},
		},
		{
			name: "Quadro RTX 8000 rendering",
			line: "0, 58, P0, 1770, 2100, 260.00, 190.44, 52, 88",
			want: types.ThermalInfo{TemperatureC: 58, PowerState: "P0", CurrentClockMHz: 1770, MaxClockMHz: 2100, PowerLimitW: "260.00", PowerDrawW: "190.44", FanSpeedPct: 52, FanSupported: true, UtilizationPct: 88},
		},
		{
			name: "power limit not supported (some vGPU / laptop firmware)",
			line: "0, 50, P0, 1500, 1500, [N/A], [N/A], [N/A], 40",
			want: types.ThermalInfo{TemperatureC: 50, PowerState: "P0", CurrentClockMHz: 1500, MaxClockMHz: 1500, UtilizationPct: 40},
		},
		{
			name: "deep idle P15 is a valid state on newer parts",
			line: "0, 30, P15, 150, 2100, 200.00, 5.00, 0, 0",
			want: types.ThermalInfo{TemperatureC: 30, PowerState: "P15", CurrentClockMHz: 150, MaxClockMHz: 2100, PowerLimitW: "200.00", PowerDrawW: "5.00", FanSupported: true},
		},
	}
	for _, tt := range tests {
		info, errs := parseThermalCSV(tt.line)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors: %+v", tt.name, errs)
		}
		if !reflect.DeepEqual(info, tt.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tt.name, info, tt.want)
		}
	}
}

func TestParseThermalCSV_FanNotAvailable(t *testing.T) {
	for _, fan := range []string{"[N/A]", "[Not Supported]"} {
		line := "0, 61, P0, 1980, 2100, 115.00, 98.20, " + fan + ", 99"
		info, errs := parseThermalCSV(line)
		if len(errs) != 0 {
			t.Errorf("fan=%s: unexpected errors: %+v", fan, errs)
		}
		if info.FanSupported {
			t.Errorf("fan=%s: FanSupported should be false", fan)
		}
		if info.FanSpeedPct != 0 {
			t.Errorf("fan=%s: FanSpeedPct = %d, want 0 (no reading stored)", fan, info.FanSpeedPct)
		}
		if info.UtilizationPct != 99 || info.TemperatureC != 61 {
			t.Errorf("fan=%s: other fields mis-parsed: %+v", fan, info)
		}
	}
}

// A 3-GPU rig: one RTX 3090 and two RTX 4090s. Every row must come back,
// keyed by its own index, not just the first.
const thermalThreeGPU = "0, 43, P8, 210, 2100, 350.00, 33.62, 0, 2\n1, 66, P0, 2730, 2805, 450.00, 431.20, 70, 98\n2, 78, P0, 2700, 2805, 450.00, 448.90, 85, 99\n"

func TestParseThermalRows_ThreeGPUs(t *testing.T) {
	rows, other := csvRows(thermalThreeGPU)
	if len(other) != 0 {
		t.Fatalf("unexpected non-CSV lines: %q", other)
	}
	infos, errs := parseThermalRows(rows)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(infos) != 3 {
		t.Fatalf("got %d entries, want 3", len(infos))
	}
	for i, want := range []struct{ idx, temp, util int }{{0, 43, 2}, {1, 66, 98}, {2, 78, 99}} {
		if infos[i].GPUIndex != want.idx || infos[i].TemperatureC != want.temp || infos[i].UtilizationPct != want.util {
			t.Errorf("row %d = idx %d temp %d util %d, want %+v", i, infos[i].GPUIndex, infos[i].TemperatureC, infos[i].UtilizationPct, want)
		}
	}
}

// Rows that arrive out of order or without an index still land on a GPU: the
// explicit index wins, a missing one falls back to the row position.
func TestParseThermalRows_IndexFallback(t *testing.T) {
	infos, errs := parseThermalRows([]string{
		"1, 66, P0, 2730, 2805, 450.00, 431.20, 70, 98",
		"0, 43, P8, 210, 2100, 350.00, 33.62, 0, 2",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if infos[0].GPUIndex != 1 || infos[1].GPUIndex != 0 {
		t.Errorf("explicit indices not honoured: %d, %d", infos[0].GPUIndex, infos[1].GPUIndex)
	}
	infos, _ = parseThermalRows([]string{"[N/A], 43, P8, 210, 2100, 350.00, 33.62, 0, 2", "[N/A], 50, P0, 1, 2, 3, 4, 5, 6"})
	if infos[0].GPUIndex != 0 || infos[1].GPUIndex != 1 || infos[1].TemperatureC != 50 {
		t.Errorf("ordinal fallback wrong: %+v", infos)
	}
}

// Zero rows and truncated rows must never panic; a short row simply leaves
// the missing fields at their zero value.
func TestParseThermalRows_ZeroAndShortRows(t *testing.T) {
	infos, errs := parseThermalRows(nil)
	if len(infos) != 0 || len(errs) != 0 {
		t.Errorf("nil rows: got %d infos, %d errs", len(infos), len(errs))
	}
	for _, short := range []string{"0", "0,", "0, 43", "0, 43, P0, 1500"} {
		info, errs := parseThermalRow(short, 0)
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %+v", short, errs)
		}
		if info.GPUIndex != 0 {
			t.Errorf("%q: index = %d", short, info.GPUIndex)
		}
		if strings.HasPrefix(short, "0, 43") && info.TemperatureC != 43 {
			t.Errorf("%q: temperature lost: %+v", short, info)
		}
	}
}

func TestParseThermalCSV_Malformed(t *testing.T) {
	_, errs := parseThermalCSV("0, abc, P8, 210, 2100, 350.00, 33.62, 0, 28")
	if len(errs) != 1 || errs[0].Collector != "thermal.temperature" {
		t.Errorf("expected one temperature parse error, got %+v", errs)
	}
	if !strings.Contains(errs[0].Error, "GPU 0") {
		t.Errorf("parse error should name the GPU: %q", errs[0].Error)
	}
}

func TestParseThrottleMask(t *testing.T) {
	tests := []struct {
		in      string
		want    uint64
		wantErr bool
	}{
		{"0x0000000000000001", 0x1, false},
		{"0x0000000000000040", 0x40, false},
		{"0x0000000000000000", 0, false},
		{"Not Active", 0, false},
		{"none", 0, false},
		{"0x0000000000000068", 0x68, false},
		{"", 0, true},
		{"garbage", 0, true},
	}
	for _, tt := range tests {
		got, err := parseThrottleMask(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseThrottleMask(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseThrottleMask(%q) = 0x%x, want 0x%x", tt.in, got, tt.want)
		}
	}
}

// parseThrottleRows keys "index, mask" rows by GPU index. The GTX 1060 on an
// R470 driver only knows clocks_throttle_reasons.active, but the output shape
// is the same, so one parser covers both field names.
func TestParseThrottleRows(t *testing.T) {
	got := parseThrottleRows("0, 0x0000000000000001\n1, 0x0000000000000004\n2, 0x0000000000000040\n")
	want := map[int]string{0: "0x0000000000000001", 1: "0x0000000000000004", 2: "0x0000000000000040"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("indexed rows = %v, want %v", got, want)
	}
	// Legacy single-column output (query run without index) is keyed by position.
	got = parseThrottleRows("0x0000000000000004\nNot Active\n")
	want = map[int]string{0: "0x0000000000000004", 1: "Not Active"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("single-column rows = %v, want %v", got, want)
	}
	if got := parseThrottleRows(""); len(got) != 0 {
		t.Errorf("empty output should yield no masks, got %v", got)
	}
}

// applyThrottleMasks matches masks to GPUs by index; a GPU with no mask row
// (for example one that fell off the bus mid-query) is left untouched.
func TestApplyThrottleMasks_ByIndex(t *testing.T) {
	infos := []types.ThermalInfo{{GPUIndex: 0, TemperatureC: 43}, {GPUIndex: 1, TemperatureC: 90}, {GPUIndex: 2}}
	var errs []types.CollectorError
	applyThrottleMasks(infos, map[int]string{0: "0x0000000000000001", 1: "0x0000000000000040", 5: "0x0"}, &errs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if infos[0].SlowdownActive || !reflect.DeepEqual(infos[0].ThrottleReasons, []string{"gpu_idle"}) {
		t.Errorf("GPU 0: %+v", infos[0])
	}
	if !infos[1].ThermalThrottle || !infos[1].SlowdownActive || infos[1].SlowdownReason != "0x0000000000000040" {
		t.Errorf("GPU 1 should be thermally throttled: %+v", infos[1])
	}
	if infos[2].SlowdownReason != "" || infos[2].ThrottleReasons != nil {
		t.Errorf("GPU 2 had no mask row and must stay untouched: %+v", infos[2])
	}
	applyThrottleMasks(infos, map[int]string{2: "garbage"}, &errs)
	if len(errs) != 1 || !strings.Contains(errs[0].Error, "GPU 2") {
		t.Errorf("undecodable mask should produce one error naming the GPU, got %+v", errs)
	}
}

func TestApplyThrottleMask(t *testing.T) {
	tests := []struct {
		name         string
		mask         uint64
		tempC        int
		wantReasons  []string
		wantSlowdown bool
		wantThermal  bool
	}{
		// gpu_idle is the normal state of an idle desktop; it must never be a slowdown.
		{"idle", 0x1, 43, []string{"gpu_idle"}, false, false},
		{"none", 0x0, 43, nil, false, false},
		{"hw_thermal", 0x40, 90, []string{"hw_thermal_slowdown"}, true, true},
		{"sw_thermal", 0x20, 84, []string{"sw_thermal_slowdown"}, true, true},
		{"sw_power_cap", 0x4, 70, []string{"sw_power_cap"}, true, false},
		{"hw_slowdown cool", 0x8, 70, []string{"hw_slowdown"}, true, false},
		{"hw_slowdown hot", 0x8, 83, []string{"hw_slowdown"}, true, true},
		{"power_brake", 0x80, 60, []string{"hw_power_brake_slowdown"}, true, false},
		{"sync_boost+display", 0x110, 60, []string{"sync_boost", "display_clock_setting"}, false, false},
		{"app_clocks", 0x2, 60, []string{"applications_clocks_setting"}, false, false},
		{"combo", 0x1 | 0x4 | 0x20, 75, []string{"gpu_idle", "sw_power_cap", "sw_thermal_slowdown"}, true, true},
	}
	for _, tt := range tests {
		info := types.ThermalInfo{TemperatureC: tt.tempC}
		applyThrottleMask(&info, tt.mask)
		if !reflect.DeepEqual(info.ThrottleReasons, tt.wantReasons) {
			t.Errorf("%s: ThrottleReasons = %v, want %v", tt.name, info.ThrottleReasons, tt.wantReasons)
		}
		if info.SlowdownActive != tt.wantSlowdown {
			t.Errorf("%s: SlowdownActive = %v, want %v", tt.name, info.SlowdownActive, tt.wantSlowdown)
		}
		if info.ThermalThrottle != tt.wantThermal {
			t.Errorf("%s: ThermalThrottle = %v, want %v", tt.name, info.ThermalThrottle, tt.wantThermal)
		}
	}
}

func TestIsNotAvailable(t *testing.T) {
	for _, s := range []string{"[N/A]", "[Not Supported]", "[Unknown Error]", "N/A", " [N/A] "} {
		if !isNotAvailable(s) {
			t.Errorf("isNotAvailable(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"0", "43", "P8", "350.00"} {
		if isNotAvailable(s) {
			t.Errorf("isNotAvailable(%q) = true, want false", s)
		}
	}
}

func TestThermalQueryFieldsUsesPstateAndIndex(t *testing.T) {
	// "power.state" is not a valid nvidia-smi field; the collector failed on
	// every machine while it was in the query string.
	if strings.Contains(ThermalQueryFields, "power.state") {
		t.Fatal("ThermalQueryFields must use pstate, not power.state")
	}
	if !strings.Contains(ThermalQueryFields, "pstate") || !strings.Contains(ThermalQueryFields, "utilization.gpu") {
		t.Fatalf("ThermalQueryFields missing required fields: %s", ThermalQueryFields)
	}
	// Rows must be self-identifying so multi-GPU rigs map each row to a GPU.
	if !strings.HasPrefix(ThermalQueryFields, "index,") || !strings.HasPrefix(PCIeQueryFields, "index,") || !strings.HasPrefix(GPUQueryFields, "index,") {
		t.Fatalf("query field lists must start with index: %q %q %q", ThermalQueryFields, PCIeQueryFields, GPUQueryFields)
	}
	if got := ClockEventQuery(ThermalEventQueryFieldsLegacy); got != "index,clocks_throttle_reasons.active" {
		t.Errorf("ClockEventQuery = %q", got)
	}
}
