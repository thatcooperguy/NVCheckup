package analyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/analyzer/fixtures"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// ── corpus ────────────────────────────────────────────────────────────

type sparkCorpusEntry struct {
	mode   types.RunMode
	report *types.Report
}

// gb10 returns the healthy GB10 fixture after applying mutations.
func gb10(mutate ...func(r *types.Report)) *types.Report {
	r := fixtures.GB10()
	for _, m := range mutate {
		m(r)
	}
	return r
}

// rtxSpark returns the RTX Spark fixture after applying mutations.
func rtxSpark(mutate ...func(r *types.Report)) *types.Report {
	r := fixtures.RTXSpark()
	for _, m := range mutate {
		m(r)
	}
	return r
}

func withLogs(lines ...string) func(r *types.Report) {
	return func(r *types.Report) {
		if r.Linux == nil {
			r.Linux = &types.LinuxInfo{}
		}
		r.Linux.DmesgSnippets += strings.Join(lines, "\n") + "\n"
	}
}

func withAI(ai *types.AIInfo) func(r *types.Report) {
	return func(r *types.Report) { r.AI = ai }
}

func withEco(eco *types.EcosystemInfo) func(r *types.Report) {
	return func(r *types.Report) { r.Ecosystem = eco }
}

func withCluster(mutate func(c *types.ClusterInfo)) func(r *types.Report) {
	return func(r *types.Report) { mutate(r.Cluster) }
}

// wedgeSampleInfo is one thermal sample carrying the PD wedge signature of
// spec 5 (util >= 90, SM < 1400 MHz, < 40 W, reasons Not Active).
func wedgeSampleInfo(idx int) types.ThermalInfo {
	return types.ThermalInfo{TemperatureC: 48, PowerState: "P0", CurrentClockMHz: 611, MaxClockMHz: 3003, PowerDrawW: "27.5", UtilizationPct: 99, GPUIndex: idx, EventCounters: map[string]int64{"sw_power_capping": 123456}}
}

