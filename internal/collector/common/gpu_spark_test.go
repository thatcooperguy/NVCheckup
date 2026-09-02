package common

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// nvidia-smi table on DGX Spark (spec 2.1: Fan N/A, Pwr cap N/A, Memory-Usage
// "Not Supported", Bus-Id 0000000F:01:00.0).
const nvidiaSmiTableGB10 = `+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 580.159.03             Driver Version: 580.159.03     CUDA Version: 13.0     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GB10                    On  |   0000000F:01:00.0 Off |                  N/A |
| N/A   38C    P8              4W /  N/A  |       Not Supported    |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|=========================================================================================|
|  No running processes found                                                             |
+-----------------------------------------------------------------------------------------+
`

// GB10 query row: memory.total/free/used print "[N/A], [N/A], [N/A]" (spec 2.1)
// and the full BDF with domain 000F is kept.
func TestApplyGPUQueryRows_GB10MemoryNotSupported(t *testing.T) {
	gpus := parseGPUList("GPU 0: NVIDIA GB10 (UUID: GPU-0f01c2c2-0000-4000-8000-0000000f0100)\n")
	var driver types.DriverInfo
	applyGPUQueryRows(gpus, &driver, "0, 580.159.03, 0000000F:01:00.0, [N/A], [N/A], [N/A], 38, 4.00\n")
	g := gpus[0]
	if g.MemoryReporting != MemoryReportingNotSupported || g.VRAMTotalMB != 0 || g.VRAMFreeMB != 0 {
		t.Errorf("GB10 row = %+v", g)
	}
	if g.PCIBusID != "0000000F:01:00.0" || g.DriverVersion != "580.159.03" || g.Temperature != 38 || g.PowerDraw != "4.00" {
		t.Errorf("other fields lost: %+v", g)
	}
	if shortBusID(g.PCIBusID) != "0f:01:00.0" && shortBusID(g.PCIBusID) != "01:00.0" {
		t.Errorf("shortBusID(%q) = %q", g.PCIBusID, shortBusID(g.PCIBusID))
	}
}

// Dedicated-VRAM rows (3090, laptop 4060, 3-GPU rig) report "dedicated";
// nothing else about them changes.
func TestApplyGPUQueryRows_DedicatedMemoryReporting(t *testing.T) {
	gpus := parseGPUList(gpuListThree)
	var driver types.DriverInfo
	applyGPUQueryRows(gpus, &driver, "0, 591.86, 00000000:41:00.0, 24576, 21357, 2970, 43, 31.49\n1, 591.86, 00000000:42:00.0, 24564, [N/A], [N/A], 66, [N/A]\n")
	if gpus[0].MemoryReporting != MemoryReportingDedicated || gpus[0].VRAMTotalMB != 24576 {
		t.Errorf("GPU 0 = %+v", gpus[0])
	}
	// memory.free/used [N/A] with a numeric total is still dedicated.
	if gpus[1].MemoryReporting != MemoryReportingDedicated || gpus[1].VRAMTotalMB != 24564 {
		t.Errorf("GPU 1 = %+v", gpus[1])
	}
	// No query row at all: reporting stays unknown.
	if gpus[2].MemoryReporting != "" {
		t.Errorf("GPU 2 without a row = %+v", gpus[2])
	}
}

func TestApplyGPUCapRows(t *testing.T) {
	gpus := parseGPUList(gpuListThree)
	applyGPUCapRows(gpus, "0, 8.6\n1, 8.9\n2, [N/A]\n")
	if gpus[0].ComputeCap != "8.6" || gpus[1].ComputeCap != "8.9" || gpus[2].ComputeCap != "" {
		t.Errorf("compute caps = %q %q %q", gpus[0].ComputeCap, gpus[1].ComputeCap, gpus[2].ComputeCap)
	}
	gb10 := parseGPUList("GPU 0: NVIDIA GB10 (UUID: GPU-x)\n")
	applyGPUCapRows(gb10, "0, 12.1\n")
	if gb10[0].ComputeCap != "12.1" {
		t.Errorf("GB10 compute cap = %q", gb10[0].ComputeCap)
	}
	// The rejection text of older drivers is not a CSV row and changes nothing.
	applyGPUCapRows(gpus, "Field \"compute_cap\" is not a valid field to query.\n")
	if gpus[0].ComputeCap != "8.6" {
		t.Errorf("rejection text altered compute cap: %q", gpus[0].ComputeCap)
	}
	applyGPUCapRows(nil, "0, 12.1\n") // must not panic
}

