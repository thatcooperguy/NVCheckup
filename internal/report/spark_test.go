package report

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/analyzer"
	"github.com/thatcooperguy/nvcheckup/internal/analyzer/fixtures"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// update rewrites the golden files: go test ./internal/report -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files under testdata")

// analyzed runs the analyzer on a fixture so the rendered report carries the
// real finding set (the golden files therefore also pin the analyzer output).
func analyzed(r *types.Report) *types.Report {
	analyzer.Analyze(r, types.ModeFull)
	return r
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	got = strings.ReplaceAll(got, "\r\n", "\n")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if w := strings.ReplaceAll(string(want), "\r\n", "\n"); w != got {
		t.Errorf("%s differs from golden file; run 'go test ./internal/report -run Golden -update' after checking the diff.\n--- got ---\n%s", name, got)
	}
}

func TestGolden_GB10Text(t *testing.T) {
	out := GenerateText(analyzed(fixtures.GB10()))
	checkGolden(t, "gb10_report.txt", out)
	for _, want := range []string{
		"== PLATFORM ==",
		"Class:          DGX Spark (dgx-spark)",
		"Vendor/Model:   NVIDIA NVIDIA_DGX_Spark (Founders Edition) [version A.7, BIOS 5.36_0ACUM023]",
		"GPU SoC:        GB10 (compute capability 12.1)",
		"DGX OS:         image 7.2.3 / OTA 7.5.0 (OTA2607)",
		"Previous boot:  clean shutdown (last line 'systemd-journald[512]: Journal stopped'); pstore empty; 0 log-less boot(s) in the last 14 days",
		"Memory pool:    119.7 GiB total, 115.9 GiB available, 131.9 GiB allocatable",
		"Cluster fabric: 2 ConnectX-7 port(s): enp1s0f0np0 4: ACTIVE 200000 Mb/s; enP2p1s0f0np0 4: ACTIVE 200000 Mb/s",
		"== UNIFIED MEMORY",
		"== DGX OS ==",
		"== FIRMWARE (fwupdmgr get-devices) ==",
		"Release:            NVIDIA DGX Spark, image 7.2.3 (DGX_SWBUILD_VERSION, built 2025-09-10-13-50-03, commit 833b4a7)",
		"OTA:                7.5.0 (DGX_OTA_VERSION) OTA2607, applied Wed Jul 15 09:06:56 AM PDT 2026",
		"Dashboard:          dgx-dashboard active, dgx-dashboard-admin active, port 11000 open",
		"Embedded Controller:           3.5.8",
		"UEFI Device Firmware:          2.155.11",
		"== CLUSTER FABRIC (ConnectX-7) ==",
		"Memory:    unified pool (nvidia-smi reports [N/A]; see PLATFORM)",
		"Compute:   CC 12.1",
		"PCIe:          n/a (on-package, NVLink-C2C)",
		"Thermal:       42°C, P8, fan N/A, 9.87 W / limit N/A",
		"Platform: DGX Spark (Founders Edition) | DGX OS 7.2.3 / OTA 7.5.0",
		"Redaction was applied to remove usernames, hostnames, home paths, IP addresses and serial numbers.",
		"Unified memory: 119.7 GiB total, 115.9 GiB available",
		"[INFO] #1:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report missing %q", want)
		}
	}
	for _, forbidden := range []string{"VRAM:", "DOWNSHIFTED", "Gen1 x1", "(impact:", footerAdvisory, "0x03000508"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("text report must not contain %q on the healthy GB10 fixture", forbidden)
		}
	}
}

func TestGolden_GB10Markdown(t *testing.T) {
	out := GenerateMarkdown(analyzed(fixtures.GB10()))
	checkGolden(t, "gb10_report.md", out)
	for _, want := range []string{
		"## Platform", "| Class | DGX Spark (dgx-spark) |",
		"## Unified Memory", "| MemTotal | 119.7 GiB |",
		"## DGX OS", "## Firmware", "## Cluster Fabric (ConnectX-7)",
		"| Memory | unified pool (nvidia-smi reports [N/A]; see Platform) |",
		"**PCIe:** n/a (on-package, NVLink-C2C)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown report missing %q", want)
		}
	}
}

