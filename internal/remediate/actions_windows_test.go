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
	hagsDisabledOutput   = "\r\nHKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\GraphicsDrivers\r\n    HwSchMode    REG_DWORD    0x1\r\n"
	gameModeQueryOutput  = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n    AutoGameModeEnabled    REG_DWORD    0x1\r\n"
	gameModeOffOutput    = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n    AutoGameModeEnabled    REG_DWORD    0x0\r\n"
	regMissingOutput     = "ERROR: The system was unable to find the specified registry key or value."
	regDeniedOutput      = "ERROR: Access is denied."
	// A non-English "not found" message. The code must reach the same
	// conclusion without recognising the text.
	regMissingOutputDE = "FEHLER: Das System konnte den angegebenen Registrierungsschl\u00fcssel oder Wert nicht finden."

	// "reg query <key>" listings (no /v): header, values, blank line, subkeys.
	hagsKeyListing          = "\r\nHKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\GraphicsDrivers\r\n    PlatformSupportMiracast    REG_DWORD    0x1\r\n    DxgKrnlVersion    REG_DWORD    0x11007\r\n\r\nHKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\GraphicsDrivers\\BlockList\r\n"
	hagsKeyListingWithValue = hagsKeyListing + "    HwSchMode    REG_DWORD    0x2\r\n"
	gmKeyListing            = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n    GamepadDoublePressIntervalMs    REG_DWORD    0x1\r\n"
	gmEmptyKeyListing       = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n"
	gmParentNoGameBar       = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Accessibility\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\GameBarApi\r\n"
	gmParentWithGameBar     = gmParentNoGameBar + "HKEY_CURRENT_USER\\Software\\Microsoft\\GameBar\r\n"
)

var errExit1 = errors.New("exit status 1")

const (
	hagsQueryCmd       = `reg query HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode`
	hagsKeyQueryCmd    = `reg query HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`
	hagsParentQueryCmd = `reg query HKLM\SYSTEM\CurrentControlSet\Control`
	hagsAddCmd         = `reg add HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /t REG_DWORD /d 1 /f`
	hagsRestoreCmd     = `reg add HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /t REG_DWORD /d 2 /f`
	hagsDeleteCmd      = `reg delete HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /f`
	gmQueryCmd         = `reg query HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled`
	gmKeyQueryCmd      = `reg query HKCU\Software\Microsoft\GameBar`
	gmParentQueryCmd   = `reg query HKCU\Software\Microsoft`
	gmAddCmd           = `reg add HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /t REG_DWORD /d 0 /f`
	gmRestoreCmd       = `reg add HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /t REG_DWORD /d 1 /f`
	gmDeleteCmd        = `reg delete HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /f`
	gmDeleteKeyCmd     = `reg delete HKCU\Software\Microsoft\GameBar /f`
	getSchemeCmd       = `powercfg /getactivescheme`
	setHighPerfCmd     = `powercfg /setactive 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c`
	restoreBalanced    = `powercfg /setactive 381b4222-f694-41f0-9685-ff5bb260df2e`
)

// ok scripts a successful command; fail scripts one that exits 1.
func ok(cmd, output string) mockResponse { return mockResponse{prefix: cmd, output: output} }
func fail(cmd, output string) mockResponse {
	return mockResponse{prefix: cmd, output: output, err: errExit1}
}

func mustAction(t *testing.T, id string) types.RemediationAction {
	t.Helper()
	a, found := lookupAction(id)
	if !found {
		t.Fatalf("action %q not registered", id)
	}
	return a
}

// applyThenUndo runs Apply, checks the command sequence and captured undo
// info, then swaps in undoResponses (the machine state after apply) and runs
// Undo on the journaled entry, checking the undo command sequence.
func applyThenUndo(t *testing.T, id string, mock *MockExecutor, wantApply []string, wantUndoInfo string, undoResponses []mockResponse, wantUndo []string) {
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
	entry, found := j.LatestUndoable(id)
	if !found {
		t.Fatal("journal should contain an undoable entry")
	}
	if entry.UndoInfo != wantUndoInfo {
		t.Fatalf("journaled UndoInfo = %q, want %q", entry.UndoInfo, wantUndoInfo)
	}

	mock.commands = nil
	if undoResponses != nil {
		mock.responses = undoResponses
	}
	if err := e.Undo(*entry); err != nil {
		t.Fatalf("Undo error: %v", err)
	}
	if !reflect.DeepEqual(mock.commands, wantUndo) {
		t.Fatalf("undo commands = %q, want %q", mock.commands, wantUndo)
	}
	if _, still := j.LatestUndoable(id); still {
		t.Error("entry should no longer be undoable after a successful undo")
	}
}

