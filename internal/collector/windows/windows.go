//go:build windows

package windows

import (
	"regexp"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectWindowsInfo gathers Windows-specific diagnostic data.
func CollectWindowsInfo(timeout int, includeLogs bool) (types.WindowsInfo, []types.CollectorError) {
	var info types.WindowsInfo
	var errs []types.CollectorError

	collectHAGS(&info, &errs, timeout)
	collectGameMode(&info, &errs, timeout)
	collectPowerPlan(&info, &errs, timeout)
	collectMonitors(&info, &errs, timeout)
	collectDriverResetEvents(&info, &errs, timeout)
	collectNvlddmkmErrors(&info, &errs, timeout)
	collectWHEAErrors(&info, &errs, timeout)
	collectRecentUpdates(&info, &errs, timeout)
	collectNVIDIAApp(&info, &errs, timeout)
	collectOverlaySoftware(&info, &errs, timeout)

	return info, errs
}

func collectHAGS(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`try { (Get-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\GraphicsDrivers" -Name HwSchMode -ErrorAction Stop).HwSchMode } catch { "Unknown" }`)
	if r.Err == nil {
		val := strings.TrimSpace(r.Stdout)
		switch val {
		case "2":
			info.HAGSEnabled = "Enabled"
		case "1":
			info.HAGSEnabled = "Disabled"
		default:
			info.HAGSEnabled = "Unknown (" + val + ")"
		}
	} else {
		info.HAGSEnabled = "Unknown"
		*errs = append(*errs, types.CollectorError{Collector: "windows.hags", Error: r.Err.Error()})
	}
}

func collectGameMode(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`try { (Get-ItemProperty -Path "HKCU:\Software\Microsoft\GameBar" -Name AutoGameModeEnabled -ErrorAction Stop).AutoGameModeEnabled } catch { "Unknown" }`)
	if r.Err == nil {
		val := strings.TrimSpace(r.Stdout)
		switch val {
		case "1":
			info.GameMode = "Enabled"
		case "0":
			info.GameMode = "Disabled"
		default:
			info.GameMode = "Unknown (" + val + ")"
		}
	} else {
		info.GameMode = "Unknown"
	}
}

func collectPowerPlan(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`(Get-CimInstance -Namespace root\cimv2\power -ClassName Win32_PowerPlan | Where-Object { $_.IsActive }).ElementName`)
	if r.Err == nil {
		info.PowerPlan = strings.TrimSpace(r.Stdout)
	} else {
		info.PowerPlan = "Unknown"
		*errs = append(*errs, types.CollectorError{Collector: "windows.powerplan", Error: r.Err.Error()})
	}
}

func collectMonitors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-CimInstance -Namespace root\wmi -ClassName WmiMonitorBasicDisplayParams -ErrorAction SilentlyContinue | ForEach-Object { "$($_.InstanceName)|$($_.MaxHorizontalImageSize)x$($_.MaxVerticalImageSize)" }`)
	if r.Err == nil && r.Stdout != "" {
		for _, line := range strings.Split(r.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 2)
			mon := types.MonitorInfo{
				Name: "Display",
			}
			if len(parts) >= 1 {
				mon.Name = parts[0]
			}
			info.Monitors = append(info.Monitors, mon)
		}
	}

	// Get resolution and refresh rate
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.CurrentHorizontalResolution)x$($_.CurrentVerticalResolution)|$($_.CurrentRefreshRate)Hz" }`)
	if r.Err == nil && r.Stdout != "" {
		lines := strings.Split(r.Stdout, "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 2)
			if i < len(info.Monitors) {
				info.Monitors[i].Resolution = parts[0]
				if len(parts) >= 2 {
					info.Monitors[i].RefreshRate = parts[1]
				}
			} else {
				mon := types.MonitorInfo{
					Name:       "Display",
					Resolution: parts[0],
				}
				if len(parts) >= 2 {
					mon.RefreshRate = parts[1]
				}
				info.Monitors = append(info.Monitors, mon)
			}
		}
	}
}

func collectDriverResetEvents(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// Event ID 4101 — Display driver stopped responding and has recovered
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-WinEvent -FilterHashtable @{LogName='System'; Id=4101; StartTime=(Get-Date).AddDays(-30)} -MaxEvents 50 -ErrorAction SilentlyContinue | ForEach-Object { "$($_.TimeCreated)|$($_.Id)|$($_.LevelDisplayName)|$($_.Message.Substring(0, [Math]::Min(200, $_.Message.Length)))" }`)
	if r.Err == nil && r.Stdout != "" {
		info.DriverResetEvents = parseEventLines(r.Stdout)
	} else if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.event4101",
			Error:     "Could not read Event ID 4101 (may require admin): " + r.Err.Error(),
		})
	}
}

func collectNvlddmkmErrors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='nvlddmkm'; StartTime=(Get-Date).AddDays(-30)} -MaxEvents 50 -ErrorAction SilentlyContinue | ForEach-Object { "$($_.TimeCreated)|$($_.Id)|$($_.LevelDisplayName)|$($_.Message.Substring(0, [Math]::Min(200, $_.Message.Length)))" }`)
	if r.Err == nil && r.Stdout != "" {
		info.NvlddmkmErrors = parseEventLines(r.Stdout)
	}
}

