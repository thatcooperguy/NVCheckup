//go:build windows

package remediate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// highPerformanceGUID is the well-known GUID of the Windows "High performance" power plan.
const highPerformanceGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"

// regDword identifies a REG_DWORD registry value that an action toggles.
type regDword struct {
	Path string // e.g. HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers
	Name string // e.g. HwSchMode
}

var (
	// hagsValue controls Hardware-Accelerated GPU Scheduling (2 = on, 1 = off).
	hagsValue = regDword{Path: `HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`, Name: "HwSchMode"}
	// gameModeValue controls Windows Game Mode (1 = on, 0 = off) for the current user.
	gameModeValue = regDword{Path: `HKCU\Software\Microsoft\GameBar`, Name: "AutoGameModeEnabled"}
)

// hiveLongNames maps the hive abbreviations accepted by reg.exe to the full
// names it prints in listings.
var hiveLongNames = map[string]string{
	"HKLM": "HKEY_LOCAL_MACHINE",
	"HKCU": "HKEY_CURRENT_USER",
	"HKU":  "HKEY_USERS",
	"HKCR": "HKEY_CLASSES_ROOT",
	"HKCC": "HKEY_CURRENT_CONFIG",
}

// longKeyPath expands the hive abbreviation of a key path to the form reg.exe
// prints ("HKCU\Software" -> "HKEY_CURRENT_USER\Software").
func longKeyPath(path string) string {
	hive, rest, found := strings.Cut(path, `\`)
	if long, ok := hiveLongNames[strings.ToUpper(hive)]; ok {
		hive = long
	}
	if !found {
		return hive
	}
	return hive + `\` + rest
}

// parentKeyPath returns the key one level up, or ok=false for a hive root.
func parentKeyPath(path string) (string, bool) {
	i := strings.LastIndex(path, `\`)
	if i <= 0 {
		return "", false
	}
	return path[:i], true
}

func (r regDword) String() string { return r.Path + `\` + r.Name }

func (r regDword) queryArgs() []string {
	return []string{"query", r.Path, "/v", r.Name}
}

func (r regDword) keyQueryArgs() []string {
	return []string{"query", r.Path}
}

func (r regDword) addArgs(value string) []string {
	return []string{"add", r.Path, "/v", r.Name, "/t", "REG_DWORD", "/d", value, "/f"}
}

func (r regDword) deleteArgs() []string {
	return []string{"delete", r.Path, "/v", r.Name, "/f"}
}

func (r regDword) deleteKeyArgs() []string {
	return []string{"delete", r.Path, "/f"}
}

// undoArgs returns the reg.exe arguments that restore a captured value: delete
// when the value did not exist beforehand, otherwise add with the old value.
func (r regDword) undoArgs(undoInfo string) []string {
	if undoInfo == absentSentinel || undoInfo == absentKeySentinel {
		return r.deleteArgs()
	}
	return r.addArgs(undoInfo)
}

// regState classifies what the read-only capture found.
type regState int

const (
	regUnknown     regState = iota // read failed; nothing safe to record
	regPresent                     // value exists
	regValueAbsent                 // key exists, value does not
	regKeyAbsent                   // the key itself does not exist
)

// undoInfoFor converts a captured state into the string recorded for undo.
func undoInfoFor(value string, state regState) string {
	switch state {
	case regValueAbsent:
		return absentSentinel
	case regKeyAbsent:
		return absentKeySentinel
	default:
		return value
	}
}

// regListingHasValue reports whether "reg query <key>" output lists a value
// named name. Value lines look like "    HwSchMode    REG_DWORD    0x2";
// subkey lines are a single full path and never match.
func regListingHasValue(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], name) {
			return true
		}
	}
	return false
}

// regListingHasSubkey reports whether "reg query <parent>" output lists the
// subkey whose full (long-hive) path is longPath.
func regListingHasSubkey(output, longPath string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), longPath) {
			return true
		}
	}
	return false
}

// regListingIsEmpty reports whether "reg query <key>" output shows a key with
// no values and no subkeys: only blank lines and the key header itself.
func regListingIsEmpty(output, longPath string) bool {
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.EqualFold(t, longPath) {
			continue
		}
		return false
	}
	return true
}

// readRegDword captures the current state of r without depending on the
// localized error text of reg.exe. reg query exits 1 both for "not found" and
// for real failures (access denied, bad hive), so when the value query fails
// the key is listed instead: a successful listing that does not mention the
// value means the value is absent; if the key cannot be listed either, the
// parent is listed to tell "key does not exist" from "key exists but is
// unreadable". Any remaining ambiguity is an error and the caller must refuse
// to apply: without a trusted "before" value there is nothing safe to record
// for undo.
func (e *Engine) readRegDword(r regDword) (value string, state regState, err error) {
	out, runErr := e.executor.Run("reg", r.queryArgs()...)
	if runErr == nil {
		parsed := parseRegDwordValue(out, r.Name)
		if parsed == "" {
			return "", regUnknown, fmt.Errorf("could not parse current value of %s from reg query output: %s", r, strings.TrimSpace(out))
		}
		return parsed, regPresent, nil
	}

	keyOut, keyErr := e.executor.Run("reg", r.keyQueryArgs()...)
	if keyErr == nil {
		if regListingHasValue(keyOut, r.Name) {
			return "", regUnknown, fmt.Errorf("could not read current value of %s although the key lists it: %v: %s", r, runErr, strings.TrimSpace(out))
		}
		return "", regValueAbsent, nil
	}

	if parent, ok := parentKeyPath(r.Path); ok {
		parentOut, parentErr := e.executor.Run("reg", "query", parent)
		if parentErr == nil {
			if regListingHasSubkey(parentOut, longKeyPath(r.Path)) {
				return "", regUnknown, fmt.Errorf("could not read current value of %s although key %s exists (access denied?): %v: %s", r, r.Path, runErr, strings.TrimSpace(out))
			}
			return "", regKeyAbsent, nil
		}
	}

	return "", regUnknown, fmt.Errorf("could not read current value of %s: %v: %s", r, runErr, strings.TrimSpace(out))
}

// describeRegDword renders the captured state for previews and outputs.
func describeRegDword(r regDword, value string, state regState) string {
	switch state {
	case regValueAbsent:
		return fmt.Sprintf("%s is not set (value absent)", r)
	case regKeyAbsent:
		return fmt.Sprintf("%s is not set (key %s does not exist)", r, r.Path)
	default:
		return fmt.Sprintf("%s = %s", r, value)
	}
}

// applyRegDword sets r to target after capturing its current value for undo.
// It is shared by disable-hags and disable-game-mode.
func (e *Engine) applyRegDword(r regDword, target, meaning string) (output, undoInfo string, err error) {
	current, state, err := e.readRegDword(r)
	if err != nil {
		return "", "", err
	}
	undoInfo = undoInfoFor(current, state)

	if state == regPresent && current == target {
		return fmt.Sprintf("%s is already %s (%s); nothing changed", r, target, meaning), undoInfo, nil
	}

	setOutput, err := e.executor.Run("reg", r.addArgs(target)...)
	if err != nil {
		return setOutput, "", fmt.Errorf("failed to set %s to %s: %w: %s", r, target, err, strings.TrimSpace(setOutput))
	}

	return fmt.Sprintf("Set %s to %s (%s); before: %s", r, target, meaning, describeRegDword(r, current, state)),
		undoInfo, nil
}

// undoRegDword restores r from captured undo info. For the absent sentinels
// the current state is read first so a retried undo stays idempotent without
// relying on the localized "not found" message of reg.exe; for
// absentKeySentinel the key that apply had to create is removed too, but only
// if nothing else has been stored in it since.
func (e *Engine) undoRegDword(r regDword, undoInfo string) error {
	if undoInfo != absentSentinel && undoInfo != absentKeySentinel {
		out, err := e.executor.Run("reg", r.addArgs(undoInfo)...)
		if err != nil {
			return fmt.Errorf("failed to restore %s: %w: %s", r, err, strings.TrimSpace(out))
		}
		return nil
	}

	_, state, err := e.readRegDword(r)
	if err != nil {
		return fmt.Errorf("cannot undo %s: %w", r, err)
	}
	if state == regPresent {
		out, err := e.executor.Run("reg", r.deleteArgs()...)
		if err != nil {
			return fmt.Errorf("failed to remove %s: %w: %s", r, err, strings.TrimSpace(out))
		}
	}
	if undoInfo == absentKeySentinel && state != regKeyAbsent {
		keyOut, keyErr := e.executor.Run("reg", r.keyQueryArgs()...)
		if keyErr == nil && regListingIsEmpty(keyOut, longKeyPath(r.Path)) {
			out, err := e.executor.Run("reg", r.deleteKeyArgs()...)
			if err != nil {
				return fmt.Errorf("removed %s but failed to remove the key %s it was created in: %w: %s", r.Name, r.Path, err, strings.TrimSpace(out))
			}
		}
	}
	return nil
}

// inspectRegDword builds the preview for a registry toggle without changing anything.
func (e *Engine) inspectRegDword(r regDword, target string) (inspection, error) {
	current, state, err := e.readRegDword(r)
	if err != nil {
		return inspection{}, err
	}
	undoInfo := undoInfoFor(current, state)
	insp := inspection{
		Current:       describeRegDword(r, current, state),
		UndoInfo:      undoInfo,
		ApplyCommands: []string{cmdString("reg", r.addArgs(target)...)},
		UndoCommands:  []string{cmdString("reg", r.undoArgs(undoInfo)...)},
	}
	if state == regKeyAbsent {
		insp.UndoCommands = append(insp.UndoCommands, cmdString("reg", r.deleteKeyArgs()...)+" (only if the key is then empty)")
	}
	return insp, nil
}

// readActivePowerScheme returns the active power plan GUID and its display
// name as reported by "powercfg /getactivescheme".
func (e *Engine) readActivePowerScheme() (guid, name string, err error) {
	out, runErr := e.executor.Run("powercfg", "/getactivescheme")
	if runErr != nil {
		return "", "", fmt.Errorf("failed to get current power plan: %w: %s", runErr, strings.TrimSpace(out))
	}
	guid = parsePowerSchemeGUID(out)
	if guid == "" {
		return "", "", fmt.Errorf("could not parse current power plan GUID from output: %s", strings.TrimSpace(out))
	}
	return guid, parsePowerSchemeName(out), nil
}

// actionSetHighPerformance switches the active Windows power plan to "High performance"
// using powercfg. Before switching, it captures the currently active plan GUID so the
// change can be undone.
func (e *Engine) actionSetHighPerformance() (output, undoInfo string, err error) {
	previousGUID, previousName, err := e.readActivePowerScheme()
	if err != nil {
		return "", "", err
	}

	if strings.EqualFold(previousGUID, highPerformanceGUID) {
		return "High performance plan is already active; nothing changed", previousGUID, nil
	}

	switchOutput, err := e.executor.Run("powercfg", "/setactive", highPerformanceGUID)
	if err != nil {
		return switchOutput, "", fmt.Errorf("failed to set high performance plan: %w: %s", err, strings.TrimSpace(switchOutput))
	}

	return fmt.Sprintf("Switched power plan to High performance (was: %s %s)", previousGUID, previousName),
		previousGUID, nil
}

func (e *Engine) inspectSetHighPerformance() (inspection, error) {
	guid, name, err := e.readActivePowerScheme()
	if err != nil {
		return inspection{}, err
	}
	current := fmt.Sprintf("active power plan: %s %s", guid, name)
	if strings.EqualFold(guid, highPerformanceGUID) {
		current += " (already High performance)"
	}
	return inspection{
		Current:       current,
		UndoInfo:      guid,
		ApplyCommands: []string{cmdString("powercfg", "/setactive", highPerformanceGUID)},
		UndoCommands:  []string{cmdString("powercfg", "/setactive", guid)},
	}, nil
}

// actionDisableHAGS disables Hardware-Accelerated GPU Scheduling by setting
// HwSchMode to 1. Windows treats an absent value as "on" for supported GPUs, but
// we record the absence itself (not a default) so undo removes the value again.
func (e *Engine) actionDisableHAGS() (output, undoInfo string, err error) {
	output, undoInfo, err = e.applyRegDword(hagsValue, "1", "HAGS disabled")
	if err == nil {
		output += ". Reboot required."
	}
	return output, undoInfo, err
}

// actionDisableGameMode disables Windows Game Mode for the current user by
// setting AutoGameModeEnabled to 0.
func (e *Engine) actionDisableGameMode() (output, undoInfo string, err error) {
	return e.applyRegDword(gameModeValue, "0", "Game Mode disabled")
}

// applyAction dispatches a remediation action by ID to the appropriate
// Windows-specific implementation.
func (e *Engine) applyAction(id string) (output string, undoInfo string, err error) {
	switch id {
	case "set-high-performance":
		return e.actionSetHighPerformance()
	case "disable-hags":
		return e.actionDisableHAGS()
	case "disable-game-mode":
		return e.actionDisableGameMode()
	default:
		return "", "", fmt.Errorf("unknown remediation action: %q", id)
	}
}

// undoAction reverses a previously applied Windows remediation action using
// the stored undo information. Callers must run validateUndoInfo first; this
// function assumes undoInfo already has the right shape.
func (e *Engine) undoAction(id string, undoInfo string) error {
	switch id {
	case "set-high-performance":
		out, err := e.executor.Run("powercfg", "/setactive", undoInfo)
		if err != nil {
			return fmt.Errorf("failed to restore power plan %s: %w: %s", undoInfo, err, strings.TrimSpace(out))
		}
		return nil
	case "disable-hags":
		return e.undoRegDword(hagsValue, undoInfo)
	case "disable-game-mode":
		return e.undoRegDword(gameModeValue, undoInfo)
	default:
		return fmt.Errorf("unknown action for undo: %q", id)
	}
}

// inspectAction performs the read-only capture for an action and describes the
// exact commands Apply and Undo would run.
func (e *Engine) inspectAction(id string) (inspection, error) {
	switch id {
	case "set-high-performance":
		return e.inspectSetHighPerformance()
	case "disable-hags":
		return e.inspectRegDword(hagsValue, "1")
	case "disable-game-mode":
		return e.inspectRegDword(gameModeValue, "0")
	default:
		return inspection{}, fmt.Errorf("unknown remediation action: %q", id)
	}
}

// getAvailableActions returns the list of remediation actions available on Windows.
// Risk labels and descriptions must stay in sync with knowledge/remediations.json.
func getAvailableActions() []types.RemediationAction {
	return []types.RemediationAction{
		{
			ID:          "set-high-performance",
			Title:       "Switch to High Performance power plan",
			Description: "Sets the active Windows power plan to 'High performance' with powercfg. Balanced and power-saver plans can hold CPU clocks down and starve the GPU.",
			DryRunDesc:  "Would run: powercfg /setactive " + highPerformanceGUID + " (after capturing the current plan with powercfg /getactivescheme)",
			UndoDesc:    "Restore the previously active plan: powercfg /setactive <captured GUID>",
			Risk:        types.RiskLow,
			NeedsAdmin:  true,
			NeedsReboot: false,
			Platform:    "windows",
			Category:    "power",
			RelatedFind: "Power plan is not set to High performance",
		},
		{
			ID:          "disable-hags",
			Title:       "Disable Hardware-Accelerated GPU Scheduling (HAGS)",
			Description: "Sets " + hagsValue.String() + " to 1 (off). HAGS can cause stutter or instability with some games and driver versions. Takes effect after a reboot.",
			DryRunDesc:  "Would run: " + cmdString("reg", hagsValue.addArgs("1")...) + " (after capturing the current value with reg query)",
			UndoDesc:    "Restore the captured HwSchMode value with reg add, or delete the value with reg delete if it did not exist before (removing the key too if apply had to create it and it is otherwise empty). Reboot required.",
			Risk:        types.RiskMedium,
			NeedsAdmin:  true,
			NeedsReboot: true,
			Platform:    "windows",
			Category:    "registry",
			RelatedFind: "HAGS is enabled",
		},
		{
			ID:          "disable-game-mode",
			Title:       "Disable Windows Game Mode",
			Description: "Sets " + gameModeValue.String() + " to 0. Game Mode can cause frame pacing issues in some titles.",
			DryRunDesc:  "Would run: " + cmdString("reg", gameModeValue.addArgs("0")...) + " (after capturing the current value with reg query)",
			UndoDesc:    "Restore the captured AutoGameModeEnabled value with reg add, or delete the value with reg delete if it did not exist before (removing the key too if apply had to create it and it is otherwise empty).",
			Risk:        types.RiskLow,
			NeedsAdmin:  false,
			NeedsReboot: false,
			Platform:    "windows",
			Category:    "registry",
			RelatedFind: "Game Mode is enabled",
		},
	}
}

// parsePowerSchemeGUID extracts the power scheme GUID from the output of
// "powercfg /getactivescheme". Expected format:
//
//	Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)
func parsePowerSchemeGUID(output string) string {
	marker := "GUID: "
	idx := strings.Index(output, marker)
	if idx == -1 {
		return ""
	}
	fields := strings.Fields(output[idx+len(marker):])
	if len(fields) == 0 || !guidPattern.MatchString(fields[0]) {
		return ""
	}
	return strings.ToLower(fields[0])
}

// parsePowerSchemeName extracts the parenthesised plan name, e.g. "(Balanced)",
// from "powercfg /getactivescheme" output. Returns "" when absent.
func parsePowerSchemeName(output string) string {
	start := strings.Index(output, "(")
	end := strings.LastIndex(output, ")")
	if start == -1 || end <= start {
		return ""
	}
	return output[start : end+1]
}

// parseRegDwordValue extracts a DWORD value from "reg query" output and returns
// it as a decimal string. Expected format:
//
//	HwSchMode    REG_DWORD    0x2
//
// reg.exe prints DWORDs in hex; converting to decimal keeps the recorded value
// unambiguous ("0x10" must not be replayed as decimal 10).
func parseRegDwordValue(output, valueName string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != valueName {
			continue
		}
		raw := fields[len(fields)-1]
		var (
			v   uint64
			err error
		)
		if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
			v, err = strconv.ParseUint(raw[2:], 16, 32)
		} else {
			v, err = strconv.ParseUint(raw, 10, 32)
		}
		if err != nil {
			return ""
		}
		return strconv.FormatUint(v, 10)
	}
	return ""
}