func TestSetHighPerformance_ApplyAndUndo(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{ok(getSchemeCmd, balancedSchemeOutput)}}
	applyThenUndo(t, "set-high-performance", mock,
		[]string{getSchemeCmd, setHighPerfCmd},
		"381b4222-f694-41f0-9685-ff5bb260df2e",
		nil, []string{restoreBalanced})
}

func TestSetHighPerformance_AlreadyActive(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{ok(getSchemeCmd, highPerfSchemeOutput)}}
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
	mock := &MockExecutor{responses: []mockResponse{ok(hagsQueryCmd, hagsQueryOutput)}}
	applyThenUndo(t, "disable-hags", mock,
		[]string{hagsQueryCmd, hagsAddCmd},
		"2",
		nil, []string{hagsRestoreCmd})
}

// The value is absent (the common case on machines that never toggled HAGS):
// the /v query fails, the key listing succeeds without the value. This must
// work regardless of the language of the reg.exe error text.
func TestDisableHAGS_AbsentValue(t *testing.T) {
	for _, msg := range []string{regMissingOutput, regMissingOutputDE} {
		mock := &MockExecutor{responses: []mockResponse{fail(hagsQueryCmd, msg), ok(hagsKeyQueryCmd, hagsKeyListing)}}
		applyThenUndo(t, "disable-hags", mock,
			[]string{hagsQueryCmd, hagsKeyQueryCmd, hagsAddCmd},
			absentSentinel,
			[]mockResponse{ok(hagsQueryCmd, hagsDisabledOutput)}, // after apply the value exists
			[]string{hagsQueryCmd, hagsDeleteCmd})
	}
}

func TestDisableHAGS_AlreadyDisabled(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{ok(hagsQueryCmd, hagsDisabledOutput)}}
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

// Any read failure other than "does not exist" must refuse the change:
// guessing the prior value would make undo unsafe.
func TestDisableHAGS_ReadFailureRefuses(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{
		fail(hagsQueryCmd, regDeniedOutput),
		fail(hagsKeyQueryCmd, regDeniedOutput),
		fail(hagsParentQueryCmd, regDeniedOutput),
	}}
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
	if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd, hagsKeyQueryCmd, hagsParentQueryCmd}) {
		t.Errorf("reg add must not run, got %q", mock.commands)
	}
	if _, undoable := NewJournal(dir).LatestUndoable("disable-hags"); undoable {
		t.Error("a refused apply must not be undoable")
	}
}

// The key listing shows the value exists, yet reading it failed: that is a
// real error, not an absent value, and must not be recorded as absent.
func TestDisableHAGS_RefusesWhenKeyListsUnreadableValue(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{
		fail(hagsQueryCmd, regDeniedOutput),
		ok(hagsKeyQueryCmd, hagsKeyListingWithValue),
	}}
	e := NewEngine(mock, t.TempDir(), false)
	result, err := e.Apply(mustAction(t, "disable-hags"))
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "although the key lists it") {
		t.Errorf("expected refusal, got %+v", result)
	}
	if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd, hagsKeyQueryCmd}) {
		t.Errorf("commands = %q", mock.commands)
	}
}

// A retried undo of an absent value finds nothing to delete and succeeds
// without issuing reg delete.
func TestDisableHAGS_UndoOfAlreadyMissingValueIsIdempotent(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{fail(hagsQueryCmd, regMissingOutputDE), ok(hagsKeyQueryCmd, hagsKeyListing)}}
	e := NewEngine(mock, t.TempDir(), false)
	if err := e.undoAction("disable-hags", absentSentinel); err != nil {
		t.Errorf("undo of an already-absent value should succeed, got %v", err)
	}
	if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd, hagsKeyQueryCmd}) {
		t.Errorf("commands = %q", mock.commands)
	}
}

