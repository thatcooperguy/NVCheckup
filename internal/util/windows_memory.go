package util

import (
	"fmt"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// WindowsMemorySummary describes only the recorded snapshot; it never probes
// the rendering host or adds commit/pagefile capacity to a GPU memory estimate.
func WindowsMemorySummary(m *types.WindowsMemoryInfo) string {
	if m == nil {
		return "unknown (not recorded; host readiness is not established)"
	}
	format := func(v *uint64, scale float64) string {
		if v == nil {
			return "unknown"
		}
		return fmt.Sprintf("%.1f GiB", float64(*v)*scale/(1024*1024*1024))
	}
	headroom := "unknown (missing or inconsistent commit counters)"
	if free, known := m.CommitHeadroomBytes(); known {
		headroom = format(&free, 1)
	}
	captured := m.CapturedAt
	if captured == "" {
		captured = "time not recorded"
	}
	return fmt.Sprintf("%s committed / %s limit; headroom %s; physical RAM free %s (snapshot: %s; source: %s)",
		format(m.CommittedBytes, 1), format(m.CommitLimitBytes, 1), headroom, format(m.PhysicalFreeKB, 1024), captured, m.Source)
}

// WindowsMemoryWarning is a host diagnostic, independent of model sizing.
func WindowsMemoryWarning(m *types.WindowsMemoryInfo) string {
	if !m.LowCommitHeadroom() {
		return ""
	}
	free, _ := m.CommitHeadroomBytes()
	return fmt.Sprintf("Windows commit headroom is low in the recorded snapshot (%.1f GiB; heuristic: below 4 GiB or 10%% of its commit limit). Allocations can fail even with physical RAM free; GPU fit does not establish host readiness. Before running a game/build alongside AI, consider unloading unneeded idle AI models/services or reducing build parallelism to 1-2 jobs, then recheck Task Manager > Performance > Memory > Committed. Commit/pagefile capacity is not fast RAM or VRAM; NVCheckup changes no settings.", float64(free)/(1024*1024*1024))
}
