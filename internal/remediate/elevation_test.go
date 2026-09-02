package remediate

import "testing"

// Trimmed real output of "whoami /groups" from an elevated and a normal prompt.
const (
	whoamiElevated = "GROUP INFORMATION\n-----------------\n\nGroup Name                           Type             SID          Attributes\n" +
		"BUILTIN\\Administrators               Alias            S-1-5-32-544 Enabled by default, Enabled group, Group owner\n" +
		"Mandatory Label\\High Mandatory Level Label            S-1-16-12288\n"
	whoamiNormal = "GROUP INFORMATION\n-----------------\n\n" +
		"BUILTIN\\Administrators                 Alias            S-1-5-32-544 Group used for deny only\n" +
		"Mandatory Label\\Medium Mandatory Level Label            S-1-16-8192\n"
	whoamiSystem = "NT AUTHORITY\\SYSTEM  Well-known group  S-1-5-18\nMandatory Label\\System Mandatory Level Label S-1-16-16384\n"
)

func TestIsElevatedFromWhoamiGroups(t *testing.T) {
	if !isElevatedFromWhoamiGroups(whoamiElevated) {
		t.Error("High integrity level should count as elevated")
	}
	if !isElevatedFromWhoamiGroups(whoamiSystem) {
		t.Error("System integrity level should count as elevated")
	}
	if isElevatedFromWhoamiGroups(whoamiNormal) {
		t.Error("Medium integrity level (admin account, not elevated) must not count as elevated")
	}
	if isElevatedFromWhoamiGroups("") {
		t.Error("empty output must not count as elevated")
	}
}

func TestElevationCheckDefaultsToIsElevated(t *testing.T) {
	// Smoke test: the real check must not panic and must be deterministic.
	if elevationCheck() != IsElevated() {
		t.Error("elevationCheck should default to IsElevated")
	}
}
