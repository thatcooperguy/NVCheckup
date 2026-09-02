//go:build linux

package linux

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// knownXidDescriptions maps NVIDIA Xid error codes to human-readable descriptions.
var knownXidDescriptions = map[int]string{
	13:  "Graphics engine fault",
	31:  "GPU memory page fault",
	32:  "Invalid or corrupted push buffer",
	43:  "GPU stopped processing",
	48:  "Double-bit ECC error",
	56:  "Display engine error",
	57:  "Encoder/decoder error",
	63:  "Row remapper failure",
	69:  "Graphics engine exception",
	79:  "GPU has fallen off the bus",
	119: "GSP firmware error",
}

var (
	// xidCodeRe matches lines like:
	//   [ 1234.567890] NVRM: Xid (PCI:0000:01:00): 79, pid=1234, ...
	//   Jan 15 10:30:45 hostname kernel: NVRM: Xid (PCI:0000:01:00): 79, pid=1234, ...
	xidCodeRe = regexp.MustCompile(`NVRM:\s*Xid\s*\([^)]*\):\s*(\d+)`)

	// xidDmesgTsRe matches dmesg-style "[ 1234.567890]" (seconds since boot).
	xidDmesgTsRe = regexp.MustCompile(`\[\s*([\d.]+)\]`)

	// xidJournalTsRe matches journalctl-style "Jan 15 10:30:45" (local time, no year).
	xidJournalTsRe = regexp.MustCompile(`^(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})`)
)

// CollectXidErrors parses NVIDIA Xid errors from kernel logs using dmesg
// and journalctl. Errors are grouped by Xid code with occurrence counts.
func CollectXidErrors(timeout int) ([]types.XidError, []types.CollectorError) {
	var errs []types.CollectorError

	// Try dmesg first
	xidLines := collectXidFromDmesg(timeout, &errs)

	// If dmesg returned nothing, try journalctl as fallback
	if len(xidLines) == 0 {
		xidLines = collectXidFromJournalctl(timeout, &errs)
	}

	if len(xidLines) == 0 {
		return nil, errs
	}

	return parseAndGroupXidErrors(xidLines, readBootTime(), time.Now()), errs
}

// xidMarker is the kernel log text that identifies an NVIDIA Xid report.
const xidMarker = "nvrm: xid"

// filterXidLines returns the lines of kernel log output that carry an NVIDIA
// Xid report (case-insensitive "NVRM: Xid"), replacing the former
// "| grep -i" pipeline. No matching lines is a normal, healthy result and is
// returned as an empty slice, never as an error.
func filterXidLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), xidMarker) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

// collectXidFromDmesg extracts Xid error lines from dmesg. dmesg is run
// directly (no shell, no grep) so that "no Xid lines" is simply empty output;
// only a failure of dmesg itself is reported, and an unprivileged read
// (kernel.dmesg_restrict=1) keeps its "may need root" wording.
func collectXidFromDmesg(timeout int, errs *[]types.CollectorError) []string {
	if !util.CommandExists("dmesg") {
		return nil
	}

	r := util.RunCommand(timeout, "dmesg")
	if detail := toolFailure(r); detail != "" {
		msg := "dmesg failed: " + detail
		if isPermissionDenied(r) {
			msg = "dmesg failed (may need root): " + detail
		}
		*errs = append(*errs, types.CollectorError{Collector: "linux.xid.dmesg", Error: msg})
		return nil
	}

	return filterXidLines(r.Stdout)
}

// collectXidFromJournalctl extracts Xid error lines from the kernel journal of
// the current boot. Like collectXidFromDmesg it filters in Go so an empty
// result is not mistaken for a failure.
func collectXidFromJournalctl(timeout int, errs *[]types.CollectorError) []string {
	if !util.CommandExists("journalctl") {
		return nil
	}

	r := util.RunCommand(timeout, "journalctl", "-k", "-b", "--no-pager")
	if detail := toolFailure(r); detail != "" {
		*errs = append(*errs, types.CollectorError{
			Collector: "linux.xid.journalctl",
			Error:     "journalctl failed: " + detail,
		})
		return nil
	}

	return filterXidLines(r.Stdout)
}

