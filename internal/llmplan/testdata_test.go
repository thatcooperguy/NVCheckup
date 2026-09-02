package llmplan

import (
	"math"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Spec 7.5 pool: measured MemTotal 119.7 GiB on a 128 GB unit; the 64 GB
// column assumes a visible pool of 57.7 GiB on Windows (F = 10 GiB).
const (
	poolGB10Bytes = 119.7 * GiB
	pool64Bytes   = 57.7 * GiB
	floorLinux    = 8 * GiB
	floorWindows  = 10 * GiB
)

func mustModel(t *testing.T, id string) ModelShape {
	t.Helper()
	m, ok := FindModel(id)
	if !ok {
		t.Fatalf("model %q not in catalogue", id)
	}
	return m
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.3f, want %.3f (+/- %.2f)", what, got, want, tol)
	}
}

func intPtr(i int) *int { return &i }

// gb10Report is a synthetic DGX Spark report with the spec 2.1 strings and
// the sim scenario's MemTotal (spec 10: 125513944 kB = 119.7 GiB).
func gb10Report() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "linux"},
		System:   types.SystemInfo{OSName: "Ubuntu 24.04.3 LTS", Architecture: "aarch64", KernelVersion: "6.17.0-1015-nvidia"},
		GPUs: []types.GPUInfo{{
			Index: 0, Name: "NVIDIA GB10", Vendor: "NVIDIA", IsNVIDIA: true,
			DriverVersion: "580.95.05", ComputeCap: "12.1", OnPackage: true, MemoryReporting: "not-supported",
		}},
		Driver: types.DriverInfo{Version: "580.95.05", CUDAVersion: "13.0"},
		Linux: &types.LinuxInfo{
			Distro: "Ubuntu", DistroVersion: "24.04", ContainerRuntime: "docker", NVContainerToolkit: "1.17.8",
			SessionType: "Unknown",
		},
		AI: &types.AIInfo{
			CUDADriverVersion: "13.0",
			PyTorchInfo: &types.PyTorchInfo{
				Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: true, DeviceName: "NVIDIA GB10",
				ArchList: []string{"sm_80", "sm_86", "sm_90", "sm_100", "sm_120", "sm_121"},
			},
		},
		Platform: types.PlatformInfo{Class: "dgx-spark", GPUSoC: "GB10", ComputeCap: "12.1", UnifiedMemory: true},
		UnifiedMemory: &types.UnifiedMemoryInfo{
			MemTotalKB: 125513944, MemFreeKB: 90000000, MemAvailableKB: 115000000,
			BuffersKB: 500000, CachedKB: 20000000, SwapTotalKB: 0, SwapFreeKB: 0,
		},
		DGXOS: &types.DGXOSInfo{Name: "DGX OS", OTATorn: intPtr(0)},
	}
}

func gb10Pool() MemoryPool {
	return poolFromUnifiedMemory(gb10Report().UnifiedMemory)
}
