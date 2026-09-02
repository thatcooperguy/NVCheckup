package analyzer

// Integration 2: the analyzer against what the collectors really produce
// (hand-off: WP1a/WP1b field map), nil-safety over every pointer the report
// can carry, and the catalog / knowledge-pack lockstep in step ORDER.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/analyzer/fixtures"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// ── non-Spark fixtures ────────────────────────────────────────────────

// windows3090 is a desktop RTX 3090 on Windows 11 with dedicated VRAM.
func windows3090() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "windows", Mode: types.ModeFull, Timestamp: fixtures.FixtureTime, RedactionEnabled: true},
		System:   types.SystemInfo{OSName: "Microsoft Windows 11 Pro", OSVersion: "24H2", OSBuild: "26100", Architecture: "amd64", CPUModel: "AMD Ryzen 9 7950X", RAMTotalMB: 65536, BootMode: "UEFI", SecureBoot: "Enabled"},
		GPUs:     []types.GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "591.86", VRAMTotalMB: 24576, VRAMFreeMB: 20000, VRAMUsedMB: 4576, Temperature: 45, ComputeCap: "8.6", MemoryReporting: "dedicated"}},
		Driver:   types.DriverInfo{Version: "591.86", CUDAVersion: "13.1", NvidiaSmiPath: `C:\Windows\System32\nvidia-smi.exe`, Source: "wmi"},
		Windows:  &types.WindowsInfo{HAGSEnabled: "Enabled", GameMode: "Enabled", PowerPlan: "High performance"},
		Thermal:  &types.ThermalInfo{TemperatureC: 45, PowerState: "P8", CurrentClockMHz: 210, MaxClockMHz: 2100, PowerLimitW: "350.00", PowerDrawW: "28.5", FanSupported: true, FanSpeedPct: 30, PowerLimitSupported: true, UtilizationPct: 1, EventCounters: map[string]int64{"sw_power_capping": 0, "hw_thermal_slowdown": 0}},
		PCIe:     &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P8", UtilizationPct: 1, IdleLikely: true},
		AI:       &types.AIInfo{CUDAToolkitVersion: "13.0", NvccPath: `C:\CUDA\bin\nvcc.exe`, PyTorchInfo: &types.PyTorchInfo{Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: true, DeviceName: "NVIDIA GeForce RTX 3090"}},
	}
}

// laptop is an Optimus laptop on Linux: Intel iGPU plus an RTX 4060 Laptop GPU
// whose fan nvidia-smi reports as [N/A].
func laptop() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "linux", Mode: types.ModeFull, Timestamp: fixtures.FixtureTime, RedactionEnabled: true},
		System:   types.SystemInfo{OSName: "Ubuntu", OSVersion: "24.04", KernelVersion: "6.8.0-45-generic", Architecture: "amd64", CPUModel: "13th Gen Intel(R) Core(TM) i7-13700H", RAMTotalMB: 32768, BootMode: "UEFI", SecureBoot: "Enabled"},
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "Intel Raptor Lake-P [Iris Xe Graphics]", Vendor: "Intel"},
			{Index: 0, Name: "NVIDIA GeForce RTX 4060 Laptop GPU", Vendor: "NVIDIA", IsNVIDIA: true, DriverVersion: "580.65.06", VRAMTotalMB: 8188, VRAMFreeMB: 8000, Temperature: 40, ComputeCap: "8.9", MemoryReporting: "dedicated"},
		},
		Driver: types.DriverInfo{Version: "580.65.06", CUDAVersion: "13.0", NvidiaSmiPath: "/usr/bin/nvidia-smi", Source: "package"},
		Linux: &types.LinuxInfo{Distro: "Ubuntu", DistroVersion: "24.04", PackageManager: "apt", NVIDIAPackages: []string{"nvidia-driver-580 580.65.06-0ubuntu1"},
			LoadedModules: map[string]bool{"nvidia": true, "nvidia_drm": true, "nvidia_modeset": true, "nvidia_uvm": true}, SecureBootState: "Enabled", SessionType: "wayland",
			PRIMEStatus: "on-demand", DevNvidiaNodes: []string{"/dev/nvidia0", "/dev/nvidiactl"}, LibCudaPath: "/usr/lib/x86_64-linux-gnu/libcuda.so.580.65.06"},
		Thermal: &types.ThermalInfo{TemperatureC: 40, PowerState: "P8", CurrentClockMHz: 210, MaxClockMHz: 2370, PowerLimitW: "80.00", PowerDrawW: "3.10", FanSupported: false, PowerLimitSupported: true, UtilizationPct: 0},
		PCIe:    &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x8", MaxWidth: "x8", PowerState: "P8", UtilizationPct: 0, IdleLikely: true},
		AI:      &types.AIInfo{CUDAToolkitVersion: "13.0", PyTorchInfo: &types.PyTorchInfo{Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: true, DeviceName: "NVIDIA GeForce RTX 4060 Laptop GPU"}},
	}
}