// sparkCorpus lists, per rule, a report that triggers it; ruleCorpus appends
// it so TestRulesJSON_MatchesAnalyzer sees every Spark rule fire.
func sparkCorpus() []sparkCorpusEntry {
	full := func(r *types.Report) sparkCorpusEntry { return sparkCorpusEntry{types.ModeFull, r} }
	falseP, trueP := false, true
	torn := 1
	return []sparkCorpusEntry{
		// dgx-spark-detected, unified-memory-nvsmi-expected, secureboot-ok
		full(gb10()),
		// rtx-spark-detected, rtx-spark-driver-developer-preview, nvidia-smi-missing (INFO)
		full(rtxSpark()),
		// grace-hopper-detected
		full(&types.Report{
			System:   types.SystemInfo{Architecture: "arm64"},
			GPUs:     []types.GPUInfo{{Index: 0, Name: "NVIDIA GH200 480GB", Vendor: "NVIDIA", IsNVIDIA: true, VRAMTotalMB: 97871, ComputeCap: "9.0"}},
			Driver:   types.DriverInfo{Version: "580.65.06", CUDAVersion: "13.0", NvidiaSmiPath: "nvidia-smi"},
			Platform: types.PlatformInfo{Class: classGraceHopper, GPUSoC: "GH200", ComputeCap: "9.0"},
		}),
		// unified-memory-pressure (WARN), unified-memory-swap-in-use (WARN), unified-memory-oom-events (CRIT)
		full(gb10(func(r *types.Report) {
			r.UnifiedMemory.GPUProcesses = 1
			r.UnifiedMemory.MemAvailableKB = 6 * kbPerGiB
			r.UnifiedMemory.SwapFreeKB = r.UnifiedMemory.SwapTotalKB - 3*kbPerGiB
			r.UnifiedMemory.OOMKills = 2
			r.UnifiedMemory.NVRMNoMemory = 1
		})),
		// unified-memory-page-cache-hold
		full(gb10(func(r *types.Report) {
			r.UnifiedMemory.MemFreeKB = 2 * kbPerGiB
			r.UnifiedMemory.MemAvailableKB = 60 * kbPerGiB
		})),
		// dgx-spark-gsp-init-failure, dgx-spark-ota-torn (pkg mismatch), xid-errors
		full(fixtures.GB10GSPFail()),
		// dgx-spark-ota-torn via torn score
		full(gb10(func(r *types.Report) { r.DGXOS.OTATorn = &torn })),
		// dgx-spark-driver-too-old
		full(gb10(func(r *types.Report) { r.Driver.Version = "570.86.10"; r.Driver.CUDAVersion = "12.8" })),
		// dgx-spark-driver-branch-unsupported
		full(gb10(func(r *types.Report) { r.Driver.Version = "590.48.01"; r.Driver.CUDAVersion = "13.1" })),
		// dgx-spark-foreign-driver-packages
		full(gb10(func(r *types.Report) {
			r.Linux.NVIDIAPackages = append(r.Linux.NVIDIAPackages, "nvidia-fabricmanager-580 580.159.03-0ubuntu1", "nvidia-dkms-580-open-server 580.159.03-0ubuntu1")
		})),
		// dgx-spark-cublas-batch-bug
		full(gb10(withLogs("vllm: CUBLAS failure 14: CUBLAS_STATUS_INTERNAL_ERROR"))),
		// dgx-spark-non-nvidia-kernel
		full(gb10(func(r *types.Report) {
			r.System.KernelVersion = "6.14.0-29-generic"
			r.Platform.NvidiaKernelFlavour = false
		})),
		// dgx-spark-ota-outdated
		full(gb10(func(r *types.Report) {
			r.DGXOS.OTAVersion = "7.2.3"
			r.Driver.Version = "580.126.09"
			r.GPUs[0].DriverVersion = "580.126.09"
		})),
		// dgx-spark-dashboard-unhealthy
		full(gb10(func(r *types.Report) {
			r.DGXOS.DashboardActive = false
			r.DGXOS.FwupdError = "libfwupd version 1.9.34 does not match daemon 1.9.30"
		})),
		// dgx-spark-firmware-behind (FE, EC one patch level behind)
		full(gb10(func(r *types.Report) { r.Platform.Firmware[0].Version = "0x03000507" })),
		// gb10-pd-power-wedge CRIT (two matching samples) + suppressed gpu-power-state-stuck
		full(gb10(func(r *types.Report) {
			a, b := wedgeSampleInfo(0), wedgeSampleInfo(0)
			r.GPUThermal = []types.ThermalInfo{a, b}
			r.Thermal = &r.GPUThermal[0]
		})),
		// gb10-logless-hard-poweroff WARN (two unclean boots)
		full(gb10(func(r *types.Report) {
			r.Platform.PrevBootClean = &falseP
			r.Platform.PrevBootLastLine = "gnome-shell[2041]: Running GNOME Shell (using mutter 46.2) as a Wayland display server"
			r.Platform.PstoreEmpty = &trueP
			r.Platform.UncleanBoots = 2
		})),
		// gb10-acpi-thermal-zone-hot
		full(gb10(func(r *types.Report) { r.Platform.ACPIThermalMC["thermal_zone2"] = 96500 })),
		// gb10-clock-cap-active
		full(gb10(func(r *types.Report) { r.Platform.ClockCapUnit = "gb10-clock-cap.service" })),
		// dgx-spark-suspend-failure
		full(gb10(func(r *types.Report) { r.Platform.SuspendAttempts = 1; r.Platform.SuspendFailed = true })),
		// dgx-spark-cx7-slot-power-benign
		full(gb10(withLogs("mlx5_core 0000:01:00.0: mlx5_pcie_event:326:(pid 165): Detected insufficient power on the PCIe slot (27W)."))),
		// arm64-cuda12-wheel-on-cuda13 (WARN) and sm121-torch-capability-warning-benign
		full(gb10(withAI(&types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.7.0+cu128", CUDAVersion: "12.8", CUDAAvailable: true, DeviceName: "NVIDIA GB10",
			Warnings: []string{"Found GPU0 NVIDIA GB10 which is of cuda capability 12.1. Minimum and Maximum cuda capability supported by this version of PyTorch is (8.0) - (12.0)"}}}))),
		// arm64-cuda12-wheel-on-cuda13 CRIT (import error) + pytorch-import-error
		full(gb10(withAI(&types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Error: "ImportError: libcudart.so.12: cannot open shared object file: No such file or directory"}}))),
		// sm121-kernel-missing (arch list without sm_120/sm_121)
		full(gb10(withAI(&types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.4.0+cu124", CUDAVersion: "12.4", CUDAAvailable: true, ArchList: []string{"sm_80", "sm_86", "sm_90"}}}))),
		// sm121-triton-ptxas-stale, arm64-flash-attn-no-wheel, arm64-container-amd64-image,
		// sm121-ngc-image-too-old, docker-snap-gpu-blocked, docker-cdi-spec-missing (WARN),
		// onnxruntime-cuda-provider-missing
		full(gb10(withEco(&types.EcosystemInfo{
			TritonPtxasVersion: "12.8.93",
			FlashAttnVersion:   "2.7.4",
			Images: []types.ContainerImage{
				{Ref: "nvcr.io/nvidia/pytorch:24.08-py3", Arch: "arm64"},
				{Ref: "ghcr.io/example/tool:latest", Arch: "amd64"},
			},
			SnapDocker:     true,
			DockerCDI:      true,
			CDISpecPresent: false,
			DockerRuntimes: []string{"runc"},
			ORTVersion:     "1.20.1",
			ORTProviders:   []string{"CPUExecutionProvider", "AzureExecutionProvider"},
		}))),
		// docker-cdi-spec-missing INFO variant (no runtimes.nvidia)
		full(gb10(withEco(&types.EcosystemInfo{DockerRuntimes: []string{"runc"}}))),
		// gb10-k8s-device-plugin-old
		full(gb10(withLogs("nvidia-device-plugin: error getting device memory: Not Supported"))),
		// cx7-not-enumerated CRIT (no 15b3 functions, hotplug removal)
		full(gb10(func(r *types.Report) { r.Cluster = nil }, withLogs("cx7-pcie-hotplug MTKP0001:00: Cable removal"))),
		// cx7-not-enumerated WARN (regression kernel, NIC present)
		full(gb10(func(r *types.Report) { r.System.KernelVersion = "6.17.0-1021-nvidia" })),
		// cx7-twin-link-mismatch, cx7-up-no-ip (INFO not persisted), cx7-mtu-mismatch
		full(gb10(withCluster(func(c *types.ClusterInfo) {
			c.Ports[1].State = "1: DOWN"
			c.Ports[1].PhysState = "3: Disabled"
			c.Ports[1].MTU = 1500
			c.Ports[0].Persistent = false
		}))),
		// cx7-link-speed-degraded, cx7-up-no-ip (WARN), cx7-firewall-blocks-cluster
		full(gb10(withCluster(func(c *types.ClusterInfo) {
			c.Ports[0].SpeedMbps = 100000
			c.Ports[1].IPv4 = nil
			c.UfwEnabled = true
		}))),
		// cx7-twins-same-subnet, cx7-mdns-hostname-conflict (WARN), nccl-env-misconfigured, nccl-gdr-assumed
		full(gb10(withCluster(func(c *types.ClusterInfo) {
			c.Ports[1].IPv4 = []string{"192.168.100.2/24"}
			c.AvahiConflicts = 1
			c.NCCLEnv = map[string]string{"NCCL_IB_DISABLE": "1", "NCCL_NET_GDR_LEVEL": "5"}
		}))),
		// cx7-mdns-hostname-conflict INFO (avahi absent while twins are Up)
		full(gb10(withCluster(func(c *types.ClusterInfo) { c.AvahiActive = false; c.RDMATools = nil }))),
		// woa-cuda-toolkit-not-native, woa-nvcheckup-emulated, woa-windows-build-too-old
		full(rtxSpark(func(r *types.Report) {
			r.AI = &types.AIInfo{CUDAToolkitVersion: "13.0", NvccPath: `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.0\bin\nvcc.exe`}
			r.Platform.ProcessEmulated = true
			r.System.Architecture = "amd64"
			r.System.OSBuild = "22631"
		})),
		// wsl-linux-driver-installed
		full(&types.Report{
			GPUs:   []types.GPUInfo{{Name: "NVIDIA GeForce RTX 4090", Vendor: "NVIDIA", IsNVIDIA: true, VRAMTotalMB: 24564}},
			Driver: types.DriverInfo{Version: "580.65.06", CUDAVersion: "13.0", NvidiaSmiPath: "nvidia-smi"},
			WSL:    &types.WSLInfo{IsWSL: true, Distro: "Ubuntu-24.04", DevDxgExists: true, NvidiaSmiOK: true},
			Linux:  &types.LinuxInfo{LoadedModules: map[string]bool{}, LibCudaPath: "/usr/lib/wsl/lib/libcuda.so", NVIDIAPackages: []string{"nvidia-driver-550 550.120-0ubuntu1"}},
		}),
		// rtx-spark-linux-unsupported
		full(&types.Report{
			Metadata: types.ReportMetadata{Timestamp: fixtures.FixtureTime},
			System:   types.SystemInfo{OSName: "Ubuntu", Architecture: "arm64", KernelVersion: "6.17.0-1031-nvidia"},
			GPUs:     []types.GPUInfo{{Index: 0, Name: "NVIDIA Device 2e03", Vendor: "NVIDIA", IsNVIDIA: true, PCIVendorID: "10de", PCIDeviceID: "2e03", MemoryReporting: "not-supported", OnPackage: true}},
			Driver:   types.DriverInfo{Version: "580.159.03", CUDAVersion: "13.0", NvidiaSmiPath: "nvidia-smi"},
			Linux:    &types.LinuxInfo{Distro: "Ubuntu", DistroVersion: "24.04", LoadedModules: map[string]bool{"nvidia": true}, LibCudaPath: "/usr/lib/libcuda.so", DevNvidiaNodes: []string{"/dev/nvidia0"}},
			Platform: types.PlatformInfo{Class: classRTXSpark, GPUSoC: "N1X", UnifiedMemory: true},
		}),
	}
}

// ── spec section 10: the healthy GB10 report ──────────────────────────

// gb10ForbiddenIDs are the ids spec section 10 says the healthy GB10 scenario
// must never produce.
var gb10ForbiddenIDs = []string{
	"low-vram", "pcie-downshift", "pcie-width-reduced", "pcie-idle-power-saving", "fan-not-spinning",
	"gpu-power-cap", "gpu-clock-slowdown", "no-nvidia-gpu", "driver-not-detected", "nvidia-smi-missing",
	"jetson-detected", "dgx-spark-gsp-init-failure", "dgx-spark-ota-torn", "dgx-spark-foreign-driver-packages",
	"cx7-not-enumerated",
}

func TestGB10Fixture_ExactFindingSet(t *testing.T) {
	r := fixtures.GB10()
	Analyze(r, types.ModeFull)
	got := ids(r.Findings)
	sort.Strings(got)
	want := []string{"dgx-spark-detected", "secureboot-ok", "unified-memory-nvsmi-expected"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("healthy GB10 findings = %v, want exactly %v", got, want)
	}
	for _, f := range r.Findings {
		if f.Severity != types.SeverityInfo {
			t.Errorf("%s: healthy GB10 must only produce INFO, got %s", f.ID, f.Severity)
		}
		if f.Impact == "" {
			t.Errorf("%s: empty impact", f.ID)
		}
	}
	for _, id := range gb10ForbiddenIDs {
		if findByID(r.Findings, id) != nil {
			t.Errorf("forbidden finding %s fired on the healthy GB10 fixture", id)
		}
	}
	det := findByID(r.Findings, "dgx-spark-detected")
	// swbuild = DGX_SWBUILD_VERSION (image 7.2.3), ota = DGX_OTA_VERSION (7.5.0) with the OTA name.
	for _, want := range []string{"NVIDIA NVIDIA_DGX_Spark (Founders Edition)", "GPU NVIDIA GB10 CC 12.1", "kernel 6.17.0-1031-nvidia", "DGX OS 7.2.3 / OTA 7.5.0 (OTA2607)"} {
		if !strings.Contains(det.Evidence, want) {
			t.Errorf("dgx-spark-detected evidence missing %q: %s", want, det.Evidence)
		}
	}
	if len(det.GPUIndexes) != 1 || det.GPUIndexes[0] != 0 {
		t.Errorf("dgx-spark-detected should name GPU 0, got %v", det.GPUIndexes)
	}
	um := findByID(r.Findings, "unified-memory-nvsmi-expected")
	for _, want := range []string{"nvidia-smi memory '[N/A]' on GPU 0 (NVIDIA GB10)", "MemTotal 119.7 GiB", "MemAvailable 115.9 GiB", "swap 0.0 GiB", "GEN 1@ 1x"} {
		if !strings.Contains(um.Evidence, want) {
			t.Errorf("unified-memory-nvsmi-expected evidence missing %q: %s", want, um.Evidence)
		}
	}
	// Summary block (spec 5.1 / 10).
	for _, want := range []string{"Platform: DGX Spark", "Unified memory: 119.7 GiB total, 115.9 GiB available", "PCIe: n/a (on-package, NVLink-C2C)"} {
		if !strings.Contains(r.SummaryBlock, want) {
			t.Errorf("summary block missing %q:\n%s", want, r.SummaryBlock)
		}
	}
	if strings.Contains(r.SummaryBlock, "VRAM:") {
		t.Errorf("summary block must not print VRAM on a unified-memory platform:\n%s", r.SummaryBlock)
	}
	// The fixture really does carry a link that would trip the width rule.
	if r.PCIe == nil || !r.PCIe.OnPackage || r.PCIe.CurrentWidth != "x1" || r.PCIe.MaxWidth != "x16" {
		t.Errorf("fixture PCIe sample should be the misreported on-package GEN 1 x1 link: %+v", r.PCIe)
	}
	if r.GPUs[0].MemoryReporting != memoryReportingNotSupported || !r.GPUs[0].OnPackage || r.UnifiedMemory.MemTotalKB != 125513944 {
		t.Errorf("fixture must mirror spec section 10 assertions: %+v %+v", r.GPUs[0], r.UnifiedMemory)
	}
}

func TestGB10Fixture_EveryModeSameSuppressions(t *testing.T) {
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeAI, types.ModeCreator, types.ModeFull} {
		r := fixtures.GB10()
		Analyze(r, mode)
		for _, id := range gb10ForbiddenIDs {
			if findByID(r.Findings, id) != nil {
				t.Errorf("mode %s: forbidden %s fired", mode, id)
			}
		}
		for _, id := range []string{"dgx-spark-detected", "unified-memory-nvsmi-expected"} {
			if findByID(r.Findings, id) == nil {
				t.Errorf("mode %s: %s missing (platform rules run in every mode)", mode, id)
			}
		}
	}
}

