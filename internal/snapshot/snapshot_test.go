package snapshot

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const fixtureA = `{
  "metadata": {"tool_version": "0.2.1", "timestamp": "2026-08-01T10:00:00Z", "mode": "full", "platform": "windows", "schema_version": "1"},
  "system": {"os_name": "Windows 11", "os_version": "24H2", "os_build": "26100", "architecture": "amd64", "cpu_model": "Ryzen", "ram_total_mb": 65536},
  "gpus": [
    {"index": 0, "name": "NVIDIA GeForce RTX 3090", "vendor": "NVIDIA", "driver_version": "580.88", "vram_total_mb": 24576, "is_nvidia": true},
    {"index": 1, "name": "NVIDIA GeForce RTX 3090", "vendor": "NVIDIA", "driver_version": "580.88", "vram_total_mb": 24576, "is_nvidia": true}
  ],
  "driver": {"version": "580.88", "cuda_version": "13.0"},
  "ai": {"cuda_toolkit_version": "12.4", "cudnn_version": "9.1", "conda_present": true,
         "pytorch_info": {"version": "2.4.0", "cuda_version": "12.4", "cuda_available": true}}
}`

const fixtureB = `{
  "metadata": {"tool_version": "0.2.1", "timestamp": "2026-09-01T10:00:00Z", "mode": "full", "platform": "windows", "schema_version": "1"},
  "system": {"os_name": "Windows 11", "os_version": "24H2", "os_build": "26200", "architecture": "amd64", "cpu_model": "Ryzen", "ram_total_mb": 65536},
  "gpus": [
    {"index": 0, "name": "NVIDIA GeForce RTX 3090", "vendor": "NVIDIA", "driver_version": "591.86", "vram_total_mb": 24576, "is_nvidia": true},
    {"index": 1, "name": "NVIDIA GeForce RTX 3090", "vendor": "NVIDIA", "driver_version": "591.86", "vram_total_mb": 24576, "is_nvidia": true}
  ],
  "driver": {"version": "591.86", "cuda_version": "13.1"},
  "ai": {"cuda_toolkit_version": "12.4", "cudnn_version": "9.1", "conda_present": true,
         "pytorch_info": {"version": "2.4.0", "cuda_version": "12.4", "cuda_available": false}}
}`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fieldSet(diffs []types.Difference) map[string]types.Difference {
	m := map[string]types.Difference{}
	for _, d := range diffs {
		m[d.Field] = d
	}
	return m
}

func TestDiff_DriverChangeReportedOnce(t *testing.T) {
	var a, b types.Snapshot
	if err := json.Unmarshal([]byte(fixtureA), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(fixtureB), &b); err != nil {
		t.Fatal(err)
	}

	diffs := Diff(&a, &b)
	got := fieldSet(diffs)

	want := []string{"os_build", "driver_version", "cuda_driver_version", "pytorch.cuda_available"}
	for _, f := range want {
		if _, ok := got[f]; !ok {
			t.Errorf("missing expected difference %q; got %v", f, keys(got))
		}
	}
	if len(diffs) != len(want) {
		t.Errorf("expected %d differences, got %d: %v", len(want), len(diffs), keys(got))
	}

	// The driver upgrade must appear once, not once per GPU.
	for f := range got {
		if strings.HasPrefix(f, "gpu[") && strings.HasSuffix(f, "driver_version") {
			t.Errorf("per-GPU driver diff %q should be suppressed when driver_version changed", f)
		}
	}
	if d := got["driver_version"]; d.ValueA != "580.88" || d.ValueB != "591.86" || d.Severity != "WARN" {
		t.Errorf("driver_version diff = %+v", d)
	}
	if d := got["pytorch.cuda_available"]; d.Severity != "CRIT" {
		t.Errorf("pytorch.cuda_available severity = %q, want CRIT", d.Severity)
	}
}

func TestDiff_PerGPUDriverWhenTopLevelUnchanged(t *testing.T) {
	a := &types.Snapshot{Driver: types.DriverInfo{Version: "591.86"}, GPUs: []types.GPUInfo{{DriverVersion: "591.86"}}}
	b := &types.Snapshot{Driver: types.DriverInfo{Version: "591.86"}, GPUs: []types.GPUInfo{{DriverVersion: "580.88"}}}
	got := fieldSet(Diff(a, b))
	if _, ok := got["gpu[0].driver_version"]; !ok {
		t.Errorf("expected gpu[0].driver_version diff when top-level driver is unchanged; got %v", keys(got))
	}
	if _, ok := got["driver_version"]; ok {
		t.Error("driver_version should not differ")
	}
}

