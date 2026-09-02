package remediate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// mockResponse is a canned reply for any command whose rendered form starts
// with prefix (e.g. "reg query" or "powercfg /getactivescheme").
type mockResponse struct {
	prefix string
	output string
	err    error
}

// MockExecutor records commands without executing them. Replies are looked up
// in responses (an exact match wins over a prefix match, then the first prefix
// match wins) and fall back to output/err, so a single mock can script a
// whole apply/undo sequence. The exact-match rule matters because
// "reg query <key>" is a prefix of "reg query <key> /v <name>".
type MockExecutor struct {
	commands  []string
	output    string
	err       error
	responses []mockResponse
}

func (m *MockExecutor) Run(name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	m.commands = append(m.commands, cmd)
	for _, r := range m.responses {
		if cmd == r.prefix {
			return r.output, r.err
		}
	}
	for _, r := range m.responses {
		if strings.HasPrefix(cmd, r.prefix) {
			return r.output, r.err
		}
	}
	return m.output, m.err
}

// anyAction returns the first platform action, or skips on platforms without any.
func anyAction(t *testing.T) types.RemediationAction {
	t.Helper()
	actions := getAvailableActions()
	if len(actions) == 0 {
		t.Skip("no remediation actions on this platform")
	}
	return actions[0]
}

// writeCorruptJournal plants a journal that Journal.Read rejects and returns
// its raw content so tests can check it was left untouched.
func writeCorruptJournal(t *testing.T, dir string) string {
	t.Helper()
	raw := `[{"action_id":"disable-hags","applied_at":"not-a-time","success":true}]`
	if err := os.WriteFile(filepath.Join(dir, journalFilename), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

// setElevation replaces the elevation check for the duration of a test.
func setElevation(t *testing.T, elevated bool) {
	t.Helper()
	old := elevationCheck
	elevationCheck = func() bool { return elevated }
	t.Cleanup(func() { elevationCheck = old })
}

// firstAdminAction returns a platform action that needs elevation, or skips.
func firstAdminAction(t *testing.T) types.RemediationAction {
	t.Helper()
	for _, a := range getAvailableActions() {
		if a.NeedsAdmin {
			return a
		}
	}
	t.Skip("no admin action on this platform")
	return types.RemediationAction{}
}

// sampleUndoInfo returns a value that passes validateUndoInfo for the action.
func sampleUndoInfo(id string) string {
	switch id {
	case "set-high-performance":
		return "381b4222-f694-41f0-9685-ff5bb260df2e"
	case "disable-hags", "disable-game-mode":
		return "2"
	case "blacklist-nouveau":
		return absentSentinel
	default:
		return ""
	}
}

func TestNewEngine_DefaultExecutor(t *testing.T) {
	e := NewEngine(nil, t.TempDir(), false)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.executor == nil {
		t.Fatal("Engine.executor should default to DefaultExecutor")
	}
}

func TestNewEngine_CustomExecutor(t *testing.T) {
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), true)
	if e.executor != mock {
		t.Fatal("Engine should use the provided executor")
	}
	if !e.dryRun {
		t.Fatal("Engine.dryRun should be true")
	}
}

func TestPreview(t *testing.T) {
	e := NewEngine(&MockExecutor{}, t.TempDir(), false)
	action := types.RemediationAction{
		ID:          "test-action",
		Title:       "Test Action",
		Description: "Does a test thing",
		DryRunDesc:  "Would run: frobnicate",
		UndoDesc:    "Unfrobnicate",
		Risk:        types.RiskLow,
		NeedsAdmin:  true,
		NeedsReboot: true,
	}

	preview := e.Preview(action)
	for _, want := range []string{"Test Action", "elevated", "reboot", "Would run: frobnicate", "Unfrobnicate", "could not be determined"} {
		if !strings.Contains(preview, want) {
			t.Errorf("Preview should contain %q, got:\n%s", want, preview)
		}
	}
}