// ── spec section 10 variant: GSP failure ──────────────────────────────

func TestGB10GSPFail_CritWithoutNoGPU(t *testing.T) {
	r := fixtures.GB10GSPFail()
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "dgx-spark-gsp-init-failure")
	if f == nil {
		t.Fatalf("expected dgx-spark-gsp-init-failure, got %v", ids(r.Findings))
	}
	if f.Severity != types.SeverityCrit || f.Confidence != 95 || f.Impact != "irreversible" {
		t.Errorf("severity/confidence/impact = %s/%d/%s", f.Severity, f.Confidence, f.Impact)
	}
	for _, want := range []string{"nvidia-smi 'No devices were found'", "Timeout after 6s of waiting for RPC response from GPU0 GSP", "580.173.02", "580.159.03", "OTA2607"} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("evidence missing %q: %s", want, f.Evidence)
		}
	}
	for _, id := range []string{"no-nvidia-gpu", "driver-not-detected"} {
		if findByID(r.Findings, id) != nil {
			t.Errorf("%s must not fire when dgx-spark-gsp-init-failure explains it (spec 5.1)", id)
		}
	}
	if findByID(r.Findings, "dgx-spark-ota-torn") == nil {
		t.Errorf("driver 580.173.02 vs firmware 580.159.03 should also report dgx-spark-ota-torn, got %v", ids(r.Findings))
	}
	// Xid 119 evidence mentions pairing on GB10 (spec 5.1).
	if x := findByID(r.Findings, "xid-errors"); x == nil || !strings.Contains(x.Evidence, "not OTA-paired") {
		t.Errorf("xid-errors evidence should mention GSP pairing on GB10: %+v", x)
	}
	// Without the kernel lines (neither GSPFailureLines nor dmesg snippets)
	// the rule still fires, at lower confidence.
	r = fixtures.GB10GSPFail()
	r.Linux.DmesgSnippets = ""
	r.Linux.GSPFailureLines = nil
	r.Linux.XidErrors = nil
	Analyze(r, types.ModeFull)
	if f := findByID(r.Findings, "dgx-spark-gsp-init-failure"); f == nil || f.Confidence != 80 || !strings.Contains(f.Evidence, "--include-logs") {
		t.Errorf("without kernel lines the rule should fire at confidence 80 and ask for --include-logs: %+v", f)
	}
	if findByID(r.Findings, "no-nvidia-gpu") != nil {
		t.Error("no-nvidia-gpu must stay suppressed")
	}
	// Off GB10 the same nvidia-smi text is the ordinary no-nvidia-gpu case.
	plain := &types.Report{Driver: types.DriverInfo{NvidiaSmiPath: "nvidia-smi", NvidiaSmiOutput: "No devices were found\n"}}
	Analyze(plain, types.ModeFull)
	if findByID(plain.Findings, "no-nvidia-gpu") == nil || findByID(plain.Findings, "dgx-spark-gsp-init-failure") != nil {
		t.Errorf("non-GB10 host: expected no-nvidia-gpu only, got %v", ids(plain.Findings))
	}
}

// ── spec 5.1 suppressions ─────────────────────────────────────────────

func TestSuppression_PCIeRulesSkippedForOnPackageGPU(t *testing.T) {
	// The GB10 link (Gen1 x1 of Gen5 x16 under load) trips both PCIe WARNs on
	// a discrete card; on-package it must produce nothing at all.
	link := types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen5", CurrentWidth: "x1", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 95}
	if got := pcieFindings(&link); findByID(got, "pcie-width-reduced") == nil {
		t.Fatalf("control: a discrete GPU with this link should report pcie-width-reduced, got %v", ids(got))
	}
	link.OnPackage = true
	if got := pcieFindings(&link); len(got) != 0 {
		t.Errorf("on-package link must produce no PCIe findings, got %v", ids(got))
	}
	idle := types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", IdleLikely: true, OnPackage: true}
	if got := pcieFindings(&idle); len(got) != 0 {
		t.Errorf("on-package idle link must not even yield pcie-idle-power-saving, got %v", ids(got))
	}
	loaded := types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99, OnPackage: true}
	if got := pcieFindings(&loaded); len(got) != 0 {
		t.Errorf("on-package loaded link must not yield pcie-downshift, got %v", ids(got))
	}
	// Through Analyze with per-GPU samples too.
	r := fixtures.GB10()
	r.GPUPCIe = []types.PCIeInfo{*r.PCIe}
	Analyze(r, types.ModeFull)
	for _, id := range []string{"pcie-downshift", "pcie-width-reduced", "pcie-idle-power-saving"} {
		if findByID(r.Findings, id) != nil {
			t.Errorf("%s fired on the GB10 fixture", id)
		}
	}
	if !strings.Contains(r.SummaryBlock, "PCIe: n/a (on-package, NVLink-C2C)") {
		t.Errorf("summary should print the on-package PCIe line:\n%s", r.SummaryBlock)
	}
}

func TestSuppression_LowVRAMNeverOnUnifiedMemory(t *testing.T) {
	// [N/A] memory parsed to 0 MB, or a small number by a future collector:
	// neither may become low-vram on a unified-memory platform.
	r := fixtures.GB10()
	r.GPUs[0].VRAMTotalMB = 2048
	if got := analyzeVRAM(r); len(got) != 0 {
		t.Errorf("low-vram fired on a unified-memory platform: %v", ids(got))
	}
	// MemoryReporting not-supported alone (class empty, flag rule B) also suppresses.
	r2 := &types.Report{GPUs: []types.GPUInfo{{Name: "NVIDIA Thor", IsNVIDIA: true, VRAMTotalMB: 1024, MemoryReporting: memoryReportingNotSupported}}}
	if got := analyzeVRAM(r2); len(got) != 0 {
		t.Errorf("low-vram fired for MemoryReporting=not-supported: %v", ids(got))
	}
	// Control: a discrete 2 GB card still reports it.
	r3 := &types.Report{GPUs: []types.GPUInfo{{Name: "GTX 1050", IsNVIDIA: true, VRAMTotalMB: 2048}}}
	if got := analyzeVRAM(r3); findByID(got, "low-vram") == nil {
		t.Errorf("control: discrete 2 GB card should report low-vram, got %v", ids(got))
	}
}

func TestSuppression_FanNAOnGB10NeverFanNotSpinning(t *testing.T) {
	r := fixtures.GB10()
	r.Thermal.TemperatureC = 75 // hot enough to trip the fan rule on a fan-equipped card
	r.Thermal.FanSpeedPct = 0
	r.Thermal.FanSupported = false // nvidia-smi Fan N/A (spec 2.1)
	Analyze(r, types.ModeFull)
	if findByID(r.Findings, "fan-not-spinning") != nil {
		t.Errorf("fan-not-spinning fired with FanSupported=false on GB10: %v", ids(r.Findings))
	}
}

