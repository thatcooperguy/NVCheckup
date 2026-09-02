package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	linuxCollector "github.com/thatcooperguy/nvcheckup/internal/collector/linux"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestMergeDGXOS_KeepsBothSides(t *testing.T) {
	torn := 0
	base := &types.DGXOSInfo{
		Name: "DGX Spark", PrettyName: "NVIDIA DGX Spark", SWBuildVersion: "7.2.3",
		OTAVersion: "7.5.0", SerialNumber: "1234567890123", FastOSVersion: "1.91.51",
	}
	extra := types.DGXOSInfo{
		Name:               "DGX Spark",
		OTAName:            "OTA2607",
		OTATorn:            &torn,
		DriverPkgVersion:   "580.159.03-0ubuntu1",
		FirmwarePkgVersion: "580.159.03-0ubuntu1",
		ModulesForKernel:   true,
		UnitsQueried:       true,
		DashboardActive:    true,
		FwupdActive:        true,
		DashboardPortOpen:  true,
	}
	got := mergeDGXOS(base, extra)
	if got == nil {
		t.Fatal("mergeDGXOS returned nil")
	}
	if got.PrettyName != "NVIDIA DGX Spark" || got.SWBuildVersion != "7.2.3" || got.OTAVersion != "7.5.0" || got.SerialNumber != "1234567890123" || got.FastOSVersion != "1.91.51" {
		t.Errorf("release-file fields lost: %+v", got)
	}
	if got.OTAName != "OTA2607" || got.OTATorn == nil || *got.OTATorn != 0 || got.DriverPkgVersion != "580.159.03-0ubuntu1" || !got.ModulesForKernel {
		t.Errorf("OTA/package fields lost: %+v", got)
	}
	if !got.UnitsQueried || !got.DashboardActive || !got.FwupdActive || !got.DashboardPortOpen || got.DashboardAdminActive {
		t.Errorf("unit fields altered: %+v", got)
	}
	// The base side stays untouched and the result is a copy.
	if base.UnitsQueried || base.OTAName != "" {
		t.Errorf("base mutated: %+v", base)
	}

	// No release-file half: the extra side is returned as is.
	if got := mergeDGXOS(nil, extra); got == nil || !got.UnitsQueried || got.OTAName != "OTA2607" {
		t.Errorf("nil base: %+v", got)
	}
	// Extra empty but a collector ran: still non-nil with the base facts and
	// UnitsQueried false (the analyzer contract for "units unknown").
	if got := mergeDGXOS(base, types.DGXOSInfo{}); got == nil || got.OTAVersion != "7.5.0" || got.UnitsQueried {
		t.Errorf("empty extra: %+v", got)
	}
	// A torn score on the base side only is copied, not aliased.
	baseTorn := 3
	got = mergeDGXOS(&types.DGXOSInfo{OTATorn: &baseTorn}, types.DGXOSInfo{})
	baseTorn = 9
	if got.OTATorn == nil || *got.OTATorn != 3 {
		t.Errorf("OTATorn aliasing: %v", got.OTATorn)
	}
}

