// Package remediate provides the remediation engine for safely fixing detected issues.
// It manages the lifecycle of remediation actions: previewing, applying, journaling,
// and undoing changes. All command execution goes through the Executor interface,
// enabling both real execution and mock-based testing.
//
// Safety model:
//   - "nvcheckup run" never calls into this package; only "fix" and "undo" do.
//   - Actions that need elevation are refused before anything runs or is journaled
//     when the process is not elevated, so a failed attempt never leaves a journal
//     entry behind and never half-applies.
//   - Every apply records exactly what was observed beforehand (or the sentinel
//     absentSentinel when a value did not exist) so undo restores the real prior
//     state instead of a guessed default.
//   - Undo validates journal-supplied undo data before using it, because the
//     journal is a plain file the user (or another program) can edit.
//   - Apply and Undo check that the journal is readable BEFORE changing the
//     system, so a corrupted or hand-edited journal can never lead to a change
//     that is applied but not recorded (and therefore not undoable).
package remediate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
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

// defaultExecTimeout bounds every command run by DefaultExecutor. It is
// generous because initramfs rebuilds (update-initramfs, dracut) legitimately
// take a minute or more on slow disks; registry and powercfg calls finish in
// milliseconds.
const defaultExecTimeout = 120 * time.Second

// execWaitDelay bounds how long Run blocks after the timeout while a child
// still holds the output pipe.
const execWaitDelay = 5 * time.Second

// windowsSystemCommands are the native tools remediation actions invoke by
// bare name on Windows. They are privileged (registry and power-plan writes),
// so they are resolved to %SystemRoot%\System32 explicitly rather than
// trusting PATH, where Git Bash/MSYS shims or a user-writable directory could
// shadow them.
var windowsSystemCommands = map[string]bool{
	"reg": true, "powercfg": true, "whoami": true, "net": true,
}

