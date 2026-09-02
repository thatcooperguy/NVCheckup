package report

// Integration 2: the renderers against the fields the collectors really
// produce (PlatformInfo.WoA, UncleanBoots, LinuxInfo.GSPFailureLines,
// DGXOSInfo.UnitsQueried), the Advisory marker in the aggregate next-step
// list, the privacy footer and nil-safety over every optional pointer.

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/analyzer/fixtures"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestRender_WoARows(t *testing.T) {
	r := fixtures.RTXSpark()
	r.Platform.WoA.NvccMachine = "AMD64"
	r.Platform.WoA.NvccPath = `C:\CUDA\v13.0\bin\nvcc.exe`
	text := GenerateText(analyzed(r))
	for _, want := range []string{
		`Adapter:        NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU) [PCI\VEN_10DE&DEV_2E03]`,
		"WDDM driver:    nv_surface_woa.inf, 616.00 Developer Preview",
		"nvcc.exe:       AMD64 (x86 toolkit running under Prism)\n",
		"[WARN] (impact: reversible) #", "(woa-cuda-toolkit-not-native)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q:\n%s", want, text)
		}
	}
	// Privacy: the nvcc path may sit under a user profile, so the Platform
	// row carries only the PE machine word; the path travels in the finding
	// evidence, which internal/redact covers.
	md := GenerateMarkdown(r)
	for _, want := range []string{"| Adapter | NVIDIA RTX Spark N1X", "| WDDM driver | nv_surface_woa.inf, 616.00 Developer Preview |", "| nvcc.exe | AMD64 (x86 toolkit running under Prism) |\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown report missing %q", want)
		}
	}
	// A WoA row without the Developer Preview says so.
	r = fixtures.RTXSpark()
	r.Platform.WoA = &types.WoAInfo{DriverVersion: "32.0.16.2100", InfFilename: "nv_dispi.inf"}
	if text := GenerateText(r); !strings.Contains(text, "WDDM driver:    WDDM 32.0.16.2100, nv_dispi.inf, not the Developer Preview") {
		t.Errorf("non-DP WDDM row:\n%s", text)
	}
}

func TestRender_DGXOSUnitsNotQueried(t *testing.T) {
	r := fixtures.GB10()
	r.DGXOS.UnitsQueried = false
	text := GenerateText(analyzed(r))
	for _, want := range []string{
		"Dashboard:          units not queried (systemctl unavailable or the DGX OS collector did not run); port 11000 not checked",
		"fwupd:              not queried",
		"Persistenced:       not queried",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q", want)
		}
	}
	if strings.Contains(text, "dgx-spark-dashboard-unhealthy") || strings.Contains(text, "dgx-dashboard active") {
		t.Errorf("unknown unit states must neither read as active nor raise the rule:\n%s", text)
	}
	// Release-file-only DGXOSInfo (what common.CollectDGXRelease yields).
	r = fixtures.GB10()
	r.DGXOS = &types.DGXOSInfo{Name: "DGX Spark", PrettyName: "NVIDIA DGX Spark", SWBuildVersion: "7.2.3", OTAVersion: "7.5.0"}
	text = GenerateText(analyzed(r))
	for _, want := range []string{
		"DGX OS:         image 7.2.3 / OTA 7.5.0\n",
		"Release:            NVIDIA DGX Spark, image 7.2.3 (DGX_SWBUILD_VERSION)",
		"OTA:                7.5.0 (DGX_OTA_VERSION)\n",
		"OTA check:          not run (nvidia-spark-ota-check absent, timed out or needs root)",
		"Modules for kernel: not checked (collector did not probe)",
		"Platform: DGX Spark (Founders Edition) | DGX OS 7.2.3 / OTA 7.5.0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q:\n%s", want, text)
		}
	}
	// Modules row follows the analyzer's UnitsQueried contract: a zero-valued
	// ModulesForKernel with a driver package but no probe is unknown, not
	// missing (analyzeDGXOS stays silent on the same state).
	r = fixtures.GB10()
	r.DGXOS.UnitsQueried = false
	r.DGXOS.ModulesForKernel = false
	text = GenerateText(analyzed(r))
	if !strings.Contains(text, "Modules for kernel: not checked (collector did not probe)") || strings.Contains(text, "Modules for kernel: missing") || strings.Contains(text, "dgx-spark-ota-torn") {
		t.Errorf("unprobed modules must render as not checked and not raise ota-torn:\n%s", text)
	}
	// Probed, dpkg silent: unknown as well.
	r = fixtures.GB10()
	r.DGXOS.ModulesForKernel = false
	r.DGXOS.DriverPkgVersion = ""
	if text := GenerateText(analyzed(r)); !strings.Contains(text, "Modules for kernel: not checked (dpkg not queried)") {
		t.Errorf("dpkg-silent modules row:\n%s", text)
	}
	// Probed with a driver package and no modules: missing, and the rule fires.
	r = fixtures.GB10()
	r.DGXOS.ModulesForKernel = false
	if text := GenerateText(analyzed(r)); !strings.Contains(text, "Modules for kernel: missing") || !strings.Contains(text, "dgx-spark-ota-torn") {
		t.Errorf("missing modules row:\n%s", text)
	}
}

