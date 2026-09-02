package remediate

import "github.com/thatcooperguy/nvcheckup/pkg/types"

// highPerformanceGUID is the well-known GUID of the Windows "High performance" power plan.
const highPerformanceGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"

// catalog is the single definition of every remediation action nvcheckup
// knows about, on every platform. knowledge/remediations.json carries the
// same text and is the canonical, human-reviewed source; TestCatalog_Matches
// KnowledgeFile keeps the two identical. The analyzer attaches these entries
// to findings (via ActionByID) and the platform action files serve them
// through getAvailableActions, so risk labels and descriptions can no longer
// drift between the report and the engine.
var catalog = []types.RemediationAction{
	{
		ID:          "set-high-performance",
		Title:       "Switch to High Performance power plan",
		Risk:        types.RiskLow,
		Description: "Sets the active Windows power plan to 'High performance' with powercfg. Balanced and power-saver plans can hold CPU clocks down and starve the GPU.",
		DryRunDesc:  "Would run: powercfg /setactive " + highPerformanceGUID + " (after capturing the current plan with powercfg /getactivescheme)",
		UndoDesc:    "Restore the previously active plan: powercfg /setactive <captured GUID>",
		Platform:    "windows",
		NeedsReboot: false,
		NeedsAdmin:  true,
		Category:    "power",
		RelatedFind: "Power plan is not set to High performance",
	},
	{
		ID:          "disable-hags",
		Title:       "Disable Hardware-Accelerated GPU Scheduling (HAGS)",
		Risk:        types.RiskMedium,
		Description: `Sets HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers\HwSchMode to 1 (off). HAGS can cause stutter or instability with some games and driver versions. Takes effect after a reboot.`,
		DryRunDesc:  `Would run: reg add HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers /v HwSchMode /t REG_DWORD /d 1 /f (after capturing the current value with reg query)`,
		UndoDesc:    "Restore the captured HwSchMode value with reg add, or delete the value with reg delete if it did not exist before (removing the key too if apply had to create it and it is otherwise empty). Reboot required.",
		Platform:    "windows",
		NeedsReboot: true,
		NeedsAdmin:  true,
		Category:    "registry",
		RelatedFind: "HAGS is enabled",
	},
	{
		ID:          "disable-game-mode",
		Title:       "Disable Windows Game Mode",
		Risk:        types.RiskLow,
		Description: `Sets HKCU\Software\Microsoft\GameBar\AutoGameModeEnabled to 0. Game Mode can cause frame pacing issues in some titles.`,
		DryRunDesc:  `Would run: reg add HKCU\Software\Microsoft\GameBar /v AutoGameModeEnabled /t REG_DWORD /d 0 /f (after capturing the current value with reg query)`,
		UndoDesc:    "Restore the captured AutoGameModeEnabled value with reg add, or delete the value with reg delete if it did not exist before (removing the key too if apply had to create it and it is otherwise empty).",
		Platform:    "windows",
		NeedsReboot: false,
		NeedsAdmin:  false,
		Category:    "registry",
		RelatedFind: "Game Mode is enabled",
	},
	{
		ID:          "blacklist-nouveau",
		Title:       "Blacklist nouveau driver",
		Risk:        types.RiskMedium,
		Description: "Writes " + nouveauBlacklistPath + " so the open-source nouveau driver stops loading, then rebuilds the initramfs. Refused unless an NVIDIA driver is installed, because blacklisting nouveau without a replacement can leave the system without a working display.",
		DryRunDesc:  "Would write " + nouveauBlacklistPath + " ('blacklist nouveau', 'options nouveau modeset=0') and run update-initramfs -u, dracut -f, or mkinitcpio -P.",
		UndoDesc:    "Remove the file (or restore its previous content) and rebuild the initramfs again. Reboot required.",
		Platform:    "linux",
		NeedsReboot: true,
		NeedsAdmin:  true,
		Category:    "driver",
		RelatedFind: "nouveau driver is loaded",
	},
	{
		ID:          "update-ldconfig",
		Title:       "Refresh shared library cache (ldconfig)",
		Risk:        types.RiskLow,
		Description: "Runs ldconfig to refresh the dynamic linker cache so libcuda.so and libnvidia-ml.so can be found after a driver install.",
		DryRunDesc:  "Would run: ldconfig",
		UndoDesc:    "Nothing to restore; ldconfig only rebuilds the cache from existing library paths.",
		Platform:    "linux",
		NeedsReboot: false,
		NeedsAdmin:  true,
		Category:    "driver",
		RelatedFind: "libcuda.so not found in library path",
	},
}

// Catalog returns a copy of every remediation action nvcheckup defines,
// regardless of the platform the binary runs on. Use ListAvailable for the
// actions that can actually be applied here.
func Catalog() []types.RemediationAction {
	out := make([]types.RemediationAction, len(catalog))
	copy(out, catalog)
	return out
}

// ActionByID returns the catalog definition of an action, on any platform.
// ok is false for unknown IDs.
func ActionByID(id string) (types.RemediationAction, bool) {
	for _, a := range catalog {
		if a.ID == id {
			return a, true
		}
	}
	return types.RemediationAction{}, false
}

// catalogForPlatform returns the catalog entries whose Platform is platform
// (a runtime.GOOS value) or "all".
func catalogForPlatform(platform string) []types.RemediationAction {
	var out []types.RemediationAction
	for _, a := range catalog {
		if a.Platform == platform || a.Platform == "all" {
			out = append(out, a)
		}
	}
	return out
}
