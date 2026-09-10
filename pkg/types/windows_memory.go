package types

// WindowsMemoryInfo is a recorded host snapshot. Commit counters are separate
// from physical RAM and GPU fit capacity. Nil means unavailable; zero is data.
type WindowsMemoryInfo struct {
	PhysicalTotalKB  *uint64 `json:"physical_total_kb,omitempty"`
	PhysicalFreeKB   *uint64 `json:"physical_free_kb,omitempty"`
	CommitLimitBytes *uint64 `json:"commit_limit_bytes,omitempty"`
	CommittedBytes   *uint64 `json:"committed_bytes,omitempty"`
	Source           string  `json:"source"`
	CapturedAt       string  `json:"captured_at,omitempty"` // UTC RFC3339, absent in old/imported snapshots
}

// CommitHeadroomBytes subtracts the measured system commit counters, without
// assuming that available virtual memory or free RAM is available commit.
func (m *WindowsMemoryInfo) CommitHeadroomBytes() (uint64, bool) {
	if m == nil || m.CommitLimitBytes == nil || m.CommittedBytes == nil ||
		*m.CommitLimitBytes == 0 || *m.CommittedBytes > *m.CommitLimitBytes {
		return 0, false
	}
	return *m.CommitLimitBytes - *m.CommittedBytes, true
}

// LowCommitHeadroom is a diagnostic heuristic, not a model sizing constraint.
// It does not claim the next allocation will fail or predict pagefile growth.
func (m *WindowsMemoryInfo) LowCommitHeadroom() bool {
	free, known := m.CommitHeadroomBytes()
	return known && (free < 4*1024*1024*1024 || float64(free)/float64(*m.CommitLimitBytes) < 0.10)
}