func TestDisableGameMode_CapturedValue_Unelevated(t *testing.T) {
	// Game Mode lives in HKCU and does not need elevation.
	mock := &MockExecutor{responses: []mockResponse{ok(gmQueryCmd, gameModeQueryOutput)}}
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
	mock := &MockExecutor{responses: []mockResponse{fail(gmQueryCmd, regMissingOutput), ok(gmKeyQueryCmd, gmKeyListing)}}
	applyThenUndo(t, "disable-game-mode", mock,
		[]string{gmQueryCmd, gmKeyQueryCmd, gmAddCmd},
		absentSentinel,
		[]mockResponse{ok(gmQueryCmd, gameModeOffOutput)},
		[]string{gmQueryCmd, gmDeleteCmd})
}

// Fresh profile: not even the GameBar key exists. Apply creates it (reg add
// does that implicitly); undo removes the value and then the key, because the
// key is empty afterwards.
func TestDisableGameMode_AbsentKey(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{
		fail(gmQueryCmd, regMissingOutput),
		fail(gmKeyQueryCmd, regMissingOutput),
		ok(gmParentQueryCmd, gmParentNoGameBar),
	}}
	applyThenUndo(t, "disable-game-mode", mock,
		[]string{gmQueryCmd, gmKeyQueryCmd, gmParentQueryCmd, gmAddCmd},
		absentKeySentinel,
		[]mockResponse{ok(gmQueryCmd, gameModeOffOutput), ok(gmKeyQueryCmd, gmEmptyKeyListing)},
		[]string{gmQueryCmd, gmDeleteCmd, gmKeyQueryCmd, gmDeleteKeyCmd})
}

// If something else stored values in the key after apply created it, undo
// removes only our value and leaves the key alone.
func TestDisableGameMode_AbsentKey_UndoKeepsKeyWithOtherValues(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{ok(gmQueryCmd, gameModeOffOutput), ok(gmKeyQueryCmd, gmKeyListing)}}
	e := NewEngine(mock, t.TempDir(), false)
	if err := e.undoAction("disable-game-mode", absentKeySentinel); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !reflect.DeepEqual(mock.commands, []string{gmQueryCmd, gmDeleteCmd, gmKeyQueryCmd}) {
		t.Errorf("commands = %q", mock.commands)
	}
}

// The parent lists the key, so the key exists but could not be read: refuse
// rather than record it as absent.
func TestDisableGameMode_RefusesWhenKeyExistsButUnreadable(t *testing.T) {
	setElevation(t, true)
	mock := &MockExecutor{responses: []mockResponse{
		fail(gmQueryCmd, regDeniedOutput),
		fail(gmKeyQueryCmd, regDeniedOutput),
		ok(gmParentQueryCmd, gmParentWithGameBar),
	}}
	e := NewEngine(mock, t.TempDir(), false)
	result, err := e.Apply(mustAction(t, "disable-game-mode"))
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "access denied?") {
		t.Errorf("expected refusal, got %+v", result)
	}
	if !reflect.DeepEqual(mock.commands, []string{gmQueryCmd, gmKeyQueryCmd, gmParentQueryCmd}) {
		t.Errorf("commands = %q", mock.commands)
	}
}

func TestPreview_DisableHAGS_ShowsCommandsCurrentAndUndo(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		mock := &MockExecutor{responses: []mockResponse{fail(hagsQueryCmd, regMissingOutput), ok(hagsKeyQueryCmd, hagsKeyListing)}}
		e := NewEngine(mock, t.TempDir(), dryRun)
		out := e.Preview(mustAction(t, "disable-hags"))
		for _, want := range []string{hagsAddCmd, "is not set (value absent)", "did not exist", hagsDeleteCmd, "Risk level:    medium"} {
			if !strings.Contains(out, want) {
				t.Errorf("dryRun=%v preview missing %q:\n%s", dryRun, want, out)
			}
		}
		// Preview runs only the read-only capture, in dry-run too.
		if !reflect.DeepEqual(mock.commands, []string{hagsQueryCmd, hagsKeyQueryCmd}) {
			t.Errorf("preview commands = %q", mock.commands)
		}
	}
}