func TestPreview_DryRun(t *testing.T) {
	e := NewEngine(&MockExecutor{}, t.TempDir(), true)
	action := types.RemediationAction{
		ID:    "test",
		Title: "Test",
		Risk:  types.RiskLow,
	}

	preview := e.Preview(action)
	if !strings.Contains(preview, "DRY RUN") {
		t.Error("Preview in dry-run mode should mention DRY RUN")
	}
}

func TestApply_DryRun(t *testing.T) {
	setElevation(t, false) // dry-run must work unelevated
	mock := &MockExecutor{}
	dir := t.TempDir()
	e := NewEngine(mock, dir, true)
	action := types.RemediationAction{
		ID:         "test-action",
		Title:      "Test Action",
		Risk:       types.RiskLow,
		NeedsAdmin: true,
	}

	result, err := e.Apply(action)
	if err != nil {
		t.Fatalf("Apply dry-run should not error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatal("Dry-run apply should report success")
	}
	if !result.DryRun {
		t.Error("Result should indicate dry run")
	}
	if len(mock.commands) != 0 {
		t.Errorf("Dry-run should not execute commands, got %d", len(mock.commands))
	}
	if _, statErr := os.Stat(NewJournal(dir).Path()); !os.IsNotExist(statErr) {
		t.Error("dry-run must not write the journal")
	}
}

func TestApply_RefusesWhenNotElevated(t *testing.T) {
	setElevation(t, false)
	mock := &MockExecutor{output: "should not run"}
	dir := t.TempDir()
	e := NewEngine(mock, dir, false)
	action := types.RemediationAction{ID: "set-high-performance", Title: "x", NeedsAdmin: true}

	result, err := e.Apply(action)
	if !errors.Is(err, errNotElevated) {
		t.Fatalf("expected errNotElevated, got %v", err)
	}
	if result != nil {
		t.Errorf("result should be nil on refusal, got %+v", result)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands may run before the elevation check, got %v", mock.commands)
	}
	if _, statErr := os.Stat(NewJournal(dir).Path()); !os.IsNotExist(statErr) {
		t.Error("refusal must not be journaled")
	}
	if !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("error should tell the user nothing changed, got %q", err.Error())
	}
}

// A caller cannot bypass the elevation check by handing Apply a struct with
// NeedsAdmin=false; the platform definition is consulted too.
func TestApply_ElevationUsesRegistryDefinition(t *testing.T) {
	setElevation(t, false)
	def := firstAdminAction(t)
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), false)

	_, err := e.Apply(types.RemediationAction{ID: def.ID, Title: def.Title, NeedsAdmin: false})
	if !errors.Is(err, errNotElevated) {
		t.Fatalf("expected errNotElevated, got %v", err)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands may run, got %v", mock.commands)
	}
}

func TestUndo_RefusesWhenNotElevated(t *testing.T) {
	setElevation(t, false)
	def := firstAdminAction(t)
	mock := &MockExecutor{}
	dir := t.TempDir()
	e := NewEngine(mock, dir, false)

	entry := types.ChangeJournalEntry{
		ActionID:  def.ID,
		Title:     def.Title,
		AppliedAt: time.Now(),
		Success:   true,
		UndoInfo:  sampleUndoInfo(def.ID),
	}
	if err := NewJournal(dir).Append(entry); err != nil {
		t.Fatal(err)
	}

	err := e.Undo(entry)
	if !errors.Is(err, errNotElevated) {
		t.Fatalf("expected errNotElevated, got %v", err)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands may run, got %v", mock.commands)
	}
	entries, _ := NewJournal(dir).Read()
	if len(entries) != 1 || !entries[0].UndoneAt.IsZero() {
		t.Errorf("journal must be untouched after refusal, got %+v", entries)
	}
}