// rig3 is the three-GPU rig of analyzer_multigpu_test.go with the GPU 0
// pointers set the way the runner sets them.
func rig3() *types.Report {
	r := threeGPURig()
	r.Metadata = types.ReportMetadata{Platform: "linux", Mode: types.ModeFull, Timestamp: fixtures.FixtureTime}
	r.Thermal = &r.GPUThermal[0]
	r.PCIe = &r.GPUPCIe[0]
	return r
}

// jetson is an Orin on JetPack 6 as WP1a classifies it: Class jetson with the
// unified-memory flag (flag rule A) and, because CollectUnifiedMemory is
// gated on that flag, a non-nil UnifiedMemory section.
func jetson() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "linux", Mode: types.ModeFull, Timestamp: fixtures.FixtureTime},
		System: types.SystemInfo{OSName: "Ubuntu", OSVersion: "22.04", KernelVersion: "5.15.148-tegra", Architecture: "arm64", IsJetson: true,
			JetsonRelease: "# R36 (release), REVISION: 4.3, GCID: 38968081, BOARD: generic, EABI: aarch64, DATE: Wed Jan 8 01:49:37 UTC 2025", RAMTotalMB: 62841},
		GPUs:          []types.GPUInfo{{Index: 0, Name: "Orin (nvgpu)", Vendor: "NVIDIA", IsNVIDIA: true, OnPackage: true}},
		Linux:         &types.LinuxInfo{Distro: "Ubuntu", DistroVersion: "22.04", LoadedModules: map[string]bool{"nvgpu": true}, DevNvidiaNodes: []string{"/dev/nvhost-ctrl-gpu"}},
		Platform:      types.PlatformInfo{Class: classJetson, Model: "NVIDIA Jetson AGX Orin Developer Kit", UnifiedMemory: true},
		UnifiedMemory: &types.UnifiedMemoryInfo{MemTotalKB: 62841 * 1024, MemAvailableKB: 50000 * 1024, MemFreeKB: 40000 * 1024, AllocatableKB: 50000 * 1024},
	}
}

func nonSparkFixtures() map[string]func() *types.Report {
	return map[string]func() *types.Report{"windows-3090": windows3090, "laptop": laptop, "rig3": rig3, "jetson": jetson}
}

func sparkFixtures() map[string]func() *types.Report {
	return map[string]func() *types.Report{"gb10": fixtures.GB10, "gb10-gsp-fail": fixtures.GB10GSPFail, "rtx-spark": fixtures.RTXSpark}
}

var allModes = []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeAI, types.ModeCreator, types.ModeFull}

// nilVariants clears one optional pointer / slice of the report each.
func nilVariants() map[string]func(r *types.Report) {
	return map[string]func(r *types.Report){
		"GPUs":    func(r *types.Report) { r.GPUs = nil },
		"Driver":  func(r *types.Report) { r.Driver = types.DriverInfo{} },
		"Windows": func(r *types.Report) { r.Windows = nil },
		"Linux":   func(r *types.Report) { r.Linux = nil },
		"WSL":     func(r *types.Report) { r.WSL = nil },
		"AI":      func(r *types.Report) { r.AI = nil },
		"AI.PyTorchInfo": func(r *types.Report) {
			if r.AI != nil {
				r.AI.PyTorchInfo = nil
			}
		},
		"Thermal":       func(r *types.Report) { r.Thermal = nil },
		"PCIe":          func(r *types.Report) { r.PCIe = nil },
		"GPUThermal":    func(r *types.Report) { r.GPUThermal = nil },
		"GPUPCIe":       func(r *types.Report) { r.GPUPCIe = nil },
		"Displays":      func(r *types.Report) { r.Displays = nil },
		"Network":       func(r *types.Report) { r.Network = nil },
		"UnifiedMemory": func(r *types.Report) { r.UnifiedMemory = nil },
		"DGXOS":         func(r *types.Report) { r.DGXOS = nil },
		"DGXOS.OTATorn": func(r *types.Report) {
			if r.DGXOS != nil {
				r.DGXOS.OTATorn = nil
			}
		},
		"Cluster": func(r *types.Report) { r.Cluster = nil },
		"Cluster.Ports": func(r *types.Report) {
			if r.Cluster != nil {
				r.Cluster.Ports = nil
				r.Cluster.NCCLEnv = nil
			}
		},
		"Ecosystem":              func(r *types.Report) { r.Ecosystem = nil },
		"Platform.WoA":           func(r *types.Report) { r.Platform.WoA = nil },
		"Platform.WoA.empty":     func(r *types.Report) { r.Platform.WoA = &types.WoAInfo{} },
		"Platform.Firmware":      func(r *types.Report) { r.Platform.Firmware = nil },
		"Platform.PrevBootClean": func(r *types.Report) { r.Platform.PrevBootClean = nil },
		"Platform.PstoreEmpty":   func(r *types.Report) { r.Platform.PstoreEmpty = nil },
		"Platform.ACPIThermalMC": func(r *types.Report) { r.Platform.ACPIThermalMC = nil },
		"Thermal.EventCounters": func(r *types.Report) {
			if r.Thermal != nil {
				r.Thermal.EventCounters = nil
			}
		},
		"Linux.GSPFailureLines": func(r *types.Report) {
			if r.Linux != nil {
				r.Linux.GSPFailureLines = nil
			}
		},
	}
}

