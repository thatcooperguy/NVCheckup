package remediate

import (
	"testing"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// MockExecutor records commands without executing them.
type MockExecutor struct {
	commands []string
	output   string
	err      error
}

func (m *MockExecutor) Run(name string, args ...string) (string, error) {
	cmd := name
	for _, a := range args {
		cmd += " " + a
	}
	m.commands = append(m.commands, cmd)
	return m.output, m.err
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
		Risk:        types.RiskLow,
		NeedsAdmin:  true,
		NeedsReboot: true,
	}

	preview := e.Preview(action)
	if preview == "" {
		t.Fatal("Preview should return non-empty string")
	}
	if !contains(preview, "Test Action") {
		t.Error("Preview should contain action title")
	}
	if !contains(preview, "elevated") {
		t.Error("Preview should mention admin requirement")
	}
	if !contains(preview, "reboot") {
		t.Error("Preview should mention reboot requirement")
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
	if !contains(preview, "DRY RUN") {
		t.Error("Preview in dry-run mode should mention DRY RUN")
	}
}

func TestApply_DryRun(t *testing.T) {
	mock := &MockExecutor{}
	e := NewEngine(mock, t.TempDir(), true)
	action := types.RemediationAction{
		ID:    "test-action",
		Title: "Test Action",
		Risk:  types.RiskLow,
	}

	result, err := e.Apply(action)
	if err != nil {
		t.Fatalf("Apply dry-run should not error: %v", err)
	}
	if !result.Success {
		t.Error("Dry-run apply should report success")
	}
	if !result.DryRun {
		t.Error("Result should indicate dry run")
	}
	if len(mock.commands) != 0 {
		t.Errorf("Dry-run should not execute commands, got %d", len(mock.commands))
	}
}

func TestListAvailable(t *testing.T) {
	e := NewEngine(&MockExecutor{}, t.TempDir(), false)
	actions := e.ListAvailable()
	// Should return at least some actions on the current platform
	// (on Windows during tests, should have set-high-performance, disable-hags, etc.)
	// This is a basic smoke test
	if actions == nil {
		// On unsupported platforms, nil is expected
		t.Log("No actions available (may be unsupported platform)")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		len(s) >= len(substr) &&
		(s == substr || containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