// A change that cannot be journaled cannot be undone, so an unreadable
// journal must stop Apply before any command runs.
func TestApply_RefusesWhenJournalUnreadable(t *testing.T) {
	setElevation(t, true)
	def := anyAction(t)
	dir := t.TempDir()
	raw := writeCorruptJournal(t, dir)
	mock := &MockExecutor{}
	e := NewEngine(mock, dir, false)

	result, err := e.Apply(def)
	if err == nil || !strings.Contains(err.Error(), "journal is unreadable") {
		t.Fatalf("expected journal refusal, got %v", err)
	}
	if result != nil {
		t.Errorf("result should be nil on refusal, got %+v", result)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands may run when the journal is unreadable, got %v", mock.commands)
	}
	got, _ := os.ReadFile(filepath.Join(dir, journalFilename))
	if string(got) != raw {
		t.Error("the corrupt journal must be left untouched for the user to inspect")
	}
}

func TestUndo_RefusesWhenJournalUnreadable(t *testing.T) {
	setElevation(t, true)
	def := anyAction(t)
	dir := t.TempDir()
	raw := writeCorruptJournal(t, dir)
	mock := &MockExecutor{}
	e := NewEngine(mock, dir, false)

	entry := types.ChangeJournalEntry{ActionID: def.ID, Title: def.Title, AppliedAt: time.Now(), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	err := e.Undo(entry)
	if err == nil || !strings.Contains(err.Error(), "journal is unreadable") {
		t.Fatalf("expected journal refusal, got %v", err)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands may run when the journal is unreadable, got %v", mock.commands)
	}
	got, _ := os.ReadFile(filepath.Join(dir, journalFilename))
	if string(got) != raw {
		t.Error("the corrupt journal must be left untouched")
	}
}

func TestUndo_RejectsInvalidUndoInfo(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), false)

	cases := []types.ChangeJournalEntry{
		{ActionID: "disable-hags", AppliedAt: time.Now(), Success: true, UndoInfo: `2 /f & calc.exe`},
		{ActionID: "set-high-performance", AppliedAt: time.Now(), Success: true, UndoInfo: "/h"},
		{ActionID: "blacklist-nouveau", AppliedAt: time.Now(), Success: true, UndoInfo: "install nouveau /bin/true\n"},
		{ActionID: "update-ldconfig", AppliedAt: time.Now(), Success: true, UndoInfo: "x"},
		{ActionID: "disable-hags", AppliedAt: time.Now(), Success: true, UndoInfo: ""},
		{ActionID: "", AppliedAt: time.Now(), Success: true, UndoInfo: "2"},
		{ActionID: "no-such-action", AppliedAt: time.Now(), Success: true, UndoInfo: "2"},
	}
	for _, c := range cases {
		if err := e.Undo(c); err == nil {
			t.Errorf("Undo(%q, %q) should fail validation", c.ActionID, c.UndoInfo)
		}
	}
	if len(mock.commands) != 0 {
		t.Errorf("invalid undo info must never reach the executor, got %v", mock.commands)
	}
}

func TestUndo_RejectsFailedEntry(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), false)
	entry := types.ChangeJournalEntry{ActionID: "disable-hags", AppliedAt: time.Now(), Success: false, UndoInfo: "2"}
	if err := e.Undo(entry); err == nil || !strings.Contains(err.Error(), "did not succeed") {
		t.Fatalf("expected 'did not succeed' error, got %v", err)
	}
	if len(mock.commands) != 0 {
		t.Errorf("no commands should run, got %v", mock.commands)
	}
}

func TestUndo_DryRun(t *testing.T) {
	setElevation(t, false)
	def := firstAdminAction(t)
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), true)
	entry := types.ChangeJournalEntry{ActionID: def.ID, AppliedAt: time.Now(), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	if err := e.Undo(entry); err != nil {
		t.Fatalf("dry-run undo should succeed unelevated, got %v", err)
	}
	if len(mock.commands) != 0 {
		t.Errorf("dry-run undo must not run commands, got %v", mock.commands)
	}
}