func TestDiff_Identical(t *testing.T) {
	var a types.Snapshot
	if err := json.Unmarshal([]byte(fixtureA), &a); err != nil {
		t.Fatal(err)
	}
	b := a
	if diffs := Diff(&a, &b); len(diffs) != 0 {
		t.Errorf("identical snapshots produced %d differences", len(diffs))
	}
}

func TestCompare_WritesMarkdown(t *testing.T) {
	dir := t.TempDir()
	pa := writeFixture(t, dir, "a.json", fixtureA)
	pb := writeFixture(t, dir, "b.json", fixtureB)
	outDir := filepath.Join(dir, "out")

	if err := Compare(pa, pb, outDir, true); err != nil {
		t.Fatalf("Compare: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "comparison.md"))
	if err != nil {
		t.Fatalf("comparison.md not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "| `driver_version` | 580.88 | 591.86 | WARN |") {
		t.Errorf("markdown missing driver row:\n%s", s)
	}
	if !strings.Contains(s, "# NVCheckup Snapshot Comparison") {
		t.Error("markdown missing title")
	}
}

func TestCompare_BadFile(t *testing.T) {
	dir := t.TempDir()
	pa := writeFixture(t, dir, "a.json", fixtureA)
	if err := Compare(pa, filepath.Join(dir, "missing.json"), "", false); err == nil {
		t.Error("expected error for missing snapshot B")
	}
	bad := writeFixture(t, dir, "bad.json", "{not json")
	if err := Compare(bad, pa, "", false); err == nil {
		t.Error("expected error for malformed snapshot A")
	}
}

// TestCreate_RedactedSnapshotHasNoIdentity runs the real collectors (about
// half a minute), so it is opt-in: set NVCHECKUP_LIVE_TESTS=1 to run it. It is
// the test that guarantees the default snapshot is safe to share, so CI on a
// GPU runner should enable it. The fast, synthetic coverage of the same
// redaction lives in internal/redact (TestApplyToSnapshot).
func TestCreate_RedactedSnapshotHasNoIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real collectors")
	}
	if os.Getenv("NVCHECKUP_LIVE_TESTS") != "1" {
		t.Skip("set NVCHECKUP_LIVE_TESTS=1 to run the live collectors")
	}
	hostname, _ := os.Hostname()
	username, homeDir := "", ""
	if u, err := user.Current(); err == nil {
		username = u.Username
		if i := strings.LastIndex(username, `\`); i >= 0 {
			username = username[i+1:]
		}
		homeDir = strings.TrimRight(u.HomeDir, `\/`)
	}

	dir := t.TempDir()
	path, err := Create(dir, 20)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var snap types.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("snapshot is not valid JSON: %v", err)
	}
	if !snap.Metadata.RedactionEnabled {
		t.Error("metadata.redaction_enabled should be true by default")
	}
	if snap.Metadata.SchemaVersion != types.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", snap.Metadata.SchemaVersion, types.SchemaVersion)
	}
	if snap.Metadata.Platform == "" || snap.Metadata.ToolVersion != types.Version {
		t.Errorf("metadata incomplete: %+v", snap.Metadata)
	}

	// The hostname is always collected, so the redacted token must be there.
	if hostname != "" && snap.System.Hostname != "<host>" {
		t.Errorf("System.Hostname = %q, want %q", snap.System.Hostname, "<host>")
	}

	// Python interpreter paths are the field most likely to embed the profile
	// directory. Anything that lived under the home directory must now start
	// with <home>, and no path may still carry the username.
	if snap.AI != nil {
		for _, py := range snap.AI.PythonVersions {
			p := py.Path
			if username != "" && strings.Contains(strings.ToLower(p), strings.ToLower(username)) {
				t.Errorf("PythonVersions path %q still contains username %q", p, username)
			}
			if homeDir != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(homeDir)) {
				t.Errorf("PythonVersions path %q still starts with the home directory", p)
			}
			if strings.Contains(p, "<home>") && !strings.HasPrefix(p, "<home>") {
				t.Errorf("PythonVersions path %q has <home> in the middle; expected it as the prefix", p)
			}
		}
		if snap.AI.NvccPath != "" && homeDir != "" && strings.HasPrefix(strings.ToLower(snap.AI.NvccPath), strings.ToLower(homeDir)) {
			t.Errorf("NvccPath %q still starts with the home directory", snap.AI.NvccPath)
		}
	}
	if snap.Driver.NvidiaSmiPath != "" && homeDir != "" && strings.HasPrefix(strings.ToLower(snap.Driver.NvidiaSmiPath), strings.ToLower(homeDir)) {
		t.Errorf("NvidiaSmiPath %q still starts with the home directory", snap.Driver.NvidiaSmiPath)
	}
}

func keys(m map[string]types.Difference) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
