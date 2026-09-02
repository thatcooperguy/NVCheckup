//go:build linux

package linux

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
)

func TestParseXidTimestampDmesgIsBootTimePlusOffset(t *testing.T) {
	boot := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	now := boot.Add(10 * time.Hour)
	line := "[ 1234.500000] NVRM: Xid (PCI:0000:01:00): 79, pid=1234, GPU has fallen off the bus."

	got := parseXidTimestamp(line, boot, now)
	want := boot.Add(1234500 * time.Millisecond)
	if !got.Equal(want) {
		t.Errorf("dmesg timestamp = %v, want bootTime+offset = %v", got, want)
	}
	// The old now-offset arithmetic would have produced this; make sure it is gone.
	if got.Equal(now.Add(-1234500 * time.Millisecond)) {
		t.Errorf("dmesg timestamp still computed as now - offset")
	}
}

func TestParseXidTimestampDmesgUnknownBootTime(t *testing.T) {
	line := "[ 99.000000] NVRM: Xid (PCI:0000:01:00): 31, pid=1, Ch 00000010"
	if got := parseXidTimestamp(line, time.Time{}, time.Now()); !got.IsZero() {
		t.Errorf("expected zero time without a boot anchor, got %v", got)
	}
}

func TestParseXidTimestampJournal(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	got := parseXidTimestamp("Sep  1 10:30:45 host kernel: NVRM: Xid (PCI:0000:01:00): 31, pid=42", time.Time{}, now)
	if want := time.Date(2026, 9, 1, 10, 30, 45, 0, time.UTC); !got.Equal(want) {
		t.Errorf("journal timestamp = %v, want %v", got, want)
	}

	// A month later than "now" in the same year must belong to last year.
	got = parseXidTimestamp("Dec 25 23:59:59 host kernel: NVRM: Xid (PCI:0000:01:00): 13, pid=42", time.Time{}, now)
	if want := time.Date(2025, 12, 25, 23, 59, 59, 0, time.UTC); !got.Equal(want) {
		t.Errorf("future journal timestamp = %v, want previous year %v", got, want)
	}
}

func TestParseAndGroupXidErrors(t *testing.T) {
	boot := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	now := boot.Add(2 * time.Hour)
	lines := []string{
		"[  100.000000] NVRM: Xid (PCI:0000:01:00): 31, pid=100, Ch 00000010",
		"[  200.000000] NVRM: Xid (PCI:0000:01:00): 79, pid=100, GPU has fallen off the bus.",
		"[  300.000000] NVRM: Xid (PCI:0000:01:00): 31, pid=101, Ch 00000011",
		"not an xid line",
	}

	got := parseAndGroupXidErrors(lines, boot, now)
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(got), got)
	}
	if got[0].Code != 31 || got[0].Count != 2 || got[0].Message != "GPU memory page fault" {
		t.Errorf("first group = %+v", got[0])
	}
	if want := boot.Add(300 * time.Second); !got[0].Timestamp.Equal(want) {
		t.Errorf("Xid 31 lastSeen = %v, want %v", got[0].Timestamp, want)
	}
	if got[1].Code != 79 || got[1].Count != 1 || !got[1].Timestamp.Equal(boot.Add(200*time.Second)) {
		t.Errorf("second group = %+v", got[1])
	}
}

func TestFilterXidLines(t *testing.T) {
	dmesg := "[    0.000000] Linux version 6.8.0-40-generic\n" +
		"[   12.345678] NVRM: loading NVIDIA UNIX x86_64 Kernel Module  550.107.02\n" +
		"[ 1234.567890] NVRM: Xid (PCI:0000:01:00): 79, pid=1234, GPU has fallen off the bus.\n" +
		"[ 1300.000000] nvrm: xid (PCI:0000:01:00): 31, pid=1, Ch 00000010\n" +
		"[ 1400.000000] nouveau 0000:01:00.0: fifo: fault\n"
	got := filterXidLines(dmesg)
	if len(got) != 2 {
		t.Fatalf("expected 2 Xid lines, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "79") || !strings.Contains(got[1], "31") {
		t.Errorf("unexpected lines: %v", got)
	}
	if got := filterXidLines("[ 0.1] nothing here\n[ 0.2] NVRM: loaded\n"); len(got) != 0 {
		t.Errorf("healthy log must yield no lines, got %v", got)
	}
	if got := filterXidLines(""); len(got) != 0 {
		t.Errorf("empty output must yield no lines, got %v", got)
	}
}

func TestToolFailure(t *testing.T) {
	if d := toolFailure(util.CommandResult{Stdout: "ok"}); d != "" {
		t.Errorf("success must not be a failure, got %q", d)
	}
	// grep-style "nothing matched": non-zero exit, nothing on stderr.
	if d := toolFailure(util.CommandResult{Err: errors.New("exit status 1"), ExitCode: 1}); d != "" {
		t.Errorf("silent non-zero exit must not be reported, got %q", d)
	}
	restricted := util.CommandResult{Err: errors.New("exit status 1"), ExitCode: 1,
		Stderr: "dmesg: read kernel buffer failed: Operation not permitted"}
	if d := toolFailure(restricted); !strings.Contains(d, "Operation not permitted") {
		t.Errorf("stderr should be surfaced, got %q", d)
	}
	if !isPermissionDenied(restricted) {
		t.Error("dmesg_restrict failure should be recognised as a permission problem")
	}
	if isPermissionDenied(util.CommandResult{Stderr: "dmesg: unknown option"}) {
		t.Error("unrelated stderr must not be a permission problem")
	}
	timedOut := util.CommandResult{Err: errors.New("command timed out after 5s: dmesg"), TimedOut: true, ExitCode: -1}
	if d := toolFailure(timedOut); !strings.Contains(d, "timed out") {
		t.Errorf("timeouts must be reported, got %q", d)
	}
	multi := util.CommandResult{Err: errors.New("exit status 2"), ExitCode: 2, Stderr: "first line\nsecond line"}
	if d := toolFailure(multi); d != "first line" {
		t.Errorf("only the first stderr line should be used, got %q", d)
	}
}