func TestGolden_RTXSparkText(t *testing.T) {
	out := GenerateText(analyzed(fixtures.RTXSpark()))
	checkGolden(t, "rtx_spark_report.txt", out)
	for _, want := range []string{
		"Class:          RTX Spark (rtx-spark)",
		"Vendor/Model:   Microsoft Surface RTX Spark Dev Box",
		"GPU SoC:        N1X (compute capability 12.1)",
		"Windows on Arm: yes (native ARM64, NVCheckup emulated: no)",
		`Adapter:        NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU) [PCI\VEN_10DE&DEV_2E03]`,
		"WDDM driver:    nv_surface_woa.inf, 616.00 Developer Preview",
		"Memory pool:    128.0 GiB total, 100.0 GiB available, 100.0 GiB allocatable",
		"  2. ! Advisory: installing a different driver replaces the Developer Preview package",
		footerAdvisory,
		"[WARN] (impact: persistent) #1: RTX Spark Developer Preview Driver (rtx-spark-driver-developer-preview)",
		"      • Check the RTX Spark Developer Preview thread (S24) and OEM/Windows Update for a production Arm64 driver (read-only).",
		"      ! Advisory: installing a different driver replaces the Developer Preview package (revert: reinstall the 616.00 DP package from the S24 thread).",
		"nvidia-smi Not Found (may be absent on RTX Spark)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report missing %q", want)
		}
	}
	if strings.Contains(out, "== DGX OS ==") || strings.Contains(out, "CLUSTER FABRIC") {
		t.Error("RTX Spark report must not print DGX OS or cluster sections")
	}
}

func TestGolden_RTXSparkMarkdown(t *testing.T) {
	out := GenerateMarkdown(analyzed(fixtures.RTXSpark()))
	checkGolden(t, "rtx_spark_report.md", out)
	for _, want := range []string{
		"| **WARN** (impact: persistent) | RTX Spark Developer Preview Driver |",
		"<summary><b>[WARN] (impact: persistent) #1: RTX Spark Developer Preview Driver</b></summary>",
		"- **Advisory:** installing a different driver replaces the Developer Preview package",
		"| Windows on Arm | yes (native ARM64, NVCheckup emulated: no) |",
		"| WDDM driver | nv_surface_woa.inf, 616.00 Developer Preview |",
		"2. **Advisory:** installing a different driver replaces the Developer Preview package",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown report missing %q", want)
		}
	}
}

