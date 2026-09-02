package redact

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// TestRedactIP_CIDR: FabricPort.IPv4 entries are CIDRs (cx7.go keeps the
// prefix length so cx7-twins-same-subnet can compare subnets); the address
// part must be redacted and the /nn suffix kept.
func TestRedactIP_CIDR(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	cases := map[string]string{
		"192.168.100.10/24":         "<lan-ip>/24",
		"192.168.101.10/24":         "<lan-ip>/24",
		"10.0.0.1/8":                "<lan-ip>/8",
		"203.0.113.7/24":            "<public-ip-redacted>/24",
		"203.0.113.7":               "<public-ip-redacted>",
		"127.0.0.1":                 "<lan-ip>",
		" 192.168.1.5/32 ":          "<lan-ip>/32",
		"580.95.05":                 "580.95.05",    // driver version: not an IP
		"2.155.11":                  "2.155.11",     // firmware version
		"32.0.16.1600":              "32.0.16.1600", // WDDM string: octet 1600 is not an IP
		"not-an-ip":                 "not-an-ip",
		"192.168.1.5/abc":           "192.168.1.5/abc", // malformed prefix: left alone rather than guessed
		"":                          "",
		"dgx-spark-firmware-behind": "dgx-spark-firmware-behind",
	}
	for in, want := range cases {
		if got := r.RedactIP(in); got != want {
			t.Errorf("RedactIP(%q) = %q, want %q", in, got, want)
		}
	}
	if got := New(false).RedactIP("192.168.100.10/24"); got != "192.168.100.10/24" {
		t.Errorf("disabled redactor altered %q", got)
	}
}

// TestApplyToReport_ClusterCIDRAndNewFields covers the fields the Spark
// collectors added after the first redaction pass: fabric CIDRs,
// LinuxInfo.GSPFailureLines, PlatformInfo.WoA.NvccPath, PyTorchInfo.Warnings
// and the Ecosystem path; and proves that versions, firmware, WDDM strings
// and rule ids survive.
func TestApplyToReport_ClusterCIDRAndNewFields(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", `C:\Users\alice`)
	rep := &types.Report{
		Cluster: &types.ClusterInfo{Ports: []types.FabricPort{
			{RDMADev: "rocep1s0f0", Netdev: "enp1s0f0np0", PCIAddr: "0000:01:00.0", Cage: 0, State: "4: ACTIVE", SpeedMbps: 200000, MTU: 9000, IPv4: []string{"192.168.100.10/24"}},
			{RDMADev: "roceP2p1s0f0", Netdev: "enP2p1s0f0np0", PCIAddr: "0002:01:00.0", Cage: 0, State: "4: ACTIVE", SpeedMbps: 200000, MTU: 9000, IPv4: []string{"192.168.101.10/24", "203.0.113.7/24"}},
		}},
		Linux: &types.LinuxInfo{GSPFailureLines: []string{
			"spark-a1b2 kernel: NVRM: RmInitAdapter failed! (0x62:0x65:2028)",
			"NVRM: GPU 000f:01:00.0: RmInitAdapter failed! (0x62:0x65:2028)",
		}},
		AI:        &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.9.0+cu130", Warnings: []string{`C:\Users\alice\venv\lib\site-packages\torch\cuda\__init__.py:262: UserWarning: Found GPU0 NVIDIA GB10 which is of cuda capability 12.1.`}}},
		Ecosystem: &types.EcosystemInfo{TritonPtxasPath: `C:\Users\alice\venv\ptxas.exe`, TritonPtxasVersion: "12.8.93"},
		Findings:  []types.Finding{{ID: "dgx-spark-firmware-behind", Evidence: "Embedded Controller 3.5.1 < 3.5.8; UEFI 2.150.3 < 2.155.11; USB PD 0.5.20 < 0.5.22; driver 580.95.05; peer 192.168.100.11/24 on spark-a1b2"}},
	}
	rep.Platform = types.PlatformInfo{
		Class: "rtx-spark", IsWindowsOnArm: true, NativeMachine: "ARM64",
		Firmware: []types.FirmwareComponent{{Name: "Embedded Controller", Version: "3.5.8"}, {Name: "UEFI Device Firmware", Version: "2.155.11", Pending: "2.160.1"}, {Name: "USB Power Delivery Controller", Version: "0.5.22"}},
		WoA:      &types.WoAInfo{AdapterName: "NVIDIA RTX Spark N1X", PNPDeviceID: `PCI\VEN_10DE&DEV_2E03&SUBSYS_00000000`, DriverVersion: "32.0.16.1600", InfFilename: "nv_surface_woa.inf", DeveloperPreview: true, NvccMachine: "ARM64", NvccPath: `C:\Users\alice\cuda\bin\nvcc.exe`},
	}
	ApplyToReport(rep, r)

	if got := rep.Cluster.Ports[0].IPv4; got[0] != "<lan-ip>/24" {
		t.Errorf("port 0 IPv4 = %v", got)
	}
	if got := rep.Cluster.Ports[1].IPv4; got[0] != "<lan-ip>/24" || got[1] != "<public-ip-redacted>/24" {
		t.Errorf("port 1 IPv4 = %v", got)
	}
	if p := rep.Cluster.Ports[0]; p.PCIAddr != "0000:01:00.0" || p.State != "4: ACTIVE" || p.SpeedMbps != 200000 || p.MTU != 9000 {
		t.Errorf("port facts altered: %+v", p)
	}
	if l := rep.Linux.GSPFailureLines; l[0] != "<host> kernel: NVRM: RmInitAdapter failed! (0x62:0x65:2028)" || l[1] != "NVRM: GPU 000f:01:00.0: RmInitAdapter failed! (0x62:0x65:2028)" {
		t.Errorf("GSP lines = %q", l)
	}
	if w := rep.AI.PyTorchInfo.Warnings[0]; !strings.HasPrefix(w, `<home>\venv`) || !strings.Contains(w, "capability 12.1") {
		t.Errorf("torch warning = %q", w)
	}
	if rep.AI.PyTorchInfo.Version != "2.9.0+cu130" {
		t.Errorf("torch version altered: %q", rep.AI.PyTorchInfo.Version)
	}
	if rep.Ecosystem.TritonPtxasPath != `<home>\venv\ptxas.exe` || rep.Ecosystem.TritonPtxasVersion != "12.8.93" {
		t.Errorf("ecosystem = %+v", rep.Ecosystem)
	}
	if rep.Platform.WoA.NvccPath != `<home>\cuda\bin\nvcc.exe` {
		t.Errorf("nvcc path = %q", rep.Platform.WoA.NvccPath)
	}
	if w := rep.Platform.WoA; w.DriverVersion != "32.0.16.1600" || w.InfFilename != "nv_surface_woa.inf" || w.PNPDeviceID != `PCI\VEN_10DE&DEV_2E03&SUBSYS_00000000` || w.AdapterName != "NVIDIA RTX Spark N1X" {
		t.Errorf("WoA facts altered: %+v", w)
	}
	for i, want := range []string{"3.5.8", "2.155.11", "0.5.22"} {
		if rep.Platform.Firmware[i].Version != want {
			t.Errorf("firmware[%d] = %q, want %q", i, rep.Platform.Firmware[i].Version, want)
		}
	}
	if rep.Platform.Firmware[1].Pending != "2.160.1" {
		t.Errorf("pending firmware altered: %q", rep.Platform.Firmware[1].Pending)
	}
	ev := rep.Findings[0].Evidence
	for _, keep := range []string{"3.5.1 < 3.5.8", "2.150.3 < 2.155.11", "0.5.20 < 0.5.22", "580.95.05", "<lan-ip>/24", "<host>"} {
		if !strings.Contains(ev, keep) {
			t.Errorf("evidence lost %q: %q", keep, ev)
		}
	}
	if strings.Contains(ev, "192.168") || strings.Contains(ev, "spark-a1b2") {
		t.Errorf("evidence not redacted: %q", ev)
	}
	if rep.Findings[0].ID != "dgx-spark-firmware-behind" {
		t.Errorf("rule id altered: %q", rep.Findings[0].ID)
	}
}

