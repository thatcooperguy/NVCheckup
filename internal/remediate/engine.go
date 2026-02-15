// Package remediate provides the remediation engine for safely fixing detected issues.
// It manages the lifecycle of remediation actions: previewing, applying, journaling,
// and undoing changes. All command execution goes through the Executor interface,
// enabling both real execution and mock-based testing.
package remediate

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// Executor abstracts command execution so remediation actions can be tested
// without actually running system commands. Production code uses DefaultExecutor;
// tests can supply a mock that records calls and returns canned output.
type Executor interface {
	// Run executes a command by name with the given arguments and returns
	// the combined stdout output or an error.
	Run(name string, args ...string) (string, error)
}

// DefaultExecutor runs real commands via os/exec.
type DefaultExecutor struct{}

// Run executes the named command with the supplied arguments. It captures
// combined stdout/stderr and returns the trimmed output or an error.
func (e *DefaultExecutor) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Engine manages the remediation lifecycle: listing available actions,
// previewing changes, applying fixes, recording a change journal, and
// undoing previously applied changes.
type Engine struct {
	executor   Executor
	journalDir string
	dryRun     bool
}

// NewEngine creates a new remediation Engine.
//
// Parameters:
//   - executor: the command executor to use (pass nil for DefaultExecutor)
//   - journalDir: directory where the change journal file is stored
//   - dryRun: when true, no commands are actually executed
func NewEngine(executor Executor, journalDir string, dryRun bool) *Engine {
	if executor == nil {
		executor = &DefaultExecutor{}
	}
	return &Engine{
		executor:   executor,
		journalDir: journalDir,
		dryRun:     dryRun,
	}
}

// Preview returns a human-readable description of what the action would do,
// including risk level, admin requirements, and reboot needs. This is intended
// to be shown to the user before they confirm an action.
func (e *Engine) Preview(action types.RemediationAction) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Action: %s\n", action.Title)
	fmt.Fprintf(&b, "  %s\n", action.Description)
	fmt.Fprintf(&b, "  Risk level:    %s\n", action.Risk)

	if action.NeedsAdmin {
		fmt.Fprintf(&b, "  Requires:      elevated/admin privileges\n")
	}
	if action.NeedsReboot {
		fmt.Fprintf(&b, "  Note:          a reboot is required after applying\n")
	}

	if e.dryRun {
		fmt.Fprintf(&b, "  Mode:          DRY RUN (no changes will be made)\n")
	}

	return b.String()
}

// Apply executes a remediation action and records the result in the change journal.
// In dry-run mode, no commands are executed but the action is still validated and
// a result is returned indicating what would have happened.
//
// The method:
//  1. Looks up the action by ID in the platform action registry
//  2. Calls the platform-specific implementation (or simulates in dry-run)
//  3. Writes a ChangeJournalEntry to disk for audit/undo purposes
//  4. Returns a RemediationResult with the outcome
func (e *Engine) Apply(action types.RemediationAction) (types.RemediationResult, error) {
	result := types.RemediationResult{
		ActionID:  action.ID,
		Timestamp: time.Now(),
		DryRun:    e.dryRun,
	}

	if e.dryRun {
		result.Success = true
		result.Output = fmt.Sprintf("[DRY RUN] Would apply: %s", action.Title)
		result.UndoInfo = ""
		return result, nil
	}

	// Execute the platform-specific action via the dispatcher
	output, undoInfo, err := e.applyAction(action.ID)
	if err != nil {
		result.Success = false
		result.Output = output
		result.Error = err.Error()
	} else {
		result.Success = true
		result.Output = output
		result.UndoInfo = undoInfo
	}

	// Record in the change journal regardless of success/failure
	journal := NewJournal(e.journalDir)
	entry := types.ChangeJournalEntry{
		ActionID:  action.ID,
		Title:     action.Title,
		AppliedAt: result.Timestamp,
		Success:   result.Success,
		Output:    result.Output,
		UndoInfo:  result.UndoInfo,
	}
	if journalErr := journal.Append(entry); journalErr != nil {
		// Journal write failure is not fatal to the remediation itself,
		// but we surface it so the caller knows the audit trail is incomplete.
		return result, fmt.Errorf("action applied but journal write failed: %w", journalErr)
	}

	return result, nil
}

// Undo reverses a previously applied remediation change using the undo
// information stored in the journal entry. It updates the journal entry
// with the undo result.
func (e *Engine) Undo(entry types.ChangeJournalEntry) error {
	if entry.UndoInfo == "" {
		return fmt.Errorf("no undo information available for action %q", entry.ActionID)
	}

	if !entry.Success {
		return fmt.Errorf("cannot undo action %q: original action did not succeed", entry.ActionID)
	}

	if e.dryRun {
		return nil
	}

	undoErr := e.undoAction(entry.ActionID, entry.UndoInfo)

	// Update the journal with undo status
	journal := NewJournal(e.journalDir)
	entries, readErr := journal.Read()
	if readErr != nil {
		return fmt.Errorf("undo executed but failed to update journal: %w", readErr)
	}

	// Find and update the matching entry by action ID and timestamp
	for i := range entries {
		if entries[i].ActionID == entry.ActionID && entries[i].AppliedAt.Equal(entry.AppliedAt) {
			entries[i].UndoneAt = time.Now()
			entries[i].UndoSuccess = (undoErr == nil)
			if undoErr != nil {
				entries[i].UndoOutput = undoErr.Error()
			} else {
				entries[i].UndoOutput = "successfully undone"
			}
			break
		}
	}

	if writeErr := journal.Write(entries); writeErr != nil {
		return fmt.Errorf("undo executed but failed to update journal: %w", writeErr)
	}

	return undoErr
}

// ListAvailable returns all remediation actions applicable to the current platform.
// This delegates to the platform-specific getAvailableActions() function, which is
// defined in the build-tagged action files (actions_windows.go, actions_linux.go, etc.).
func (e *Engine) ListAvailable() []types.RemediationAction {
	return getAvailableActions()
}
