package common

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Captured from nvidia-smi 591.86 on an RTX 3090 (process names altered).
const nvidiaSmiTableWithProcesses = `Tue Sep  1 19:10:20 2026
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 591.86                 Driver Version: 591.86         CUDA Version: 13.1     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                  Driver-Model | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3090      WDDM  |   00000000:41:00.0  On |                  N/A |
|  0%   36C    P8             30W /  350W |    3545MiB /  24576MiB |     15%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|    0   N/A  N/A           12664    C+G   ...indows\System32\ShellHost.exe      N/A      |
|    0   N/A  N/A           20488    C+G   ...4__8wekyb3d8bbwe\ms-teams.exe      N/A      |
|    0   N/A  N/A           32304    C+G   ...secret-project\app\private.exe      N/A      |
+-----------------------------------------------------------------------------------------+
`

func TestStripProcessSection(t *testing.T) {
	got := stripProcessSection(nvidiaSmiTableWithProcesses)

	for _, private := range []string{"Processes:", "ShellHost.exe", "ms-teams.exe", "private.exe", "PID"} {
		if strings.Contains(got, private) {
			t.Errorf("stored output still contains %q", private)
		}
	}
	for _, keep := range []string{"Driver Version: 591.86", "CUDA Version: 13.1", "NVIDIA GeForce RTX 3090", "3545MiB /  24576MiB"} {
		if !strings.Contains(got, keep) {
			t.Errorf("stored output lost GPU table content %q", keep)
		}
	}
	if !strings.Contains(got, processSectionOmittedNote) {
		t.Errorf("expected omission note in output")
	}

	// The GPU table's own closing border stays; the Processes box border goes.
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("too few lines kept: %d", len(lines))
	}
	if lines[len(lines)-1] != processSectionOmittedNote {
		t.Errorf("last line = %q, want omission note", lines[len(lines)-1])
	}
	if !isTableBorder(lines[len(lines)-2]) {
		t.Errorf("expected GPU table closing border before note, got %q", lines[len(lines)-2])
	}
	if strings.TrimSpace(lines[len(lines)-3]) == "" {
		t.Errorf("dangling Processes box border was not removed")
	}
}

func TestStripProcessSection_NoProcesses(t *testing.T) {
	in := "+---+\n| NVIDIA-SMI 535.00 |\n+---+\n"
	if got := stripProcessSection(in); got != in {
		t.Errorf("output without Processes section should be unchanged, got %q", got)
	}
}

func TestIsTableBorder(t *testing.T) {
	yes := []string{"+---+", "+-----------------------------------------+------------------------+", "|=====|"}
	// "|=====|" is a header separator, not a border, so it must be false.
	if !isTableBorder(yes[0]) || !isTableBorder(yes[1]) {
		t.Error("expected borders to be recognised")
	}
	if isTableBorder(yes[2]) || isTableBorder("| GPU  Name |") || isTableBorder("") {
		t.Error("non-border lines recognised as borders")
	}
}

func TestGPUQueryFields(t *testing.T) {
	if !strings.Contains(GPUQueryFields, "driver_version") || !strings.Contains(GPUQueryFields, "pci.bus_id") {
		t.Fatalf("GPUQueryFields missing expected fields: %s", GPUQueryFields)
	}
}

// nvidia-smi -L on a 3-GPU rig (one RTX 3090, two RTX 4090s).
const gpuListThree = `GPU 0: NVIDIA GeForce RTX 3090 (UUID: GPU-2f1c7b58-0000-0000-0000-000000000001)
GPU 1: NVIDIA GeForce RTX 4090 (UUID: GPU-2f1c7b58-0000-0000-0000-000000000002)
GPU 2: NVIDIA GeForce RTX 4090 (UUID: GPU-2f1c7b58-0000-0000-0000-000000000003)
`

func TestParseGPUList_ThreeGPUs(t *testing.T) {
	gpus := parseGPUList(gpuListThree)
	if len(gpus) != 3 {
		t.Fatalf("got %d GPUs, want 3: %+v", len(gpus), gpus)
	}
	wantNames := []string{"NVIDIA GeForce RTX 3090", "NVIDIA GeForce RTX 4090", "NVIDIA GeForce RTX 4090"}
	for i, g := range gpus {
		if g.Index != i || g.Name != wantNames[i] || !g.IsNVIDIA || g.Vendor != "NVIDIA" {
			t.Errorf("GPU %d = %+v", i, g)
		}
	}
}

