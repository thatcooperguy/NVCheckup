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

// gh200MiB is the nvidia-smi memory.total the spec quotes for GH200 (spec 2.3
// / S29: "NVIDIA GH200 480GB", 97871MiB, CC 9.0): discrete HBM, not unified.
const gh200MiB = 97871

// gh200Report is a Grace Hopper superchip as the detector classifies it (spec
// 3.1 row 7 Class=grace-hopper, GPUSoC "GH200", flag rule C UnifiedMemory=false)
// with the 570 / CUDA 12.8 pairing that is fine off Spark.
func gh200Report() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "linux"},
		System:   types.SystemInfo{OSName: "Ubuntu 22.04.5 LTS", Architecture: "aarch64", KernelVersion: "6.8.0-1015-nvidia", RAMTotalMB: 491520},
		GPUs: []types.GPUInfo{{
			Index: 0, Name: "NVIDIA GH200 480GB", Vendor: "NVIDIA", IsNVIDIA: true,
			DriverVersion: "570.86.15", ComputeCap: "9.0", VRAMTotalMB: gh200MiB, VRAMFreeMB: 97000, MemoryReporting: "dedicated",
		}},
		Driver:   types.DriverInfo{Version: "570.86.15", CUDAVersion: "12.8"},
		Linux:    &types.LinuxInfo{Distro: "Ubuntu", DistroVersion: "22.04", ContainerRuntime: "docker", NVContainerToolkit: "1.17.8"},
		AI:       &types.AIInfo{CUDADriverVersion: "12.8"},
		Platform: types.PlatformInfo{Class: "grace-hopper", GPUSoC: "GH200", ComputeCap: "9.0"},
	}
}

// n1xWindowsReport is an RTX Spark laptop on Windows on Arm as the detector
// classifies it (spec 2.2 / 3.1 rows 1-2): no UnifiedMemoryInfo struct on
// Windows, so the pool falls back to the report's RAM figure offline.
func n1xWindowsReport() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "windows"},
		System:   types.SystemInfo{OSName: "Windows 11 Pro", Architecture: "arm64", RAMTotalMB: 130000},
		GPUs: []types.GPUInfo{{
			Index: 0, Name: "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)", Vendor: "NVIDIA", IsNVIDIA: true,
			DriverVersion: "32.0.16.1600", MemoryReporting: "not-supported",
		}},
		Driver:   types.DriverInfo{Version: "616.00", CUDAVersion: "13.0"},
		Platform: types.PlatformInfo{Class: "rtx-spark", GPUSoC: "N1X", UnifiedMemory: true, IsWindowsOnArm: true},
	}
}