func TestSuppression_PowerCapEvidenceSaysLimitNAOnUnifiedMemory(t *testing.T) {
	// gpu-power-cap and gpu-clock-slowdown are kept on GB10 (clocks are real)
	// but the limit reads "N/A (unified memory)" instead of "unknown".
	r := fixtures.GB10()
	r.Thermal.PowerState = "P0"
	r.Thermal.UtilizationPct = 99
	r.Thermal.SlowdownActive = true
	r.Thermal.ThrottleReasons = []string{"sw_power_cap"}
	r.Thermal.PowerDrawW = "98.2"
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "gpu-power-cap")
	if f == nil {
		t.Fatalf("gpu-power-cap should still fire on GB10, got %v", ids(r.Findings))
	}
	if !strings.Contains(f.Evidence, "limit N/A (unified memory)") {
		t.Errorf("evidence should print the unified-memory limit wording: %s", f.Evidence)
	}
	r.Thermal.ThrottleReasons = []string{"hw_slowdown"}
	Analyze(r, types.ModeFull)
	if f := findByID(r.Findings, "gpu-clock-slowdown"); f == nil || !strings.Contains(f.Evidence, "limit N/A (unified memory)") {
		t.Errorf("gpu-clock-slowdown should fire with the unified-memory limit wording: %+v", f)
	}
	// A discrete card keeps the old wording.
	disc := thermalFindings(&types.ThermalInfo{TemperatureC: 70, PowerState: "P0", UtilizationPct: 99, SlowdownActive: true, ThrottleReasons: []string{"sw_power_cap"}, PowerDrawW: "350", PowerLimitW: "350"}, false)
	if f := findByID(disc, "gpu-power-cap"); f == nil || !strings.Contains(f.Evidence, "limit 350") {
		t.Errorf("discrete card evidence changed: %+v", f)
	}
}

func TestSuppression_PDWedgeTakesPrecedenceOverPowerStateStuck(t *testing.T) {
	r := fixtures.GB10()
	s := wedgeSampleInfo(0)
	s.PowerState = "P5" // would also trip gpu-power-state-stuck (P5+ at >= 60 % util)
	r.Thermal = &s
	Analyze(r, types.ModeFull)
	if findByID(r.Findings, "gb10-pd-power-wedge") == nil {
		t.Fatalf("expected gb10-pd-power-wedge, got %v", ids(r.Findings))
	}
	if findByID(r.Findings, "gpu-power-state-stuck") != nil {
		t.Errorf("gpu-power-state-stuck must yield to gb10-pd-power-wedge (spec 5.1)")
	}
	// Without the wedge the generic rule still applies on GB10.
	r = fixtures.GB10()
	r.Thermal.PowerState = "P5"
	r.Thermal.UtilizationPct = 80
	r.Thermal.CurrentClockMHz = 2200
	Analyze(r, types.ModeFull)
	if findByID(r.Findings, "gpu-power-state-stuck") == nil {
		t.Errorf("gpu-power-state-stuck should still work on GB10 without a wedge: %v", ids(r.Findings))
	}
}

func TestSuppression_NvidiaSmiMissingIsInfoOnRTXSpark(t *testing.T) {
	r := fixtures.RTXSpark()
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "nvidia-smi-missing")
	if f == nil {
		t.Fatalf("expected nvidia-smi-missing INFO, got %v", ids(r.Findings))
	}
	if f.Severity != types.SeverityInfo || !strings.Contains(f.Evidence, "unconfirmed") {
		t.Errorf("rtx-spark should downgrade nvidia-smi-missing to INFO wording: %+v", f)
	}
	// Elsewhere it stays WARN.
	plain := analyzeDriverBasics(&types.Report{Driver: types.DriverInfo{Version: "580.65.06"}})
	if f := findByID(plain, "nvidia-smi-missing"); f == nil || f.Severity != types.SeverityWarn {
		t.Errorf("nvidia-smi-missing should remain WARN off RTX Spark: %+v", f)
	}
}

func TestSuppression_NvidiaAppNotExpectedOnWindowsArm(t *testing.T) {
	// The rule only fires on collected data; a WoA report without an NVIDIA
	// App version (nothing ships for Arm64 yet, spec 5.1) yields nothing.
	r := fixtures.RTXSpark()
	Analyze(r, types.ModeGaming)
	if findByID(r.Findings, "nvidia-app-detected") != nil {
		t.Errorf("nvidia-app-detected must not fire without an app version on Windows on Arm")
	}
}

func TestSuppression_JetsonWordingCoversThor(t *testing.T) {
	f := analyzeJetson(&types.Report{System: types.SystemInfo{IsJetson: true, JetsonRelease: "# R38 (release), REVISION: 2.0"}})
	if len(f) != 1 || strings.Contains(f[0].Evidence, "not available on Tegra") || !strings.Contains(f[0].Evidence, "Thor") {
		t.Errorf("jetson-detected must not claim nvidia-smi is absent on every Tegra (Thor ships it): %+v", f)
	}
}

// ── individual rule behaviours the spec pins down ─────────────────────

func TestUnifiedMemorySwapInUse_InfoVsWarn(t *testing.T) {
	r := fixtures.GB10()
	r.UnifiedMemory.GPUProcesses = 1
	r.UnifiedMemory.SwapFreeKB = r.UnifiedMemory.SwapTotalKB - 3*kbPerGiB
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "unified-memory-swap-in-use")
	if f == nil || f.Severity != types.SeverityInfo {
		t.Fatalf("3 GiB swapped with 115.9 GiB available should be INFO: %+v (%v)", f, ids(r.Findings))
	}
	if !strings.Contains(f.Evidence, "Swap in use: 3.0 of 16.0 GiB (/swapfile)") {
		t.Errorf("evidence: %s", f.Evidence)
	}
	// Never advise swapoff without the stop-the-workload wording, and the
	// read-only steps come first.
	for i, step := range f.NextSteps {
		if strings.Contains(step, "swapoff") && !strings.Contains(step, "Do not run swapoff while the workload is loaded") && !strings.Contains(step, "after stopping the workload") {
			t.Errorf("step %d advises swapoff without the stop-the-workload wording: %s", i, step)
		}
	}
	if strings.HasPrefix(f.NextSteps[0], "Advisory") || !strings.Contains(f.NextSteps[0], "Reduce the footprint first") {
		t.Errorf("first step must be the read-only footprint advice: %v", f.NextSteps)
	}
	// WARN only with MemAvailable below 8 GiB.
	r.UnifiedMemory.MemAvailableKB = 7 * kbPerGiB
	Analyze(r, types.ModeFull)
	if f := findByID(r.Findings, "unified-memory-swap-in-use"); f == nil || f.Severity != types.SeverityWarn {
		t.Errorf("swap in use with MemAvailable < 8 GiB should be WARN: %+v", f)
	}
	// A few hundred MB of cold pages never fires; nor does swap without a GPU process.
	r = fixtures.GB10()
	r.UnifiedMemory.GPUProcesses = 1
	r.UnifiedMemory.SwapFreeKB = r.UnifiedMemory.SwapTotalKB - 300*1024
	Analyze(r, types.ModeFull)
	if findByID(r.Findings, "unified-memory-swap-in-use") != nil {
		t.Error("300 MB of swap must not fire")
	}
	r.UnifiedMemory.GPUProcesses = 0
	r.UnifiedMemory.SwapFreeKB = r.UnifiedMemory.SwapTotalKB - 5*kbPerGiB
	Analyze(r, types.ModeFull)
	if findByID(r.Findings, "unified-memory-swap-in-use") != nil {
		t.Error("swap without a GPU process must not fire")
	}
}