// sparkPointerVariants are the Spark-only sections; clearing them must not
// change the findings of a non-Spark report (they never had them).
var sparkPointerVariants = map[string]bool{"UnifiedMemory": true, "DGXOS": true, "DGXOS.OTATorn": true, "Cluster": true, "Cluster.Ports": true, "Ecosystem": true, "Platform.WoA": true, "Platform.WoA.empty": true, "Platform.Firmware": true, "Platform.PrevBootClean": true, "Platform.PstoreEmpty": true, "Platform.ACPIThermalMC": true, "Linux.GSPFailureLines": true}

// analyzeNoPanic runs Analyze and converts a panic into a test failure.
func analyzeNoPanic(t *testing.T, label string, r *types.Report, mode types.RunMode) (ok bool) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s (mode %s): Analyze panicked: %v", label, mode, p)
			ok = false
		}
	}()
	Analyze(r, mode)
	return true
}

func sortedIDs(fs []types.Finding) string {
	s := ids(fs)
	sort.Strings(s)
	return strings.Join(s, ",")
}

// TestAnalyze_NilSafety: an empty report, Platform-only reports and every
// fixture with each optional pointer cleared in turn never panic; for the
// non-Spark fixtures clearing a Spark-only section leaves the findings
// unchanged.
func TestAnalyze_NilSafety(t *testing.T) {
	for _, mode := range allModes {
		analyzeNoPanic(t, "empty report", &types.Report{}, mode)
		for _, class := range []string{classDGXSpark, classRTXSpark, classJetson, classGraceHopper, classArm64DGPU, ""} {
			analyzeNoPanic(t, "platform-only "+class, &types.Report{Platform: types.PlatformInfo{Class: class, UnifiedMemory: class == classDGXSpark || class == classRTXSpark || class == classJetson}}, mode)
		}
		analyzeNoPanic(t, "platform-only rtx-spark on WoA", &types.Report{Metadata: types.ReportMetadata{Platform: "windows"}, Platform: types.PlatformInfo{Class: classRTXSpark, IsWindowsOnArm: true, ProcessEmulated: true, WoA: &types.WoAInfo{}}}, mode)
		analyzeNoPanic(t, "dgx-spark with empty sections", &types.Report{Platform: types.PlatformInfo{Class: classDGXSpark, UnifiedMemory: true}, UnifiedMemory: &types.UnifiedMemoryInfo{}, DGXOS: &types.DGXOSInfo{}, Cluster: &types.ClusterInfo{}, Ecosystem: &types.EcosystemInfo{}, Linux: &types.LinuxInfo{}}, mode)
	}
	all := map[string]func() *types.Report{}
	for k, v := range nonSparkFixtures() {
		all[k] = v
	}
	for k, v := range sparkFixtures() {
		all[k] = v
	}
	for name, build := range all {
		_, nonSpark := nonSparkFixtures()[name]
		for _, mode := range allModes {
			base := build()
			if !analyzeNoPanic(t, name+" baseline", base, mode) {
				continue
			}
			baseIDs := sortedIDs(base.Findings)
			for vname, mutate := range nilVariants() {
				r := build()
				mutate(r)
				if !analyzeNoPanic(t, name+" without "+vname, r, mode) {
					continue
				}
				if nonSpark && sparkPointerVariants[vname] {
					if got := sortedIDs(r.Findings); got != baseIDs {
						t.Errorf("%s (mode %s): clearing %s changed the findings: %s -> %s", name, mode, vname, baseIDs, got)
					}
				}
				for _, f := range r.Findings {
					if f.Impact == "" {
						t.Errorf("%s (mode %s, without %s): %s has no impact", name, mode, vname, f.ID)
					}
				}
			}
		}
	}
}

