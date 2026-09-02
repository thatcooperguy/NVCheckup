//go:build windows

package windows

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// CollectWindowsInfo gathers Windows-specific diagnostic data. It is
// read-only: every probe is a registry read, a WMI query or an event-log
// query.
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

	// The Uninstall hive is enumerated once and shared: it feeds both the
	// overlay detection and the NVIDIA App / GeForce Experience versions.
	programs := queryInstalledPrograms(timeout, &errs)
	collectNVIDIAApp(&info, programs, timeout)
	collectOverlaySoftware(&info, programs, timeout)

	return info, errs
}

// ---------------------------------------------------------------------------
// Registry toggles (HAGS, Game Mode)
// ---------------------------------------------------------------------------

// absentSentinel is printed by registryValueScript when the key or value does
// not exist, so Go can tell "not configured" apart from a real failure.
const absentSentinel = "__ABSENT__"

// registryValueScript reads one registry value and prints it, the absent
// sentinel, or "ERROR: <message>". Get-ItemProperty -Name throws the same
// generic exception for "value missing" and for genuine failures, so the value
// is looked up on the property bag instead.
func registryValueScript(path, name string) string {
	return `$ErrorActionPreference = 'Stop'; try { $p = Get-ItemProperty -Path '` + path + `'; ` +
		`$v = $p.PSObject.Properties['` + name + `']; ` +
		`if ($null -eq $v) { '` + absentSentinel + `' } else { [string]$v.Value } } ` +
		`catch [System.Management.Automation.ItemNotFoundException] { '` + absentSentinel + `' } ` +
		`catch { 'ERROR: ' + $_.Exception.Message }`
}

// registryProbe is the interpreted result of registryValueScript.
type registryProbe struct {
	Value  string
	Absent bool
	Err    string // non-empty when the probe itself failed
}

func interpretRegistryProbe(stdout, stderr string, runErr error) registryProbe {
	if runErr != nil {
		msg := runErr.Error()
		if s := firstLine(stderr); s != "" {
			msg += ": " + s
		}
		return registryProbe{Err: msg}
	}
	out := strings.TrimSpace(stdout)
	switch {
	case out == absentSentinel:
		return registryProbe{Absent: true}
	case strings.HasPrefix(out, "ERROR: "):
		return registryProbe{Err: strings.TrimPrefix(out, "ERROR: ")}
	}
	return registryProbe{Value: out}
}

// describeToggle maps a registry DWORD probe to a report label. An absent
// value means the user never changed the setting, so the OS default applies;
// that is a normal state, not an unknown one.
func describeToggle(p registryProbe, enabledValue, disabledValue string) string {
	switch {
	case p.Err != "":
		return "Unknown"
	case p.Absent:
		return "Default (not configured)"
	case p.Value == enabledValue:
		return "Enabled"
	case p.Value == disabledValue:
		return "Disabled"
	default:
		return "Unknown (value " + p.Value + ")"
	}
}

func collectHAGS(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		registryValueScript(`HKLM:\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`, "HwSchMode"))
	p := interpretRegistryProbe(r.Stdout, r.Stderr, r.Err)
	// HwSchMode 2 = hardware-accelerated GPU scheduling on, 1 = off.
	info.HAGSEnabled = describeToggle(p, "2", "1")
	if p.Err != "" {
		*errs = append(*errs, types.CollectorError{Collector: "windows.hags", Error: p.Err})
	}
}

func collectGameMode(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		registryValueScript(`HKCU:\Software\Microsoft\GameBar`, "AutoGameModeEnabled"))
	p := interpretRegistryProbe(r.Stdout, r.Stderr, r.Err)
	info.GameMode = describeToggle(p, "1", "0")
	if p.Err != "" {
		*errs = append(*errs, types.CollectorError{Collector: "windows.gamemode", Error: p.Err})
	}
}

// ---------------------------------------------------------------------------
// Power plan
// ---------------------------------------------------------------------------

