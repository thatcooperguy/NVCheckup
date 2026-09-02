package analyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/remediate"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// knowledgeRule mirrors one entry of knowledge/rules.json.
type knowledgeRule struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	BaseConfidence int      `json:"base_confidence"`
	Modes          []string `json:"modes"`
	Platform       string   `json:"platform"`
	RemediationID  *string  `json:"remediation_id"`
	Description    string   `json:"description"`
}

func loadKnowledgeRules(t *testing.T) (version string, rules map[string]knowledgeRule) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "knowledge", "rules.json"))
	if err != nil {
		t.Fatalf("read rules.json: %v", err)
	}
	var doc struct {
		Version string          `json:"version"`
		Rules   []knowledgeRule `json:"rules"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse rules.json: %v", err)
	}
	rules = map[string]knowledgeRule{}
	for _, r := range doc.Rules {
		if _, dup := rules[r.ID]; dup {
			t.Errorf("rules.json lists %q twice", r.ID)
		}
		rules[r.ID] = r
	}
	return doc.Version, rules
}

// analyzerSourceIDs collects every Finding.ID literal assigned in analyzer.go.
// It is the ground truth for "which rules exist": a rule nobody's synthetic
// report triggers still shows up here, so the corpus below cannot silently
// hide a rule from the rules.json comparison.
func analyzerSourceIDs(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("analyzer.go")
	if err != nil {
		t.Fatalf("read analyzer.go: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s+ID:\s+"([a-z0-9-]+)",$`)
	seen := map[string]bool{}
	var ids []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	if len(ids) < 40 {
		t.Fatalf("only found %d finding ids in analyzer.go; the regexp is probably out of date", len(ids))
	}
	return ids
}

