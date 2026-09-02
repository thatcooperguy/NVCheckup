//go:build windows

package remediate

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Captured outputs from a real Windows 11 machine.
const (
	balancedSchemeOutput = "Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)"
	highPerfSchemeOutput = "Power Scheme GUID: 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c  (High performance)"
	hagsQueryOutput      = "\r\nHKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\GraphicsDrivers\r\n    HwSchMode    REG_DWORD    0x2\r\n"
	gameModeQueryOutput  = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n    AutoGameModeEnabled    REG_DWORD    0x1\r\n"
	regMissingOutput     = "ERROR: The system was unable to find the specified registry key or value."
	regDeniedOutput      = "ERROR: Access is denied."
)

var errExit1 = errors.New("exit status 1")

const (
	hagsQueryCmd    = `reg query HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode`
	hagsAddCmd      = `reg add HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /t REG_DWORD /d 1 /f`
	hagsRestoreCmd  = `reg add HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /t REG_DWORD /d 2 /f`
	hagsDeleteCmd   = `reg delete HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /f`
	gmQueryCmd      = `reg query HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled`
	gmAddCmd        = `reg add HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /t REG_DWORD /d 0 /f`
	gmRestoreCmd    = `reg add HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /t REG_DWORD /d 1 /f`
	gmDeleteCmd     = `reg delete HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /f`
	getSchemeCmd    = `powercfg /getactivescheme`
	setHighPerfCmd  = `powercfg /setactive 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c`
	restoreBalanced = `powercfg /setactive 381b4222-f694-41f0-9685-ff5bb260df2e`
)

func mustAction(t *testing.T, id string) types.RemediationAction {
	t.Helper()
	a, ok := lookupAction(id)
	if !ok {
		t.Fatalf("action %q not registered", id)
	}
	return a
}

// applyThenUndo runs Apply, checks the command sequence and captured undo info,
// then runs Undo on the journaled entry and checks the undo command.
func applyThenUndo(t *testing.T, id string, mock *MockExecutor, wantApply []string, wantUndoInfo string, wantUndoCmd string) {
	t.Helper()
	setElevation(t, true)
	dir := t.TempDir()
	e := NewEngine(mock, dir, false)

	result, err := e.Apply(mustAction(t, id))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Apply failed: %s / %s", result.Output, result.Error)
	}
	if !reflect.DeepEqual(mock.commands, wantApply) {
		t.Fatalf("apply commands = %q, want %q", mock.commands, wantApply)
	}
	if result.UndoInfo != wantUndoInfo {
		t.Fatalf("UndoInfo = %q, want %q", result.UndoInfo, wantUndoInfo)
	}

	j := NewJournal(dir)
	entry, ok := j.LatestUndoable(id)
	if !ok {
		t.Fatal("journal should contain an undoable entry")
	}
	if entry.UndoInfo != wantUndoInfo {
		t.Fatalf("journaled UndoInfo = %q, want %q", entry.UndoInfo, wantUndoInfo)
	}

	mock.commands = nil
	if err := e.Undo(*entry); err != nil {
		t.Fatalf("Undo error: %v", err)
	}
	if !reflect.DeepEqual(mock.commands, []string{wantUndoCmd}) {
		t.Fatalf("undo commands = %q, want [%q]", mock.commands, wantUndoCmd)
	}
	if _, still := j.LatestUndoable(id); still {
		t.Error("entry should no longer be undoable after a successful undo")
	}
}

func TestSetHighPerformance_ApplyAndUndo(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{{prefix: getSchemeCmd, output: balancedSchemeOutput}}}
	applyThenUndo(t, "set-high-performance", mock,
		[]string{getSchemeCmd, setHighPerfCmd},
		"381b4222-f694-41f0-9685-ff5bb260df2e",
		restoreBalanced)
}