func TestGPUCapQueryFieldsIsSeparate(t *testing.T) {
	if GPUCapQueryFields != "index,compute_cap" {
		t.Errorf("GPUCapQueryFields = %q", GPUCapQueryFields)
	}
	if strings.Contains(GPUQueryFields, "compute_cap") {
		t.Error("compute_cap must not be part of GPUQueryFields (older drivers reject the whole query)")
	}
}

func TestTableShowsMemoryNotSupported(t *testing.T) {
	if !tableShowsMemoryNotSupported(nvidiaSmiTableGB10) {
		t.Error("GB10 table not recognised")
	}
	if tableShowsMemoryNotSupported(nvidiaSmiTableWithProcesses) {
		t.Error("RTX 3090 table (3545MiB / 24576MiB) flagged as Not Supported")
	}
	// Only GPUs without a query answer are marked; an answered GPU keeps its value.
	gpus := []types.GPUInfo{
		{Index: 0, IsNVIDIA: true},
		{Index: 1, IsNVIDIA: true, MemoryReporting: MemoryReportingDedicated},
		{Index: 2, Vendor: "Intel"},
	}
	applyTableMemoryReporting(gpus, nvidiaSmiTableGB10)
	if gpus[0].MemoryReporting != MemoryReportingNotSupported || gpus[1].MemoryReporting != MemoryReportingDedicated || gpus[2].MemoryReporting != "" {
		t.Errorf("table reporting = %q %q %q", gpus[0].MemoryReporting, gpus[1].MemoryReporting, gpus[2].MemoryReporting)
	}
	if got := stripProcessSection(nvidiaSmiTableGB10); strings.Contains(got, "Processes:") || !strings.Contains(got, "Not Supported") {
		t.Errorf("stripProcessSection on GB10 table = %q", got)
	}
}

// lspci on DGX Spark lists the GB10 as a VGA controller with device id 2e12 at
// domain 000f; it becomes an NVIDIA GPU even when nvidia-smi found nothing, so
// no-nvidia-gpu cannot fire on a GSP failure.
func TestParseLspciGPUs_GB10(t *testing.T) {
	gpus := parseLspciGPUs(lspciGB10, nil)
	if len(gpus) != 1 || !gpus[0].IsNVIDIA || gpus[0].PCIDeviceID != "2e12" || gpus[0].PCIBusID != "000f:01:00.0" {
		t.Errorf("lspci GB10 = %+v", gpus)
	}
	// When nvidia-smi already listed it with the full BDF, it is not duplicated.
	existing := []types.GPUInfo{{Index: 0, Name: "NVIDIA GB10", IsNVIDIA: true, PCIBusID: "0000000F:01:00.0"}}
	if added := parseLspciGPUs(lspciGB10, existing); len(added) != 0 {
		t.Errorf("GB10 duplicated: %+v", added)
	}
	if !errorsMention([]types.CollectorError{{Error: "nvidia-smi -L failed: \"No devices were found\" (...)"}}, noDevicesFoundText) {
		t.Error("errorsMention should find the nvidia-smi text")
	}
	if errorsMention(nil, noDevicesFoundText) {
		t.Error("errorsMention(nil) must be false")
	}
	if !strings.Contains(gb10NoDevicesExplanation, "10de:2e12") || !strings.Contains(gb10NoDevicesExplanation, "GSP") {
		t.Errorf("explanation lacks the spec strings: %q", gb10NoDevicesExplanation)
	}
}

func TestIsNotAvailable_BareNotSupported(t *testing.T) {
	for _, s := range []string{"Not Supported", "not supported", " Not Supported ", "[Not Supported]", "[N/A]", "N/A", "[Unknown Error]"} {
		if !isNotAvailable(s) {
			t.Errorf("isNotAvailable(%q) = false", s)
		}
	}
	for _, s := range []string{"NVIDIA GB10", "Supported", "12.1", "0000000F:01:00.0"} {
		if isNotAvailable(s) {
			t.Errorf("isNotAvailable(%q) = true", s)
		}
	}
}
