//go:build !linux

package util

import (
	"os/exec"
	"runtime"
	"strconv"
)

// configureProcessTreeKill makes the context Cancel hook take down the whole
// process tree on Windows: killing only the direct child leaves its children
// (a shell's ping, PowerShell's native helpers) running and holding our
// pipes. taskkill /T walks the tree; fall back to Kill if taskkill itself
// fails. Other platforms keep the default (kill the child only).
func configureProcessTreeKill(cmd *exec.Cmd) {
	if runtime.GOOS != "windows" {
		return
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		if err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
