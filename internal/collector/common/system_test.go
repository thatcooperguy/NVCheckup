package common

import (
	"testing"
	"time"
)

func TestFormatTimezone(t *testing.T) {
	cdt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	if got := formatTimezone(cdt); got != "CDT (UTC-05:00)" {
		t.Errorf("formatTimezone(CDT) = %q", got)
	}
	ist := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("IST", 5*3600+30*60))
	if got := formatTimezone(ist); got != "IST (UTC+05:30)" {
		t.Errorf("formatTimezone(IST) = %q", got)
	}
	// A named location whose abbreviation differs is shown with both.
	local := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("Local", 3600))
	if got := formatTimezone(local); got != "Local (UTC+01:00)" {
		t.Errorf("formatTimezone(Local) = %q", got)
	}
	if got := formatTimezone(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "UTC (UTC+00:00)" {
		t.Errorf("formatTimezone(UTC) = %q", got)
	}
}

func TestParseSecureBootProbe(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		secureBoot string
		bootMode   string
	}{
		{"cmdlet elevated true", "cmdlet=True\nregistry=1\nfirmware=UEFI\n", "Enabled", "UEFI"},
		{"cmdlet elevated false", "cmdlet=False\r\nregistry=0\r\nfirmware=UEFI\r\n", "Disabled", "UEFI"},
		{"non-elevated registry disabled", "cmdlet=Unknown\nregistry=0\nfirmware=UEFI\n", "Disabled", "UEFI"},
		{"non-elevated registry enabled", "cmdlet=Unknown\nregistry=1\nfirmware=UEFI\n", "Enabled", "UEFI"},
		{"legacy bios", "cmdlet=Unknown\nregistry=__ABSENT__\nfirmware=Legacy\n", "Not supported/Legacy", "Legacy/BIOS"},
		{"registry present but no firmware env", "cmdlet=Unknown\nregistry=1\nfirmware=\n", "Enabled", "UEFI"},
		{"nothing available", "cmdlet=Unknown\nregistry=__ABSENT__\nfirmware=\n", "Not supported/Legacy", "Unknown"},
		{"garbage", "", "Unknown", "Unknown"},
	}
	for _, c := range cases {
		sb, bm := parseSecureBootProbe(c.out)
		if sb != c.secureBoot || bm != c.bootMode {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, sb, bm, c.secureBoot, c.bootMode)
		}
	}
}
