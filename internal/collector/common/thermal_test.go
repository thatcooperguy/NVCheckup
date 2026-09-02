package common

import (
	"reflect"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Captured from an RTX 3090 on driver 591.86 at idle.
const thermalSampleIdle = "43, P8, 210, 2100, 350.00, 33.62, 0, 28"

func TestParseThermalCSV_RealSample(t *testing.T) {
	info, errs := parseThermalCSV(thermalSampleIdle)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := types.ThermalInfo{
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

func TestParseThermalCSV_FanNotAvailable(t *testing.T) {
	for _, fan := range []string{"[N/A]", "[Not Supported]"} {
		line := "61, P0, 1980, 2100, 115.00, 98.20, " + fan + ", 99"
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

func TestParseThermalCSV_MultiGPUUsesFirstLine(t *testing.T) {
	out := "43, P8, 210, 2100, 350.00, 33.62, 0, 28\n70, P0, 1900, 2100, 350.00, 300.10, 65, 100\n"
	info, _ := parseThermalCSV(firstLine(out))
	if info.TemperatureC != 43 || info.UtilizationPct != 28 {
		t.Errorf("expected first GPU, got %+v", info)
	}
}

func TestParseThermalCSV_Malformed(t *testing.T) {
	_, errs := parseThermalCSV("abc, P8, 210, 2100, 350.00, 33.62, 0, 28")
	if len(errs) != 1 || errs[0].Collector != "thermal.temperature" {
		t.Errorf("expected one temperature parse error, got %+v", errs)
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

func TestThermalQueryFieldsUsesPstate(t *testing.T) {
	// "power.state" is not a valid nvidia-smi field; the collector failed on
	// every machine while it was in the query string.
	if contains(ThermalQueryFields, "power.state") {
		t.Fatal("ThermalQueryFields must use pstate, not power.state")
	}
	if !contains(ThermalQueryFields, "pstate") || !contains(ThermalQueryFields, "utilization.gpu") {
		t.Fatalf("ThermalQueryFields missing required fields: %s", ThermalQueryFields)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
