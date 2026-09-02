//go:build linux

package util

import (
	"os/exec"
	"syscall"
)

// configureProcessTreeKill puts the child in its own process group and makes
// the context Cancel hook kill that whole group. Without it a timed-out
// "sh -c ..." is killed but its forked grandchild keeps the stdout pipe open,
// and Run only returns once WaitDelay expires (observed as multi-second
// overruns under the -race CI leg). Falls back to killing just the child if
// the group kill fails.
func configureProcessTreeKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