func TestMergeWoAPlatform_Precedence(t *testing.T) {
	base := types.PlatformInfo{
		Class: common.ClassRTXSpark, GPUSoC: "N1X", Vendor: "Dell", Model: "Pro Max",
		IsWindowsOnArm: true, ProcessEmulated: true, NativeMachine: "ARM64",
		BIOSVersion: "1.2.3",
	}
	// CollectWoA on a copy: IsWow64Process2 says native (not emulated) and
	// found no adapter row, so Class/GPUSoC came back empty.
	woa := base
	woa.Class, woa.GPUSoC = "", ""
	woa.ProcessEmulated = false
	woa.Vendor = "Other"
	got := mergeWoAPlatform(base, woa)
	if got.Class != common.ClassRTXSpark || got.GPUSoC != "N1X" {
		t.Errorf("non-empty Class overwritten: %+v", got)
	}
	if got.ProcessEmulated || !got.IsWindowsOnArm || got.NativeMachine != "ARM64" {
		t.Errorf("IsWow64Process2 result must win: %+v", got)
	}
	if got.Vendor != "Dell" || got.Model != "Pro Max" || got.BIOSVersion != "1.2.3" {
		t.Errorf("base facts altered: %+v", got)
	}

	// The reverse: phase 1 knew nothing, CollectWoA classified the adapter.
	empty := types.PlatformInfo{}
	woa = types.PlatformInfo{Class: common.ClassRTXSpark, GPUSoC: "N1X", IsWindowsOnArm: true, NativeMachine: "ARM64",
		Vendor: "NVIDIA", Model: "RTX Spark", WoA: &types.WoAInfo{AdapterName: "NVIDIA RTX Spark N1X", DriverVersion: "32.0.16.1600", DeveloperPreview: true}}
	got = mergeWoAPlatform(empty, woa)
	if got.Class != common.ClassRTXSpark || got.GPUSoC != "N1X" || !got.IsWindowsOnArm || got.NativeMachine != "ARM64" || got.Vendor != "NVIDIA" || got.Model != "RTX Spark" {
		t.Errorf("CollectWoA facts not adopted: %+v", got)
	}
	if got.WoA == nil || got.WoA.DriverVersion != "32.0.16.1600" || !got.WoA.DeveloperPreview {
		t.Errorf("WoA not adopted: %+v", got.WoA)
	}

	// x64 desktop: nothing to merge, nothing invented.
	got = mergeWoAPlatform(types.PlatformInfo{NativeMachine: "AMD64"}, types.PlatformInfo{NativeMachine: "AMD64"})
	if got.Class != "" || got.IsWindowsOnArm || got.ProcessEmulated || got.WoA != nil || got.NativeMachine != "AMD64" {
		t.Errorf("x64 host: %+v", got)
	}
}

func TestApplyNVRMMessages(t *testing.T) {
	r := &types.Report{
		UnifiedMemory: &types.UnifiedMemoryInfo{OOMKills: 2, NVRMNoMemory: 0},
		Cluster:       &types.ClusterInfo{},
	}
	nv := linuxCollector.NVRMMessages{
		GSPFailureLines:  []string{"NVRM: RmInitAdapter failed! (0x62:0x65:2028)"},
		NoMemoryCount:    3,
		OOMKillCount:     1,
		PeermemAttempted: true,
	}
	applyNVRMMessages(r, nv)
	if r.Linux == nil || len(r.Linux.GSPFailureLines) != 1 || r.Linux.GSPFailureLines[0] != nv.GSPFailureLines[0] {
		t.Errorf("GSP lines: %+v", r.Linux)
	}
	if r.UnifiedMemory.NVRMNoMemory != 3 || r.UnifiedMemory.OOMKills != 2 {
		t.Errorf("counts must be the max of both scans, got %+v", r.UnifiedMemory)
	}
	if !r.Cluster.PeermemAttempted {
		t.Error("PeermemAttempted not set")
	}

	// Nothing to report: no LinuxInfo is allocated, nil sections stay nil.
	empty := &types.Report{}
	applyNVRMMessages(empty, linuxCollector.NVRMMessages{NoMemoryCount: 5, PeermemAttempted: true})
	if empty.Linux != nil || empty.UnifiedMemory != nil || empty.Cluster != nil {
		t.Errorf("sections invented: %+v", empty)
	}
	applyNVRMMessages(nil, nv)
}

func TestSyncTorchFacts(t *testing.T) {
	r := &types.Report{
		Ecosystem: &types.EcosystemInfo{TorchArchList: []string{"sm_80", "sm_120"}},
		AI:        &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Warnings: []string{"Found GPU0 NVIDIA GB10 which is of cuda capability 12.1."}}},
	}
	syncTorchFacts(r)
	if len(r.AI.PyTorchInfo.ArchList) != 2 || r.AI.PyTorchInfo.ArchList[1] != "sm_120" {
		t.Errorf("arch list not copied to PyTorchInfo: %+v", r.AI.PyTorchInfo)
	}
	if len(r.Ecosystem.TorchWarnings) != 1 || r.Ecosystem.TorchWarnings[0] != r.AI.PyTorchInfo.Warnings[0] {
		t.Errorf("warnings not copied to Ecosystem: %+v", r.Ecosystem)
	}
	// A missing torch probe is never invented.
	r2 := &types.Report{Ecosystem: &types.EcosystemInfo{TorchArchList: []string{"sm_120"}}, AI: &types.AIInfo{}}
	syncTorchFacts(r2)
	if r2.AI.PyTorchInfo != nil {
		t.Error("PyTorchInfo must not be allocated")
	}
	syncTorchFacts(nil)
}

