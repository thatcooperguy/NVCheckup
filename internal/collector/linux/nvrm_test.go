package linux

import (
	"reflect"
	"strings"
	"testing"
)

// gspFailureDmesg carries the exact GSP failure strings of spec 3.2, the
// NVRM out-of-memory and OOM-killer lines of rule unified-memory-oom-events,
// and the nvidia_peermem symbol error of rule nccl-gdr-assumed (S69).
const gspFailureDmesg = `[    3.312611] NVRM: loading NVIDIA Open GPU Kernel Module for aarch64  580.159.03  Release Build
[   12.345678] NVRM: Xid (PCI:000f:01:00): 119, Timeout after 6s of waiting for RPC response from GPU0 GSP!
[   12.345680] NVRM: ksec2PrepareBootCommands_GB20B: SEC2 secure boot partition timed out.
[   12.345681] NVRM: RmInitAdapter: Cannot initialize GSP firmware RM
[   12.345682] NVRM: GPU 000f:01:00.0: RmInitAdapter failed! (0x62:0x65:2028)
[   40.000000] NVRM: gpuHandleSanityCheckRegReadError_GH100: Possible bad register read: addr: 0x611ef0, regvalue: 0xbadf5600
[  100.000000] NVRM: nvCheckOkFailedNoLog: Check failed: Out of memory [NV_ERR_NO_MEMORY]
[  101.000000] Out of memory: Killed process 4242 (python) total-vm:123456kB
[    5.000000] nvidia_peermem: Unknown symbol ib_register_peer_memory_client (err -2)
[    6.101234] mlx5_core 0000:01:00.0: mlx5_pcie_event:326:(pid 165): Detected insufficient power on the PCIe slot (27W).
`

func TestScanNVRMMessages(t *testing.T) {
	res := ScanNVRMMessages(gspFailureDmesg)
	if len(res.GSPFailureLines) != 5 {
		t.Fatalf("GSP failure lines = %d, want 5: %v", len(res.GSPFailureLines), res.GSPFailureLines)
	}
	wantMarkers := []string{
		"Timeout after 6s of waiting for RPC response from GPU0 GSP!",
		"SEC2 secure boot partition timed out",
		"Cannot initialize GSP firmware RM",
		"RmInitAdapter failed! (0x62:0x65:2028)",
		"0xbadf5600",
	}
	for i, m := range wantMarkers {
		if !strings.Contains(res.GSPFailureLines[i], m) {
			t.Errorf("line %d = %q, want it to contain %q", i, res.GSPFailureLines[i], m)
		}
	}
	if res.NoMemoryCount != 1 || res.OOMKillCount != 1 {
		t.Errorf("NoMemory=%d OOM=%d, want 1/1", res.NoMemoryCount, res.OOMKillCount)
	}
	if !res.PeermemAttempted {
		t.Error("nvidia_peermem line must set PeermemAttempted")
	}
}

func TestScanNVRMMessagesHealthy(t *testing.T) {
	healthy := "[    3.312611] NVRM: loading NVIDIA Open GPU Kernel Module for aarch64  580.159.03  Release Build\n" +
		"[    6.101234] mlx5_core 0000:01:00.0: mlx5_pcie_event:326:(pid 165): Detected insufficient power on the PCIe slot (27W).\n"
	if res := ScanNVRMMessages(healthy); !reflect.DeepEqual(res, NVRMMessages{}) {
		t.Errorf("healthy log must give an empty result, got %+v", res)
	}
}

func TestScanNVRMMessagesCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxGSPFailureLines+7; i++ {
		b.WriteString("NVRM: Xid (PCI:000f:01:00): 120, GSP task exception line ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	res := ScanNVRMMessages(b.String())
	if len(res.GSPFailureLines) != maxGSPFailureLines {
		t.Errorf("cap = %d lines, want %d", len(res.GSPFailureLines), maxGSPFailureLines)
	}
}