func TestListAvailable_ConsistentWithKnowledgeLabels(t *testing.T) {
	e := NewEngine(&MockExecutor{}, t.TempDir(), false)
	// Canonical risk labels (see knowledge/remediations.json).
	wantRisk := map[string]types.RiskLevel{
		"set-high-performance": types.RiskLow,
		"disable-hags":         types.RiskMedium,
		"disable-game-mode":    types.RiskLow,
		"blacklist-nouveau":    types.RiskMedium,
		"update-ldconfig":      types.RiskLow,
	}
	for _, a := range e.ListAvailable() {
		if want, ok := wantRisk[a.ID]; ok && a.Risk != want {
			t.Errorf("%s risk = %s, want %s", a.ID, a.Risk, want)
		}
		if a.DryRunDesc == "" || a.UndoDesc == "" {
			t.Errorf("%s must populate DryRunDesc and UndoDesc", a.ID)
		}
	}
}

func TestDescribeUndoInfo(t *testing.T) {
	if got := describeUndoInfo(absentSentinel); !strings.Contains(got, "did not exist") {
		t.Errorf("absent sentinel description = %q", got)
	}
	if got := describeUndoInfo(absentKeySentinel); !strings.Contains(got, "key and value did not exist") {
		t.Errorf("absent key sentinel description = %q", got)
	}
	if got := describeUndoInfo(""); !strings.Contains(got, "nothing") {
		t.Errorf("empty description = %q", got)
	}
	if got := describeUndoInfo("a\nb\n"); !strings.Contains(got, "file content") {
		t.Errorf("multi-line description = %q", got)
	}
	if got := describeUndoInfo("2"); got != "2" {
		t.Errorf("scalar description = %q", got)
	}
}

func TestCmdString(t *testing.T) {
	got := cmdString("reg", "add", `HKLM\Software\X Y`, "/f")
	want := `reg add "HKLM\Software\X Y" /f`
	if got != want {
		t.Errorf("cmdString = %q, want %q", got, want)
	}
}

// mockSafeAction returns a platform action whose undo runs only through the
// executor (no direct file writes), or skips when there is none.
func mockSafeAction(t *testing.T) types.RemediationAction {
	t.Helper()
	for _, a := range getAvailableActions() {
		if a.ID != "blacklist-nouveau" {
			return a
		}
	}
	t.Skip("no executor-only action on this platform")
	return types.RemediationAction{}
}

