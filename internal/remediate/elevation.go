package remediate

import (
	"path/filepath"
	"strings"
)

// Windows integrity-level SIDs reported by "whoami /groups". A process running
// at High (elevated administrator) or System integrity can write HKLM and
// change the power plan; Medium (a normal user, even an admin account that
// has not elevated) cannot.
const (
	sidHighIntegrity   = "S-1-16-12288"
	sidSystemIntegrity = "S-1-16-16384"
)

// isElevatedFromWhoamiGroups parses the output of "whoami /groups" and reports
// whether it lists a High or System mandatory integrity level. It is a pure
// function so it can be unit-tested on any OS.
func isElevatedFromWhoamiGroups(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, sidHighIntegrity) || strings.Contains(line, sidSystemIntegrity) {
			return true
		}
	}
	return false
}

// systemBinary returns the explicit path of a native Windows tool under
// <systemRoot>\System32, so it cannot be shadowed by a same-named program
// earlier on PATH (Git's coreutils whoami.exe, for example). When systemRoot
// is empty the bare name is returned and normal PATH lookup applies.
func systemBinary(systemRoot, name string) string {
	if systemRoot == "" {
		return name
	}
	return filepath.Join(systemRoot, "System32", name)
}