// powerSchemeRe matches a bare GUID anywhere on a powercfg line. It does not
// anchor on the literal "GUID:" label because localized powercfg prints e.g.
// "GUID des Energieschemas: 381b4222-...  (Ausbalanciert)" (de-DE) or
// "GUID du mode de gestion de l'alimentation : 381b4222-...  (Utilisation
// normale)" (fr-FR); requiring the English label defeated the GUID-based
// canonicalisation in wellKnownPowerPlans and forced the Win32_PowerPlan
// fallback, which returns zero rows on some machines.
var powerSchemeRe = regexp.MustCompile(`(?i)\b([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)

// wellKnownPowerPlans maps the built-in scheme GUIDs to their English names so
// the analyzer's "high performance" check works on localized Windows and on
// renamed built-in plans.
var wellKnownPowerPlans = map[string]string{
	"381b4222-f694-41f0-9685-ff5bb260df2e": "Balanced",
	"8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c": "High performance",
	"a1841308-3541-4fab-bc81-f71556f20b4a": "Power saver",
	"e9a42b02-d5df-448d-aa00-03f14749eb61": "Ultimate Performance",
}

// parsePowerPlanScheme parses "Power Scheme GUID: 381b4222-...  (Balanced)"
// or any localized equivalent that carries a GUID followed by "(name)".
// The name is everything between the first "(" after the GUID and the last
// ")" on the line, so a plan called "My plan (custom)" survives intact; the
// GUID is returned lower-cased.
func parsePowerPlanScheme(out string) (name, guid string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		idx := powerSchemeRe.FindStringSubmatchIndex(line)
		if idx == nil {
			continue
		}
		guid = strings.ToLower(line[idx[2]:idx[3]])
		rest := line[idx[1]:]
		if open := strings.Index(rest, "("); open >= 0 {
			if closeIdx := strings.LastIndex(rest, ")"); closeIdx > open {
				name = strings.TrimSpace(rest[open+1 : closeIdx])
			}
		}
		return name, guid
	}
	return "", ""
}

// powerPlanLabel prefers the canonical English name for built-in schemes and
// falls back to the text powercfg printed.
func powerPlanLabel(name, guid string) string {
	if canonical, ok := wellKnownPowerPlans[strings.ToLower(guid)]; ok {
		return canonical
	}
	return name
}

func collectPowerPlan(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// powercfg is the primary source: it does not go through WMI, which on
	// some sessions returns zero Win32_PowerPlan rows or a Terminal Services
	// permission error even for a local interactive user.
	r := util.RunCommand(timeout, "powercfg", "/getactivescheme")
	if r.Err == nil {
		if label := powerPlanLabel(parsePowerPlanScheme(r.Stdout)); label != "" {
			info.PowerPlan = label
			return
		}
	}
	powercfgErr := "powercfg /getactivescheme printed no scheme"
	if r.Err != nil {
		powercfgErr = "powercfg: " + r.Err.Error()
	}

	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`$ErrorActionPreference = 'SilentlyContinue'; (Get-CimInstance -Namespace root\cimv2\power -ClassName Win32_PowerPlan | Where-Object { $_.IsActive }).ElementName; exit 0`)
	if r.Err == nil && strings.TrimSpace(r.Stdout) != "" {
		info.PowerPlan = strings.TrimSpace(r.Stdout)
		return
	}

	info.PowerPlan = "Unknown"
	*errs = append(*errs, types.CollectorError{
		Collector: "windows.powerplan",
		Error:     "could not determine the active power plan (" + powercfgErr + "; Win32_PowerPlan returned nothing)",
	})
}

// ---------------------------------------------------------------------------
// Monitors
// ---------------------------------------------------------------------------

func collectMonitors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	screens, idents, ctls, probeErrs := displayProbes.load(timeout)
	*errs = append(*errs, probeErrs...)
	info.Monitors = buildMonitors(screens, idents, ctls)
}

// buildMonitors produces one MonitorInfo per enumerated screen, named from
// the EDID identity (e.g. "DEL DELL U2720Q") rather than the PnP instance
// path. Entries without a resolution are skipped rather than emitted empty.
func buildMonitors(screens []screenInfo, idents []monitorIdentity, ctls []wmiVideoController) []types.MonitorInfo {
	var monitors []types.MonitorInfo
	adapter, _ := pickAdapter(ctls)
	active := activeIdentities(idents)

	if len(screens) == 0 {
		// Without screen enumeration the only resolution source is the
		// adapter; pair adapters and identities by ordinal.
		for i, id := range active {
			if i >= len(ctls) || ctls[i].CurrentHorizontalResolution == 0 || ctls[i].CurrentVerticalResolution == 0 {
				continue
			}
			mon := types.MonitorInfo{
				Name:        friendlyMonitorName(id),
				Resolution:  strconv.Itoa(ctls[i].CurrentHorizontalResolution) + "x" + strconv.Itoa(ctls[i].CurrentVerticalResolution),
				RefreshRate: refreshLabel(ctls[i].CurrentRefreshRate),
				Primary:     i == 0,
			}
			if mon.Name == "" {
				mon.Name = "Display " + strconv.Itoa(i+1)
			}
			monitors = append(monitors, mon)
		}
		return monitors
	}

	for i, s := range screens {
		if s.Width == 0 || s.Height == 0 {
			continue
		}
		monitors = append(monitors, types.MonitorInfo{
			Name:        identityName(active, i, "Display "+strconv.Itoa(i+1)),
			Resolution:  strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height),
			RefreshRate: refreshLabel(screenRefresh(s, adapter)),
			Primary:     s.Primary,
		})
	}
	return monitors
}

func refreshLabel(hz int) string {
	if hz <= 0 {
		return ""
	}
	return strconv.Itoa(hz) + "Hz"
}

// ---------------------------------------------------------------------------
// System event log
// ---------------------------------------------------------------------------

// eventLookbackDays bounds every System-log query.
const eventLookbackDays = 30

// noMatchingEventsFQID is the locale-independent FullyQualifiedErrorId prefix
// PowerShell attaches to the error record Get-WinEvent emits when the filter
// matches zero events (the full id is
// "NoMatchingEventsFound,Microsoft.PowerShell.Commands.GetWinEventCommand").
// eventQueryScript filters on this id rather than on the English exception
// text so a healthy machine on a non-English Windows UI culture is reported as
// "0 events" and not as "could not read the System log".
const noMatchingEventsFQID = "NoMatchingEventsFound"

// eventQueryScript builds a Get-WinEvent probe that always exits 0.
//
// Get-WinEvent -ErrorAction SilentlyContinue still makes powershell.exe exit 1
// when the filter matches nothing, which previously made every healthy machine
// look like it could not read the log. Errors other than "no events" (matched
// by noMatchingEventsFQID, with the English message kept as a secondary
// match) are echoed to stderr so Go can tell access-denied apart from a
// verified empty result. Messages are collapsed to one line (newlines -> " | ") and capped at
// 300 characters, which keeps the WHEA "Component:" and "Primary Device Name:"
// fields the analyzer needs. TimeCreated is emitted in round-trip ("o") UTC
// form so it parses unambiguously; the culture-formatted local string used
// before was read as UTC and landed hours off. Both the numeric Level and the
// localized LevelDisplayName are emitted: the analyzer matches on the English
// level names, so parseEventLines maps the number to canonical English and
// only falls back to the display name when the number is missing.
func eventQueryScript(filter string, maxEvents int) string {
	return `$ErrorActionPreference = 'SilentlyContinue'; ` +
		`$e = @(Get-WinEvent -FilterHashtable @{LogName='System'; ` + filter + `; StartTime=(Get-Date).AddDays(-` + strconv.Itoa(eventLookbackDays) + `)} -MaxEvents ` + strconv.Itoa(maxEvents) + `); ` +
		`$e | ForEach-Object { $m = ([string]$_.Message) -replace '\r?\n', ' | '; if ($m.Length -gt 300) { $m = $m.Substring(0, 300) }; ` +
		`"$($_.TimeCreated.ToUniversalTime().ToString('o'))|$($_.Id)|$($_.Level)|$($_.LevelDisplayName)|$m" }; ` +
		`$Error | Where-Object { $_.FullyQualifiedErrorId -notmatch '^` + noMatchingEventsFQID + `' -and $_.Exception.Message -notmatch 'No events were found' } | ForEach-Object { [Console]::Error.WriteLine($_.Exception.Message) }; ` +
		`exit 0`
}

// queryEventLog runs one System-log probe. A nil error with zero entries is a
// verified "0 events"; a non-nil error means the log could not be read.
func queryEventLog(timeout int, collector, filter string, maxEvents int) ([]types.EventLogEntry, *types.CollectorError) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", eventQueryScript(filter, maxEvents))
	if r.Err != nil {
		return nil, &types.CollectorError{Collector: collector, Error: "could not read the System log: " + r.Err.Error()}
	}
	if isAccessDenied(r.Stderr) {
		return nil, &types.CollectorError{Collector: collector, Error: "requires Administrator to read the System log"}
	}
	if s := firstLine(r.Stderr); s != "" {
		return nil, &types.CollectorError{Collector: collector, Error: "could not read the System log: " + s}
	}
	return parseEventLines(r.Stdout), nil
}

// isAccessDenied recognises the two phrasings Windows uses for a log the
// caller may not open.
func isAccessDenied(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "unauthorized") || strings.Contains(s, "access is denied")
}

func collectDriverResetEvents(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// Event ID 4101: "Display driver stopped responding and has recovered" (TDR).
	events, cerr := queryEventLog(timeout, "windows.event4101", "Id=4101", 50)
	info.DriverResetEvents = events
	if cerr != nil {
		*errs = append(*errs, *cerr)
	}
}

func collectNvlddmkmErrors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	events, cerr := queryEventLog(timeout, "windows.nvlddmkm", "ProviderName='nvlddmkm'", 50)
	info.NvlddmkmErrors = events
	if cerr != nil {
		*errs = append(*errs, *cerr)
	}
}

func collectWHEAErrors(info *types.WindowsInfo, errs *[]types.CollectorError, timeout int) {
	// The full message is kept (not a placeholder) because the analyzer must
	// separate corrected PCIe endpoint errors (Event ID 17, Warning, e.g. a
	// NIC) from fatal GPU faults, and the component/device live in the text.
	events, cerr := queryEventLog(timeout, "windows.whea", "ProviderName='Microsoft-Windows-WHEA-Logger'", 20)
	info.WHEAErrors = events
	if cerr != nil {
		*errs = append(*errs, *cerr)
	}
}

// eventLevelNames maps the numeric Windows event level to its canonical
// English name. LevelDisplayName is localized ("Warnung", "Avertissement"),
// which would defeat the analyzer's level matching on non-English Windows.
var eventLevelNames = map[int64]string{
	1: "Critical",
	2: "Error",
	3: "Warning",
	4: "Information",
	5: "Verbose",
}

// eventLevelName returns the canonical English level for a numeric level,
// falling back to the (possibly localized) display name when the number is
// absent or unknown.
func eventLevelName(numeric, display string) string {
	numeric = strings.TrimSpace(numeric)
	if numeric != "" {
		if name, ok := eventLevelNames[parseIntSafe(numeric)]; ok {
			return name
		}
	}
	return strings.TrimSpace(display)
}

// repeatedEventSeparatorRe matches two or more adjacent " | " separators,
// which eventQueryScript produces from blank lines inside a message.
var repeatedEventSeparatorRe = regexp.MustCompile(`( \| ){2,}`)

// cleanEventMessage collapses repeated separators (" |  | " -> " | ") and
// trims leading/trailing separators and whitespace, so a WHEA message reads
// "A corrected hardware error has occurred. | Component: PCI Express Endpoint | ...".
func cleanEventMessage(m string) string {
	m = repeatedEventSeparatorRe.ReplaceAllString(m, " | ")
	return strings.Trim(m, " |\t\r")
}

// parseEventLines parses "2026-09-01T18:33:15.9783373Z|17|3|Warning|message"
// lines (time, id, numeric level, level display name, message). The message
// may itself contain "|" (collapsed newlines), so the split is limited to five
// fields.
func parseEventLines(output string) []types.EventLogEntry {
	var events []types.EventLogEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 2 {
			continue
		}
		entry := types.EventLogEntry{
			Source:  "System",
			Time:    parseEventTime(parts[0]),
			EventID: int(parseIntSafe(parts[1])),
		}
		var numeric, display string
		if len(parts) >= 3 {
			numeric = parts[2]
		}
		if len(parts) >= 4 {
			display = parts[3]
		}
		entry.Level = eventLevelName(numeric, display)
		if len(parts) >= 5 {
			entry.Message = cleanEventMessage(parts[4])
		}
		events = append(events, entry)
	}
	return events
}

// parseEventTime expects the .NET round-trip ("o") format, which is
// RFC 3339 with seven fractional digits and a Z suffix.
func parseEventTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ---------------------------------------------------------------------------
// Windows updates
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Installed programs: NVIDIA App / GeForce Experience and overlay software
// ---------------------------------------------------------------------------

// installedProgram is one Uninstall registry entry.
type installedProgram struct {
	Name    string
	Version string
}

// installedProgramsScript lists DisplayName|DisplayVersion for machine-wide
// (64- and 32-bit) and per-user installs. HKCU matters because Discord and
// similar overlay apps install per user only.
const installedProgramsScript = `$ErrorActionPreference = 'SilentlyContinue'; ` +
	`Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*' | ` +
	`Where-Object { $_.DisplayName } | ForEach-Object { "$($_.DisplayName)|$($_.DisplayVersion)" }; exit 0`

func queryInstalledPrograms(timeout int, errs *[]types.CollectorError) []installedProgram {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", installedProgramsScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.programs",
			Error:     "could not enumerate installed programs: " + r.Err.Error(),
		})
		return nil
	}
	return parseInstalledPrograms(r.Stdout)
}