func TestClusterCollected(t *testing.T) {
	if clusterCollected(types.ClusterInfo{}) {
		t.Error("empty cluster must not be published")
	}
	if !clusterCollected(types.ClusterInfo{HotplugFileEnabled: true}) || !clusterCollected(types.ClusterInfo{Ports: []types.FabricPort{{Netdev: "enp1s0f0np0"}}}) {
		t.Error("ports or the hotplug marker publish the section")
	}
}

// TestGB10SimRootParsers drives the DGX OS, host-state and ConnectX-7
// collectors against the committed GB10 fixture tree
// (.github/fieldtest/simroot/gb10) with an empty PATH, so only the file
// parsers run (the Linux-only command paths cannot execute on the dev box).
// It proves that the wiring receives the fields the CI job asserts on
// (spec section 10) from the fixtures alone.
func TestGB10SimRootParsers(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".github", "fieldtest", "simroot", "gb10"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "dgx-release")); err != nil {
		t.Skipf("GB10 simroot fixture not present: %v", err)
	}
	t.Setenv(common.SimRootEnv, root)
	t.Setenv("PATH", t.TempDir()) // no shims, no host tools: parsers only

	base, _ := common.CollectDGXRelease()
	if base == nil || base.OTAVersion != "7.5.0" {
		t.Fatalf("CollectDGXRelease from simroot: %+v", base)
	}
	extra, _ := linuxCollector.CollectDGXOS(5)
	dgx := mergeDGXOS(base, extra)
	if dgx.Name != "DGX Spark" || dgx.PrettyName != "NVIDIA DGX Spark" || dgx.OTAVersion != "7.5.0" || dgx.SWBuildVersion != "7.2.3" || dgx.FastOSVersion != "1.91.51" {
		t.Errorf("release fields: %+v", dgx)
	}
	if dgx.SerialNumber != "1234567890123" {
		t.Errorf("serial must be raw before redaction: %q", dgx.SerialNumber)
	}
	if !dgx.DashboardPortOpen {
		t.Error("DashboardPortOpen: /proc/net/tcp fixture lists 0x2AF8 (11000) in LISTEN")
	}
	if dgx.UnitsQueried {
		t.Error("UnitsQueried must stay false when systemctl is unavailable")
	}
	if dgx.AptSourceCorrupt != "" {
		t.Errorf("apt source flagged corrupt: %q", dgx.AptSourceCorrupt)
	}

	cl, _ := linuxCollector.CollectCluster(5)
	if !clusterCollected(cl) {
		t.Fatal("cluster not collected from the sysfs fixture")
	}
	if len(cl.Ports) != 4 {
		t.Fatalf("ports = %d, want 4: %+v", len(cl.Ports), cl.Ports)
	}
	active, cages := 0, map[int]bool{}
	for _, p := range cl.Ports {
		cages[p.Cage] = true
		if p.State == "4: ACTIVE" {
			active++
			if p.Cage != 0 || p.SpeedMbps != 200000 || p.MTU != 9000 || p.PhysState != "5: LinkUp" {
				t.Errorf("active port not healthy per spec 9: %+v", p)
			}
		}
		if p.PCIAddr == "" || p.Netdev == "" || p.RDMADev == "" {
			t.Errorf("port incomplete: %+v", p)
		}
	}
	if active != 2 || !cages[0] || !cages[1] || len(cages) != 2 {
		t.Errorf("active=%d cages=%v: %+v", active, cages, cl.Ports)
	}
	if !cl.HotplugFileEnabled || cl.UfwEnabled {
		t.Errorf("hotplug=%v ufw=%v", cl.HotplugFileEnabled, cl.UfwEnabled)
	}

	var p types.PlatformInfo
	linuxCollector.CollectDGXHostState(5, &p)
	if p.ACPIThermalMC["thermal_zone0"] != 45000 {
		t.Errorf("acpitz zone: %v", p.ACPIThermalMC)
	}
	if p.ClockCapUnit != "" {
		t.Errorf("stock unit must have no clock-cap service: %q", p.ClockCapUnit)
	}
	if p.PstoreEmpty != nil && !*p.PstoreEmpty {
		t.Error("pstore fixture is empty when present")
	}
}
