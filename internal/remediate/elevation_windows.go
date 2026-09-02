//go:build windows

package remediate

import "os/exec"

// IsElevated reports whether the current process runs with administrative
// rights. It first inspects the token integrity level via "whoami /groups"
// (works even when the Server service is stopped), then falls back to
// "net session", which exits 0 only for elevated processes.
func IsElevated() bool {
	if out, err := exec.Command("whoami", "/groups").CombinedOutput(); err == nil {
		return isElevatedFromWhoamiGroups(string(out))
	}
	return exec.Command("net", "session").Run() == nil
}
