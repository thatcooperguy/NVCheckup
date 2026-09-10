package types

import (
	"encoding/json"
	"testing"
)

func memoryU64(n uint64) *uint64 { return &n }

func TestWindowsCommitHeadroom(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)
	for _, tc := range []struct {
		name       string
		memory     *WindowsMemoryInfo
		free       uint64
		known, low bool
	}{
		{"absent", nil, 0, false, false},
		{"missing", &WindowsMemoryInfo{}, 0, false, false},
		{"partial", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(80 * gib)}, 0, false, false},
		{"zero limit", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(0), CommittedBytes: memoryU64(0)}, 0, false, false},
		{"inconsistent", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(80 * gib), CommittedBytes: memoryU64(81 * gib)}, 0, false, false},
		{"zero used is known", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(80 * gib), CommittedBytes: memoryU64(0)}, 80 * gib, true, false},
		{"exhausted is known", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(80 * gib), CommittedBytes: memoryU64(80 * gib)}, 0, true, true},
		{"absolute threshold", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(16 * gib), CommittedBytes: memoryU64(13 * gib)}, 3 * gib, true, true},
		{"relative threshold", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(80 * gib), CommittedBytes: memoryU64(73 * gib)}, 7 * gib, true, true},
		{"at threshold", &WindowsMemoryInfo{CommitLimitBytes: memoryU64(40 * gib), CommittedBytes: memoryU64(36 * gib)}, 4 * gib, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			free, known := tc.memory.CommitHeadroomBytes()
			if free != tc.free || known != tc.known || tc.memory.LowCommitHeadroom() != tc.low {
				t.Fatalf("headroom = %d, %v; low = %v", free, known, tc.memory.LowCommitHeadroom())
			}
		})
	}
}

func TestWindowsMemoryJSONKeepsKnownZero(t *testing.T) {
	m := WindowsMemoryInfo{CommittedBytes: memoryU64(0), CommitLimitBytes: memoryU64(80 * 1024 * 1024 * 1024)}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip WindowsMemoryInfo
	if err := json.Unmarshal(b, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.CommittedBytes == nil || *roundtrip.CommittedBytes != 0 || roundtrip.PhysicalFreeKB != nil {
		t.Fatalf("known zero/missing lost: %s", b)
	}
}