func TestUndo_NoMatchingJournalEntry_LeavesJournalAlone(t *testing.T) {
	setElevation(t, true)
	def := mockSafeAction(t)
	dir := t.TempDir()
	mock := &MockExecutor{}
	e := NewEngine(mock, dir, false)

	// An entry the (empty) journal never recorded.
	entry := types.ChangeJournalEntry{ActionID: def.ID, AppliedAt: time.Now(), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	err := e.Undo(entry)
	if err == nil {
		t.Fatal("Undo of an entry the journal does not contain must return an error")
	}
	if !strings.Contains(err.Error(), "no journal entry matched") {
		t.Errorf("error should explain the mismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "ran successfully") {
		t.Errorf("error should include the undo outcome, got %v", err)
	}
	if len(mock.commands) == 0 {
		t.Error("the undo itself should still have run through the executor")
	}
	if _, statErr := os.Stat(filepath.Join(dir, journalFilename)); !os.IsNotExist(statErr) {
		t.Errorf("journal must not be created/rewritten when nothing matched (stat err = %v)", statErr)
	}
}

func TestUndo_NoMatchingJournalEntry_ReportsUndoFailureToo(t *testing.T) {
	setElevation(t, true)
	def := mockSafeAction(t)
	dir := t.TempDir()
	// Plant an unrelated, valid entry so the journal exists and can be checked for changes.
	other := types.ChangeJournalEntry{ActionID: def.ID, Title: def.Title, AppliedAt: time.Now().Add(-time.Hour), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	if err := NewJournal(dir).Append(other); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, journalFilename))

	mock := &MockExecutor{err: errors.New("exit status 1"), output: "boom"}
	e := NewEngine(mock, dir, false)
	entry := types.ChangeJournalEntry{ActionID: def.ID, AppliedAt: time.Now(), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	err := e.Undo(entry)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no journal entry matched") || !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should carry both the undo failure and the mismatch, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, journalFilename))
	if string(before) != string(after) {
		t.Error("journal must be left byte-for-byte unchanged when nothing matched")
	}
}

func TestUndo_MatchingEntryIsMarkedUndone(t *testing.T) {
	setElevation(t, true)
	def := mockSafeAction(t)
	dir := t.TempDir()
	entry := types.ChangeJournalEntry{ActionID: def.ID, Title: def.Title, AppliedAt: time.Now().Add(-time.Minute), Success: true, UndoInfo: sampleUndoInfo(def.ID)}
	if err := NewJournal(dir).Append(entry); err != nil {
		t.Fatal(err)
	}
	// Read it back so AppliedAt has exactly the precision the journal stores.
	entries, err := NewJournal(dir).Read()
	if err != nil || len(entries) != 1 {
		t.Fatalf("journal read: %v (%d entries)", err, len(entries))
	}

	mock := &MockExecutor{}
	e := NewEngine(mock, dir, false)
	if err := e.Undo(entries[0]); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	entries, err = NewJournal(dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].UndoneAt.IsZero() || !entries[0].UndoSuccess || entries[0].UndoOutput != "successfully undone" {
		t.Errorf("journal entry should be marked undone, got %+v", entries[0])
	}
}

func TestResolveSystemCommand(t *testing.T) {
	root := `C:\WINDOWS`
	sys32 := func(base string) string { return filepath.Join(root, "System32", base+".exe") }
	exists := func(p string) bool { return p == sys32("reg") || p == sys32("powercfg") }
	cases := []struct {
		goos, name, want string
	}{
		{"windows", "reg", sys32("reg")},
		{"windows", "REG.exe", sys32("reg")},
		{"windows", "powercfg", sys32("powercfg")},
		// whoami is in the allow-list but the predicate says System32 lacks it: bare name.
		{"windows", "whoami", "whoami"},
		// Not a privileged system tool: untouched.
		{"windows", "nvidia-smi", "nvidia-smi"},
		// Already a path: untouched (never re-rooted under System32).
		{"windows", `D:\tools\reg.exe`, `D:\tools\reg.exe`},
		{"windows", "./reg", "./reg"},
		// Other OSes never resolve.
		{"linux", "reg", "reg"},
	}
	for _, c := range cases {
		if got := resolveSystemCommand(c.goos, root, c.name, exists); got != c.want {
			t.Errorf("resolveSystemCommand(%s, %q) = %q, want %q", c.goos, c.name, got, c.want)
		}
	}
	if got := resolveSystemCommand("windows", "", "reg", exists); got != "reg" {
		t.Errorf("without SystemRoot the bare name must be kept, got %q", got)
	}
}

func TestResolveCommandOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("System32 resolution only applies on Windows")
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		t.Skip("SystemRoot not set")
	}
	for _, name := range []string{"reg", "powercfg", "whoami", "net"} {
		got := resolveCommand(name)
		want := filepath.Join(root, "System32", name+".exe")
		if !strings.EqualFold(got, want) {
			t.Errorf("resolveCommand(%q) = %q, want %q", name, got, want)
		}
		if !fileExists(got) {
			t.Errorf("resolved %q does not exist", got)
		}
	}
	if got := resolveCommand("nvidia-smi"); got != "nvidia-smi" {
		t.Errorf("non-system command must stay bare, got %q", got)
	}
	// The resolved reg.exe must actually run: this is what "fix --dry-run"
	// previews depend on.
	out, err := (&DefaultExecutor{}).Run("reg", "query", `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "/v", "CurrentBuild")
	if err != nil || !strings.Contains(out, "CurrentBuild") {
		t.Errorf("System32 reg.exe query failed: %v: %s", err, out)
	}
}

func TestDefaultExecutorMissingBinaryIsNotTimeout(t *testing.T) {
	// A command that cannot start must surface the exec error, not the
	// timeout message (the timeout path itself is not exercised here because
	// it would take defaultExecTimeout to trigger).
	_, err := (&DefaultExecutor{}).Run("nvcheckup-definitely-missing-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("missing binary must not be reported as a timeout: %v", err)
	}
}
