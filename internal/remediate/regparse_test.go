package remediate

import "testing"

func TestParseRegDwordValue(t *testing.T) {
	const hagsQuery = "\r\nHKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\GraphicsDrivers\r\n    HwSchMode    REG_DWORD    0x2\r\n"
	cases := map[string]string{
		hagsQuery:                                   "2",
		"    HwSchMode    REG_DWORD    0x10":        "16",
		"    HwSchMode    REG_DWORD    0xffffffff":  "4294967295",
		"    HwSchMode    REG_DWORD    7":           "7",
		"    hwschmode    REG_DWORD    0x2":         "2", // registry names are case-insensitive
		"    HWSCHMODE    REG_DWORD    0x1":         "1",
		"    HwSchModeX   REG_DWORD    0x2":         "",
		"    HwSchMode    REG_DWORD    0xZZ":        "",
		"    HwSchMode    REG_DWORD    0x100000000": "",
		"": "",
	}
	for in, want := range cases {
		if got := parseRegDwordValue(in, "HwSchMode"); got != want {
			t.Errorf("parseRegDwordValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePowerSchemeGUID(t *testing.T) {
	cases := map[string]string{
		// English (Windows 11, captured).
		"Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)": "381b4222-f694-41f0-9685-ff5bb260df2e",
		// Upper-case hex is normalised.
		"Power Scheme GUID: 8C5E7FDA-E8BF-4A96-9A85-A6E23A8C635C  (High performance)": highPerformanceGUID,
		// Localized labels: no "GUID: " marker in the English position.
		"GUID des Energieschemas: 381b4222-f694-41f0-9685-ff5bb260df2e  (Ausbalanciert)":                         "381b4222-f694-41f0-9685-ff5bb260df2e",
		"GUID du mode de gestion de l'alimentation : a1841308-3541-4fab-bc81-f71556f20b4a  (Economie d'energie)": "a1841308-3541-4fab-bc81-f71556f20b4a",
		"電源設定の GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (バランス)":                                               "381b4222-f694-41f0-9685-ff5bb260df2e",
		// Surrounding whitespace / CRLF.
		"\r\nPower Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)\r\n": "381b4222-f694-41f0-9685-ff5bb260df2e",
		// Rejections.
		"Power Scheme GUID: not-a-guid (x)":   "",
		"381b4222-f694-41f0-9685-ff5bb260df2": "", // one digit short
		"381b4222f69441f09685ff5bb260df2e":    "", // no dashes
		"":                                    "",
	}
	for in, want := range cases {
		if got := parsePowerSchemeGUID(in); got != want {
			t.Errorf("parsePowerSchemeGUID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePowerSchemeName(t *testing.T) {
	cases := map[string]string{
		"Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)":            "(Balanced)",
		"GUID des Energieschemas: 381b4222-f694-41f0-9685-ff5bb260df2e  (Ausbalanciert)": "(Ausbalanciert)",
		"Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e":                        "",
		"": "",
	}
	for in, want := range cases {
		if got := parsePowerSchemeName(in); got != want {
			t.Errorf("parsePowerSchemeName(%q) = %q, want %q", in, got, want)
		}
	}
}