// TestRedact_VersionTable is the table the docs rely on: none of these
// strings may ever be touched by the IP filter or the token patterns.
func TestRedact_VersionTable(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	for _, s := range []string{
		"2.155.11", "3.5.8", "0.5.22", "0x02009b0b",
		"580.95.05", "580.159.03", "580.173.02", "32.0.16.1600", "616.00",
		"6.17.0-1026-nvidia", "7.5.0", "OTA2607", "1.91.51", "12.8.93", "2.9.0+cu130",
		"dgx-spark-firmware-behind", "gb10-pd-power-wedge", "cx7-twins-same-subnet", "rtx-spark-driver-developer-preview",
		"nvidia-spark-ota-check", "dgx-spark-fieldiag", "sm_121", "GB10", "N1X",
	} {
		if got := r.Redact(s); got != s {
			t.Errorf("Redact(%q) = %q", s, got)
		}
		if got := r.RedactIP(s); got != s {
			t.Errorf("RedactIP(%q) = %q", s, got)
		}
	}
}

func TestApplyToSnapshot_GSPAndCIDR(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	snap := &types.Snapshot{
		Linux:    &types.LinuxInfo{GSPFailureLines: []string{"spark-a1b2 kernel: NVRM: GPU requires reset"}},
		Platform: &types.PlatformInfo{Firmware: []types.FirmwareComponent{{Name: "Embedded Controller", Version: "3.5.8"}}, WoA: &types.WoAInfo{NvccPath: "/home/alice/cuda/bin/nvcc"}},
		DGXOS:    &types.DGXOSInfo{SerialNumber: "1234567890123", DriverPkgVersion: "580.159.03-0ubuntu1"},
	}
	ApplyToSnapshot(snap, r)
	if snap.Linux.GSPFailureLines[0] != "<host> kernel: NVRM: GPU requires reset" {
		t.Errorf("snapshot GSP line = %q", snap.Linux.GSPFailureLines[0])
	}
	if snap.Platform.WoA.NvccPath != "<home>/cuda/bin/nvcc" || snap.Platform.Firmware[0].Version != "3.5.8" {
		t.Errorf("snapshot platform = %+v %+v", snap.Platform.WoA, snap.Platform.Firmware)
	}
	if snap.DGXOS.SerialNumber != "<serial>" || snap.DGXOS.DriverPkgVersion != "580.159.03-0ubuntu1" {
		t.Errorf("snapshot dgx os = %+v", snap.DGXOS)
	}
}