func parseInstalledPrograms(out string) []installedProgram {
	var programs []installedProgram
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		p := installedProgram{Name: strings.TrimSpace(parts[0])}
		if len(parts) == 2 {
			p.Version = strings.TrimSpace(parts[1])
		}
		if p.Name != "" {
			programs = append(programs, p)
		}
	}
	return programs
}

var trailingVersionRe = regexp.MustCompile(`\d+(?:\.\d+)+$`)

// nvidiaVersionsFromPrograms derives the NVIDIA App and GeForce Experience
// versions from their Uninstall entries. The dedicated registry key
// HKLM\SOFTWARE\NVIDIA Corporation\NVIDIA App does not exist on current
// NVIDIA App installs, whereas the Uninstall entry ("NVIDIA App 11.0.7.247")
// always does.
func nvidiaVersionsFromPrograms(programs []installedProgram) (app, gfe string) {
	for _, p := range programs {
		lower := strings.ToLower(strings.TrimSpace(p.Name))
		switch {
		case app == "" && (lower == "nvidia app" || strings.HasPrefix(lower, "nvidia app ")):
			app = programVersion(p)
		case gfe == "" && strings.HasPrefix(lower, "nvidia geforce experience"):
			gfe = programVersion(p)
		}
	}
	return app, gfe
}

