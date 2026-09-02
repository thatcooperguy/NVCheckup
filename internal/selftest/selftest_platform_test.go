package selftest

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestPlatformDetail(t *testing.T) {
	p := types.PlatformInfo{Class: "dgx-spark", GPUSoC: "GB10", UnifiedMemory: true, ComputeCap: "12.1", Vendor: "NVIDIA", Model: "NVIDIA_DGX_Spark"}
	got := platformDetail(p)
	for _, want := range []string{"dgx-spark (GB10)", "unified memory yes", "compute cap 12.1", "NVIDIA NVIDIA_DGX_Spark"} {
		if !strings.Contains(got, want) {
			t.Errorf("platformDetail = %q, missing %q", got, want)
		}
	}
	got = platformDetail(types.PlatformInfo{})
	if !strings.Contains(got, "generic") || !strings.Contains(got, "unified memory no") || strings.Contains(got, "compute cap") {
		t.Errorf("empty platform detail = %q", got)
	}
	got = platformDetail(types.PlatformInfo{Class: "rtx-spark", GPUSoC: "N1X", UnifiedMemory: true, IsWindowsOnArm: true, ProcessEmulated: true})
	if !strings.Contains(got, "Windows on Arm") || !strings.Contains(got, "emulated") {
		t.Errorf("WoA detail = %q", got)
	}
}

func TestMemoryReportingCheck(t *testing.T) {
	gb10 := types.GPUInfo{Index: 0, Name: "NVIDIA GB10", IsNVIDIA: true, MemoryReporting: common.MemoryReportingNotSupported}
	rtx := types.GPUInfo{Index: 0, Name: "NVIDIA GeForce RTX 3090", IsNVIDIA: true, VRAMTotalMB: 24576, MemoryReporting: common.MemoryReportingDedicated}

	// [N/A] on a unified-memory platform is expected.
	r := &types.Report{GPUs: []types.GPUInfo{gb10}}
	r.Platform.UnifiedMemory = true
	res, ok := memoryReportingCheck(r)
	if !ok || res.Status != "INFO" || !strings.Contains(res.Detail, "expected on unified-memory") {
		t.Errorf("unified [N/A] = %+v %v", res, ok)
	}
	// [N/A] without a recognised platform is a warning.
	r = &types.Report{GPUs: []types.GPUInfo{gb10}}
	if res, ok = memoryReportingCheck(r); !ok || res.Status != "WARN" {
		t.Errorf("unexplained [N/A] = %+v %v", res, ok)
	}
	// Dedicated VRAM is OK and quotes the size.
	r = &types.Report{GPUs: []types.GPUInfo{rtx}}
	if res, ok = memoryReportingCheck(r); !ok || res.Status != "OK" || !strings.Contains(res.Detail, "24576 MiB") {
		t.Errorf("dedicated = %+v %v", res, ok)
	}
	// No NVIDIA GPU: no row.
	if _, ok = memoryReportingCheck(&types.Report{GPUs: []types.GPUInfo{{Name: "Intel UHD", Vendor: "Intel"}}}); ok {
		t.Error("iGPU-only inventory must not produce a memory row")
	}
	// The whole check on a unified-memory report yields Platform + memory rows.
	r = &types.Report{GPUs: []types.GPUInfo{gb10}}
	r.Platform = types.PlatformInfo{Class: "dgx-spark", UnifiedMemory: true}
	rows := checkPlatform(r)
	if len(rows) != 2 || rows[0].Name != "Platform" || rows[0].Status != "INFO" || rows[1].Name != "nvidia-smi memory" || rows[1].Status != "INFO" {
		t.Errorf("checkPlatform rows = %+v", rows)
	}
	for _, row := range rows {
		if concernsTool(row) {
			t.Errorf("%q must not count as a missing tool", row.Name)
		}
	}
}

// platformReport without GPUs applies the phase-1 result unchanged and never
// sets unified memory on an unclassified host.
func TestPlatformReport_NoGPUs(t *testing.T) {
	r := platformReport(types.PlatformInfo{}, false)
	if r.Platform.UnifiedMemory || r.Platform.Class != "" || len(r.GPUs) != 0 {
		t.Errorf("platformReport = %+v", r.Platform)
	}
	r = platformReport(types.PlatformInfo{Class: common.ClassDGXSpark}, false)
	if !r.Platform.UnifiedMemory || r.Platform.GPUSoC != "GB10" {
		t.Errorf("dgx-spark without GPUs = %+v", r.Platform)
	}
}