// ruleCorpus is a set of synthetic reports that, taken together, trigger
// every rule the analyzer defines, each tagged with the mode it is analyzed
// in. When a rule is added, add a report here that triggers it (the test
// tells you which one is missing).
func ruleCorpus() []struct {
	mode   types.RunMode
	report *types.Report
} {
	type entry = struct {
		mode   types.RunMode
		report *types.Report
	}
	var corpus []entry
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeAI, types.ModeCreator, types.ModeFull} {
		corpus = append(corpus, entry{mode, syntheticBusyReport()})
	}
	full := func(r *types.Report) entry { return entry{types.ModeFull, r} }
	nv := []types.GPUInfo{{Name: "RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true, VRAMTotalMB: 24576}}
	drv := types.DriverInfo{Version: "591.86", CUDAVersion: "12.4", NvidiaSmiPath: "nvidia-smi"}
	corpus = append(corpus,
		// no GPU, no driver, no nvidia-smi, nothing to encode with
		full(&types.Report{}),
		// Linux: nvidia loaded but nodes missing; Secure Boot fine; Wayland
		full(&types.Report{GPUs: nv, Driver: drv, Linux: &types.LinuxInfo{
			LoadedModules: map[string]bool{"nvidia": true}, LibCudaPath: "/usr/lib/libcuda.so",
			SecureBootState: "Enabled", SessionType: "wayland"}}),
		// Linux: module missing and driver missing
		full(&types.Report{GPUs: nv, Linux: &types.LinuxInfo{LoadedModules: map[string]bool{}, LibCudaPath: "/usr/lib/libcuda.so"}}),
		// PyTorch variants
		full(&types.Report{GPUs: nv, Driver: drv, AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1", CUDAVersion: "12.1", CUDAAvailable: true}}}),
		full(&types.Report{GPUs: nv, Driver: drv, AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1"}}}),
		full(&types.Report{GPUs: nv, Driver: drv, AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Error: "boom"}, TensorFlowInfo: &types.TFInfo{Error: "boom"}}}),
		full(&types.Report{GPUs: nv, Driver: types.DriverInfo{Version: "591.86", CUDAVersion: "12.8", NvidiaSmiPath: "nvidia-smi"},
			AI: &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.5.1", CUDAVersion: "12.1"}}}),
		// torch major ahead of the driver but working; toolkit minor newer
		full(&types.Report{GPUs: nv, Driver: drv, AI: &types.AIInfo{CUDAToolkitVersion: "12.6",
			PyTorchInfo:    &types.PyTorchInfo{Version: "2.9.0+cu130", CUDAVersion: "13.0", CUDAAvailable: true, DeviceName: "RTX 3090"},
			TensorFlowInfo: &types.TFInfo{Version: "2.16", GPUs: []string{"/GPU:0"}}}}),
		// WSL with dxg but nvidia-smi failing
		full(&types.Report{GPUs: nv, Driver: drv, WSL: &types.WSLInfo{IsWSL: true, DevDxgExists: true, NvidiaSmiOK: false}}),
		// thermal: real thermal bit; hot without bits; fan stopped
		full(&types.Report{GPUs: nv, Driver: drv, Thermal: &types.ThermalInfo{TemperatureC: 95, SlowdownReason: "0x40", FanSupported: true, FanSpeedPct: 0}}),
		full(&types.Report{GPUs: nv, Driver: drv, Thermal: &types.ThermalInfo{TemperatureC: 95}}),
		// PCIe: idle Gen1 (expected) and Gen1 under load (fault)
		full(&types.Report{GPUs: nv, Driver: drv, PCIe: &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", IdleLikely: true}}),
		full(&types.Report{GPUs: nv, Driver: drv, PCIe: &types.PCIeInfo{CurrentSpeed: "Gen1", MaxSpeed: "Gen4", CurrentWidth: "x16", MaxWidth: "x16", PowerState: "P0", UtilizationPct: 99}}),
		// network healthy
		full(&types.Report{GPUs: nv, Driver: drv, Network: &types.NetworkInfo{LatencyMs: 10}}),
		// GeForce Experience rather than NVIDIA App
		full(&types.Report{GPUs: nv, Driver: drv, Windows: &types.WindowsInfo{GFEVersion: "3.28"}}),
	)
	return corpus
}

// corpusFindings runs the corpus and returns every finding with the mode it
// was produced in.
func corpusFindings() (byID map[string][]types.Finding, modesByID map[string]map[types.RunMode]bool) {
	byID = map[string][]types.Finding{}
	modesByID = map[string]map[types.RunMode]bool{}
	for _, c := range ruleCorpus() {
		Analyze(c.report, c.mode)
		for _, f := range c.report.Findings {
			byID[f.ID] = append(byID[f.ID], f)
			if modesByID[f.ID] == nil {
				modesByID[f.ID] = map[types.RunMode]bool{}
			}
			modesByID[f.ID][c.mode] = true
		}
	}
	return byID, modesByID
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestRulesJSON_MatchesAnalyzer is the drift guard between the analyzer and
// knowledge/rules.json: same id set (both directions), same category, same
// remediation id, and the modes listed in the JSON cover every mode the
// analyzer actually emits the rule in.
func TestRulesJSON_MatchesAnalyzer(t *testing.T) {
	version, rules := loadKnowledgeRules(t)
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		t.Errorf("rules.json version %q is not semver", version)
	}

	sourceIDs := analyzerSourceIDs(t)
	byID, modesByID := corpusFindings()

	// Every id in the source must be produced by the corpus (so the checks
	// below actually see a finding for it) and exist in rules.json.
	for _, id := range sourceIDs {
		if _, ok := byID[id]; !ok {
			t.Errorf("analyzer defines %q but no report in ruleCorpus() triggers it; add one", id)
		}
		if _, ok := rules[id]; !ok {
			t.Errorf("analyzer emits %q but knowledge/rules.json has no such rule; add it", id)
		}
	}
	// Nothing may be produced that the source scan did not see (regexp drift).
	src := map[string]bool{}
	for _, id := range sourceIDs {
		src[id] = true
	}
	for _, id := range sortedKeys(byID) {
		if !src[id] {
			t.Errorf("finding id %q is emitted but analyzerSourceIDs did not find it in analyzer.go", id)
		}
	}
	// And every rules.json entry must still exist in the analyzer.
	for _, id := range sortedKeys(rules) {
		if !src[id] {
			t.Errorf("knowledge/rules.json rule %q is not produced by the analyzer any more; remove it", id)
		}
	}

	for _, id := range sortedKeys(byID) {
		rule, ok := rules[id]
		if !ok {
			continue
		}
		findings := byID[id]
		f := findings[0]
		if f.Category != rule.Category {
			t.Errorf("%s: analyzer category %q, rules.json category %q", id, f.Category, rule.Category)
		}
		if rule.RemediationID != nil && *rule.RemediationID != "" {
			if f.Remediation == nil || f.Remediation.ID != *rule.RemediationID {
				t.Errorf("%s: rules.json says remediation %q, finding carries %+v", id, *rule.RemediationID, f.Remediation)
			}
		} else if f.Remediation != nil {
			t.Errorf("%s: finding carries remediation %q but rules.json lists none", id, f.Remediation.ID)
		}
		listed := map[string]bool{}
		for _, m := range rule.Modes {
			listed[m] = true
		}
		for mode := range modesByID[id] {
			if !listed[string(mode)] {
				t.Errorf("%s: emitted in mode %s but rules.json modes are %v", id, mode, rule.Modes)
			}
		}
		if rule.BaseConfidence <= 0 || rule.BaseConfidence > 100 {
			t.Errorf("%s: base_confidence %d out of range", id, rule.BaseConfidence)
		}
		switch rule.Severity {
		case "CRIT", "WARN", "INFO":
		default:
			t.Errorf("%s: unknown severity %q", id, rule.Severity)
		}
	}
}

// TestFindingRemediation_EqualsCatalogEntry: the four findings that offer a
// fix must carry the remediate catalog entry verbatim (G1), so the risk label
// and descriptions in the report are the ones the engine will show and act on.
func TestFindingRemediation_EqualsCatalogEntry(t *testing.T) {
	win := analyzeWindowsPerfSettings(&types.Report{Windows: &types.WindowsInfo{PowerPlan: "Balanced", HAGSEnabled: "Enabled"}})
	lin := analyzeLinuxModules(&types.Report{Linux: &types.LinuxInfo{LoadedModules: map[string]bool{"nouveau": true}}})
	cases := []struct {
		findings  []types.Finding
		findingID string
		actionID  string
	}{
		{win, "power-plan-suboptimal", "set-high-performance"},
		{win, "hags-enabled", "disable-hags"},
		{lin, "nouveau-active", "blacklist-nouveau"},
		{lin, "libcuda-not-found", "update-ldconfig"},
	}
	for _, c := range cases {
		f := findByID(c.findings, c.findingID)
		if f == nil {
			t.Errorf("expected finding %s, got %v", c.findingID, ids(c.findings))
			continue
		}
		want, ok := remediate.ActionByID(c.actionID)
		if !ok {
			t.Fatalf("catalog has no action %q", c.actionID)
		}
		if f.Remediation == nil {
			t.Errorf("%s: no remediation attached", c.findingID)
			continue
		}
		if *f.Remediation != want {
			t.Errorf("%s: attached remediation differs from catalog entry:\n  finding: %+v\n  catalog: %+v", c.findingID, *f.Remediation, want)
		}
		if f.Remediation.Risk == "" || f.Remediation.DryRunDesc == "" || f.Remediation.UndoDesc == "" {
			t.Errorf("%s: catalog entry is missing risk/dry-run/undo text: %+v", c.findingID, *f.Remediation)
		}
	}
	if remediationFor("no-such-action") != nil {
		t.Error("remediationFor must return nil for unknown ids")
	}
}

// TestRemediationsJSON_FindingIDsMatchAnalyzer: knowledge/remediations.json
// lists, per action, the finding ids that attach it; those must be the
// findings that actually carry the action.
func TestRemediationsJSON_FindingIDsMatchAnalyzer(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "knowledge", "remediations.json"))
	if err != nil {
		t.Fatalf("read remediations.json: %v", err)
	}
	var doc struct {
		Actions []struct {
			ID         string   `json:"id"`
			FindingIDs []string `json:"finding_ids"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse remediations.json: %v", err)
	}
	byID, _ := corpusFindings()
	attached := map[string][]string{} // action id -> finding ids that carry it
	for _, id := range sortedKeys(byID) {
		if r := byID[id][0].Remediation; r != nil {
			attached[r.ID] = append(attached[r.ID], id)
		}
	}
	for _, a := range doc.Actions {
		if _, ok := remediate.ActionByID(a.ID); !ok {
			t.Errorf("remediations.json action %q is not in the remediate catalog", a.ID)
		}
		want := append([]string(nil), a.FindingIDs...)
		sort.Strings(want)
		got := attached[a.ID]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: remediations.json finding_ids %v, analyzer attaches it to %v", a.ID, want, got)
		}
	}
}

// ── G2: Linux module and Secure Boot rules run in every mode ──────────

func TestLinuxModulesAndSecureBoot_RunInEveryMode(t *testing.T) {
	for _, mode := range []types.RunMode{types.ModeGaming, types.ModeStreaming, types.ModeAI, types.ModeCreator, types.ModeFull} {
		r := &types.Report{
			GPUs:   []types.GPUInfo{{Name: "RTX 3090", IsNVIDIA: true}},
			Driver: types.DriverInfo{Version: "591.86", NvidiaSmiPath: "nvidia-smi"},
			Linux:  &types.LinuxInfo{LoadedModules: map[string]bool{"nouveau": true}, SecureBootState: "Enabled"},
		}
		Analyze(r, mode)
		for _, id := range []string{"nouveau-active", "libcuda-not-found", "secureboot-blocking"} {
			if findByID(r.Findings, id) == nil {
				t.Errorf("mode %s: Linux info is collected in every mode, expected %s, got %v", mode, id, ids(r.Findings))
			}
		}
	}
}

// ── G3: ThermalThrottle fallback needs a thermal reason ──────────────

func TestThermalState_FallbackNeedsThermalReason(t *testing.T) {
	tests := []struct {
		name        string
		thermal     types.ThermalInfo
		wantThermal bool
	}{
		{"flag alone without mask is not trusted", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true}, false},
		{"flag with unparseable mask is not trusted", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true, SlowdownReason: "garbage"}, false},
		{"flag with a non-thermal reason is not trusted", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true, ThrottleReasons: []string{"sw_power_cap"}}, false},
		{"flag with sw_thermal_slowdown reason is trusted", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true, ThrottleReasons: []string{"sw_thermal_slowdown"}}, true},
		{"flag with hw_thermal_slowdown reason is trusted", types.ThermalInfo{TemperatureC: 70, ThermalThrottle: true, ThrottleReasons: []string{"sw_power_cap", "HW_Thermal_Slowdown"}}, true},
		{"thermal reason without the flag is not thermal", types.ThermalInfo{TemperatureC: 86, ThrottleReasons: []string{"sw_thermal_slowdown"}}, false},
		{"mask wins over flag and reasons: no thermal bits", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true, ThrottleReasons: []string{"sw_thermal_slowdown"}, SlowdownReason: "0x4"}, false},
		{"mask wins over flag: thermal bit set", types.ThermalInfo{TemperatureC: 86, ThermalThrottle: false, SlowdownReason: "0x20"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := tt.thermal
			thermal, _, _ := thermalState(&th)
			if thermal != tt.wantThermal {
				t.Errorf("thermal = %v, want %v", thermal, tt.wantThermal)
			}
		})
	}

	// End to end: the temperature thresholds decide when the flag is not trusted.
	hot := analyzeThermal(&types.Report{Thermal: &types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true}})
	if findByID(hot, "thermal-throttling") != nil || findByID(hot, "gpu-running-hot") == nil {
		t.Errorf("86C with an untrusted flag should be gpu-running-hot only, got %v", ids(hot))
	}
	cool := analyzeThermal(&types.Report{Thermal: &types.ThermalInfo{TemperatureC: 60, ThermalThrottle: true}})
	if len(cool) != 0 {
		t.Errorf("60C with an untrusted flag should produce nothing, got %v", ids(cool))
	}
	real := analyzeThermal(&types.Report{Thermal: &types.ThermalInfo{TemperatureC: 86, ThermalThrottle: true, ThrottleReasons: []string{"hw_thermal_slowdown"}}})
	if f := findByID(real, "thermal-throttling"); f == nil || f.Severity != types.SeverityCrit {
		t.Errorf("a thermal reason should make the flag CRIT thermal-throttling, got %v", ids(real))
	} else if !strings.Contains(f.Evidence, "hw_thermal_slowdown") {
		t.Errorf("evidence should carry the reason, got %q", f.Evidence)
	}
}

// ── G5: PyTorch wheel tags snap to published indexes ─────────────────

func TestTorchWheelTag(t *testing.T) {
	cases := map[string]string{
		"11.8":   "cu118",
		"12.0":   "cu118", // 12.0 has no tag of its own
		"12.1":   "cu121",
		"12.2":   "cu121",
		"12.4":   "cu124",
		"12.5":   "cu124",
		"12.6":   "cu126",
		"12.7":   "cu126",
		"12.8":   "cu128",
		"12.9":   "cu128",
		"13.0":   "cu130",
		"13.1":   "cu130", // the driver on the dev box: never "cu131"
		"14.0":   "cu130", // newest known tag, not a made-up one
		"12.4.1": "cu124",
		"11.7":   "", // older than every published tag
		"":       "",
		"cu124":  "",
	}
	for in, want := range cases {
		if got := torchWheelTag(in); got != want {
			t.Errorf("torchWheelTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyzeCUDA_TorchNewerHintSnapsToPublishedTag(t *testing.T) {
	report := &types.Report{
		Driver: types.DriverInfo{CUDAVersion: "13.1"},
		AI:     &types.AIInfo{PyTorchInfo: &types.PyTorchInfo{Version: "2.11.0+cu132", CUDAVersion: "13.2"}},
	}
	f := findByID(analyzeCUDA(report), "pytorch-cuda-newer-than-driver")
	if f == nil {
		t.Fatal("expected pytorch-cuda-newer-than-driver")
	}
	joined := strings.Join(f.NextSteps, "\n")
	if !strings.Contains(joined, "whl/cu130 ") {
		t.Errorf("hint should snap 13.1 to the published cu130 index, got %q", joined)
	}
	if strings.Contains(joined, "cu131") {
		t.Errorf("hint must not invent an unpublished cu131 index, got %q", joined)
	}
	if !strings.Contains(joined, "or the nearest tag listed at https://pytorch.org/get-started/locally/") {
		t.Errorf("hint should point at the live tag list, got %q", joined)
	}

	// A driver older than every published tag falls back to the generic hint.
	report.Driver.CUDAVersion = "11.6"
	report.AI.PyTorchInfo.CUDAVersion = "11.8"
	f = findByID(analyzeCUDA(report), "pytorch-cuda-newer-than-driver")
	if f == nil {
		t.Fatal("expected pytorch-cuda-newer-than-driver")
	}
	joined = strings.Join(f.NextSteps, "\n")
	if strings.Contains(joined, "download.pytorch.org/whl/") || !strings.Contains(joined, "pytorch.org/get-started/locally/") {
		t.Errorf("without a published tag the hint should only link the selector, got %q", joined)
	}
}