func TestUnifiedMemoryPressure_Thresholds(t *testing.T) {
	cases := []struct {
		name     string
		avail    int64
		psi      float64
		procs    int
		wantSev  types.Severity
		wantFire bool
	}{
		{"healthy", 100 * kbPerGiB, 0, 1, "", false},
		{"low without GPU process", 6 * kbPerGiB, 0, 0, "", false},
		{"below 8 GiB with GPU process", 6 * kbPerGiB, 0, 1, types.SeverityWarn, true},
		{"below 4 GiB", 3 * kbPerGiB, 0, 1, types.SeverityCrit, true},
		{"psi warn", 100 * kbPerGiB, 0.5, 0, types.SeverityWarn, true},
		{"psi crit", 100 * kbPerGiB, 1.5, 0, types.SeverityCrit, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := fixtures.GB10()
			r.UnifiedMemory.MemAvailableKB = c.avail
			r.UnifiedMemory.PSIFullAvg10 = c.psi
			r.UnifiedMemory.GPUProcesses = c.procs
			f := findByID(analyzeUnifiedMemory(r), "unified-memory-pressure")
			if (f != nil) != c.wantFire {
				t.Fatalf("fired=%v want %v", f != nil, c.wantFire)
			}
			if f != nil && f.Severity != c.wantSev {
				t.Errorf("severity %s want %s", f.Severity, c.wantSev)
			}
		})
	}
}

func TestLoglessHardPoweroff_Rules(t *testing.T) {
	falseP, trueP := false, true
	// Silent for a boot ending in "Journal stopped" even if the flag is false.
	r := fixtures.GB10()
	r.Platform.PrevBootClean = &falseP
	r.Platform.PrevBootLastLine = "systemd-journald[512]: Journal stopped"
	r.Platform.UncleanBoots = 3
	if f := findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff"); f != nil {
		t.Errorf("clean marker in the last line must keep the rule silent: %+v", f)
	}
	// Silent when the last line already explains the reset.
	r.Platform.PrevBootLastLine = "kernel: Kernel panic - not syncing"
	if f := findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff"); f != nil {
		t.Errorf("a panic line is not log-less: %+v", f)
	}
	// One unclean boot: INFO.
	r.Platform.PrevBootLastLine = "gnome-shell[2041]: Running GNOME Shell"
	r.Platform.UncleanBoots = 1
	r.Platform.PstoreEmpty = &trueP
	f := findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff")
	if f == nil || f.Severity != types.SeverityInfo {
		t.Fatalf("one unclean boot should be INFO: %+v", f)
	}
	// Two or more: WARN, with the alternative explanations named.
	r.Platform.UncleanBoots = 2
	f = findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff")
	if f == nil || f.Severity != types.SeverityWarn || !strings.Contains(f.Evidence, "2 boots without a clean-shutdown marker in 14 days") || !strings.Contains(f.Evidence, "mains loss") {
		t.Errorf("two unclean boots should be WARN naming mains loss: %+v", f)
	}
	if !strings.Contains(f.Evidence, "EC 3.5.8") {
		t.Errorf("evidence should decode the hex EC version: %s", f.Evidence)
	}
	// pstore holding a crash record is not log-less.
	r.Platform.PstoreEmpty = &falseP
	if f := findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff"); f != nil {
		t.Errorf("non-empty pstore must keep the rule silent: %+v", f)
	}
	// Unreadable journal: nil pointer, silent.
	r.Platform.PrevBootClean = nil
	r.Platform.PstoreEmpty = &trueP
	if f := findByID(analyzeGB10Power(r), "gb10-logless-hard-poweroff"); f != nil {
		t.Errorf("unreadable journal must keep the rule silent: %+v", f)
	}
}

func TestPDPowerWedge_WarnOnOneSampleCritWhenAllMatch(t *testing.T) {
	r := fixtures.GB10()
	one := wedgeSampleInfo(0)
	r.Thermal = &one
	f := findByID(analyzeGB10Power(r), "gb10-pd-power-wedge")
	if f == nil || f.Severity != types.SeverityWarn {
		t.Fatalf("single matching sample should be WARN: %+v", f)
	}
	for _, want := range []string{"611 MHz (max 3003)", "99%", "27.5 W", "reasons Not Active in 1/1 samples", "SW Power Capping counter 123456 us"} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("evidence missing %q: %s", want, f.Evidence)
		}
	}
	if len(f.GPUIndexes) != 1 || f.GPUIndexes[0] != 0 {
		t.Errorf("wedge should name GPU 0: %v", f.GPUIndexes)
	}
	// Every sample matching (>= 2): CRIT.
	r.GPUThermal = []types.ThermalInfo{wedgeSampleInfo(0), wedgeSampleInfo(0), wedgeSampleInfo(0)}
	r.Thermal = &r.GPUThermal[0]
	f = findByID(analyzeGB10Power(r), "gb10-pd-power-wedge")
	if f == nil || f.Severity != types.SeverityCrit || !strings.Contains(f.Evidence, "3/3 samples") {
		t.Errorf("all samples matching should be CRIT: %+v", f)
	}
	// One of three matching: WARN (a DVFS transition can match once).
	healthy := wedgeSampleInfo(0)
	healthy.CurrentClockMHz = 2400
	healthy.PowerDrawW = "92.0"
	r.GPUThermal = []types.ThermalInfo{wedgeSampleInfo(0), healthy, healthy}
	f = findByID(analyzeGB10Power(r), "gb10-pd-power-wedge")
	if f == nil || f.Severity != types.SeverityWarn || !strings.Contains(f.Evidence, "1/3 samples") {
		t.Errorf("one of three matching should be WARN: %+v", f)
	}
	// An active clock event reason is not the wedge (the reasons are Not Active by definition).
	capped := wedgeSampleInfo(0)
	capped.SlowdownActive = true
	capped.ThrottleReasons = []string{"sw_power_cap"}
	r.GPUThermal = []types.ThermalInfo{capped}
	if f := findByID(analyzeGB10Power(r), "gb10-pd-power-wedge"); f != nil {
		t.Errorf("active sw_power_cap is not the wedge signature: %+v", f)
	}
	// Healthy load never fires.
	r.GPUThermal = []types.ThermalInfo{healthy}
	if f := findByID(analyzeGB10Power(r), "gb10-pd-power-wedge"); f != nil {
		t.Errorf("healthy load fired the wedge: %+v", f)
	}
	// Steps: cold drain first (read-only), the firmware advisory after it.
	f = findByID(analyzeGB10Power(gb10(func(r *types.Report) { s := wedgeSampleInfo(0); r.Thermal = &s })), "gb10-pd-power-wedge")
	if !strings.HasPrefix(f.NextSteps[0], "Cold drain") || !strings.HasPrefix(f.NextSteps[1], "If it recurs") || !strings.HasPrefix(f.NextSteps[2], "Advisory") {
		t.Errorf("steps order (read-only first, then Advisory): %v", f.NextSteps)
	}
}

func TestForeignDriverPackages_ExcludesDKMSOpenMetapackage(t *testing.T) {
	// The fixture ships nvidia-dkms-580-open (spec 10 placeholder) and must be clean.
	if f := findByID(analyzeDGXOS(fixtures.GB10()), "dgx-spark-foreign-driver-packages"); f != nil {
		t.Fatalf("nvidia-dkms-580-open must be excluded: %+v", f)
	}
	cases := map[string]bool{
		"nvidia-driver-570-server 570.x":       true,
		"nvidia-dkms-580-open-server 580.x":    true,
		"nvidia-fabricmanager-580 580.x":       true,
		"nvidia-nvswitch-580 580.x":            true,
		"nvidia-system-station 1.0":            true,
		"nvidia-driver-580 580.x":              true,
		"nvidia-driver-580-open 580.x":         false,
		"nvidia-dkms-580-open 580.x":           false,
		"nvidia-firmware-580-580.159.03 580.x": false,
		"nvidia-kernel-common-580 580.x":       false,
	}
	for pkg, want := range cases {
		r := fixtures.GB10()
		r.Linux.NVIDIAPackages = append(r.Linux.NVIDIAPackages, pkg)
		f := findByID(analyzeDGXOS(r), "dgx-spark-foreign-driver-packages")
		if (f != nil) != want {
			t.Errorf("%s: fired=%v want %v", pkg, f != nil, want)
		}
		if f != nil && !strings.Contains(f.Evidence, strings.Fields(pkg)[0]) {
			t.Errorf("%s: evidence should list the package: %s", pkg, f.Evidence)
		}
	}
	// The nvidia-smi "NVIDIA-GB10 not supported" text (spec 6) fires it too.
	r := fixtures.GB10()
	r.Driver.NvidiaSmiOutput = "NVIDIA-GB10 not supported\n"
	if f := findByID(analyzeDGXOS(r), "dgx-spark-foreign-driver-packages"); f == nil || !strings.Contains(f.Evidence, "NVIDIA-GB10 not supported") {
		t.Errorf("nvidia-smi 'NVIDIA-GB10 not supported' should fire: %+v", f)
	}
}

