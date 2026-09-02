package linux

import (
	"os"
	"strings"
)

// simRootEnv names the fixture root of the simulated GB10 scenario
// (docs/roadmap/spark-support.md section 10): when set, every absolute file
// path a collector reads is prefixed with it, while commands keep resolving
// through PATH so shims can answer.
const simRootEnv = "NVC_SIM_ROOT"

// simPath prefixes an absolute path with NVC_SIM_ROOT when the variable is
// set. It is deliberately tiny and local to this package; the integrator may
// replace it with the shared helper from internal/collector/common.
func simPath(p string) string {
	root := os.Getenv(simRootEnv)
	if root == "" {
		return p
	}
	return strings.TrimRight(root, "/") + p
}

// readSimFile reads an absolute path through simPath and returns its trimmed
// contents, or "" when the file is missing or unreadable.
func readSimFile(p string) string {
	data, err := os.ReadFile(simPath(p))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// simFileExists reports whether an absolute path exists (through simPath).
func simFileExists(p string) bool {
	_, err := os.Stat(simPath(p))
	return err == nil
}