func TestPreview_DisableGameMode_AbsentKey(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{
		fail(gmQueryCmd, regMissingOutput),
		fail(gmKeyQueryCmd, regMissingOutput),
		ok(gmParentQueryCmd, gmParentNoGameBar),
	}}
	e := NewEngine(mock, t.TempDir(), false)
	out := e.Preview(mustAction(t, "disable-game-mode"))
	for _, want := range []string{gmAddCmd, "does not exist)", "key and value did not exist", gmDeleteCmd, gmDeleteKeyCmd + " (only if the key is then empty)"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
}

func TestPreview_SetHighPerformance_ShowsCapturedGUID(t *testing.T) {
	mock := &MockExecutor{responses: []mockResponse{ok(getSchemeCmd, balancedSchemeOutput)}}
	e := NewEngine(mock, t.TempDir(), false)
	out := e.Preview(mustAction(t, "set-high-performance"))
	for _, want := range []string{setHighPerfCmd, "381b4222-f694-41f0-9685-ff5bb260df2e (Balanced)", restoreBalanced} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
}

func TestRegListingParsers(t *testing.T) {
	if !regListingHasValue(hagsKeyListingWithValue, "HwSchMode") {
		t.Error("listing with HwSchMode should report the value")
	}
	if !regListingHasValue(hagsKeyListingWithValue, "hwschmode") {
		t.Error("registry value names are case-insensitive")
	}
	if regListingHasValue(hagsKeyListing, "HwSchMode") {
		t.Error("listing without HwSchMode must not report it")
	}
	if regListingHasValue(gmParentWithGameBar, "GameBar") {
		t.Error("subkey lines must not be mistaken for values")
	}

	gameBar := longKeyPath(gameModeValue.Path)
	if gameBar != `HKEY_CURRENT_USER\Software\Microsoft\GameBar` {
		t.Errorf("longKeyPath = %q", gameBar)
	}
	if !regListingHasSubkey(gmParentWithGameBar, gameBar) {
		t.Error("parent listing with GameBar should report the subkey")
	}
	if regListingHasSubkey(gmParentNoGameBar, gameBar) {
		t.Error("GameBarApi must not match GameBar")
	}

	if !regListingIsEmpty(gmEmptyKeyListing, gameBar) {
		t.Error("header-only listing is an empty key")
	}
	if regListingIsEmpty(gmKeyListing, gameBar) {
		t.Error("a key with values is not empty")
	}
	if regListingIsEmpty(hagsKeyListing, longKeyPath(hagsValue.Path)) {
		t.Error("a key with subkeys is not empty")
	}

	if got := longKeyPath(`hklm\Software`); got != `HKEY_LOCAL_MACHINE\Software` {
		t.Errorf("longKeyPath(hklm) = %q", got)
	}
	if got := longKeyPath("HKCU"); got != "HKEY_CURRENT_USER" {
		t.Errorf("longKeyPath(HKCU) = %q", got)
	}
	if p, found := parentKeyPath(gameModeValue.Path); !found || p != `HKCU\Software\Microsoft` {
		t.Errorf("parentKeyPath = %q %v", p, found)
	}
	if _, found := parentKeyPath("HKCU"); found {
		t.Error("a hive root has no parent")
	}
}

func TestUndoArgs(t *testing.T) {
	if got := hagsValue.undoArgs(absentSentinel); !reflect.DeepEqual(got, hagsValue.deleteArgs()) {
		t.Errorf("absent value should delete, got %q", got)
	}
	if got := hagsValue.undoArgs(absentKeySentinel); !reflect.DeepEqual(got, hagsValue.deleteArgs()) {
		t.Errorf("absent key should delete the value, got %q", got)
	}
	if got := hagsValue.undoArgs("2"); !reflect.DeepEqual(got, hagsValue.addArgs("2")) {
		t.Errorf("captured value should be re-added, got %q", got)
	}
}
