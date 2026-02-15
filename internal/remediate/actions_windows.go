//go:build windows

package remediate

import (
	"fmt"
	"strings"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// Windows power plan GUIDs (well-known Microsoft constants)
const (
	// highPerformanceGUID is the GUID for the Windows "High performance" power plan.
	highPerformanceGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"
)

// actionSetHighPerformance switches the active Windows power plan to "High performance"
// using powercfg. Before switching, it captures the currently active plan GUID so the
// change can be undone.
func (e *Engine) actionSetHighPerformance() (output, undoInfo string, err error) {
	// Capture the current active power plan GUID before making any changes.
	// The output of "powercfg /getactivescheme" looks like:
	//   Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)
	currentOutput, err := e.executor.Run("powercfg", "/getactivescheme")
	if err != nil {
		return currentOutput, "", fmt.Errorf("failed to get current power plan: %w", err)
	}

	// Parse the GUID from the output
	previousGUID := parsePowerSchemeGUID(currentOutput)
	if previousGUID == "" {
		return currentOutput, "", fmt.Errorf("could not parse current power plan GUID from output: %s", currentOutput)
	}

	// If already on high performance, nothing to do
	if strings.EqualFold(previousGUID, highPerformanceGUID) {
		return "High performance plan is already active", previousGUID, nil
	}

	// Switch to high performance
	switchOutput, err := e.executor.Run("powercfg", "/setactive", highPerformanceGUID)
	if err != nil {
		return switchOutput, "", fmt.Errorf("failed to set high performance plan: %w", err)
	}

	return fmt.Sprintf("Switched power plan to High performance (was: %s)", previousGUID),
		previousGUID, nil
}

// actionDisableHAGS disables Hardware-Accelerated GPU Scheduling by setting the
// HwSchMode registry value to 1 (off). The default enabled value is 2.
// This change requires a reboot to take effect.
func (e *Engine) actionDisableHAGS() (output, undoInfo string, err error) {
	regPath := `HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`
	valueName := "HwSchMode"

	// Read the current value before modifying. "reg query" output looks like:
	//   HwSchMode    REG_DWORD    0x2
	queryOutput, queryErr := e.executor.Run("reg", "query", regPath, "/v", valueName)

	// Capture undo info: if the key exists, we'll restore its value; if it
	// doesn't exist, undo means deleting the value we create.
	currentVal := "2" // default: HAGS enabled
	if queryErr == nil {
		parsed := parseRegDwordValue(queryOutput, valueName)
		if parsed != "" {
			currentVal = parsed
		}
	}

	// Set HwSchMode to 1 (disabled)
	setOutput, err := e.executor.Run("reg", "add", regPath, "/v", valueName,
		"/t", "REG_DWORD", "/d", "1", "/f")
	if err != nil {
		return setOutput, "", fmt.Errorf("failed to set %s to 1: %w", valueName, err)
	}

	return fmt.Sprintf("Set %s\\%s to 1 (HAGS disabled). Reboot required.", regPath, valueName),
		currentVal, nil
}

// actionDisableGameMode disables Windows Game Mode by setting the AutoGameModeEnabled
// registry value to 0 under the current user's GameBar key. Undo restores it to 1.
func (e *Engine) actionDisableGameMode() (output, undoInfo string, err error) {
	regPath := `HKCU\Software\Microsoft\GameBar`
	valueName := "AutoGameModeEnabled"

	// Read the current value before modifying
	queryOutput, queryErr := e.executor.Run("reg", "query", regPath, "/v", valueName)

	currentVal := "1" // default: Game Mode enabled
	if queryErr == nil {
		parsed := parseRegDwordValue(queryOutput, valueName)
		if parsed != "" {
			currentVal = parsed
		}
	}

	// Set AutoGameModeEnabled to 0 (disabled)
	setOutput, err := e.executor.Run("reg", "add", regPath, "/v", valueName,
		"/t", "REG_DWORD", "/d", "0", "/f")
	if err != nil {
		return setOutput, "", fmt.Errorf("failed to set %s to 0: %w", valueName, err)
	}

	return fmt.Sprintf("Set %s\\%s to 0 (Game Mode disabled).", regPath, valueName),
		currentVal, nil
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
// the stored undo information.
func (e *Engine) undoAction(id string, undoInfo string) error {
	switch id {
	case "set-high-performance":
		// undoInfo contains the previous power plan GUID
		_, err := e.executor.Run("powercfg", "/setactive", undoInfo)
		return err

	case "disable-hags":
		// undoInfo contains the previous HwSchMode value
		regPath := `HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`
		_, err := e.executor.Run("reg", "add", regPath, "/v", "HwSchMode",
			"/t", "REG_DWORD", "/d", undoInfo, "/f")
		return err

	case "disable-game-mode":
		// undoInfo contains the previous AutoGameModeEnabled value
		regPath := `HKCU\Software\Microsoft\GameBar`
		_, err := e.executor.Run("reg", "add", regPath, "/v", "AutoGameModeEnabled",
			"/t", "REG_DWORD", "/d", undoInfo, "/f")
		return err

	default:
		return fmt.Errorf("unknown action for undo: %q", id)
	}
}

// getAvailableActions returns the list of remediation actions available on Windows.
func getAvailableActions() []types.RemediationAction {
	return []types.RemediationAction{
		{
			ID:          "set-high-performance",
			Title:       "Switch to High Performance power plan",
			Description: "Sets the Windows power plan to 'High performance' using powercfg. This prevents CPU throttling and ensures maximum GPU performance.",
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
			Description: "Sets the HwSchMode registry value to 1 to disable HAGS. Some games and applications experience stuttering or instability with HAGS enabled.",
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
			Description: "Sets the AutoGameModeEnabled registry value to 0 to disable Game Mode. Game Mode can cause frame pacing issues in some titles.",
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
	// Look for the GUID pattern after "GUID: "
	marker := "GUID: "
	idx := strings.Index(output, marker)
	if idx == -1 {
		return ""
	}
	rest := output[idx+len(marker):]

	// The GUID is the next 36 characters (8-4-4-4-12 format)
	// but we'll take everything up to the next space
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

// parseRegDwordValue extracts a DWORD value from "reg query" output.
// Expected format:
//
//	HwSchMode    REG_DWORD    0x2
func parseRegDwordValue(output, valueName string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, valueName) {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				val := fields[len(fields)-1]
				// Convert hex (0x2) to decimal string
				val = strings.TrimPrefix(val, "0x")
				val = strings.TrimPrefix(val, "0X")
				return val
			}
		}
	}
	return ""
}
