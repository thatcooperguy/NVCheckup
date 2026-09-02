package redact

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Version, firmware, kernel, BIOS and OTA strings from docs/roadmap/
// spark-support.md must survive redaction untouched: none of them is an IP,
// a MAC or a hostname.
func TestRedact_SparkVersionStringsSurvive(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	keep := []string{
		"580.159.03", "580.173.02", "2.155.11", "3.5.8", "0.5.22", "0x02009b0b", "0x03000508", "0x00000516",
		"6.17.0-1026-nvidia", "6.11.0-1004-nvidia-64k", "5.36_0ACUM023", "GX10DGX.0102.2025.1111.1531",
		"580.159.03-0ubuntu0.24.04.1", "32.0.16.1600", "OTA2607", "7.5.0", "V13.0.88", "1.91.51",
		"0000000F:01:00.0", "000f:01:00.0", "GPU-0f01c2c2-0000-4000-8000-0000000f0100",
		"dgx-spark-fieldiag", "nvidia-spark-ota-check", "DGX Spark", "dgx-spark-detected", "NVIDIA GB10",
	}
	for _, s := range keep {
		in := "value " + s + " end"
		if got := r.Redact(in); got != in {
			t.Errorf("Redact(%q) = %q, want unchanged", in, got)
		}
	}
	// The firmware line as fwupdmgr prints it.
	in := "UEFI Device Firmware: current version 2.155.11, EC 3.5.8, USB PD 0.5.22 (driver 580.159.03, kernel 6.17.0-1026-nvidia)"
	if got := r.Redact(in); got != in {
		t.Errorf("firmware line altered: %q", got)
	}
}