// TestSparkRules_NeverFireOnNonSparkFixtures: none of the 51 catalog rules
// fires on the Windows 3090, laptop, rig3 or Jetson reports in any mode.
func TestSparkRules_NeverFireOnNonSparkFixtures(t *testing.T) {
	for name, build := range nonSparkFixtures() {
		for _, mode := range allModes {
			r := build()
			Analyze(r, mode)
			for _, f := range r.Findings {
				if _, spark := sparkRules[f.ID]; spark {
					t.Errorf("%s (mode %s): Spark rule %s fired: %s", name, mode, f.ID, f.Evidence)
				}
			}
			if strings.Contains(r.SummaryBlock, "Platform: DGX Spark") || strings.Contains(r.SummaryBlock, "Platform: RTX Spark") {
				t.Errorf("%s: summary names a Spark platform:\n%s", name, r.SummaryBlock)
			}
		}
	}
}

// ── catalog lockstep in step order ─────────────────────────────────────

type catalogRule struct {
	ID        string   `json:"id"`
	Severity  string   `json:"severity"`
	Impact    string   `json:"impact"`
	NextSteps []string `json:"next_steps"`
}

func loadCatalog(t *testing.T) []catalogRule {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "roadmap", "spark-rules.json"))
	if err != nil {
		t.Fatalf("read spark-rules.json: %v", err)
	}
	var doc struct {
		Rules []catalogRule `json:"rules"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse spark-rules.json: %v", err)
	}
	return doc.Rules
}

// TestCatalog_KnowledgeAgreesWithSparkRulesInOrder: knowledge/rules.json and
// docs/roadmap/spark-rules.json agree on id, severity, impact and next_steps
// (text and order) for the 51 Spark rules; the catalog lists read-only steps
// before Advisory ones; the System Recovery reimage steps carry the
// "Advisory: (data loss)" prefix so ^Advisory\b is the only contract.
func TestCatalog_KnowledgeAgreesWithSparkRulesInOrder(t *testing.T) {
	_, rules := loadKnowledgeRules(t)
	catalog := loadCatalog(t)
	if len(catalog) != 51 {
		t.Fatalf("catalog has %d rules, expected 51", len(catalog))
	}
	reimage := 0
	for _, c := range catalog {
		j, ok := rules[c.ID]
		if !ok {
			t.Errorf("%s: missing from knowledge/rules.json", c.ID)
			continue
		}
		if j.Severity != c.Severity || j.Impact != c.Impact {
			t.Errorf("%s: severity/impact %s/%s in rules.json vs %s/%s in the catalog", c.ID, j.Severity, j.Impact, c.Severity, c.Impact)
		}
		if strings.Join(j.NextSteps, "\n") != strings.Join(c.NextSteps, "\n") {
			t.Errorf("%s: next_steps differ (text or order):\n rules.json: %q\n catalog:    %q", c.ID, j.NextSteps, c.NextSteps)
		}
		if g, ok := sparkRules[c.ID]; !ok || strings.Join(g.Steps, "\n") != strings.Join(c.NextSteps, "\n") {
			t.Errorf("%s: generated Go table differs from the catalog", c.ID)
		}
		sawAdvisory := false
		for _, step := range c.NextSteps {
			if strings.HasPrefix(step, "Last resort") {
				t.Errorf("%s: step %q must carry the 'Advisory: (data loss)' prefix", c.ID, step)
			}
			if strings.Contains(step, "System Recovery image") {
				reimage++
				if !strings.HasPrefix(step, "Advisory: (data loss) ") {
					t.Errorf("%s: reimage step %q is not prefixed 'Advisory: (data loss) '", c.ID, step)
				}
			}
			switch {
			case advisoryRe.MatchString(step):
				sawAdvisory = true
			case sawAdvisory:
				t.Errorf("%s: read-only step %q follows an Advisory step in the catalog", c.ID, step)
			}
		}
	}
	if reimage != 2 {
		t.Errorf("expected exactly 2 System Recovery reimage steps, found %d", reimage)
	}
}

// ── field semantics against the producers ──────────────────────────────

// TestDashboardUnhealthy_RequiresUnitsQueried: a DGXOSInfo filled from
// /etc/dgx-release alone (UnitsQueried false, zero-valued booleans) never
// reports the units unhealthy; with systemctl answers it does.
func TestDashboardUnhealthy_RequiresUnitsQueried(t *testing.T) {
	r := fixtures.GB10()
	r.DGXOS = &types.DGXOSInfo{Name: "DGX Spark", PrettyName: "NVIDIA DGX Spark", SWBuildVersion: "7.2.3", OTAVersion: "7.5.0"}
	Analyze(r, types.ModeFull)
	for _, id := range []string{"dgx-spark-dashboard-unhealthy", "dgx-spark-ota-torn"} {
		if findByID(r.Findings, id) != nil {
			t.Errorf("%s fired on a release-file-only DGXOSInfo: %v", id, ids(r.Findings))
		}
	}
	r.DGXOS.UnitsQueried = true
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "dgx-spark-dashboard-unhealthy")
	if f == nil || !strings.Contains(f.Evidence, "dgx-dashboard inactive, dgx-dashboard-admin inactive, port 11000 closed, fwupd inactive") {
		t.Errorf("with systemctl answers the all-inactive unit set must fire: %+v", f)
	}
	// One inactive unit on an otherwise healthy fixture.
	r = fixtures.GB10()
	r.DGXOS.DashboardActive = false
	r.DGXOS.UnitsQueried = false
	if f := findByID(analyzeDGXOS(r), "dgx-spark-dashboard-unhealthy"); f != nil {
		t.Errorf("UnitsQueried false must keep the rule silent: %+v", f)
	}
	r.DGXOS.UnitsQueried = true
	if f := findByID(analyzeDGXOS(r), "dgx-spark-dashboard-unhealthy"); f == nil || !strings.Contains(f.Evidence, "dgx-dashboard inactive") || f.Impact != "persistent" {
		t.Errorf("dashboard inactive with UnitsQueried should fire: %+v", f)
	}
}

// TestOTATorn_ModulesClauseRequiresUnitsQueried: the "modules for the
// running kernel missing" clause only counts when the collector probed.
func TestOTATorn_ModulesClauseRequiresUnitsQueried(t *testing.T) {
	r := fixtures.GB10()
	r.DGXOS.ModulesForKernel = false
	r.DGXOS.UnitsQueried = false
	if f := findByID(analyzeDGXOS(r), "dgx-spark-ota-torn"); f != nil {
		t.Errorf("modules clause must not fire when the collector did not probe: %+v", f)
	}
	r.DGXOS.UnitsQueried = true
	if f := findByID(analyzeDGXOS(r), "dgx-spark-ota-torn"); f == nil || !strings.Contains(f.Evidence, "modules for 6.17.0-1031-nvidia: missing") {
		t.Errorf("modules missing with UnitsQueried should fire: %+v", f)
	}
	// The torn score and the package mismatch do not depend on systemctl.
	r = fixtures.GB10()
	r.DGXOS.UnitsQueried = false
	r.DGXOS.FirmwarePkgVersion = "580.126.09-0ubuntu0.24.04.1"
	if f := findByID(analyzeDGXOS(r), "dgx-spark-ota-torn"); f == nil || !strings.Contains(f.Evidence, "580.159.03-0ubuntu0.24.04.1 vs firmware pkg 580.126.09-0ubuntu0.24.04.1") {
		t.Errorf("package mismatch is dpkg-based and fires regardless of UnitsQueried: %+v", f)
	}
}

// TestGSPInitFailure_ReadsGSPFailureLines: the SEC2 / GSP lines arrive in
// LinuxInfo.GSPFailureLines (linux.CollectNVRMMessages) without
// --include-logs, and raise the confidence to 95 like dmesg snippets do.
func TestGSPInitFailure_ReadsGSPFailureLines(t *testing.T) {
	r := fixtures.GB10GSPFail()
	if r.Linux.DmesgSnippets != "" || len(r.Linux.GSPFailureLines) != 4 {
		t.Fatalf("fixture should carry the lines in GSPFailureLines only: %q %d", r.Linux.DmesgSnippets, len(r.Linux.GSPFailureLines))
	}
	r.Linux.XidErrors = nil
	Analyze(r, types.ModeFull)
	f := findByID(r.Findings, "dgx-spark-gsp-init-failure")
	if f == nil || f.Confidence != 95 || !strings.Contains(f.Evidence, "Timeout after 6s of waiting for RPC response from GPU0 GSP") {
		t.Fatalf("GSPFailureLines must feed the rule at confidence 95: %+v", f)
	}
	if findByID(r.Findings, "no-nvidia-gpu") != nil || findByID(r.Findings, "driver-not-detected") != nil {
		t.Errorf("spec 5.1 suppressions must hold: %v", ids(r.Findings))
	}
	// The 'No devices were found' path alone still fires (confidence 80).
	r = fixtures.GB10GSPFail()
	r.Linux.GSPFailureLines = nil
	r.Linux.XidErrors = nil
	Analyze(r, types.ModeFull)
	if f := findByID(r.Findings, "dgx-spark-gsp-init-failure"); f == nil || f.Confidence != 80 {
		t.Errorf("'No devices were found' on GB10 without kernel lines: %+v", f)
	}
}

// TestSwapInUse_PswpinDelta: a positive pswpin delta fires the INFO on its own
// while a GPU process is present; zero proves nothing.
func TestSwapInUse_PswpinDelta(t *testing.T) {
	r := fixtures.GB10()
	r.UnifiedMemory.GPUProcesses = 1
	r.UnifiedMemory.PswpinDelta = 4096
	f := findByID(analyzeUnifiedMemory(r), "unified-memory-swap-in-use")
	if f == nil || f.Severity != types.SeverityInfo || !strings.Contains(f.Evidence, "pswpin delta 4096 pages") {
		t.Errorf("pswpin growth should fire INFO: %+v", f)
	}
	r.UnifiedMemory.GPUProcesses = 0
	if f := findByID(analyzeUnifiedMemory(r), "unified-memory-swap-in-use"); f != nil {
		t.Errorf("without a GPU process the rule stays silent: %+v", f)
	}
	r = fixtures.GB10()
	r.UnifiedMemory.GPUProcesses = 1
	if f := findByID(analyzeUnifiedMemory(r), "unified-memory-swap-in-use"); f != nil {
		t.Errorf("no swap used and delta 0 must not fire: %+v", f)
	}
}

// TestNvsmiExpected_PoolSourceByPlatform: the pool source named in the
// evidence follows the collector (spec 3.3 vs spec 8).
func TestNvsmiExpected_PoolSourceByPlatform(t *testing.T) {
	r := fixtures.RTXSpark()
	if f := findByID(analyzeUnifiedMemory(r), "unified-memory-nvsmi-expected"); f == nil || !strings.Contains(f.Evidence, "measured from Win32_OperatingSystem TotalVisibleMemorySize") || strings.Contains(f.Evidence, "/proc/meminfo") {
		t.Errorf("RTX Spark pool source: %+v", f)
	}
	if f := findByID(analyzeUnifiedMemory(fixtures.GB10()), "unified-memory-nvsmi-expected"); f == nil || !strings.Contains(f.Evidence, "measured from /proc/meminfo") {
		t.Errorf("GB10 pool source: %+v", f)
	}
}

// TestWoA_PlatformWoAFields: rtx-spark-driver-developer-preview and
// woa-cuda-toolkit-not-native read PlatformInfo.WoA first and keep the
// Driver.Source / GPUInfo.DriverVersion / toolkit-version fallbacks.
func TestWoA_PlatformWoAFields(t *testing.T) {
	// INF and DeveloperPreview from the WoA collector, no nvidia-smi at all.
	r := rtxSpark(func(r *types.Report) { r.Driver = types.DriverInfo{}; r.GPUs[0].DriverVersion = "" })
	f := findByID(analyzeWoA(r), "rtx-spark-driver-developer-preview")
	if f == nil || !strings.Contains(f.Evidence, "(nv_surface_woa.inf)") {
		t.Errorf("WoA INF should identify the DP driver: %+v", f)
	}
	// WDDM suffix from the WoA row.
	r = rtxSpark(func(r *types.Report) {
		r.Driver = types.DriverInfo{}
		r.GPUs[0].DriverVersion = ""
		r.Platform.WoA = &types.WoAInfo{DriverVersion: "32.0.16.1600"}
	})
	if f := findByID(analyzeWoA(r), "rtx-spark-driver-developer-preview"); f == nil || !strings.Contains(f.Evidence, "Driver 32.0.16.1600 (WDDM 32.0.16.1600)") {
		t.Errorf("WoA WDDM suffix: %+v", f)
	}
	// A four-part WDDM string in Driver.Version is not "below 616.00".
	r = rtxSpark(func(r *types.Report) {
		r.Driver = types.DriverInfo{Version: "32.0.16.2100"}
		r.GPUs[0].DriverVersion = "32.0.16.2100"
		r.Platform.WoA = &types.WoAInfo{DriverVersion: "32.0.16.2100", InfFilename: "nv_dispi.inf"}
	})
	if f := findByID(analyzeWoA(r), "rtx-spark-driver-developer-preview"); f != nil {
		t.Errorf("a production four-part WDDM version must not look old: %+v", f)
	}
	// A release string below 616 on Arm64 is the extracted-build case.
	r = rtxSpark(func(r *types.Report) {
		r.Driver = types.DriverInfo{Version: "580.88"}
		r.GPUs[0].DriverVersion = "580.88"
		r.Platform.WoA = nil
	})
	if f := findByID(analyzeWoA(r), "rtx-spark-driver-developer-preview"); f == nil || !strings.Contains(f.Evidence, "below the 616.00 Developer Preview") {
		t.Errorf("old release string: %+v", f)
	}
	// nvcc.exe PE machine decides the toolkit rule when collected.
	r = rtxSpark(func(r *types.Report) {
		r.Platform.WoA.NvccMachine = "AMD64"
		r.Platform.WoA.NvccPath = `C:\CUDA\v13.0\bin\nvcc.exe`
	})
	if f := findByID(analyzeWoA(r), "woa-cuda-toolkit-not-native"); f == nil || !strings.Contains(f.Evidence, "nvcc.exe PE machine AMD64") || !strings.Contains(f.Evidence, `C:\CUDA\v13.0\bin\nvcc.exe`) {
		t.Errorf("AMD64 nvcc.exe should fire without AI data: %+v", f)
	}
	r = rtxSpark(func(r *types.Report) {
		r.Platform.WoA.NvccMachine = "ARM64"
		r.AI = &types.AIInfo{CUDAToolkitVersion: "13.3"}
	})
	if f := findByID(analyzeWoA(r), "woa-cuda-toolkit-not-native"); f != nil {
		t.Errorf("a native ARM64 nvcc.exe is not emulated whatever the version: %+v", f)
	}
	r = rtxSpark(func(r *types.Report) { r.Platform.WoA = nil; r.AI = &types.AIInfo{CUDAToolkitVersion: "13.3"} })
	if f := findByID(analyzeWoA(r), "woa-cuda-toolkit-not-native"); f == nil || !strings.Contains(f.Evidence, "version <= 13.3") {
		t.Errorf("version fallback without the PE machine: %+v", f)
	}
	// rtx-spark-detected takes cores and DEV id from the WMI adapter row when
	// nvidia-smi.exe enumerated nothing.
	r = rtxSpark(func(r *types.Report) { r.GPUs = nil })
	if f := findByID(analyzePlatformDetected(r), "rtx-spark-detected"); f == nil || !strings.Contains(f.Evidence, "RTX Spark N1X (6144-core, DEV_2E03)") {
		t.Errorf("rtx-spark-detected from PlatformInfo.WoA: %+v", f)
	}
}

// TestCluster_StateWordsAndUnknownCage: FabricPort.State may be the sysfs
// text, the ibdev2netdev word or the operstate; Cage -1 (unknown) ports are
// never paired as twins.
func TestCluster_StateWordsAndUnknownCage(t *testing.T) {
	r := gb10(withCluster(func(c *types.ClusterInfo) {
		c.Ports[0].State, c.Ports[0].PhysState = "Up", ""
		c.Ports[1].State, c.Ports[1].PhysState = "up", ""
	}))
	if got := analyzeCluster(r); len(got) != 0 {
		t.Errorf("Up/up twins with addresses are healthy, got %v", ids(got))
	}
	r = gb10(withCluster(func(c *types.ClusterInfo) { c.Ports[1].State, c.Ports[1].PhysState = "down", "" }))
	if f := findByID(analyzeCluster(r), "cx7-twin-link-mismatch"); f == nil || !strings.Contains(f.Evidence, "enP2p1s0f0np0 (roceP2p1s0f0) down") {
		t.Errorf("operstate down should pair as a mismatch: %+v", f)
	}
	r = gb10(withCluster(func(c *types.ClusterInfo) {
		c.Ports[0].Cage, c.Ports[1].Cage = -1, -1
		c.Ports[1].State, c.Ports[1].PhysState = "1: DOWN", "3: Disabled"
		c.Ports[1].IPv4 = []string{"192.168.100.2/24"}
		c.Ports[1].MTU = 1500
	}))
	got := analyzeCluster(r)
	for _, id := range []string{"cx7-twin-link-mismatch", "cx7-twins-same-subnet"} {
		if findByID(got, id) != nil {
			t.Errorf("%s must not pair ports of unknown cage: %v", id, ids(got))
		}
	}
}

// TestArm64Rules_ClassGated: arm64-flash-attn-no-wheel and
// arm64-container-amd64-image follow the catalog platforms (dgx-spark,
// arm64-dgpu), not the CPU architecture, so Jetson stays quiet.
func TestArm64Rules_ClassGated(t *testing.T) {
	eco := &types.EcosystemInfo{FlashAttnVersion: "2.7.4", Images: []types.ContainerImage{{Ref: "ghcr.io/example/tool:latest", Arch: "amd64"}}}
	j := jetson()
	j.Ecosystem = eco
	Analyze(j, types.ModeFull)
	for _, id := range []string{"arm64-flash-attn-no-wheel", "arm64-container-amd64-image"} {
		if findByID(j.Findings, id) != nil {
			t.Errorf("%s fired on Jetson: %v", id, ids(j.Findings))
		}
	}
	d := jetson()
	d.System.IsJetson = false
	d.Platform = types.PlatformInfo{Class: classArm64DGPU}
	d.Ecosystem = eco
	Analyze(d, types.ModeFull)
	for _, id := range []string{"arm64-flash-attn-no-wheel", "arm64-container-amd64-image"} {
		if findByID(d.Findings, id) == nil {
			t.Errorf("%s should fire on arm64-dgpu: %v", id, ids(d.Findings))
		}
	}
}

// TestFirmwareVersionOf_CX7: a ConnectX-7 firmware row is quoted by
// cx7-link-speed-degraded and never measured against the FE thresholds.
func TestFirmwareVersionOf_CX7(t *testing.T) {
	r := gb10(func(r *types.Report) {
		r.Platform.Firmware = append(r.Platform.Firmware, types.FirmwareComponent{Name: "ConnectX-7 Network Adapter", Version: "28.43.1014"})
		r.Cluster.Ports[0].SpeedMbps = 100000
	})
	if f := findByID(analyzeCluster(r), "cx7-link-speed-degraded"); f == nil || !strings.Contains(f.Evidence, "CX-7 firmware 28.43.1014") {
		t.Errorf("cx7 firmware in evidence: %+v", f)
	}
	if f := findByID(analyzeFirmware(r), "dgx-spark-firmware-behind"); f != nil {
		t.Errorf("the NIC row has no FE threshold: %+v", f)
	}
}

// TestSummary_PlatformLineReading: the summary Platform line uses the same
// swbuild / OTA reading as dgx-spark-detected and stays within 72 columns.
func TestSummary_PlatformLineReading(t *testing.T) {
	r := fixtures.GB10()
	Analyze(r, types.ModeFull)
	if !strings.Contains(r.SummaryBlock, "Platform: DGX Spark (Founders Edition) | DGX OS 7.2.3 / OTA 7.5.0\n") {
		t.Errorf("summary platform line:\n%s", r.SummaryBlock)
	}
	if det := findByID(r.Findings, "dgx-spark-detected"); det == nil || !strings.Contains(det.Evidence, "DGX OS 7.2.3 / OTA 7.5.0 (OTA2607)") {
		t.Errorf("dgx-spark-detected evidence reading: %+v", det)
	}
	// OEM unit with a long vendor/model: truncated but still identifiable.
	r = fixtures.GB10()
	r.Platform.Vendor, r.Platform.Model = "ASUSTeK COMPUTER INC.", "Ascent GX10 Developer Workstation Edition"
	Analyze(r, types.ModeFull)
	for _, line := range strings.Split(strings.TrimRight(r.SummaryBlock, "\n"), "\n") {
		if n := len([]rune(line)); n > 72 {
			t.Errorf("summary line is %d runes (> 72): %q", n, line)
		}
	}
	if !strings.Contains(r.SummaryBlock, "Platform: DGX Spark (OEM: ASUSTeK") {
		t.Errorf("OEM summary line:\n%s", r.SummaryBlock)
	}
	// Only the OTA name known: it stands in for the version.
	r = fixtures.GB10()
	r.DGXOS.OTAVersion = ""
	Analyze(r, types.ModeFull)
	if !strings.Contains(r.SummaryBlock, "| DGX OS 7.2.3 / OTA2607\n") {
		t.Errorf("OTA name fallback:\n%s", r.SummaryBlock)
	}
}