func TestDriverRules_Thresholds(t *testing.T) {
	cases := []struct {
		driver, cuda string
		want         string
	}{
		{"580.159.03", "13.0", ""},
		{"580.173.02", "13.0", ""},
		{"570.86.10", "12.8", "dgx-spark-driver-too-old"},
		{"580.95.05", "12.9", "dgx-spark-driver-too-old"}, // CUDA 12.x clause
		{"590.48.01", "13.1", "dgx-spark-driver-branch-unsupported"},
		{"595.10.01", "13.2", "dgx-spark-driver-branch-unsupported"},
		{"", "", ""},
	}
	for _, c := range cases {
		r := fixtures.GB10()
		r.Driver.Version, r.Driver.CUDAVersion = c.driver, c.cuda
		got := analyzeDGXOS(r)
		for _, id := range []string{"dgx-spark-driver-too-old", "dgx-spark-driver-branch-unsupported"} {
			if (findByID(got, id) != nil) != (id == c.want) {
				t.Errorf("driver %q cuda %q: %s fired=%v want %v", c.driver, c.cuda, id, findByID(got, id) != nil, id == c.want)
			}
		}
	}
}

func TestOTAOutdated_Clauses(t *testing.T) {
	if f := findByID(analyzeDGXOS(fixtures.GB10()), "dgx-spark-ota-outdated"); f != nil {
		t.Fatalf("the current stack must not be outdated: %+v", f)
	}
	for name, mut := range map[string]func(r *types.Report){
		"ota":    func(r *types.Report) { r.DGXOS.OTAVersion = "7.2.3" },
		"kernel": func(r *types.Report) { r.System.KernelVersion = "6.14.0-1015-nvidia" },
		"driver": func(r *types.Report) { r.Driver.Version = "580.126.09" },
	} {
		r := fixtures.GB10()
		mut(r)
		if f := findByID(analyzeDGXOS(r), "dgx-spark-ota-outdated"); f == nil || !strings.Contains(f.Evidence, otaCurrentDisplay) {
			t.Errorf("%s clause should fire and cite the current stack: %+v", name, f)
		}
	}
	// A 6.14 stop-gap kernel on a host with the cx7 regression variant firing
	// cannot co-occur (the variant keys on the running 6.17 kernel), so the
	// suppression is a no-op here; the regression kernel itself is 6.17.
	r := fixtures.GB10()
	r.System.KernelVersion = "6.17.0-1021-nvidia"
	if f := findByID(analyzeDGXOS(r), "dgx-spark-ota-outdated"); f != nil {
		t.Errorf("6.17.0-1021 is not < 6.17: %+v", f)
	}
}

func TestFirmwareBehind_FEThresholdsAndOEMPending(t *testing.T) {
	if f := findByID(analyzeFirmware(fixtures.GB10()), "dgx-spark-firmware-behind"); f != nil {
		t.Fatalf("current FE firmware must not fire: %+v", f)
	}
	// Hex and dotted forms decode identically (assumption documented in the code).
	for in, want := range map[string]string{"0x03000508": "3.5.8", "0x02009b0b": "2.155.11", "0x00000516": "0.5.22", "3.5.8": "3.5.8"} {
		if t3, ok := fwVersionTriplet(in); !ok || fwTripletString(t3) != want {
			t.Errorf("fwVersionTriplet(%q) = %v,%v want %s", in, t3, ok, want)
		}
	}
	for name, mut := range map[string]func(r *types.Report){
		"ec":  func(r *types.Report) { r.Platform.Firmware[0].Version = "3.5.7" },
		"soc": func(r *types.Report) { r.Platform.Firmware[1].Version = "0x02009b0a" },
		"pd":  func(r *types.Report) { r.Platform.Firmware[2].Version = "0.5.21" },
	} {
		r := fixtures.GB10()
		mut(r)
		f := findByID(analyzeFirmware(r), "dgx-spark-firmware-behind")
		if f == nil || !strings.Contains(f.Evidence, "behind: "+name) || !strings.Contains(f.Evidence, "Founders Edition") {
			t.Errorf("%s behind should fire: %+v", name, f)
		}
	}
	// OEM units: thresholds do not apply, only pending capsules.
	r := fixtures.GB10()
	r.Platform.Vendor = "ASUS"
	r.Platform.Model = "Ascent GX10"
	r.Platform.Firmware[0].Version = "3.5.7"
	if f := findByID(analyzeFirmware(r), "dgx-spark-firmware-behind"); f != nil {
		t.Errorf("OEM unit must not be measured against FE thresholds: %+v", f)
	}
	r.Platform.Firmware[0].Pending = "3.5.8"
	if f := findByID(analyzeFirmware(r), "dgx-spark-firmware-behind"); f == nil || !strings.Contains(f.Evidence, "OEM: ASUS") || !strings.Contains(f.Evidence, "pending Embedded Controller -> 3.5.8") {
		t.Errorf("OEM pending capsule should fire: %+v", f)
	}
}

func TestClusterRules_HealthyCageIsSilent(t *testing.T) {
	r := fixtures.GB10()
	got := analyzeCluster(r)
	if len(got) != 0 {
		t.Errorf("healthy cabled cage must produce nothing, got %v", ids(got))
	}
	// Off dgx-spark the cluster analyzer stays quiet even with data.
	r.Platform.Class = ""
	r.Cluster.UfwEnabled = true
	if got := analyzeCluster(r); len(got) != 0 {
		t.Errorf("cluster rules are dgx-spark only, got %v", ids(got))
	}
}

func TestClusterRules_Variants(t *testing.T) {
	// Twins in the same subnet.
	r := gb10(withCluster(func(c *types.ClusterInfo) { c.Ports[1].IPv4 = []string{"192.168.100.2/24"} }))
	if f := findByID(analyzeCluster(r), "cx7-twins-same-subnet"); f == nil || !strings.Contains(f.Evidence, "share 192.168.100.0/24") {
		t.Errorf("same subnet: %+v", f)
	}
	// Bonded twin.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.Ports[0].Bond = "bond0" }))
	if f := findByID(analyzeCluster(r), "cx7-twins-same-subnet"); f == nil || !strings.Contains(f.Evidence, "bond0") {
		t.Errorf("bond: %+v", f)
	}
	// Speed degraded names the interface and the expected speed.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.Ports[0].SpeedMbps = 100000 }))
	if f := findByID(analyzeCluster(r), "cx7-link-speed-degraded"); f == nil || !strings.Contains(f.Evidence, "enp1s0f0np0 (rocep1s0f0) negotiated 100000 Mb/s; expected 200000") {
		t.Errorf("speed: %+v", f)
	}
	// Up without IP is WARN; not persisted is INFO.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.Ports[0].IPv4 = nil; c.Ports[1].Persistent = false }))
	got := analyzeCluster(r)
	var warn, info int
	for _, f := range got {
		if f.ID == "cx7-up-no-ip" {
			switch f.Severity {
			case types.SeverityWarn:
				warn++
			case types.SeverityInfo:
				info++
			}
		}
	}
	if warn != 1 || info != 1 {
		t.Errorf("cx7-up-no-ip: warn=%d info=%d, findings %v", warn, info, ids(got))
	}
	// NCCL_IB_HCA templated from the ACTIVE twins, never hard-coded.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.NCCLEnv = map[string]string{"NCCL_IB_HCA": "enp1s0f0np0"} }))
	f := findByID(analyzeCluster(r), "nccl-env-misconfigured")
	if f == nil || !strings.Contains(f.Evidence, "NCCL_IB_HCA names netdev enp1s0f0np0") {
		t.Fatalf("nccl netdev: %+v", f)
	}
	if !strings.Contains(f.NextSteps[0], "export NCCL_IB_HCA=rocep1s0f0,roceP2p1s0f0") || !strings.Contains(f.NextSteps[0], "NCCL_SOCKET_IFNAME=enp1s0f0np0,enP2p1s0f0np0") || strings.Contains(f.NextSteps[0], "{active_twins}") {
		t.Errorf("HCA list should be templated from the ACTIVE twins: %s", f.NextSteps[0])
	}
	// One twin of a cage listed.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.NCCLEnv = map[string]string{"NCCL_IB_HCA": "rocep1s0f0"} }))
	if f := findByID(analyzeCluster(r), "nccl-env-misconfigured"); f == nil || !strings.Contains(f.Evidence, "lists one twin of cage 0") {
		t.Errorf("one twin: %+v", f)
	}
	// Both twins listed: fine.
	r = gb10(withCluster(func(c *types.ClusterInfo) {
		c.NCCLEnv = map[string]string{"NCCL_IB_HCA": "rocep1s0f0,roceP2p1s0f0", "NCCL_NET_PLUGIN": "none"}
		c.NCCLVersion = "2.30.7"
	}))
	if f := findByID(analyzeCluster(r), "nccl-env-misconfigured"); f != nil {
		t.Errorf("correct NCCL env fired: %+v", f)
	}
	// Old NCCL.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.NCCLVersion = "2.27.7" }))
	if f := findByID(analyzeCluster(r), "nccl-env-misconfigured"); f == nil || !strings.Contains(f.Evidence, "libnccl 2.27.7 < 2.28") {
		t.Errorf("old nccl: %+v", f)
	}
	// Regression kernel WARN variant even with the NIC present.
	r = gb10(func(r *types.Report) { r.System.KernelVersion = "6.17.0-1029-nvidia" })
	if f := findByID(analyzeCluster(r), "cx7-not-enumerated"); f == nil || f.Severity != types.SeverityWarn {
		t.Errorf("regression kernel variant: %+v", f)
	}
	// Firewall without addressed twins stays quiet.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.UfwEnabled = true; c.Ports[0].IPv4 = nil; c.Ports[1].IPv4 = nil }))
	if f := findByID(analyzeCluster(r), "cx7-firewall-blocks-cluster"); f != nil {
		t.Errorf("ufw without fabric addresses: %+v", f)
	}
}