func TestRedact_MACAndSparkHostname(t *testing.T) {
	r := NewWithIdentity(true, "", "", "")
	cases := map[string]string{
		"link/ether a8:1e:84:0f:01:00 brd ff:ff:ff:ff:ff:ff":         "link/ether <mac> brd <mac>",
		"Physical Address. . . . . . . . . : 00-15-5D-01-02-03":      "Physical Address. . . . . . . . . : <mac>",
		"avahi-daemon: Host name conflict, retrying with spark-2e12": "avahi-daemon: Host name conflict, retrying with <host>",
		"peer spark-a1b2c3 at 192.168.100.2":                         "peer <host> at <lan-ip>",
		// Not MACs: PCI bus ids, a GPU UUID, an Xid PCI tag.
		"Bus-Id 00000000:41:00.0 and 0000000F:01:00.0":                 "Bus-Id 00000000:41:00.0 and 0000000F:01:00.0",
		"NVRM: Xid (PCI:000f:01:00): 119, Timeout after 6s":            "NVRM: Xid (PCI:000f:01:00): 119, Timeout after 6s",
		"UUID: GPU-0f01c2c2-0000-4000-8000-0000000f0100":               "UUID: GPU-0f01c2c2-0000-4000-8000-0000000f0100",
		"packages dgx-spark-fieldiag nvidia-spark-ota-check dgx-spark": "packages dgx-spark-fieldiag nvidia-spark-ota-check dgx-spark",
	}
	for in, want := range cases {
		if got := r.Redact(in); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestApplyToReport_SparkStructs(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	rep := &types.Report{
		DGXOS: &types.DGXOSInfo{Name: "DGX Spark", OTAVersion: "7.5.0", SerialNumber: "1234567890123", FwupdError: "libfwupd version 1.9.34 does not match daemon 1.9.30 on spark-a1b2"},
		Cluster: &types.ClusterInfo{
			Ports:   []types.FabricPort{{RDMADev: "rocep1s0f0", Netdev: "enp1s0f0np0", IPv4: []string{"192.168.100.1", "203.0.113.7"}, SpeedMbps: 200000}},
			NCCLEnv: map[string]string{"NCCL_IB_HCA": "rocep1s0f0,roceP2p1s0f0", "NCCL_SOCKET_IFNAME": "enp1s0f0np0", "UCX_NET_DEVICES": "spark-a1b2:1"},
		},
		Ecosystem: &types.EcosystemInfo{
			Images:        []types.ContainerImage{{Ref: "registry.local/alice/vllm:cu130-nightly", Arch: "arm64"}, {Ref: "nvcr.io/nvidia/vllm:26.05-py3", Arch: "arm64"}},
			TorchWarnings: []string{"Found GPU0 NVIDIA GB10 which is of cuda capability 12.1. (/home/alice/.venv)"},
		},
	}
	rep.Platform = types.PlatformInfo{Class: "dgx-spark", Vendor: "NVIDIA", Model: "NVIDIA_DGX_Spark", BIOSVersion: "5.36_0ACUM023",
		Firmware: []types.FirmwareComponent{{Name: "UEFI Device Firmware", Version: "2.155.11"}}, PrevBootLastLine: "spark-a1b2 systemd-shutdown[1]: Journal stopped"}
	ApplyToReport(rep, r)

	if rep.DGXOS.SerialNumber != "<serial>" {
		t.Errorf("serial = %q", rep.DGXOS.SerialNumber)
	}
	if rep.DGXOS.OTAVersion != "7.5.0" || rep.DGXOS.Name != "DGX Spark" {
		t.Errorf("DGX OS versions altered: %+v", rep.DGXOS)
	}
	if !strings.Contains(rep.DGXOS.FwupdError, "1.9.34") || !strings.Contains(rep.DGXOS.FwupdError, "<host>") {
		t.Errorf("fwupd error = %q", rep.DGXOS.FwupdError)
	}
	if got := rep.Cluster.Ports[0].IPv4; got[0] != "<lan-ip>" || got[1] != "<public-ip-redacted>" {
		t.Errorf("fabric IPv4 = %v", got)
	}
	if rep.Cluster.Ports[0].RDMADev != "rocep1s0f0" || rep.Cluster.Ports[0].Netdev != "enp1s0f0np0" || rep.Cluster.Ports[0].SpeedMbps != 200000 {
		t.Errorf("fabric names altered: %+v", rep.Cluster.Ports[0])
	}
	if rep.Cluster.NCCLEnv["NCCL_IB_HCA"] != "rocep1s0f0,roceP2p1s0f0" || rep.Cluster.NCCLEnv["UCX_NET_DEVICES"] != "<host>:1" {
		t.Errorf("NCCL env = %v", rep.Cluster.NCCLEnv)
	}
	if rep.Ecosystem.Images[0].Ref != "registry.local/<user>/vllm:cu130-nightly" || rep.Ecosystem.Images[1].Ref != "nvcr.io/nvidia/vllm:26.05-py3" {
		t.Errorf("images = %+v", rep.Ecosystem.Images)
	}
	if !strings.Contains(rep.Ecosystem.TorchWarnings[0], "<home>") || !strings.Contains(rep.Ecosystem.TorchWarnings[0], "12.1") {
		t.Errorf("torch warning = %q", rep.Ecosystem.TorchWarnings[0])
	}
	if rep.Platform.PrevBootLastLine != "<host> systemd-shutdown[1]: Journal stopped" {
		t.Errorf("prev boot line = %q", rep.Platform.PrevBootLastLine)
	}
	if rep.Platform.Firmware[0].Version != "2.155.11" || rep.Platform.BIOSVersion != "5.36_0ACUM023" || rep.Platform.Model != "NVIDIA_DGX_Spark" {
		t.Errorf("platform versions altered: %+v", rep.Platform)
	}

	// Nil pointers and a disabled redactor are no-ops.
	ApplyToReport(&types.Report{}, r)
	plain := &types.Report{DGXOS: &types.DGXOSInfo{SerialNumber: "1234567890123"}}
	ApplyToReport(plain, New(false))
	if plain.DGXOS.SerialNumber != "1234567890123" {
		t.Error("disabled redactor must not touch the serial")
	}
}

func TestApplyToSnapshot_SparkStructs(t *testing.T) {
	r := NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice")
	snap := &types.Snapshot{
		Platform: &types.PlatformInfo{Class: "dgx-spark", PrevBootLastLine: "spark-a1b2 kernel: reboot"},
		DGXOS:    &types.DGXOSInfo{SerialNumber: "1234567890123", OTAVersion: "7.5.0"},
	}
	ApplyToSnapshot(snap, r)
	if snap.DGXOS.SerialNumber != "<serial>" || snap.DGXOS.OTAVersion != "7.5.0" || snap.Platform.PrevBootLastLine != "<host> kernel: reboot" {
		t.Errorf("snapshot = %+v %+v", snap.Platform, snap.DGXOS)
	}
	ApplyToSnapshot(&types.Snapshot{}, r) // nil platform / dgx os
}

func TestSummaryMentionsNewTokens(t *testing.T) {
	s := New(true).Summary()
	for _, tok := range []string{"<mac>", "<serial>"} {
		if !strings.Contains(s, tok) {
			t.Errorf("Summary lacks %s", tok)
		}
	}
}