// programVersion prefers DisplayVersion and falls back to a trailing
// dotted version in the DisplayName.
func programVersion(p installedProgram) string {
	if v := strings.TrimSpace(p.Version); v != "" {
		return v
	}
	return trailingVersionRe.FindString(strings.TrimSpace(p.Name))
}

func collectNVIDIAApp(info *types.WindowsInfo, programs []installedProgram, timeout int) {
	info.NVIDIAAppVersion, info.GFEVersion = nvidiaVersionsFromPrograms(programs)
	// Legacy fallback: vendor registry keys from older installers.
	if info.NVIDIAAppVersion == "" {
		info.NVIDIAAppVersion = registryVersion(timeout, `HKLM:\SOFTWARE\NVIDIA Corporation\NVIDIA App`)
	}
	if info.GFEVersion == "" {
		info.GFEVersion = registryVersion(timeout, `HKLM:\SOFTWARE\NVIDIA Corporation\Global\GFExperience`)
	}
}

func registryVersion(timeout int, key string) string {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty '`+key+`' -ErrorAction SilentlyContinue).Version`)
	if r.Err != nil {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

// overlaySignatures pairs a DisplayName substring with a report label. It is
// a slice, not a map, so the report order is stable across runs (compare
// would otherwise see spurious diffs).
var overlaySignatures = []struct {
	pattern string
	label   string
}{
	{"xbox game bar", "Xbox Game Bar"},
	{"discord", "Discord (may have overlay)"},
	{"msi afterburner", "MSI Afterburner"},
	{"rivatuner", "RivaTuner Statistics Server (RTSS)"},
	{"obs studio", "OBS Studio"},
	{"shadowplay", "NVIDIA ShadowPlay"},
	{"overwolf", "Overwolf"},
	{"medal", "Medal.tv"},
	{"action!", "Action! Screen Recorder"},
}

func detectOverlays(programs []installedProgram) []string {
	var found []string
	for _, sig := range overlaySignatures {
		for _, p := range programs {
			if strings.Contains(strings.ToLower(p.Name), sig.pattern) {
				found = append(found, sig.label)
				break
			}
		}
	}
	return found
}

func collectOverlaySoftware(info *types.WindowsInfo, programs []installedProgram, timeout int) {
	info.OverlaySoftware = detectOverlays(programs)

	// Xbox Game Bar is an Appx package, so it has no Uninstall entry.
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-AppxPackage -Name Microsoft.XboxGamingOverlay -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Version`)
	if r.Err == nil && strings.TrimSpace(r.Stdout) != "" && !containsSubstring(info.OverlaySoftware, "Xbox") {
		info.OverlaySoftware = append(info.OverlaySoftware, "Xbox Game Bar (v"+strings.TrimSpace(r.Stdout)+")")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
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