// TestAdvisorySteps_RenderedDistinctAndAfterReadOnly: the renderers never
// list an Advisory step before a read-only one, whatever order the finding
// carries, and only Advisory steps get the marker.
func TestAdvisorySteps_RenderedDistinctAndAfterReadOnly(t *testing.T) {
	report := createTestReport()
	report.Findings = []types.Finding{{
		ID: "x-rule", Severity: types.SeverityCrit, Title: "X", Impact: "data-loss",
		NextSteps: []string{
			"Advisory: (data loss) sudo snap remove docker deletes the snap's containers (revert: snap restore).",
			"Read-only: docker info.",
			"Advisory sudo apt install docker-ce (revert: apt remove docker-ce).",
			"Advisory: (data loss) Last resort, only if nothing else works: the System Recovery image ERASES ALL DATA.",
			"Second read-only check.",
		},
	}}
	text := GenerateText(report)
	idx := func(s, sub string) int { return strings.Index(s, sub) }
	if !strings.Contains(text, "[CRIT] (impact: data-loss) #1: X (x-rule)") {
		t.Errorf("impact should be printed next to the severity:\n%s", text)
	}
	if !strings.Contains(text, "      • Read-only: docker info.\n      • Second read-only check.\n      ! Advisory: (data loss) sudo snap remove docker") {
		t.Errorf("read-only steps must come first with the bullet, advisories after with '!':\n%s", text)
	}
	if !strings.Contains(text, "      ! Advisory sudo apt install docker-ce") {
		t.Errorf("'Advisory' without a colon also qualifies (^Advisory word boundary):\n%s", text)
	}
	if idx(text, "! Advisory sudo apt") > idx(text, "! Advisory: (data loss) Last resort") || idx(text, "! Advisory: (data loss) Last resort") < 0 {
		t.Errorf("the prefixed last-resort step is an Advisory step: '!' marker, original order among the Advisory steps:\n%s", text)
	}
	if !strings.Contains(text, footerAdvisory) {
		t.Errorf("a report with Advisory steps carries the footer sentence:\n%s", text)
	}
	md := GenerateMarkdown(report)
	if !strings.Contains(md, "| **CRIT** (impact: data-loss) | X |  | Read-only: docker info. |") {
		t.Errorf("markdown table should show impact and the first read-only step:\n%s", md)
	}
	if !strings.Contains(md, "- Read-only: docker info.\n- Second read-only check.\n- **Advisory:** (data loss) sudo snap remove docker") {
		t.Errorf("markdown details should order read-only first and bold Advisory:\n%s", md)
	}
	if !strings.Contains(md, "- **Advisory** sudo apt install docker-ce") {
		t.Errorf("markdown should bold the bare Advisory token:\n%s", md)
	}
	// The RECOMMENDED NEXT STEPS list (report.NextSteps, built by the
	// analyzer) carries the same marker: "!" in text, bold in markdown.
	report.NextSteps = []string{
		"Read-only: docker info.",
		"Advisory: (data loss) sudo snap remove docker deletes the snap's containers (revert: snap restore).",
		"Advisory sudo apt install docker-ce (revert: apt remove docker-ce).",
	}
	text = GenerateText(report)
	if !strings.Contains(text, "== RECOMMENDED NEXT STEPS ==\n\n  1. Read-only: docker info.\n  2. ! Advisory: (data loss) sudo snap remove docker") || !strings.Contains(text, "  3. ! Advisory sudo apt install docker-ce") {
		t.Errorf("RECOMMENDED NEXT STEPS should mark Advisory steps with '!':\n%s", text)
	}
	md = GenerateMarkdown(report)
	if !strings.Contains(md, "## Recommended Next Steps\n\n1. Read-only: docker info.\n2. **Advisory:** (data loss) sudo snap remove docker") || !strings.Contains(md, "3. **Advisory** sudo apt install docker-ce") {
		t.Errorf("Recommended Next Steps should bold the Advisory token:\n%s", md)
	}
	// INFO findings and findings without impact keep the plain header.
	report.Findings = []types.Finding{{ID: "y", Severity: types.SeverityInfo, Title: "Y", Impact: "none"}, {ID: "z", Severity: types.SeverityWarn, Title: "Z"}}
	text = GenerateText(report)
	if !strings.Contains(text, "[INFO] #1: Y (y)") || !strings.Contains(text, "[WARN] #2: Z (z)") {
		t.Errorf("INFO and impact-less findings must not print an impact label:\n%s", text)
	}
}

func TestThermalSummary_LimitNAOnUnifiedMemoryOnly(t *testing.T) {
	// A GB10 sample whose collector filled PowerLimitSupported=false but no
	// EventCounters still prints the draw with "limit N/A" on a unified
	// platform; a legacy discrete sample with no limit prints no power.
	sample := &types.ThermalInfo{TemperatureC: 42, PowerState: "P8", PowerDrawW: "9.87", PowerLimitSupported: false}
	if got := thermalSummary(sample, true); got != "42°C, P8, fan N/A, 9.87 W / limit N/A" {
		t.Errorf("unified: %q", got)
	}
	if got := thermalSummary(sample, false); got != "42°C, P8, fan N/A" {
		t.Errorf("legacy discrete: %q", got)
	}
	full := &types.ThermalInfo{TemperatureC: 60, PowerState: "P0", PowerDrawW: "250.0", PowerLimitW: "450.0", FanSupported: true, FanSpeedPct: 40}
	if got := thermalSummary(full, true); got != "60°C, P0, fan 40%, 250.0 / 450.0 W" {
		t.Errorf("real limit wins even when unified: %q", got)
	}
	// Through the renderers: the fixture without EventCounters.
	r := fixtures.GB10()
	r.Thermal.EventCounters = nil
	if out := GenerateText(r); !strings.Contains(out, "Thermal:       42°C, P8, fan N/A, 9.87 W / limit N/A") {
		t.Errorf("text renderer should gate on the platform, not on EventCounters:\n%s", out)
	}
	if out := GenerateMarkdown(r); !strings.Contains(out, "**Thermal:** 42°C, P8, fan N/A, 9.87 W / limit N/A") {
		t.Errorf("markdown renderer should gate on the platform, not on EventCounters:\n%s", out)
	}
	// Per-GPU path on a two-GPU unified report (MemoryReporting drives it).
	r = fixtures.GB10()
	r.Platform.UnifiedMemory = false
	r.GPUThermal = []types.ThermalInfo{{TemperatureC: 42, PowerState: "P8", PowerDrawW: "9.87", GPUIndex: 0}, {TemperatureC: 43, PowerState: "P8", PowerDrawW: "9.90", GPUIndex: 1}}
	r.GPUs = append(r.GPUs, types.GPUInfo{Index: 1, Name: "NVIDIA GB10", IsNVIDIA: true, MemoryReporting: "not-supported"})
	if out := GenerateText(r); !strings.Contains(out, "Thermal:   42°C, P8, fan N/A, 9.87 W / limit N/A") {
		t.Errorf("per-GPU path should honour MemoryReporting not-supported:\n%s", out)
	}
}