func TestEcosystemRules_ModeGating(t *testing.T) {
	r := gb10(withEco(&types.EcosystemInfo{SnapDocker: true}))
	Analyze(r, types.ModeGaming)
	if findByID(r.Findings, "docker-snap-gpu-blocked") != nil {
		t.Error("ecosystem rules must not run in gaming mode")
	}
	for _, mode := range []types.RunMode{types.ModeAI, types.ModeCreator, types.ModeFull} {
		r := gb10(withEco(&types.EcosystemInfo{SnapDocker: true}))
		Analyze(r, mode)
		if findByID(r.Findings, "docker-snap-gpu-blocked") == nil {
			t.Errorf("mode %s: expected docker-snap-gpu-blocked, got %v", mode, ids(r.Findings))
		}
	}
	// Cluster rules: ai and full only.
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.UfwEnabled = true }))
	Analyze(r, types.ModeCreator)
	if findByID(r.Findings, "cx7-firewall-blocks-cluster") != nil {
		t.Error("cluster rules must not run in creator mode")
	}
	Analyze(r, types.ModeAI)
	if findByID(r.Findings, "cx7-firewall-blocks-cluster") == nil {
		t.Errorf("ai mode should run cluster rules, got %v", ids(r.Findings))
	}
}

func TestNGCImageTooOld_Table(t *testing.T) {
	cases := map[string]bool{
		"nvcr.io/nvidia/pytorch:24.08-py3":              true,
		"nvcr.io/nvidia/pytorch:25.09-py3":              true,
		"nvcr.io/nvidia/pytorch:25.10-py3":              false,
		"nvcr.io/nvidia/pytorch:25.11-py3":              false,
		"nvcr.io/nvidia/tensorflow:25.02-tf2-py3":       true,
		"lmsysorg/sglang:latest":                        true,
		"lmsysorg/sglang:latest-cu130":                  false,
		"nvcr.io/nvidia/tensorrt-llm/release:1.1.0rc2":  true,
		"nvcr.io/nvidia/tensorrt-llm/release:1.3.0rc13": false,
		"nvcr.io/nvidia/vllm:26.05-py3":                 false,
		"ghcr.io/example/tool:latest":                   false,
	}
	for ref, want := range cases {
		if got := ngcImageTooOld(ref); got != want {
			t.Errorf("ngcImageTooOld(%q) = %v want %v", ref, got, want)
		}
	}
}

func TestWoARules(t *testing.T) {
	r := fixtures.RTXSpark()
	Analyze(r, types.ModeFull)
	got := ids(r.Findings)
	sort.Strings(got)
	want := []string{"nvidia-smi-missing", "rtx-spark-detected", "rtx-spark-driver-developer-preview", "unified-memory-nvsmi-expected"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RTX Spark findings = %v want %v", got, want)
	}
	det := findByID(r.Findings, "rtx-spark-detected")
	if !strings.Contains(det.Evidence, "RTX Spark N1X (6144-core, DEV_2E03), Windows build 26100 ARM64, Microsoft Surface RTX Spark Dev Box, pool 128.0 GiB") {
		t.Errorf("rtx-spark-detected evidence: %s", det.Evidence)
	}
	if !strings.Contains(r.SummaryBlock, "Platform: RTX Spark (Microsoft Surface RTX Spark Dev Box)") {
		t.Errorf("summary platform line:\n%s", r.SummaryBlock)
	}
	for _, id := range []string{"low-vram", "pcie-downshift", "driver-not-detected", "woa-nvcheckup-emulated", "woa-windows-build-too-old"} {
		if findByID(r.Findings, id) != nil {
			t.Errorf("%s must not fire on the healthy RTX Spark fixture", id)
		}
	}
	// Emulated process, old build, emulated toolkit.
	r = rtxSpark(func(r *types.Report) {
		r.Platform.ProcessEmulated = true
		r.System.OSBuild = "10.0.22631"
		r.AI = &types.AIInfo{CUDAToolkitVersion: "13.3"}
	})
	Analyze(r, types.ModeFull)
	for _, id := range []string{"woa-nvcheckup-emulated", "woa-windows-build-too-old", "woa-cuda-toolkit-not-native"} {
		if findByID(r.Findings, id) == nil {
			t.Errorf("expected %s, got %v", id, ids(r.Findings))
		}
	}
	if f := findByID(r.Findings, "woa-windows-build-too-old"); f != nil && !strings.Contains(f.Evidence, "Windows build 22631 predates 24H2 (26100)") {
		t.Errorf("build evidence: %s", f.Evidence)
	}
	// Native 13.4 toolkit: quiet.
	r = rtxSpark(func(r *types.Report) { r.AI = &types.AIInfo{CUDAToolkitVersion: "13.4"} })
	if f := findByID(analyzeWoA(r), "woa-cuda-toolkit-not-native"); f != nil {
		t.Errorf("13.4 is native: %+v", f)
	}
	// Developer preview via the WDDM suffix only (GPUInfo fallback, no WoA row).
	r = rtxSpark(func(r *types.Report) {
		r.Driver = types.DriverInfo{}
		r.GPUs[0].DriverVersion = "32.0.16.1600"
		r.Platform.WoA = nil
	})
	if f := findByID(analyzeWoA(r), "rtx-spark-driver-developer-preview"); f == nil || !strings.Contains(f.Evidence, "WDDM 32.0.16.1600") {
		t.Errorf("WDDM suffix match: %+v", f)
	}
	// Linux N1X.
	lin := sparkCorpus()[len(sparkCorpus())-1].report
	Analyze(lin, types.ModeFull)
	if f := findByID(lin.Findings, "rtx-spark-linux-unsupported"); f == nil || !strings.Contains(f.Evidence, "[10de:2e03]") || !strings.Contains(f.Evidence, "2026-09-02") {
		t.Errorf("rtx-spark-linux-unsupported: %+v", f)
	}
	if findByID(lin.Findings, "rtx-spark-detected") != nil {
		t.Error("rtx-spark-detected is a Windows-on-Arm rule and must not fire on Linux")
	}
}

func TestWSLLinuxDriverInstalled(t *testing.T) {
	r := sparkCorpus()
	var wsl *types.Report
	for _, c := range r {
		if c.report.WSL != nil && c.report.WSL.IsWSL && c.report.Platform.Class == "" {
			wsl = c.report
		}
	}
	Analyze(wsl, types.ModeFull)
	f := findByID(wsl.Findings, "wsl-linux-driver-installed")
	if f == nil || !strings.Contains(f.Evidence, "nvidia-driver-550") || !strings.Contains(f.Evidence, "/usr/lib/wsl/lib/libcuda.so present") {
		t.Fatalf("wsl-linux-driver-installed: %+v (%v)", f, ids(wsl.Findings))
	}
	// Not in gaming mode (WSL is analyzed in ai/full).
	Analyze(wsl, types.ModeGaming)
	if findByID(wsl.Findings, "wsl-linux-driver-installed") != nil {
		t.Error("wsl-linux-driver-installed must follow the WSL collector modes")
	}
	// Only nvidia-utils / libnvidia packages: quiet.
	wsl.Linux.NVIDIAPackages = []string{"libnvidia-container1 1.17", "nvidia-container-toolkit 1.17"}
	Analyze(wsl, types.ModeFull)
	if findByID(wsl.Findings, "wsl-linux-driver-installed") != nil {
		t.Error("container toolkit packages are not the Linux driver")
	}
}