func TestRender_UncleanBootsAndGSPLines(t *testing.T) {
	r := fixtures.GB10()
	unclean := false
	r.Platform.PrevBootClean = &unclean
	r.Platform.PrevBootLastLine = "gnome-shell[2041]: Running GNOME Shell"
	r.Platform.UncleanBoots = 2
	text := GenerateText(analyzed(r))
	if !strings.Contains(text, "Previous boot:  no clean-shutdown marker (last line 'gnome-shell[2041]: Running GNOME Shell'); pstore empty; 2 log-less boot(s) in the last 14 days") {
		t.Errorf("unclean boots row:\n%s", text)
	}
	if !strings.Contains(text, "(gb10-logless-hard-poweroff)") {
		t.Errorf("two log-less boots should also raise the rule:\n%s", text)
	}
	md := GenerateMarkdown(r)
	if !strings.Contains(md, "| Previous boot | no clean-shutdown marker") {
		t.Errorf("markdown previous boot row missing")
	}

	g := analyzed(fixtures.GB10GSPFail())
	text = GenerateText(g)
	for _, want := range []string{
		"== GSP / NVRM KERNEL LINES (4 matched, spec 3.2) ==",
		"  NVRM: ksec2PrepareBootCommands_GB20B: SEC2 secure boot partition timed out.",
		"[CRIT] (impact: irreversible) #1: GB10 GPU Missing: GSP Firmware Initialization Failed (dgx-spark-gsp-init-failure)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q", want)
		}
	}
	if strings.Contains(text, "(no-nvidia-gpu)") {
		t.Error("no-nvidia-gpu must stay suppressed on the GSP failure")
	}
	md = GenerateMarkdown(g)
	if !strings.Contains(md, "## GSP / NVRM Kernel Lines") || !strings.Contains(md, "- `NVRM: RmInitAdapter failed! (0x62:0x65:2028)`") {
		t.Errorf("markdown GSP section missing:\n%s", md)
	}
	// More than gspLinesShown lines are capped with a pointer to report.json.
	g = fixtures.GB10GSPFail()
	for i := 0; i < 10; i++ {
		g.Linux.GSPFailureLines = append(g.Linux.GSPFailureLines, "NVRM: RmInitAdapter failed! (0x62:0x65:2028)")
	}
	if text := GenerateText(g); !strings.Contains(text, "(14 matched, spec 3.2)") || !strings.Contains(text, "  ... 8 more in report.json") {
		t.Errorf("GSP line cap:\n%s", text)
	}
}