func TestOrderedSteps(t *testing.T) {
	// "Last resort" without the Advisory prefix is a read-only step by
	// contract (^Advisory\b is the only marker).
	in := []string{"Advisory: b", "a", "Last resort: d", "c", "Advisory e"}
	got := orderedSteps(in)
	want := "a|Last resort: d|c|Advisory: b|Advisory e"
	if strings.Join(got, "|") != want {
		t.Errorf("orderedSteps = %v", got)
	}
	if strings.Join(in, "|") != "Advisory: b|a|Last resort: d|c|Advisory e" {
		t.Error("orderedSteps must not mutate its input")
	}
	if isAdvisory("Advisory: x") != true || isAdvisory("Advisories are fun") != false || isAdvisory("Last resort") != false {
		t.Error("isAdvisory word boundary")
	}
	if markdownStep("Advisory: (data loss) x") != "**Advisory:** (data loss) x" || markdownStep("plain") != "plain" {
		t.Error("markdownStep")
	}
}

// TestGenerateJSON_SparkFieldsSerialise: json.go needs no change beyond the
// struct tags; verify the new fields reach the document.
func TestGenerateJSON_SparkFieldsSerialise(t *testing.T) {
	r := analyzed(fixtures.GB10())
	out, err := GenerateJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	platform := doc["platform"].(map[string]interface{})
	if platform["class"] != "dgx-spark" || platform["unified_memory"] != true {
		t.Errorf("platform: %v", platform)
	}
	um := doc["unified_memory"].(map[string]interface{})
	if um["mem_total_kb"].(float64) != 125513944 {
		t.Errorf("unified_memory.mem_total_kb = %v", um["mem_total_kb"])
	}
	pcie := doc["pcie"].(map[string]interface{})
	if pcie["on_package"] != true {
		t.Errorf("pcie.on_package = %v", pcie["on_package"])
	}
	gpu := doc["gpus"].([]interface{})[0].(map[string]interface{})
	if gpu["on_package"] != true || gpu["memory_reporting"] != "not-supported" || gpu["compute_cap"] != "12.1" {
		t.Errorf("gpus[0] = %v", gpu)
	}
	if _, ok := doc["dgx_os"]; !ok {
		t.Error("dgx_os missing")
	}
	if _, ok := doc["cluster"]; !ok {
		t.Error("cluster missing")
	}
	for _, f := range doc["findings"].([]interface{}) {
		fm := f.(map[string]interface{})
		if fm["impact"] == nil || fm["impact"] == "" {
			t.Errorf("finding %v has no impact in JSON", fm["id"])
		}
	}
	// Legacy reports (no platform data) keep omitting the optional
	// top-level objects; platform itself is always present (Class "").
	legacy, _ := GenerateJSON(createTestReport())
	var ldoc map[string]interface{}
	if err := json.Unmarshal([]byte(legacy), &ldoc); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"unified_memory", "dgx_os", "cluster", "ecosystem"} {
		if _, ok := ldoc[absent]; ok {
			t.Errorf("legacy report should omit top-level %s", absent)
		}
	}
	if strings.Contains(legacy, `"impact"`) {
		t.Error("legacy report without findings should carry no impact key")
	}
	if ldoc["platform"].(map[string]interface{})["class"] != "" {
		t.Error("legacy platform.class should be empty")
	}
}