// nvidia-smi -L on an H100 with MIG enabled lists the MIG instances under the
// GPU; they are not GPUs and must not inflate the inventory.
const gpuListMIG = `GPU 0: NVIDIA H100 80GB HBM3 (UUID: GPU-3b4e0c9a-0000-0000-0000-000000000004)
  MIG 1g.10gb     Device  0: (UUID: MIG-8d6a4f2e-0000-0000-0000-000000000005)
  MIG 1g.10gb     Device  1: (UUID: MIG-8d6a4f2e-0000-0000-0000-000000000006)
  MIG 3g.40gb     Device  2: (UUID: MIG-8d6a4f2e-0000-0000-0000-000000000007)
`

func TestParseGPUList_MIGInstancesAreNotGPUs(t *testing.T) {
	gpus := parseGPUList(gpuListMIG)
	if len(gpus) != 1 || gpus[0].Name != "NVIDIA H100 80GB HBM3" {
		t.Errorf("got %+v, want exactly the H100", gpus)
	}
	// Names without a UUID suffix (older drivers) still parse.
	gpus = parseGPUList("GPU 0: Tesla T4\nGPU 1: Quadro RTX 8000 (UUID: GPU-x)\n")
	if len(gpus) != 2 || gpus[0].Name != "Tesla T4" || gpus[1].Name != "Quadro RTX 8000" {
		t.Errorf("legacy list parsed as %+v", gpus)
	}
	if got := parseGPUList("No devices were found\n"); len(got) != 0 {
		t.Errorf("failure text parsed as GPUs: %+v", got)
	}
}

// Query rows are matched to GPUs by index, so the order nvidia-smi prints
// them in does not matter, and [N/A] fields stay at zero.
func TestApplyGPUQueryRows_ThreeGPUsByIndex(t *testing.T) {
	gpus := parseGPUList(gpuListThree)
	var driver types.DriverInfo
	out := "2, 591.86, 00000000:61:00.0, 24564, 1200, 23364, 78, 448.90\n" +
		"0, 591.86, 00000000:41:00.0, 24576, 21357, 2970, 43, 31.49\n" +
		"1, 591.86, 00000000:42:00.0, 24564, [N/A], [N/A], 66, [N/A]\n"
	applyGPUQueryRows(gpus, &driver, out)
	if driver.Version != "591.86" {
		t.Errorf("driver version = %q", driver.Version)
	}
	if gpus[0].PCIBusID != "00000000:41:00.0" || gpus[0].VRAMTotalMB != 24576 || gpus[0].VRAMFreeMB != 21357 || gpus[0].Temperature != 43 || gpus[0].PowerDraw != "31.49" {
		t.Errorf("GPU 0 = %+v", gpus[0])
	}
	if gpus[1].PCIBusID != "00000000:42:00.0" || gpus[1].VRAMTotalMB != 24564 || gpus[1].VRAMFreeMB != 0 || gpus[1].Temperature != 66 || gpus[1].PowerDraw != "" {
		t.Errorf("GPU 1 ([N/A] fields) = %+v", gpus[1])
	}
	if gpus[2].PCIBusID != "00000000:61:00.0" || gpus[2].Temperature != 78 || gpus[2].DriverVersion != "591.86" {
		t.Errorf("GPU 2 = %+v", gpus[2])
	}
	// A row for an index the list did not contain, and failure text, are ignored.
	applyGPUQueryRows(gpus, &driver, "7, 591.86, 00000000:99:00.0, 1, 1, 1, 1, 1\nNo devices were found\n")
	if len(gpus) != 3 || gpus[0].PCIBusID != "00000000:41:00.0" {
		t.Errorf("stray rows changed the inventory: %+v", gpus)
	}
	// Zero GPUs and empty output must not panic.
	applyGPUQueryRows(nil, &driver, "")
	applyGPUQueryRows(nil, &driver, "0, 591.86")
}

// Windows hybrid laptop: nvidia-smi already listed the dGPU; WMI reports both
// the Intel iGPU and the same dGPU. Only the iGPU may be added.
func TestParseWMIVideoControllers_HybridLaptop(t *testing.T) {
	existing := []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 4060 Laptop GPU", Vendor: "NVIDIA", IsNVIDIA: true}}
	out := "Intel(R) Iris(R) Xe Graphics|31.0.101.5186|1073741824|PCI\\VEN_8086&DEV_A7A0&SUBSYS_0B7E1028&REV_04\\3&11583659&0&10\n" +
		"NVIDIA GeForce RTX 4060 Laptop GPU|32.0.15.6094|4293918720|PCI\\VEN_10DE&DEV_28A0&SUBSYS_0B7E1028&REV_A1\\4&2D78AB8F&0&0008\n"
	added := parseWMIVideoControllers(out, existing)
	if len(added) != 1 {
		t.Fatalf("added %d adapters, want 1 (the iGPU): %+v", len(added), added)
	}
	igpu := added[0]
	if igpu.Index != 1 || igpu.Vendor != "Intel" || igpu.IsNVIDIA || igpu.PCIVendorID != "8086" || igpu.PCIDeviceID != "A7A0" || igpu.DriverVersion != "31.0.101.5186" {
		t.Errorf("iGPU = %+v", igpu)
	}
	// Without nvidia-smi (driver missing) WMI is the only source and the
	// NVIDIA adapter is classified by name.
	added = parseWMIVideoControllers(out, nil)
	if len(added) != 2 || !added[1].IsNVIDIA || added[1].Vendor != "NVIDIA" || added[1].Index != 1 {
		t.Errorf("WMI-only inventory = %+v", added)
	}
	// Placeholder adapters and blank lines are skipped.
	if got := parseWMIVideoControllers("\n|\n", nil); len(got) != 0 {
		t.Errorf("blank rows added: %+v", got)
	}
}