func TestRender_NextStepsAdvisoryMarkerAndFooter(t *testing.T) {
	r := analyzed(fixtures.RTXSpark())
	text := GenerateText(r)
	section := text[strings.Index(text, "== RECOMMENDED NEXT STEPS =="):]
	section = section[:strings.Index(section, "== PRIVACY")]
	if !strings.Contains(section, ". ! Advisory: installing a different driver") {
		t.Errorf("aggregate next steps must keep the Advisory marker:\n%s", section)
	}
	if !strings.Contains(section, "1. Check the RTX Spark Developer Preview thread") {
		t.Errorf("read-only step first in the aggregate list:\n%s", section)
	}
	if !strings.Contains(text, "  "+footerAdvisory+"\n") || !strings.Contains(text, "IP addresses and serial numbers.") {
		t.Errorf("privacy footer:\n%s", text)
	}
	md := GenerateMarkdown(r)
	if !strings.Contains(md, "## Recommended Next Steps\n\n1. Check the RTX Spark Developer Preview thread") || !strings.Contains(md, "2. **Advisory:** installing a different driver") {
		t.Errorf("markdown aggregate next steps:\n%s", md)
	}
	if !strings.Contains(md, "*"+footerAdvisory+"*  \n") {
		t.Errorf("markdown footer:\n%s", md)
	}
	// No Advisory steps: no footer sentence.
	if text := GenerateText(analyzed(fixtures.GB10())); strings.Contains(text, footerAdvisory) {
		t.Error("healthy GB10 has no Advisory steps and must not print the sentence")
	}
	if text := GenerateText(createTestReport()); strings.Contains(text, footerAdvisory) || strings.Contains(text, "== PLATFORM ==") {
		t.Error("legacy report unchanged apart from the redaction sentence")
	}
}

// TestRender_NilSafety: every renderer survives an empty report, a
// Platform-only report and the fixtures with each optional pointer cleared.
func TestRender_NilSafety(t *testing.T) {
	render := func(label string, r *types.Report) {
		t.Helper()
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("%s: renderer panicked: %v", label, p)
			}
		}()
		GenerateText(r)
		GenerateMarkdown(r)
		if _, err := GenerateJSON(r); err != nil {
			t.Errorf("%s: json: %v", label, err)
		}
	}
	render("empty", &types.Report{})
	for _, class := range []string{"dgx-spark", "rtx-spark", "jetson", "grace-hopper", "arm64-dgpu", ""} {
		render("platform-only "+class, &types.Report{Platform: types.PlatformInfo{Class: class, UnifiedMemory: true, WoA: &types.WoAInfo{}}})
	}
	render("empty sections", &types.Report{Platform: types.PlatformInfo{Class: "dgx-spark"}, UnifiedMemory: &types.UnifiedMemoryInfo{}, DGXOS: &types.DGXOSInfo{}, Cluster: &types.ClusterInfo{Ports: []types.FabricPort{{Cage: -1}}}, Ecosystem: &types.EcosystemInfo{}, Linux: &types.LinuxInfo{GSPFailureLines: []string{"x"}}})
	variants := map[string]func(r *types.Report){
		"Linux":         func(r *types.Report) { r.Linux = nil },
		"Windows":       func(r *types.Report) { r.Windows = nil },
		"AI":            func(r *types.Report) { r.AI = nil },
		"Thermal":       func(r *types.Report) { r.Thermal = nil },
		"PCIe":          func(r *types.Report) { r.PCIe = nil },
		"GPUs":          func(r *types.Report) { r.GPUs = nil },
		"UnifiedMemory": func(r *types.Report) { r.UnifiedMemory = nil },
		"DGXOS":         func(r *types.Report) { r.DGXOS = nil },
		"DGXOS.OTATorn": func(r *types.Report) {
			if r.DGXOS != nil {
				r.DGXOS.OTATorn = nil
			}
		},
		"Cluster":       func(r *types.Report) { r.Cluster = nil },
		"Ecosystem":     func(r *types.Report) { r.Ecosystem = nil },
		"WoA":           func(r *types.Report) { r.Platform.WoA = nil },
		"Firmware":      func(r *types.Report) { r.Platform.Firmware = nil },
		"PrevBootClean": func(r *types.Report) { r.Platform.PrevBootClean = nil },
		"PstoreEmpty":   func(r *types.Report) { r.Platform.PstoreEmpty = nil },
		"Findings":      func(r *types.Report) { r.Findings = nil; r.NextSteps = nil; r.TopIssues = nil },
		"GSPFailureLines": func(r *types.Report) {
			if r.Linux != nil {
				r.Linux.GSPFailureLines = nil
			}
		},
		"PlatformFallback": func(r *types.Report) { r.Platform = types.PlatformInfo{} },
	}
	for name, build := range map[string]func() *types.Report{"gb10": fixtures.GB10, "gsp-fail": fixtures.GB10GSPFail, "rtx": fixtures.RTXSpark, "legacy": createTestReport, "two-gpu": twoGPUReport} {
		for vname, mutate := range variants {
			r := build()
			mutate(r)
			render(name+" without "+vname, analyzed(r))
		}
	}
}