// toolFailure returns a description of why a command failed, or "" when it
// succeeded. A non-zero exit that produced neither stderr nor a Go-level
// error detail (a timeout or a failure to start) is not treated as a failure
// worth reporting, mirroring how grep's exit 1 "no match" used to be
// misreported. stderr is preferred over the bare "exit status N".
func toolFailure(r util.CommandResult) string {
	if r.Err == nil {
		return ""
	}
	if stderr := strings.TrimSpace(r.Stderr); stderr != "" {
		return firstLineOf(stderr)
	}
	if r.TimedOut || r.ExitCode < 0 {
		return r.Err.Error()
	}
	return ""
}

// isPermissionDenied reports whether a failed command's stderr indicates an
// unprivileged caller (dmesg under kernel.dmesg_restrict, journal ACLs).
func isPermissionDenied(r util.CommandResult) bool {
	e := strings.ToLower(r.Stderr)
	return strings.Contains(e, "operation not permitted") || strings.Contains(e, "permission denied")
}

// firstLineOf returns the first line of s, trimmed.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// readBootTime returns the kernel boot time. /proc/stat "btime" is seconds
// since the epoch and is the anchor that turns dmesg's seconds-since-boot
// into wall-clock time; now minus /proc/uptime is the fallback. A zero time
// means the anchor is unknown and dmesg timestamps are left unset.
func readBootTime() time.Time {
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "btime" {
				if secs, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return time.Unix(secs, 0)
				}
			}
		}
	}
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if up, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return time.Now().Add(-time.Duration(up * float64(time.Second)))
			}
		}
	}
	return time.Time{}
}

// parseAndGroupXidErrors parses raw kernel log lines containing Xid errors,
// extracts the Xid code and timestamp, and groups by code with counts.
// bootTime anchors dmesg timestamps; now supplies the year for journalctl
// timestamps. Both are parameters so the parser is deterministic in tests.
func parseAndGroupXidErrors(lines []string, bootTime, now time.Time) []types.XidError {
	// Group by Xid code: track count and last seen timestamp
	type xidGroup struct {
		code     int
		count    int
		lastSeen time.Time
	}
	groups := make(map[int]*xidGroup)
	var seenOrder []int // preserve order of first occurrence

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Extract Xid code
		m := xidCodeRe.FindStringSubmatch(line)
		if m == nil || len(m) < 2 {
			continue
		}

		code, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		ts := parseXidTimestamp(line, bootTime, now)

		if g, ok := groups[code]; ok {
			g.count++
			if !ts.IsZero() {
				g.lastSeen = ts
			}
		} else {
			groups[code] = &xidGroup{
				code:     code,
				count:    1,
				lastSeen: ts,
			}
			seenOrder = append(seenOrder, code)
		}
	}

	// Build result slice preserving first-seen order
	var result []types.XidError
	for _, code := range seenOrder {
		g := groups[code]

		msg, ok := knownXidDescriptions[code]
		if !ok {
			msg = "Unknown Xid error"
		}

		result = append(result, types.XidError{
			Code:      g.code,
			Message:   msg,
			Timestamp: g.lastSeen,
			Count:     g.count,
		})
	}

	return result
}

// parseXidTimestamp converts a kernel log timestamp to wall-clock time.
//
// dmesg prints "[ 1234.567890]", seconds since boot, so the wall time is
// bootTime + offset. (The previous code computed now - offset, which is only
// right for an event that happened exactly "offset" ago and is otherwise off
// by the whole uptime.) journalctl prints a local time without a year; the
// year is taken from now and rolled back by one if the result would be in
// the future. A zero time is returned when no timestamp can be recovered.
func parseXidTimestamp(line string, bootTime, now time.Time) time.Time {
	if m := xidJournalTsRe.FindStringSubmatch(line); m != nil {
		// "_2" accepts both "Jan  5" and "Jan 15".
		tsStr := m[1] + " " + strconv.Itoa(now.Year())
		if t, err := time.ParseInLocation("Jan _2 15:04:05 2006", tsStr, now.Location()); err == nil {
			if t.After(now) {
				t = t.AddDate(-1, 0, 0)
			}
			return t
		}
	}

	if m := xidDmesgTsRe.FindStringSubmatch(line); m != nil {
		secsSinceBoot, err := strconv.ParseFloat(m[1], 64)
		if err == nil && !bootTime.IsZero() {
			return bootTime.Add(time.Duration(secsSinceBoot * float64(time.Second)))
		}
	}

	return time.Time{}
}