func collectWHEAErrors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-WHEA-Logger'; StartTime=(Get-Date).AddDays(-30)} -MaxEvents 20 -ErrorAction SilentlyContinue | ForEach-Object { "$($_.TimeCreated)|$($_.Id)|$($_.LevelDisplayName)|WHEA Error" }`)
	if r.Err == nil && r.Stdout != "" {
		info.WHEAErrors = parseEventLines(r.Stdout)
	}
}

func collectRecentUpdates(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-HotFix | Where-Object { $_.InstalledOn -gt (Get-Date).AddDays(-60) } | Sort-Object InstalledOn -Descending | ForEach-Object { "$($_.HotFixID)|$($_.Description)|$($_.InstalledOn.ToString('yyyy-MM-dd'))" }`)
	if r.Err == nil && r.Stdout != "" {
		for _, line := range strings.Split(r.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 3)
			kb := types.WindowsUpdate{
				KBID: parts[0],
			}
			if len(parts) >= 2 {
				kb.Title = parts[1]
			}
			if len(parts) >= 3 {
				t, err := time.Parse("2006-01-02", parts[2])
				if err == nil {
					kb.InstalledOn = t
				}
			}
			info.RecentKBs = append(info.RecentKBs, kb)
		}
	} else if r.Err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "windows.updates", Error: r.Err.Error()})
	}
}

func collectNVIDIAApp(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// Check for NVIDIA App
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-ItemProperty "HKLM:\SOFTWARE\NVIDIA Corporation\NVIDIA App" -ErrorAction SilentlyContinue | ForEach-Object { $_.Version }`)
	if r.Err == nil && strings.TrimSpace(r.Stdout) != "" {
		info.NVIDIAAppVersion = strings.TrimSpace(r.Stdout)
	}

	// Check for GeForce Experience
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-ItemProperty "HKLM:\SOFTWARE\NVIDIA Corporation\Global\GFExperience" -ErrorAction SilentlyContinue | ForEach-Object { $_.Version }`)
	if r.Err == nil && strings.TrimSpace(r.Stdout) != "" {
		info.GFEVersion = strings.TrimSpace(r.Stdout)
	}
}

func collectOverlaySoftware(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// Detect overlay software by checking installed programs
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`$apps = Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*","HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty DisplayName -ErrorAction SilentlyContinue; $apps -join "`+"`n"+`"`)
	if r.Err == nil {
		appList := strings.ToLower(r.Stdout)
		overlays := map[string]string{
			"xbox game bar":     "Xbox Game Bar",
			"discord":           "Discord (may have overlay)",
			"msi afterburner":   "MSI Afterburner",
			"rivatuner":         "RivaTuner Statistics Server (RTSS)",
			"obs studio":        "OBS Studio",
			"shadowplay":        "NVIDIA ShadowPlay",
			"overwolf":          "Overwolf",
			"medal":             "Medal.tv",
			"action!":           "Action! Screen Recorder",
		}
		for pattern, label := range overlays {
			if strings.Contains(appList, pattern) {
				info.OverlaySoftware = append(info.OverlaySoftware, label)
			}
		}
	}

	// Check Xbox Game Bar specifically
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-AppxPackage -Name Microsoft.XboxGamingOverlay -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Version`)
	if r.Err == nil && strings.TrimSpace(r.Stdout) != "" {
		found := false
		for _, s := range info.OverlaySoftware {
			if strings.Contains(s, "Xbox") {
				found = true
				break
			}
		}
		if !found {
			info.OverlaySoftware = append(info.OverlaySoftware, "Xbox Game Bar (v"+strings.TrimSpace(r.Stdout)+")")
		}
	}
}

func parseEventLines(output string) []types.EventLogEntry {
	var events []types.EventLogEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		entry := types.EventLogEntry{
			Source: "System",
		}

		// Parse time
		timeStr := strings.TrimSpace(parts[0])
		// Try multiple formats
		for _, fmt := range []string{
			"01/02/2006 15:04:05",
			"1/2/2006 3:04:05 PM",
			"2006-01-02 15:04:05",
			time.RFC3339,
		} {
			t, err := time.Parse(fmt, timeStr)
			if err == nil {
				entry.Time = t
				break
			}
		}

		if len(parts) >= 2 {
			// Use regexp to avoid import issues
			idRe := regexp.MustCompile(`\d+`)
			if m := idRe.FindString(parts[1]); m != "" {
				entry.EventID = int(parseIntSafe(m))
			}
		}
		if len(parts) >= 3 {
			entry.Level = strings.TrimSpace(parts[2])
		}
		if len(parts) >= 4 {
			entry.Message = strings.TrimSpace(parts[3])
		}
		events = append(events, entry)
	}
	return events
}

func parseIntSafe(s string) int64 {
	s = strings.TrimSpace(s)
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else {
			break
		}
	}
	return n
}