// Linux hybrid laptop: nvidia-smi prints the bus id with a domain prefix
// ("00000000:01:00.0") while lspci prints "01:00.0". The dGPU must be
// recognised as already listed and only the iGPU appended.
func TestParseLspciGPUs_HybridLaptop(t *testing.T) {
	existing := []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 4060 Laptop GPU", Vendor: "NVIDIA", IsNVIDIA: true, PCIBusID: "00000000:01:00.0"}}
	out := "00:02.0 VGA compatible controller [0300]: Intel Corporation Raptor Lake-P [Iris Xe Graphics] [8086:a7a0] (rev 04)\n" +
		"00:1f.3 Audio device [0403]: Intel Corporation Raptor Lake-P/U/H cAVS [8086:51ca] (rev 01)\n" +
		"01:00.0 3D controller [0302]: NVIDIA Corporation AD107M [GeForce RTX 4060 Max-Q / Mobile] [10de:28a0] (rev a1)\n"
	added := parseLspciGPUs(out, existing)
	if len(added) != 1 {
		t.Fatalf("added %d devices, want 1 (the iGPU): %+v", len(added), added)
	}
	if added[0].Vendor != "Intel" || added[0].PCIBusID != "00:02.0" || added[0].PCIVendorID != "8086" || added[0].Index != 1 {
		t.Errorf("iGPU = %+v", added[0])
	}
	// Without nvidia-smi both appear and the NVIDIA part is identified by vendor id.
	added = parseLspciGPUs(out, nil)
	if len(added) != 2 || !added[1].IsNVIDIA || added[1].PCIDeviceID != "28a0" {
		t.Errorf("lspci-only inventory = %+v", added)
	}
	if shortBusID("00000000:41:00.0") != "41:00.0" || shortBusID("41:00.0") != "41:00.0" {
		t.Error("shortBusID normalisation wrong")
	}
}

// Two physical cards with the same marketing name are two GPUs. WMI emits one
// Win32_VideoController row per adapter, and when nvidia-smi is unavailable
// (driver missing, or one card fell off the bus) WMI is the only inventory
// source, so collapsing identical names would hide half of a multi-GPU rig.
func TestParseWMIVideoControllers_IdenticalNamesAreSeparateGPUs(t *testing.T) {
	out := `NVIDIA GeForce RTX 4090|32.0.15.9186|4293918720|PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043&REV_A1\4&1A2B3C4D&0&0008
NVIDIA GeForce RTX 4090|32.0.15.9186|4293918720|PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043&REV_A1\4&5E6F7A8B&0&0019
`
	gpus := parseWMIVideoControllers(out, nil)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs from two identically named adapters, got %d: %+v", len(gpus), gpus)
	}
	for i, g := range gpus {
		if g.Index != i {
			t.Errorf("gpu %d: index = %d, want %d", i, g.Index, i)
		}
		if !g.IsNVIDIA || g.PCIVendorID != "10DE" || g.PCIDeviceID != "2684" {
			t.Errorf("gpu %d: vendor/device not parsed: %+v", i, g)
		}
	}

	// The exact same PNPDeviceID repeated is a duplicate row, not a second card.
	dup := `NVIDIA GeForce RTX 4090|32.0.15.9186|4293918720|PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043&REV_A1\4&1A2B3C4D&0&0008
NVIDIA GeForce RTX 4090|32.0.15.9186|4293918720|PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043&REV_A1\4&1A2B3C4D&0&0008
`
	if got := parseWMIVideoControllers(dup, nil); len(got) != 1 {
		t.Fatalf("expected duplicate PNPDeviceID rows to collapse to 1 GPU, got %d", len(got))
	}

	// Names already reported by nvidia-smi are still skipped.
	existing := []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 4090", IsNVIDIA: true}}
	if got := parseWMIVideoControllers(out, existing); len(got) != 0 {
		t.Fatalf("expected WMI rows matching nvidia-smi names to be skipped, got %d", len(got))
	}
}
