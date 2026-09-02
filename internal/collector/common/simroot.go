package common

import (
	"os"
	"path/filepath"
	"strings"
)

// SimRootEnv is the environment variable of the simulation contract in
// docs/roadmap/spark-support.md section 10: when set, every absolute file path
// a collector reads (/etc/..., /proc/..., /sys/..., /var/..., /run/...) is
// prefixed with its value so CI can inject fixtures for hardware it does not
// have. Commands (nvidia-smi, lspci, dmidecode, lscpu, ...) are still resolved
// through PATH, which is where the shims live.
const SimRootEnv = "NVC_SIM_ROOT"

// SimRoot returns the simulation root directory, or "" when the process runs
// against the real file system.
func SimRoot() string {
	return strings.TrimSpace(os.Getenv(SimRootEnv))
}

// SimPath maps an absolute file path to its location under NVC_SIM_ROOT. With
// the variable unset, or for a relative path, the input is returned unchanged.
// Every collector in this module reads system files through this function so
// the simulation contract has exactly one implementation.
func SimPath(p string) string {
	root := SimRoot()
	if root == "" || !strings.HasPrefix(p, "/") {
		return p
	}
	return filepath.Join(root, filepath.FromSlash(p))
}

// ReadSimFile is os.ReadFile through SimPath.
func ReadSimFile(p string) ([]byte, error) {
	return os.ReadFile(SimPath(p))
}

// SimFileExists reports whether the (simulation-mapped) path exists.
func SimFileExists(p string) bool {
	_, err := os.Stat(SimPath(p))
	return err == nil
}

// readSimString returns the trimmed content of a small system file, or "" when
// it is missing or unreadable. NUL bytes (device-tree strings) are removed.
func readSimString(p string) string {
	data, err := ReadSimFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", ""))
}
