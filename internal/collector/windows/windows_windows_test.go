//go:build windows

package windows

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Captured from Get-WinEvent on a machine with a corrected PCIe NIC error
// (time|id|numeric level|display name|message; blank message lines show up
// as doubled " |  | " separators).
const wheaSampleLine = `2026-09-01T18:33:15.9783373Z|17|3|Warning|A corrected hardware error has occurred. |  | Component: PCI Express Endpoint | Error Source: Generic |  | Primary Bus:Device:Function: 0x1:0x0:0x0 | Secondary Bus:Device:Function: 0x0:0x0:0x0 | Primary Device Name:PCI\VEN_1D6A&DEV_07B1&SUBSYS_104617AA&REV_02 | Secondary Device Name:PCI\VEN_1022&DEV`

func TestParseEventLinesISO(t *testing.T) {
	events := parseEventLines(wheaSampleLine + "\n\n")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.EventID != 17 {
		t.Errorf("EventID = %d, want 17", e.EventID)
	}
	if e.Level != "Warning" {
		t.Errorf("Level = %q, want Warning", e.Level)
	}
	if e.Source != "System" {
		t.Errorf("Source = %q, want System", e.Source)
	}
	want := time.Date(2026, 9, 1, 18, 33, 15, 978337300, time.UTC)
	if !e.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", e.Time, want)
	}
	for _, needle := range []string{"Component: PCI Express Endpoint", `Primary Device Name:PCI\VEN_1D6A&DEV_07B1`} {
		if !containsSubstring([]string{e.Message}, needle) {
			t.Errorf("Message missing %q: %q", needle, e.Message)
		}
	}
	if containsSubstring([]string{e.Message}, " |  | ") {
		t.Errorf("Message still carries doubled separators: %q", e.Message)
	}
	const wantPrefix = "A corrected hardware error has occurred. | Component: PCI Express Endpoint | Error Source: Generic | Primary Bus:Device:Function: 0x1:0x0:0x0"
	if !strings.HasPrefix(e.Message, wantPrefix) {
		t.Errorf("Message = %q, want prefix %q", e.Message, wantPrefix)
	}
}

func TestParseEventLinesLocalizedLevel(t *testing.T) {
	// de-DE Windows: numeric level 3 with display name "Warnung" must come out
	// as the canonical "Warning" the analyzer matches on.
	events := parseEventLines("2026-09-01T18:33:15.9783373Z|17|3|Warnung|Ein korrigierter Hardwarefehler ist aufgetreten. |  | Komponente: PCI Express-Endpunkt |  | \n")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Level != "Warning" {
		t.Errorf("Level = %q, want Warning (from numeric level 3)", e.Level)
	}
	if want := "Ein korrigierter Hardwarefehler ist aufgetreten. | Komponente: PCI Express-Endpunkt"; e.Message != want {
		t.Errorf("Message = %q, want %q", e.Message, want)
	}

	// Every numeric level maps to English regardless of the display name.
	levels := map[string]string{"1": "Critical", "2": "Error", "3": "Warning", "4": "Information", "5": "Verbose"}
	for num, want := range levels {
		if got := eventLevelName(num, "Localized"); got != want {
			t.Errorf("eventLevelName(%q) = %q, want %q", num, got, want)
		}
	}
	// Missing or unknown numeric level falls back to the display name.
	if got := eventLevelName("", "Warnung"); got != "Warnung" {
		t.Errorf("empty numeric level should fall back to display name, got %q", got)
	}
	if got := eventLevelName("0", "LogAlways"); got != "LogAlways" {
		t.Errorf("unknown numeric level should fall back to display name, got %q", got)
	}
}

