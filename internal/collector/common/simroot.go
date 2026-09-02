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

// SimGlob is filepath.Glob for an absolute pattern through SimPath. The
// matches are returned as logical paths (the simulation root stripped again),
// so a report produced under NVC_SIM_ROOT lists /dev/nvidia0, not the fixture
// location. Callers that need to read a match must map it with SimPath.
func SimGlob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(SimPath(pattern))
	if err != nil {
		return nil, err
	}
	root := SimRoot()
	if root == "" || !strings.HasPrefix(pattern, "/") {
		return matches, nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(root, m)
		if err != nil {
			out = append(out, m)
			continue
		}
		out = append(out, "/"+filepath.ToSlash(rel))
	}
	return out, nil
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