// resolveSystemCommand maps a bare command name to its %SystemRoot%\System32
// binary on Windows. It is a pure function of goos, systemRoot, and an
// existence predicate so it can be unit-tested anywhere. Names that carry a
// path, names outside windowsSystemCommands, or names whose System32 binary
// does not exist are returned unchanged and resolve via PATH as before.
func resolveSystemCommand(goos, systemRoot, name string, exists func(string) bool) string {
	if goos != "windows" || systemRoot == "" {
		return name
	}
	if strings.ContainsAny(name, `/\`) {
		return name
	}
	base := strings.TrimSuffix(strings.ToLower(name), ".exe")
	if !windowsSystemCommands[base] {
		return name
	}
	candidate := systemBinary(systemRoot, base+".exe")
	if exists(candidate) {
		return candidate
	}
	return name
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// resolveCommand applies resolveSystemCommand for the running OS.
func resolveCommand(name string) string {
	return resolveSystemCommand(runtime.GOOS, os.Getenv("SystemRoot"), name, fileExists)
}

// Run executes the named command with the supplied arguments. It captures
// combined stdout/stderr and returns the trimmed output or an error. On
// Windows the privileged system tools are resolved under System32 (see
// resolveSystemCommand); the action files keep passing bare names so the
// previews and journal stay readable. Every command is bounded by
// defaultExecTimeout so a hung tool cannot wedge "fix" indefinitely.
func (e *DefaultExecutor) Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolveCommand(name), args...)
	cmd.WaitDelay = execWaitDelay
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("%s timed out after %s; the system may be partially changed, re-run the preview to inspect the current state",
			cmdString(name, args...), defaultExecTimeout)
	}
	return output, err
}

// elevationCheck reports whether the current process has administrative rights.
// It is a package-level variable so tests can inject a deterministic answer.
var elevationCheck = IsElevated

// errNotElevated is returned by Apply and Undo when an action needs admin rights
// and the process does not have them. It is checked before any command runs and
// before anything is journaled.
var errNotElevated = fmt.Errorf("this action requires elevated privileges (Administrator/root); nothing was changed")

// inspection is the read-only view of an action produced before it runs. It is
// used by Preview so the user sees the exact commands, the value that would be
// captured for undo, and the undo commands that value implies.
type inspection struct {
	// Current is a human-readable description of the present state.
	Current string
	// UndoInfo is the value that Apply would record for undo.
	UndoInfo string
	// ApplyCommands lists the exact commands Apply would run, in order.
	ApplyCommands []string
	// UndoCommands lists the exact commands Undo would run given UndoInfo.
	UndoCommands []string
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
//   - dryRun: when true, no commands that change state are executed
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

// lookupAction returns the platform definition of an action by ID. The
// definition on record (not the caller-supplied struct) is what decides
// whether elevation is required, so a caller cannot bypass the check by
// passing NeedsAdmin=false.
func lookupAction(id string) (types.RemediationAction, bool) {
	for _, a := range getAvailableActions() {
		if a.ID == id {
			return a, true
		}
	}
	return types.RemediationAction{}, false
}

// needsElevation reports whether an action requires admin rights, consulting
// both the caller-supplied action and the platform registry.
func needsElevation(action types.RemediationAction) bool {
	if action.NeedsAdmin {
		return true
	}
	if def, ok := lookupAction(action.ID); ok {
		return def.NeedsAdmin
	}
	return false
}

// Preview returns a human-readable description of what the action would do,
// including risk level, admin requirements, reboot needs, the exact commands
// that will run, the current value captured for undo, and the planned undo.
// Only read-only capture commands are executed, so Preview is safe in dry-run
// mode and without elevation.
func (e *Engine) Preview(action types.RemediationAction) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Action: %s\n", action.Title)
	if action.Description != "" {
		fmt.Fprintf(&b, "  %s\n", action.Description)
	}
	fmt.Fprintf(&b, "  Risk level:    %s\n", action.Risk)

	if needsElevation(action) {
		fmt.Fprintf(&b, "  Requires:      elevated/admin privileges\n")
	}
	if action.NeedsReboot {
		fmt.Fprintf(&b, "  Note:          a reboot is required after applying\n")
	}
	if e.dryRun {
		fmt.Fprintf(&b, "  Mode:          DRY RUN (no changes will be made)\n")
	}

	insp, err := e.inspectAction(action.ID)
	if err != nil {
		// Fall back to the static descriptions when the live inspection is not
		// possible (unknown action on this platform, tool missing, etc.).
		if action.DryRunDesc != "" {
			fmt.Fprintf(&b, "  Will run:      %s\n", action.DryRunDesc)
		}
		fmt.Fprintf(&b, "  Current state: could not be determined (%v)\n", err)
		if action.UndoDesc != "" {
			fmt.Fprintf(&b, "  Undo:          %s\n", action.UndoDesc)
		}
		return b.String()
	}

	fmt.Fprintf(&b, "  Will run:\n")
	for _, c := range insp.ApplyCommands {
		fmt.Fprintf(&b, "    %s\n", c)
	}
	fmt.Fprintf(&b, "  Current state: %s\n", insp.Current)
	fmt.Fprintf(&b, "  Undo records:  %s\n", describeUndoInfo(insp.UndoInfo))
	if action.UndoDesc != "" {
		fmt.Fprintf(&b, "  Undo:          %s\n", action.UndoDesc)
	} else {
		fmt.Fprintf(&b, "  Undo:\n")
	}
	for _, c := range insp.UndoCommands {
		fmt.Fprintf(&b, "    %s\n", c)
	}

	return b.String()
}

// describeUndoInfo renders an undo value for display, making the absent
// sentinel and empty values readable.
func describeUndoInfo(undoInfo string) string {
	switch {
	case undoInfo == absentSentinel:
		return "value did not exist (undo will remove it)"
	case undoInfo == absentKeySentinel:
		return "key and value did not exist (undo will remove the value, and the key if it is otherwise empty)"
	case undoInfo == "":
		return "nothing (action has no state to restore)"
	case strings.Contains(undoInfo, "\n"):
		return fmt.Sprintf("previous file content (%d bytes)", len(undoInfo))
	default:
		return undoInfo
	}
}

// Apply executes a remediation action and records the result in the change journal.
// In dry-run mode, no commands are executed and a result describing what would
// have happened is returned.
//
// The method:
//  1. Refuses (before running or journaling anything) if the action needs
//     elevation and the process is not elevated
//  2. Refuses if the change journal exists but cannot be read, because a
//     change that cannot be journaled cannot be undone
//  3. Calls the platform-specific implementation
//  4. Writes a ChangeJournalEntry to disk for audit/undo purposes
//  5. Returns a RemediationResult with the outcome
func (e *Engine) Apply(action types.RemediationAction) (*types.RemediationResult, error) {
	result := &types.RemediationResult{
		ActionID:  action.ID,
		Timestamp: time.Now(),
		DryRun:    e.dryRun,
	}

	if e.dryRun {
		result.Success = true
		result.Output = fmt.Sprintf("[DRY RUN] Would apply: %s", action.Title)
		if action.DryRunDesc != "" {
			result.Output += "\n" + action.DryRunDesc
		}
		return result, nil
	}

	if needsElevation(action) && !elevationCheck() {
		return nil, errNotElevated
	}

	// Make sure the journal can take the entry before touching the system.
	// Read is strict (it rejects malformed entries), so this is the point
	// where a corrupted journal is caught, not after the change is applied.
	journal := NewJournal(e.journalDir)
	if _, err := journal.Read(); err != nil {
		return nil, fmt.Errorf("refusing to apply: change journal is unreadable, fix or move it first: %w", err)
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

	// Record in the change journal regardless of success/failure so the user
	// has an audit trail of every attempt that actually ran commands.
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
//
// The undo information is validated against the action's expected shape
// before use: the journal is an ordinary file, so its contents must never be
// trusted enough to write arbitrary strings into the registry or /etc.
func (e *Engine) Undo(entry types.ChangeJournalEntry) error {
	if entry.ActionID == "" {
		return fmt.Errorf("cannot undo: journal entry has no action id")
	}

	if !entry.Success {
		return fmt.Errorf("cannot undo action %q: original action did not succeed", entry.ActionID)
	}

	if err := validateUndoInfo(entry.ActionID, entry.UndoInfo); err != nil {
		return fmt.Errorf("cannot undo action %q: %w", entry.ActionID, err)
	}

	def, known := lookupAction(entry.ActionID)
	if !known {
		return fmt.Errorf("cannot undo action %q: not available on this platform", entry.ActionID)
	}

	if e.dryRun {
		return nil
	}

	if def.NeedsAdmin && !elevationCheck() {
		return errNotElevated
	}

	// Read the journal before undoing so an unreadable journal stops us
	// before any command runs, not after (the undo result must be recorded).
	journal := NewJournal(e.journalDir)
	entries, readErr := journal.Read()
	if readErr != nil {
		return fmt.Errorf("refusing to undo: change journal is unreadable, fix or move it first: %w", readErr)
	}

	undoErr := e.undoAction(entry.ActionID, entry.UndoInfo)

	// Update the journal with the undo status: find the matching entry by
	// action ID and timestamp.
	matched := false
	for i := range entries {
		if entries[i].ActionID == entry.ActionID && entries[i].AppliedAt.Equal(entry.AppliedAt) {
			matched = true
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

	if !matched {
		// The caller handed us an entry the journal does not contain (edited,
		// rotated, or from another machine). Rewriting the file would only
		// re-serialize what is already there, so leave it alone and make the
		// mismatch visible together with what the undo actually did.
		outcome := "the undo commands ran successfully"
		if undoErr != nil {
			outcome = "the undo commands failed: " + undoErr.Error()
		}
		return errors.Join(undoErr, fmt.Errorf("%s, but no journal entry matched action %q applied at %s; the journal was left unchanged and does not record this undo",
			outcome, entry.ActionID, entry.AppliedAt.Format(time.RFC3339)))
	}

	if writeErr := journal.Write(entries); writeErr != nil {
		// Keep the undo outcome visible alongside the journal failure.
		return errors.Join(undoErr, fmt.Errorf("undo executed but failed to update journal: %w", writeErr))
	}

	return undoErr
}

// ListAvailable returns all remediation actions applicable to the current platform.
// This delegates to the platform-specific getAvailableActions() function, which is
// defined in the build-tagged action files (actions_windows.go, actions_linux.go, etc.).
func (e *Engine) ListAvailable() []types.RemediationAction {
	return getAvailableActions()
}

// cmdString renders a command and its arguments the way a user would type them,
// quoting arguments that contain whitespace. Used for previews and outputs only.
func cmdString(name string, args ...string) string {
	parts := []string{name}
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
