package remediate

import (
	"regexp"
	"strconv"
	"strings"
)

// This file holds the OS-independent parsers for the output of the Windows
// tools the remediation actions drive (powercfg, reg.exe), so they can be
// unit-tested on any platform. The actions themselves live in
// actions_windows.go.

// guidAnywherePattern finds a GUID (8-4-4-4-12 hex digits) anywhere in a
// string. Unlike guidPattern it is not anchored, so it can pull the scheme
// GUID out of localized powercfg output whose labels are not in English.
var guidAnywherePattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// parsePowerSchemeGUID extracts the power scheme GUID from the output of
// "powercfg /getactivescheme". English output looks like
//
//	Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced)
//
// but the label is localized ("GUID des Energieschemas: ..."), so the whole
// output is scanned for the first GUID instead of relying on a marker. The
// GUID is returned lower-cased; "" when none is present.
func parsePowerSchemeGUID(output string) string {
	m := guidAnywherePattern.FindString(output)
	if m == "" {
		return ""
	}
	return strings.ToLower(m)
}

// parsePowerSchemeName extracts the parenthesised plan name, e.g. "(Balanced)",
// from "powercfg /getactivescheme" output. This is best-effort display text
// only (the GUID is what gets recorded); returns "" when absent.
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
// The value name is matched case-insensitively because registry names are
// case-insensitive and reg.exe echoes whatever casing the key stores.
// reg.exe prints DWORDs in hex; converting to decimal keeps the recorded value
// unambiguous ("0x10" must not be replayed as decimal 10).
func parseRegDwordValue(output, valueName string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.EqualFold(fields[0], valueName) {
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
