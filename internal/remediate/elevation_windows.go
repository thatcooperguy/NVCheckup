//go:build windows

package remediate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// elevationCheckTimeout bounds each probe: whoami and net normally return in
// well under a second, and a hung probe must not stall "fix" forever. On
// timeout the process is treated as not elevated (the safe answer).
const elevationCheckTimeout = 10 * time.Second

// IsElevated reports whether the current process runs with administrative
// rights. It first inspects the token integrity level via "whoami /groups"
// (works even when the Server service is stopped), then falls back to
// "net session", which exits 0 only for elevated processes.
//
// Both tools are resolved under %SystemRoot%\System32 explicitly: from MSYS or
// Git Bash shells PATH resolves "whoami" to Git's coreutils whoami.exe, which
// does not understand "/groups", so a PATH lookup would silently fall through.
//
// Each probe runs under elevationCheckTimeout; a timeout counts as a failed
// probe (and, for the last probe, as not elevated).
func IsElevated() bool {
	whoami := systemBinary(os.Getenv("SystemRoot"), "whoami.exe")
	if out, err := runWithTimeout(whoami, "/groups"); err == nil {
		return isElevatedFromWhoamiGroups(string(out))
	}
	net := systemBinary(os.Getenv("SystemRoot"), "net.exe")
	_, err := runWithTimeout(net, "session")
	return err == nil
}

// runWithTimeout runs a probe command bounded by elevationCheckTimeout and
// returns its combined output; a timeout is reported as an error.
func runWithTimeout(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), elevationCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s timed out after %s", name, elevationCheckTimeout)
	}
	return out, err
}
