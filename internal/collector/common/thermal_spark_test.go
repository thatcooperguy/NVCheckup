package common

import (
	"reflect"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// GB10 thermal row (spec 2.1: power limit N/A, fan N/A; idle clocks and
// utilization from the gb10 scenario). No parse errors, PowerLimitSupported
// false, FanSupported false.
func TestParseThermalCSV_GB10(t *testing.T) {
	info, errs := parseThermalCSV("0, 38, P8, 210, 3003, [N/A], 4.00, [N/A], 0")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := types.ThermalInfo{TemperatureC: 38, PowerState: "P8", CurrentClockMHz: 210, MaxClockMHz: 3003, PowerDrawW: "4.00", FanSupported: false, PowerLimitSupported: false, UtilizationPct: 0}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("GB10 row\n got %+v\nwant %+v", info, want)
	}
	// The bare "Not Supported" spelling is unsupported too, never a parse error.
	info, errs = parseThermalCSV("0, 38, P8, 210, 3003, Not Supported, 4.00, Not Supported, 0")
	if len(errs) != 0 || info.PowerLimitSupported || info.FanSupported || info.PowerLimitW != "" {
		t.Errorf("bare Not Supported: errs %+v info %+v", errs, info)
	}
	if !anyPowerLimitUnsupported([]types.ThermalInfo{info}) {
		t.Error("anyPowerLimitUnsupported should be true for the GB10 row")
	}
	rtx, _ := parseThermalCSV(thermalSampleIdle)
	if !rtx.PowerLimitSupported || anyPowerLimitUnsupported([]types.ThermalInfo{rtx}) {
		t.Errorf("RTX 3090 row should have a supported power limit: %+v", rtx)
	}
}

// Layout of nvidia-smi -q -d PERFORMANCE (two GPUs to prove section alignment;
// the exact GB10 text is an open question of the spec, so the parser only
// relies on the "Clocks Event Reasons Counters" header and "Name : N us" lines).
const performanceQuerySample = `
==============NVSMI LOG==============

Timestamp                                 : Tue Sep  1 19:10:20 2026
Driver Version                            : 580.159.03
CUDA Version                              : 13.0

Attached GPUs                             : 2
GPU 0000000F:01:00.0
    Performance
        Clocks Event Reasons
            Idle                              : Active
            Applications Clocks Setting       : Not Active
            SW Power Cap                      : Not Active
            HW Slowdown                       : Not Active
                HW Thermal Slowdown           : Not Active
                HW Power Brake Slowdown       : Not Active
            Sync Boost                        : Not Active
            SW Thermal Slowdown               : Not Active
            Display Clock Setting             : Not Active
        Clocks Event Reasons Counters
            SW Power Capping                  : 123456 us
            Sync Boost                        : 0 us
            SW Thermal Slowdown               : 7 us
            HW Thermal Slowdown               : 0 us
            HW Power Brake Slowdown           : 0 us
        Sparse Operation Mode                 : N/A

GPU 00000000:41:00.0
    Performance
        Clocks Event Reasons
            Idle                              : Not Active
        Clocks Event Reasons Counters
            SW Power Capping                  : 99 us
            Sync Boost                        : N/A

`

func TestParsePerformanceCounters(t *testing.T) {
	got := parsePerformanceCounters(performanceQuerySample)
	if len(got) != 2 {
		t.Fatalf("got %d GPU sections, want 2: %v", len(got), got)
	}
	want0 := map[string]int64{"sw_power_capping": 123456, "sync_boost": 0, "sw_thermal_slowdown": 7, "hw_thermal_slowdown": 0, "hw_power_brake_slowdown": 0}
	if !reflect.DeepEqual(got[0], want0) {
		t.Errorf("GPU 0 counters = %v, want %v", got[0], want0)
	}
	// "N/A" values are skipped; "Sparse Operation Mode" outside the block is ignored.
	if !reflect.DeepEqual(got[1], map[string]int64{"sw_power_capping": 99}) {
		t.Errorf("GPU 1 counters = %v", got[1])
	}
	if got := parsePerformanceCounters("No devices were found\n"); len(got) != 0 {
		t.Errorf("failure text produced sections: %v", got)
	}

	infos := []types.ThermalInfo{{GPUIndex: 0}, {GPUIndex: 1}, {GPUIndex: 2}}
	applyEventCounters(infos, parsePerformanceCounters(performanceQuerySample))
	if infos[0].EventCounters["sw_power_capping"] != 123456 || infos[1].EventCounters["sw_power_capping"] != 99 || infos[2].EventCounters != nil {
		t.Errorf("applyEventCounters = %v %v %v", infos[0].EventCounters, infos[1].EventCounters, infos[2].EventCounters)
	}
	if snakeKey("  HW Power Brake  Slowdown ") != "hw_power_brake_slowdown" {
		t.Error("snakeKey wrong")
	}
	if len(PerformanceQueryArgs) != 3 || PerformanceQueryArgs[2] != "PERFORMANCE" {
		t.Errorf("PerformanceQueryArgs = %v", PerformanceQueryArgs)
	}
}

// The GB10 PCIe row "GEN 1@ 1x" (spec 2.1) parses as Gen1/x1 at max, so
// Downshifted is false; OnPackage is set later by ApplyPlatformFlags.
func TestParsePCIeCSV_GB10Gen1x1(t *testing.T) {
	info, errs := parsePCIeCSV("0, 1, 1, 1, 1, P8, 0")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if info.CurrentSpeed != "Gen1" || info.MaxSpeed != "Gen1" || info.CurrentWidth != "x1" || info.MaxWidth != "x1" || info.Downshifted || info.OnPackage {
		t.Errorf("GB10 PCIe row = %+v", info)
	}
	// Some fields may print [N/A] or Not Supported on unified-memory parts.
	info, errs = parsePCIeCSV("0, [N/A], Not Supported, [N/A], [N/A], P8, 0")
	if len(errs) != 0 || info.CurrentSpeed != "" || info.MaxSpeed != "" || info.Downshifted {
		t.Errorf("all-N/A PCIe row: errs %+v info %+v", errs, info)
	}
}
