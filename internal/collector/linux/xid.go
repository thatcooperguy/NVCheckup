//go:build linux

package linux

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
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

	// Parse and group the Xid errors
	xidErrors := parseAndGroupXidErrors(xidLines)

	return xidErrors, errs
}

// collectXidFromDmesg attempts to extract Xid error lines from dmesg output.
func collectXidFromDmesg(timeout int, errs *[]types.CollectorError) []string {
	if !util.CommandExists("dmesg") {
		return nil
	}

	r := util.RunCommand(timeout, "sh", "-c", `dmesg 2>/dev/null | grep -i "NVRM: Xid"`)
	if r.Err != nil {
		// dmesg may require root; this is non-fatal
		*errs = append(*errs, types.CollectorError{
			Collector: "linux.xid.dmesg",
			Error:     "dmesg Xid grep failed (may need root): " + r.Err.Error(),
		})
		return nil
	}

	output := strings.TrimSpace(r.Stdout)
	if output == "" {
		return nil
	}

	return strings.Split(output, "\n")
}

// collectXidFromJournalctl attempts to extract Xid error lines from journalctl.
func collectXidFromJournalctl(timeout int, errs *[]types.CollectorError) []string {
	if !util.CommandExists("journalctl") {
		return nil
	}

	r := util.RunCommand(timeout, "sh", "-c", `journalctl -k -b --no-pager 2>/dev/null | grep -i "NVRM: Xid"`)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "linux.xid.journalctl",
			Error:     "journalctl Xid grep failed: " + r.Err.Error(),
		})
		return nil
	}

	output := strings.TrimSpace(r.Stdout)
	if output == "" {
		return nil
	}

	return strings.Split(output, "\n")
}

// parseAndGroupXidErrors parses raw kernel log lines containing Xid errors,
// extracts the Xid code and timestamp, and groups by code with counts.
func parseAndGroupXidErrors(lines []string) []types.XidError {
	// Pattern matches lines like:
	//   [ 1234.567890] NVRM: Xid (PCI:0000:01:00): 79, pid=1234, ...
	//   Jan 15 10:30:45 hostname kernel: NVRM: Xid (PCI:0000:01:00): 79, pid=1234, ...
	xidCodeRe := regexp.MustCompile(`NVRM:\s*Xid\s*\([^)]*\):\s*(\d+)`)

	// For dmesg-style timestamps: [ 1234.567890]
	dmesgTsRe := regexp.MustCompile(`\[\s*([\d.]+)\]`)

	// For journalctl-style timestamps: Jan 15 10:30:45
	journalTsRe := regexp.MustCompile(`^(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})`)

	// Group by Xid code: track count and last seen timestamp
	type xidGroup struct {
		code      int
		count     int
		lastSeen  time.Time
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

		// Try to parse timestamp
		ts := parseXidTimestamp(line, dmesgTsRe, journalTsRe)

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

// parseXidTimestamp attempts to extract a timestamp from a kernel log line.
// It tries dmesg-style (seconds since boot) and journalctl-style formats.
func parseXidTimestamp(line string, dmesgTsRe, journalTsRe *regexp.Regexp) time.Time {
	// Try journalctl-style timestamp first (more precise)
	if m := journalTsRe.FindStringSubmatch(line); m != nil {
		// Parse "Jan 15 10:30:45" — year is not included, use current year
		now := time.Now()
		tsStr := m[1] + " " + strconv.Itoa(now.Year())
		t, err := time.Parse("Jan  2 15:04:05 2006", tsStr)
		if err != nil {
			// Try single-digit day format
			t, err = time.Parse("Jan 2 15:04:05 2006", tsStr)
		}
		if err == nil {
			// If the parsed time is in the future, it's from last year
			if t.After(now) {
				t = t.AddDate(-1, 0, 0)
			}
			return t
		}
	}

	// Try dmesg-style timestamp: [ 1234.567890]
	// This is seconds since boot; convert to approximate wall clock time
	if m := dmesgTsRe.FindStringSubmatch(line); m != nil {
		secsSinceBoot, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			bootDuration := time.Duration(secsSinceBoot * float64(time.Second))
			// Approximate: current time minus uptime plus log offset
			approxTime := time.Now().Add(-bootDuration)
			return approxTime
		}
	}

	return time.Time{}
}
