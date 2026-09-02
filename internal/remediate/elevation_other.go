//go:build !windows

package remediate

import "os"

// IsElevated reports whether the current process runs as root. Remediation
// actions on Linux write under /etc and run initramfs tools, which need
// effective UID 0.
func IsElevated() bool {
	return os.Geteuid() == 0
}