func TestSetHighPerformance_AlreadyActive(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{{prefix: getSchemeCmd, output: highPerfSchemeOutput}}}
	e := NewEngine(mock, t.TempDir(), false)
	result, err := e.Apply(mustAction(t, "set-high-performance"))
	if err != nil || !result.Success {
		t.Fatalf("Apply: %v / %+v", err, result)
	}
	if !reflect.DeepEqual(mock.commands, []string{getSchemeCmd}) {
		t.Errorf("should only query, got %q", mock.commands)
	}
	if result.UndoInfo != highPerformanceGUID {
		t.Errorf("UndoInfo = %q", result.UndoInfo)
	}
}

func TestSetHighPerformance_UnparseableScheme(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{output: "garbage"}
	e := NewEngine(mock, t.TempDir(), false)
	result, err := e.Apply(mustAction(t, "set-high-performance"))
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "could not parse") {
		t.Errorf("expected parse refusal, got %+v", result)
	}
	if len(mock.commands) != 1 {
		t.Errorf("must not run setactive without a captured GUID, got %q", mock.commands)
	}
}

func TestDisableHAGS_CapturedValue(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{{prefix: hagsQueryCmd, output: hagsQueryOutput}}}
	applyThenUndo(t, "disable-hags", mock,
		[]string{hagsQueryCmd, hagsAddCmd},
		"2",
		hagsRestoreCmd)
}

func TestDisableHAGS_AbsentValue(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{{prefix: hagsQueryCmd, output: regMissingOutput, err: errExit1}}}
	applyThenUndo(t, "disable-hags", mock,
		[]string{hagsQueryCmd, hagsAddCmd},
		absentSentinel,
		hagsDeleteCmd)
}

func TestDisableHAGS_AlreadyDisabled(t *testing.T) {
	setElevation(t, true)
	out := strings.Replace(hagsQueryOutput, "0x2", "0x1", 1)
	mock := &MockExecutor{responses: []mockResponse{{prefix: hagsQueryCmd, output: out}}}
	e := NewEngine(mock, t.TempDir(), false)
	result, err := e.Apply(mustAction(t, "disable-hags"))
	if err != nil || !result.Success {
		t.Fatalf("Apply: %v / %+v", err, result)
	}
	if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd}) {
		t.Errorf("should not rewrite an already-disabled value, got %q", mock.commands)
	}
	if result.UndoInfo != "1" {
		t.Errorf("UndoInfo = %q, want 1", result.UndoInfo)
	}
}

// Any read failure other than "value does not exist" must refuse the change:
// guessing the prior value would make undo unsafe.
func TestDisableHAGS_ReadFailureRefuses(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{{prefix: hagsQueryCmd, output: regDeniedOutput, err: errExit1}}}
	dir := t.TempDir()
	e := NewEngine(mock, dir, false)
	result, err := e.Apply(mustAction(t, "disable-hags"))
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if result.Success {
		t.Fatal("apply should have been refused")
	}
	if !strings.Contains(result.Error, "could not read current value") {
		t.Errorf("error = %q", result.Error)
	}
	if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd}) {
		t.Errorf("reg add must not run, got %q", mock.commands)
	}
	if _, ok := NewJournal(dir).LatestUndoable("disable-hags"); ok {
		t.Error("a refused apply must not be undoable")
	}
}

func TestDisableHAGS_UndoDeleteOfAlreadyMissingValueIsIdempotent(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{{prefix: hagsDeleteCmd, output: regMissingOutput, err: errExit1}}}
	e := NewEngine(mock, t.TempDir(), false)
	if err := e.undoAction("disable-hags", absentSentinel); err != nil {
		t.Errorf("deleting an already-absent value should succeed, got %v", err)
	}
}

