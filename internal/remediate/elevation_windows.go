//go:build windows

package remediate

import (
	"os"
	"os/exec"
)

// IsElevated reports whether the current process runs with administrative
// rights. It first inspects the token integrity level via "whoami /groups"
// (works even when the Server service is stopped), then falls back to
// "net session", which exits 0 only for elevated processes.
//
// Both tools are resolved under %SystemRoot%\System32 explicitly: from MSYS or
// Git Bash shells PATH resolves "whoami" to Git's coreutils whoami.exe, which
// does not understand "/groups", so a PATH lookup would silently fall through.
func IsElevated() bool {
	whoami := systemBinary(os.Getenv("SystemRoot"), "whoami.exe")
	if out, err := exec.Command(whoami, "/groups").CombinedOutput(); err == nil {
		return isElevatedFromWhoamiGroups(string(out))
	}
	net := systemBinary(os.Getenv("SystemRoot"), "net.exe")
	return exec.Command(net, "session").Run() == nil
}