func TestCleanEventMessage(t *testing.T) {
	cases := map[string]string{
		"A. |  | B |  |  | C": "A. | B | C",
		"A | B | ":            "A | B",
		"A | B |":             "A | B",
		" | A":                "A",
		"Primary Bus:Device:Function: 0x1:0x0:0x0": "Primary Bus:Device:Function: 0x1:0x0:0x0",
		"": "",
	}
	for in, want := range cases {
		if got := cleanEventMessage(in); got != want {
			t.Errorf("cleanEventMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEventLinesEmptyIsZeroEvents(t *testing.T) {
	if got := parseEventLines(""); len(got) != 0 {
		t.Errorf("expected no events, got %d", len(got))
	}
}

func TestIsAccessDenied(t *testing.T) {
	cases := map[string]bool{
		"Attempted to perform an unauthorized operation.":                   true,
		"Access is denied. (Exception from HRESULT: 0x80070005)":            true,
		"No events were found that match the specified selection criteria.": false,
		"": false,
	}
	for in, want := range cases {
		if got := isAccessDenied(in); got != want {
			t.Errorf("isAccessDenied(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestInterpretRegistryProbe(t *testing.T) {
	if p := interpretRegistryProbe("__ABSENT__\r\n", "", nil); !p.Absent || p.Err != "" {
		t.Errorf("absent sentinel not recognised: %+v", p)
	}
	if p := interpretRegistryProbe("2\n", "", nil); p.Value != "2" || p.Absent {
		t.Errorf("value not parsed: %+v", p)
	}
	if p := interpretRegistryProbe("ERROR: Requested registry access is not allowed.", "", nil); p.Err == "" || p.Absent {
		t.Errorf("script error not recognised: %+v", p)
	}
	if p := interpretRegistryProbe("", "boom", errors.New("exit status 1")); p.Err != "exit status 1: boom" {
		t.Errorf("run error not surfaced: %+v", p)
	}
}

func TestDescribeToggle(t *testing.T) {
	cases := []struct {
		probe registryProbe
		want  string
	}{
		{registryProbe{Absent: true}, "Default (not configured)"},
		{registryProbe{Value: "2"}, "Enabled"},
		{registryProbe{Value: "1"}, "Disabled"},
		{registryProbe{Value: "7"}, "Unknown (value 7)"},
		{registryProbe{Err: "denied"}, "Unknown"},
	}
	for _, c := range cases {
		if got := describeToggle(c.probe, "2", "1"); got != c.want {
			t.Errorf("describeToggle(%+v) = %q, want %q", c.probe, got, c.want)
		}
	}
}

func TestEventQueryScriptFiltersZeroEventsByFQID(t *testing.T) {
	// The "no events" record must be dropped by its locale-independent
	// FullyQualifiedErrorId, not only by the English message text, so a
	// healthy non-English machine is reported as 0 events rather than as
	// "could not read the System log".
	script := eventQueryScript("Id=4101", 50)
	if !containsSubstring([]string{script}, "|$($_.Level)|$($_.LevelDisplayName)|") {
		t.Errorf("script must emit the numeric Level ahead of the localized display name:\n%s", script)
	}
	if !containsSubstring([]string{script}, "$_.FullyQualifiedErrorId -notmatch '^"+noMatchingEventsFQID+"'") {
		t.Errorf("script does not filter on FullyQualifiedErrorId %q:\n%s", noMatchingEventsFQID, script)
	}
	if noMatchingEventsFQID != "NoMatchingEventsFound" {
		t.Errorf("FQID constant drifted from PowerShell's GetWinEventCommand id: %q", noMatchingEventsFQID)
	}
	if !containsSubstring([]string{script}, "exit 0") || !containsSubstring([]string{script}, "$ErrorActionPreference = 'SilentlyContinue'") {
		t.Errorf("script must always exit 0 with SilentlyContinue:\n%s", script)
	}
}

func TestParsePowerPlanScheme(t *testing.T) {
	name, guid := parsePowerPlanScheme("Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)\r\n")
	if name != "Balanced" || guid != "381b4222-f694-41f0-9685-ff5bb260df2e" {
		t.Errorf("got name=%q guid=%q", name, guid)
	}
	// Localized powercfg does not print the literal "GUID:" token before the
	// GUID; the parser must still find it and the label must canonicalise.
	localized := []struct{ line, wantName string }{
		{"GUID des Energieschemas: 381b4222-f694-41f0-9685-ff5bb260df2e  (Ausbalanciert)", "Ausbalanciert"},
		{"GUID du mode de gestion de l'alimentation : 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c  (Performances élevées)", "Performances élevées"},
	}
	wantCanonical := []string{"Balanced", "High performance"}
	for i, c := range localized {
		n, g := parsePowerPlanScheme(c.line + "\r\n")
		if n != c.wantName || g == "" {
			t.Errorf("localized parse of %q: name=%q guid=%q", c.line, n, g)
		}
		if got := powerPlanLabel(n, g); got != wantCanonical[i] {
			t.Errorf("localized label for %q = %q, want %q", c.line, got, wantCanonical[i])
		}
	}
	if _, g := parsePowerPlanScheme("Power Scheme GUID: 381B4222-F694-41F0-9685-FF5BB260DF2E  (Balanced)"); g != "381b4222-f694-41f0-9685-ff5bb260df2e" {
		t.Errorf("GUID not lower-cased: %q", g)
	}
	// Names may themselves contain parentheses and must survive whole.
	name, _ = parsePowerPlanScheme("Power Scheme GUID: 11111111-2222-3333-4444-555555555555  (My plan (custom))")
	if name != "My plan (custom)" {
		t.Errorf("nested-parentheses parse got %q", name)
	}
	if n, g := parsePowerPlanScheme("Invalid Parameters -- try \"/?\" for help"); n != "" || g != "" {
		t.Errorf("expected empty result on error text, got %q %q", n, g)
	}
}

func TestPowerPlanLabel(t *testing.T) {
	if got := powerPlanLabel("Ausbalanciert", "381B4222-F694-41F0-9685-FF5BB260DF2E"); got != "Balanced" {
		t.Errorf("localized built-in plan not canonicalised: %q", got)
	}
	if got := powerPlanLabel("Custom", "11111111-2222-3333-4444-555555555555"); got != "Custom" {
		t.Errorf("custom plan renamed: %q", got)
	}
}

func TestNvidiaVersionsFromPrograms(t *testing.T) {
	programs := parseInstalledPrograms("NVIDIA Graphics Driver 591.86|591.86\nNVIDIA App 11.0.7.247|11.0.7.247\nNVIDIA Backend|11.0.7.247\nNVIDIA GeForce Experience 3.28.0.417|\n")
	app, gfe := nvidiaVersionsFromPrograms(programs)
	if app != "11.0.7.247" {
		t.Errorf("NVIDIA App version = %q", app)
	}
	if gfe != "3.28.0.417" {
		t.Errorf("GFE version (from name fallback) = %q", gfe)
	}
	if app, gfe := nvidiaVersionsFromPrograms(nil); app != "" || gfe != "" {
		t.Errorf("expected empty versions, got %q %q", app, gfe)
	}
}

func TestDetectOverlaysStableOrder(t *testing.T) {
	programs := parseInstalledPrograms("OBS Studio|30.0\nDiscord|1.0.9\nNVIDIA ShadowPlay 11.0.7.0|11.0.7.0\n")
	want := []string{"Discord (may have overlay)", "OBS Studio", "NVIDIA ShadowPlay"}
	if got := detectOverlays(programs); !reflect.DeepEqual(got, want) {
		t.Errorf("detectOverlays = %v, want %v", got, want)
	}
}

func TestParseMonitorIdentityLines(t *testing.T) {
	idents := parseMonitorIdentityLines("DEL|DELL U2720Q|True|10\r\nGSM|LG ULTRAGEAR|False|5\n")
	if len(idents) != 2 {
		t.Fatalf("expected 2 identities, got %d", len(idents))
	}
	if friendlyMonitorName(idents[0]) != "DEL DELL U2720Q" || idents[0].OutputType != "DP" || !idents[0].Active {
		t.Errorf("first identity wrong: %+v", idents[0])
	}
	if idents[1].Active || idents[1].OutputType != "HDMI" {
		t.Errorf("second identity wrong: %+v", idents[1])
	}
	if got := len(activeIdentities(idents)); got != 1 {
		t.Errorf("activeIdentities = %d, want 1", got)
	}
}

func TestParseScreensJSON(t *testing.T) {
	screens, err := parseScreensJSON(`[{"DeviceName":"\\\\.\\DISPLAY1","Primary":false,"W":3840,"H":2160},{"DeviceName":"\\\\.\\DISPLAY2","Primary":true,"W":3840,"H":2160}]`)
	if err != nil || len(screens) != 2 || !screens[1].Primary || screens[0].Width != 3840 {
		t.Fatalf("array parse: err=%v screens=%+v", err, screens)
	}
	screens, err = parseScreensJSON(`{"DeviceName":"\\\\.\\DISPLAY1","Primary":true,"W":2560,"H":1440}`)
	if err != nil || len(screens) != 1 || screens[0].Height != 1440 || screens[0].RefreshHz != 0 {
		t.Fatalf("single-object parse (no Hz field): err=%v screens=%+v", err, screens)
	}
	// Captured from screensScript with the EnumDisplaySettings P/Invoke.
	screens, err = parseScreensJSON(`[{"DeviceName":"\\\\.\\DISPLAY1","Primary":false,"W":3840,"H":2160,"Hz":60},{"DeviceName":"\\\\.\\DISPLAY2","Primary":true,"W":2560,"H":1440,"Hz":144}]`)
	if err != nil || len(screens) != 2 || screens[0].RefreshHz != 60 || screens[1].RefreshHz != 144 {
		t.Fatalf("per-monitor Hz parse: err=%v screens=%+v", err, screens)
	}
	if screens, err := parseScreensJSON(""); err != nil || len(screens) != 0 {
		t.Errorf("empty input: err=%v screens=%+v", err, screens)
	}
}

func TestParseVideoControllerJSONNullFields(t *testing.T) {
	ctls, err := parseVideoControllerJSON(`{"Name":"NVIDIA GeForce RTX 3090","CurrentHorizontalResolution":null,"CurrentVerticalResolution":null,"CurrentRefreshRate":59,"AdapterCompatibility":"NVIDIA"}`)
	if err != nil || len(ctls) != 1 {
		t.Fatalf("err=%v ctls=%+v", err, ctls)
	}
	if ctls[0].CurrentHorizontalResolution != 0 || ctls[0].CurrentRefreshRate != 59 {
		t.Errorf("unexpected controller: %+v", ctls[0])
	}
}

func sampleDisplayProbes() ([]screenInfo, []monitorIdentity, []wmiVideoController) {
	screens := []screenInfo{
		{DeviceName: `\\.\DISPLAY1`, Primary: false, Width: 3840, Height: 2160},
		{DeviceName: `\\.\DISPLAY2`, Primary: true, Width: 3840, Height: 2160},
	}
	idents := parseMonitorIdentityLines("DEL|DELL U2720Q|True|10\nDEL|DELL U2720Q|True|10\n")
	ctls := []wmiVideoController{{Name: "NVIDIA GeForce RTX 3090", CurrentHorizontalResolution: 3840, CurrentVerticalResolution: 2160, CurrentRefreshRate: 59, AdapterCompatibility: "NVIDIA"}}
	return screens, idents, ctls
}

func TestBuildDisplaysOnePerMonitor(t *testing.T) {
	displays := buildDisplays(sampleDisplayProbes())
	if len(displays) != 2 {
		t.Fatalf("expected 2 displays for a 1-GPU/2-monitor machine, got %d", len(displays))
	}
	for i, d := range displays {
		if d.Name != "DEL DELL U2720Q" || d.Resolution != "3840x2160" || d.RefreshHz != 59 || d.OutputType != "DP" || d.GPUIndex != 0 {
			t.Errorf("display %d unexpected: %+v", i, d)
		}
		if d.HDREnabled || d.VRREnabled || d.HDRCapable {
			t.Errorf("display %d claims HDR/VRR state without a source: %+v", i, d)
		}
	}
	if displays[0].Primary || !displays[1].Primary {
		t.Errorf("primary flags wrong: %+v", displays)
	}
}

func TestBuildDisplaysPerMonitorRefresh(t *testing.T) {
	screens, idents, ctls := sampleDisplayProbes()
	screens[0].RefreshHz = 60
	screens[1].RefreshHz = 144
	displays := buildDisplays(screens, idents, ctls)
	if len(displays) != 2 || displays[0].RefreshHz != 60 || displays[1].RefreshHz != 144 {
		t.Fatalf("mixed refresh rates not preserved per monitor: %+v", displays)
	}
	monitors := buildMonitors(screens, idents, ctls)
	if len(monitors) != 2 || monitors[0].RefreshRate != "60Hz" || monitors[1].RefreshRate != "144Hz" {
		t.Errorf("MonitorInfo refresh not per monitor: %+v", monitors)
	}
}

func TestScreenRefreshFallsBackToAdapter(t *testing.T) {
	adapter := wmiVideoController{CurrentRefreshRate: 59}
	if got := screenRefresh(screenInfo{RefreshHz: 0}, adapter); got != 59 {
		t.Errorf("Hz 0 should fall back to adapter, got %d", got)
	}
	if got := screenRefresh(screenInfo{RefreshHz: 120}, adapter); got != 120 {
		t.Errorf("per-screen Hz should win, got %d", got)
	}
}

func TestScreensScriptDeclaresEnumDisplaySettings(t *testing.T) {
	for _, needle := range []string{"SetProcessDPIAware", "EnumDisplaySettings", "RefreshHz", "@{n='Hz'"} {
		if !containsSubstring([]string{screensScript}, needle) {
			t.Errorf("screensScript missing %q", needle)
		}
	}
	// The C# is embedded in a single-quoted PowerShell literal.
	if containsSubstring([]string{nvcDpiTypeDefinition}, "'") {
		t.Errorf("nvcDpiTypeDefinition contains a single quote, which would end the PowerShell literal")
	}
}

func TestBuildDisplaysFallbackWithoutScreens(t *testing.T) {
	_, idents, ctls := sampleDisplayProbes()
	displays := buildDisplays(nil, idents, ctls)
	if len(displays) != 1 || displays[0].Resolution != "3840x2160" || displays[0].Name != "DEL DELL U2720Q" {
		t.Errorf("fallback displays = %+v", displays)
	}
}

func TestBuildMonitors(t *testing.T) {
	monitors := buildMonitors(sampleDisplayProbes())
	if len(monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(monitors))
	}
	for _, m := range monitors {
		if m.Name != "DEL DELL U2720Q" || m.Resolution != "3840x2160" || m.RefreshRate != "59Hz" {
			t.Errorf("unexpected monitor: %+v", m)
		}
		if containsSubstring([]string{m.Name}, `DISPLAY\`) {
			t.Errorf("monitor name leaks a PnP instance path: %q", m.Name)
		}
	}
	if !monitors[1].Primary || monitors[0].Primary {
		t.Errorf("primary flags wrong: %+v", monitors)
	}
}

func TestBuildMonitorsSkipsEmptyResolution(t *testing.T) {
	_, idents, _ := sampleDisplayProbes()
	// No screens and a headless adapter: nothing has a resolution, so nothing is emitted.
	monitors := buildMonitors(nil, idents, []wmiVideoController{{Name: "NVIDIA GeForce RTX 3090", AdapterCompatibility: "NVIDIA"}})
	if len(monitors) != 0 {
		t.Errorf("expected no monitors without a resolution, got %+v", monitors)
	}
}

func TestPickAdapterReturnsChosenOrdinal(t *testing.T) {
	intel := wmiVideoController{Name: "Intel(R) UHD Graphics 770", AdapterCompatibility: "Intel Corporation", CurrentRefreshRate: 60}
	nvidia := wmiVideoController{Name: "NVIDIA GeForce RTX 3090", AdapterCompatibility: "NVIDIA", CurrentRefreshRate: 144}
	idle := wmiVideoController{Name: "Microsoft Basic Display Adapter", AdapterCompatibility: "Microsoft"}

	if ctl, idx := pickAdapter([]wmiVideoController{intel, nvidia}); idx != 1 || ctl.Name != nvidia.Name {
		t.Errorf("NVIDIA at position 1: got idx=%d ctl=%q", idx, ctl.Name)
	}
	if ctl, idx := pickAdapter([]wmiVideoController{idle, intel}); idx != 1 || ctl.Name != intel.Name {
		t.Errorf("first adapter with a mode set at position 1: got idx=%d ctl=%q", idx, ctl.Name)
	}
	if ctl, idx := pickAdapter([]wmiVideoController{idle}); idx != 0 || ctl.Name != idle.Name {
		t.Errorf("single headless adapter: got idx=%d ctl=%q", idx, ctl.Name)
	}
	if _, idx := pickAdapter(nil); idx != 0 {
		t.Errorf("no adapters: got idx=%d", idx)
	}

	// The chosen ordinal must flow through to every display's GPUIndex.
	screens := []screenInfo{{DeviceName: `\\.\DISPLAY1`, Primary: true, Width: 2560, Height: 1440, RefreshHz: 144}}
	displays := buildDisplays(screens, nil, []wmiVideoController{intel, nvidia})
	if len(displays) != 1 || displays[0].GPUIndex != 1 {
		t.Errorf("display GPUIndex should be the chosen adapter ordinal 1: %+v", displays)
	}
}