// ── every finding carries an impact; advisory steps are ordered ───────

// advisoryRe is the only ordering contract (spec 5): a step that changes
// system state starts with "Advisory" (word boundary); the catalog's System
// Recovery reimage steps carry "Advisory: (data loss)" themselves.
var advisoryRe = regexp.MustCompile(`^Advisory\b`)

func TestAnalyze_EveryFindingHasImpactAndOrderedAdvisories(t *testing.T) {
	allowed := map[string]bool{"none": true, "reversible": true, "persistent": true, "irreversible": true, "data-loss": true}
	seen := map[string]bool{}
	for _, c := range ruleCorpus() {
		Analyze(c.report, c.mode)
		for _, f := range c.report.Findings {
			seen[f.ID] = true
			if !allowed[f.Impact] {
				t.Errorf("%s (mode %s): impact %q not in the allowed set", f.ID, c.mode, f.Impact)
			}
			sawAdvisory := false
			for _, step := range f.NextSteps {
				if advisoryRe.MatchString(step) {
					sawAdvisory = true
				} else if sawAdvisory {
					t.Errorf("%s: read-only step %q follows an Advisory step", f.ID, step)
				}
			}
		}
	}
	if len(seen) < 100 {
		t.Errorf("corpus produced only %d distinct ids; the Spark corpus seems incomplete", len(seen))
	}
}

// ── knowledge pack: Spark schema and lockstep with the Go table ────────

var allowedPlatforms = map[string]bool{"dgx-spark": true, "rtx-spark": true, "jetson": true, "grace-hopper": true, "arm64-dgpu": true, "all": true}

func TestRulesJSON_SparkSchema(t *testing.T) {
	_, rules := loadKnowledgeRules(t)
	allowedImpact := map[string]bool{"none": true, "reversible": true, "persistent": true, "irreversible": true, "data-loss": true}
	for _, id := range sortedKeys(rules) {
		r := rules[id]
		if !allowedImpact[r.Impact] {
			t.Errorf("%s: impact %q must be one of none|reversible|persistent|irreversible|data-loss", id, r.Impact)
		}
		if len(r.Platforms) == 0 {
			t.Errorf("%s: platforms[] missing", id)
		}
		for _, p := range r.Platforms {
			if !allowedPlatforms[p] {
				t.Errorf("%s: platform %q outside the closed set", id, p)
			}
		}
		sawAdvisory := false
		for _, step := range r.NextSteps {
			adv := advisoryRe.MatchString(step)
			if strings.HasPrefix(strings.ToLower(step), "advisory") && !adv {
				t.Errorf("%s: step %q looks advisory but does not match ^Advisory\\b", id, step)
			}
			if adv {
				sawAdvisory = true
			} else if sawAdvisory {
				t.Errorf("%s: Advisory step precedes the read-only step %q", id, step)
			}
		}
		if _, spark := sparkRules[id]; spark {
			if len(r.NextSteps) == 0 || len(r.SourceIDs) == 0 || len(r.Sources) == 0 || r.Trigger == "" || r.EvidenceTmpl == "" {
				t.Errorf("%s: Spark rule must carry next_steps, source_ids, sources, trigger and evidence_template", id)
			}
			for _, sid := range r.SourceIDs {
				if !regexp.MustCompile(`^S\d+$`).MatchString(sid) {
					t.Errorf("%s: source id %q is not an Sn reference", id, sid)
				}
			}
		}
	}
	// The 51 catalog rows are all present.
	if got := len(sparkRules); got != 51 {
		t.Errorf("sparkRules has %d rows, the catalog has 51", got)
	}
}

func TestSparkRuleTable_MatchesRulesJSON(t *testing.T) {
	_, rules := loadKnowledgeRules(t)
	for _, id := range sortedKeys(sparkRules) {
		g := sparkRules[id]
		j, ok := rules[id]
		if !ok {
			t.Errorf("%s: in the Go table but not in knowledge/rules.json", id)
			continue
		}
		if g.Title != j.Title || g.Category != j.Category || string(g.Severity) != j.Severity || g.Impact != j.Impact || g.Confidence != j.BaseConfidence || g.Why != j.WhyItMatters {
			t.Errorf("%s: Go table differs from rules.json:\n go:   %q %q %s %q %d\n json: %q %q %s %q %d", id, g.Title, g.Category, g.Severity, g.Impact, g.Confidence, j.Title, j.Category, j.Severity, j.Impact, j.BaseConfidence)
		}
		if strings.Join(g.Steps, "\n") != strings.Join(j.NextSteps, "\n") {
			t.Errorf("%s: next steps differ from rules.json (they must be verbatim):\n go:   %q\n json: %q", id, g.Steps, j.NextSteps)
		}
	}
	// And the catalog file itself agrees with the knowledge pack.
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "roadmap", "spark-rules.json"))
	if err != nil {
		t.Skipf("spec catalog not available: %v", err)
	}
	var catalog struct {
		Rules []struct {
			ID        string   `json:"id"`
			Severity  string   `json:"severity"`
			Impact    string   `json:"impact"`
			Platforms []string `json:"platforms"`
			NextSteps []string `json:"next_steps"`
		} `json:"rules"`
	}
	if err := jsonUnmarshal(data, &catalog); err != nil {
		t.Fatalf("parse spark-rules.json: %v", err)
	}
	if len(catalog.Rules) != 51 {
		t.Errorf("spark-rules.json has %d rules, expected 51", len(catalog.Rules))
	}
	for _, c := range catalog.Rules {
		j, ok := rules[c.ID]
		if !ok {
			t.Errorf("%s: in spark-rules.json but not in knowledge/rules.json", c.ID)
			continue
		}
		// Step text AND order are verbatim: the catalog itself lists the
		// read-only steps first (partitioned in integration 2).
		if j.Severity != c.Severity || j.Impact != c.Impact || strings.Join(j.Platforms, ",") != strings.Join(c.Platforms, ",") || strings.Join(j.NextSteps, "\n") != strings.Join(c.NextSteps, "\n") {
			t.Errorf("%s: knowledge/rules.json drifted from docs/roadmap/spark-rules.json (severity, impact, platforms and ordered next_steps must match)", c.ID)
		}
	}
	// Legacy impacts cover exactly the non-Spark rules.
	for _, id := range sortedKeys(rules) {
		_, spark := sparkRules[id]
		imp, legacy := legacyImpact[id]
		if spark == legacy {
			t.Errorf("%s: must be in exactly one of sparkRules / legacyImpact (spark=%v legacy=%v)", id, spark, legacy)
		}
		if legacy && imp != rules[id].Impact {
			t.Errorf("%s: legacyImpact %q, rules.json impact %q", id, imp, rules[id].Impact)
		}
	}
	for id := range legacyImpact {
		if _, ok := rules[id]; !ok {
			t.Errorf("legacyImpact lists %q which rules.json does not know", id)
		}
	}
}

// TestSparkFinding_UnknownIDIsSafe: a typo never panics in production.
func TestSparkFinding_UnknownIDIsSafe(t *testing.T) {
	f := sparkFinding("no-such-rule", "e")
	if f.ID != "no-such-rule" || f.Impact == "" {
		t.Errorf("fallback finding: %+v", f)
	}
}

func TestVersionHelpers(t *testing.T) {
	if !versionLess("580.126.09", "580.159.03") || versionLess("580.159.03", "580.159.03") || versionLess("580.173.02", "580.159.03") {
		t.Error("versionLess on driver versions")
	}
	if !versionLess("6.14.0-1015-nvidia", "6.17") || versionLess("6.17.0-1031-nvidia", "6.17") {
		t.Error("versionLess on kernels")
	}
	if versionLess("garbage", "1.0") || versionLess("1.0", "") {
		t.Error("unparseable input must yield false")
	}
	if versionMajor("616.00") != 616 || versionMajor("") != 0 {
		t.Error("versionMajor")
	}
	if windowsBuild("26100") != 26100 || windowsBuild("10.0.26100") != 26100 || windowsBuild("") != 0 {
		t.Error("windowsBuild")
	}
	if subnet24("192.168.100.1/24") != "192.168.100" || subnet24("<lan-ip>") != "" {
		t.Error("subnet24")
	}
}

// jsonUnmarshal is a thin alias so the test file reads naturally.
func jsonUnmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }
