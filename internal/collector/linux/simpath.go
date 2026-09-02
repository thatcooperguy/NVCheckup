package linux

import (
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
)

// readSimFile reads an absolute path through common.SimPath (the NVC_SIM_ROOT
// contract of docs/roadmap/spark-support.md section 10) and returns its
// trimmed contents, or "" when the file is missing or unreadable. The path
// mapping itself lives in internal/collector/common (SimPath, ReadSimFile,
// SimFileExists, SimGlob) so the simulation contract has one implementation.
func readSimFile(p string) string {
	data, err := common.ReadSimFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