func TestDisableGameMode_CapturedValue_Unelevated(t *testing.T) {
	// Game Mode lives in HKCU and does not need elevation.
	mock := &MockExecutor{responses: []mockResponse{{prefix: gmQueryCmd, output: gameModeQueryOutput}}}
	setElevation(t, false)
	dir := t.TempDir()
	e := NewEngine(mock, dir, false)
	result, err := e.Apply(mustAction(t, "disable-game-mode"))
	if err != nil || !result.Success {
		t.Fatalf("Apply: %v / %+v", err, result)
	}
	if !reflect.DeepEqual(mock.commands, []string{gmQueryCmd, gmAddCmd}) {
		t.Fatalf("commands = %q", mock.commands)
	}
	if result.UndoInfo != "1" {
		t.Fatalf("UndoInfo = %q", result.UndoInfo)
	}
	entry, _ := NewJournal(dir).LatestUndoable("disable-game-mode")
	mock.commands = nil
	if err := e.Undo(*entry); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !reflect.DeepEqual(mock.commands, []string{gmRestoreCmd}) {
		t.Errorf("undo commands = %q", mock.commands)
	}
}

func TestDisableGameMode_AbsentValue(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{{prefix: gmQueryCmd, output: regMissingOutput, err: errExit1}}}
	applyThenUndo(t, "disable-game-mode", mock,
		[]string{gmQueryCmd, gmAddCmd},
		absentSentinel,
		gmDeleteCmd)
}

func TestPreview_DisableHAGS_ShowsCommandsCurrentAndUndo(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		mock := &MockExecutor{responses: []mockResponse{{prefix: hagsQueryCmd, output: regMissingOutput, err: errExit1}}}
		e := NewEngine(mock, t.TempDir(), dryRun)
		out := e.Preview(mustAction(t, "disable-hags"))
		for _, want := range []string{hagsAddCmd, "is not set (value absent)", "did not exist", hagsDeleteCmd, "Risk level:    medium"} {
			if !strings.Contains(out, want) {
				t.Errorf("dryRun=%v preview missing %q:\n%s", dryRun, want, out)
			}
		}
		// Preview runs only the read-only capture, in dry-run too.
		if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd}) {
			t.Errorf("preview commands = %q", mock.commands)
		}
	}
}

func TestPreview_SetHighPerformance_ShowsCapturedGUID(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{{prefix: getSchemeCmd, output: balancedSchemeOutput}}}
	e := NewEngine(mock, t.TempDir(), false)
	out := e.Preview(mustAction(t, "set-high-performance"))
	for _, want := range []string{setHighPerfCmd, "381b4222-f694-41f0-9685-ff5bb260df2e (Balanced)", restoreBalanced} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
}

func TestParseRegDwordValue(t *testing.T) {
	cases := map[string]string{
		hagsQueryOutput:                            "2",
		"    HwSchMode    REG_DWORD    0x10":       "16",
		"    HwSchMode    REG_DWORD    0xffffffff": "4294967295",
		"    HwSchMode    REG_DWORD    7":          "7",
		"    HwSchModeX   REG_DWORD    0x2":        "",
		"    HwSchMode    REG_DWORD    0xZZ":       "",
		"":                                         "",
	}
	for in, want := range cases {
		if got := parseRegDwordValue(in, "HwSchMode"); got != want {
			t.Errorf("parseRegDwordValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePowerScheme(t *testing.T) {
	if got := parsePowerSchemeGUID(balancedSchemeOutput); got != "381b4222-f694-41f0-9685-ff5bb260df2e" {
		t.Errorf("GUID = %q", got)
	}
	if got := parsePowerSchemeGUID("Power Scheme GUID: 8C5E7FDA-E8BF-4A96-9A85-A6E23A8C635C  (High performance)"); got != highPerformanceGUID {
		t.Errorf("GUID should be lower-cased, got %q", got)
	}
	if got := parsePowerSchemeGUID("Power Scheme GUID: not-a-guid (x)"); got != "" {
		t.Errorf("invalid GUID should be rejected, got %q", got)
	}
	if got := parsePowerSchemeName(balancedSchemeOutput); got != "(Balanced)" {
		t.Errorf("name = %q", got)
	}
}

func TestRegValueMissing(t *testing.T) {
	if !regValueMissing(regMissingOutput) {
		t.Error("should detect missing value message")
	}
	if regValueMissing(regDeniedOutput) {
		t.Error("access denied is not 'missing'")
	}
}
