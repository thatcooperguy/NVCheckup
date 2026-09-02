package linux

// NVRM kernel-log scan for GB10 (spec 3.2 "GSP failure", section 6 and rule
// unified-memory-oom-events). It complements the Xid collector in xid.go
// without changing it: GSP/SEC2 boot-failure lines are kept verbatim (capped)
// so dgx-spark-gsp-init-failure can fire even without --include-logs, NVRM
// out-of-memory events are counted, and the nvidia_peermem load attempt is
// flagged. Untagged so the parser is tested on every OS; the runner calls
// CollectNVRMMessages on Linux only.

import (
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// gspFailureMarkers are the exact substrings of spec 3.2 ("GSP failure") and
// section 6 (register-read sentinel) that identify a GSP/SEC2 init failure.
var gspFailureMarkers = []string{
	"waiting for RPC response from GPU",    // Xid 119: Timeout after 6s of waiting for RPC response from GPU0 GSP!
	"SEC2 secure boot partition timed out", // ksec2PrepareBootCommands_GB20B
	"Cannot initialize GSP firmware RM",    // RmInitAdapter: Cannot initialize GSP firmware RM
	"RmInitAdapter failed!",                // RmInitAdapter failed! (0x62:0x65:2028)
	"GSP task exception",                   // Xid 120
	"0xbadf5600",                           // gpuHandleSanityCheckRegReadError sentinel (section 6)
	"GPU requires reset",                   // section 6
}

// nvrmNoMemoryMarker is the NVRM allocation failure text of rule
// unified-memory-oom-events ("Check failed: Out of memory [NV_ERR_NO_MEMORY]").
const nvrmNoMemoryMarker = "NV_ERR_NO_MEMORY"

// oomKillMarker is the kernel OOM-killer line of the same rule.
const oomKillMarker = "Out of memory: Killed process"

// maxGSPFailureLines caps the lines kept in the report.
const maxGSPFailureLines = 20

// NVRMMessages is the result of scanning the kernel log once.
type NVRMMessages struct {
	GSPFailureLines  []string // verbatim lines matching gspFailureMarkers (last maxGSPFailureLines)
	NoMemoryCount    int      // NVRM NV_ERR_NO_MEMORY lines -> UnifiedMemoryInfo.NVRMNoMemory
	OOMKillCount     int      // kernel OOM-killer lines -> UnifiedMemoryInfo.OOMKills
	PeermemAttempted bool     // a line names nvidia_peermem -> ClusterInfo.PeermemAttempted
}

// CollectNVRMMessages reads the kernel log (dmesg, journalctl -k -b fallback)
// and scans it. An unreadable log is reported once as a non-fatal error.
func CollectNVRMMessages(timeout int) (NVRMMessages, []types.CollectorError) {
	var errs []types.CollectorError
	out, source, ok := readKernelLog(timeout)
	if !ok {
		if source != "" {
			errs = append(errs, types.CollectorError{Collector: "linux.nvrm", Error: source + " unavailable; GSP/NVRM scan skipped"})
		}
		return NVRMMessages{}, errs
	}
	return ScanNVRMMessages(out), errs
}

// readKernelLog returns the kernel log text from dmesg or journalctl. The
// second value names the tool tried when neither produced output.
func readKernelLog(timeout int) (out, tried string, ok bool) {
	if util.CommandExists("dmesg") {
		r := util.RunCommand(timeout, "dmesg")
		if r.Err == nil {
			return r.Stdout, "dmesg", true
		}
		tried = "dmesg"
	}
	if util.CommandExists("journalctl") {
		// -o cat prints the message field only: the marker match needs no
		// timestamp and the hostname of the default short format must not
		// reach GSPFailureLines verbatim.
		r := util.RunCommand(timeout, "journalctl", "-k", "-b", "--no-pager", "-q", "-o", "cat")
		if r.Err == nil {
			return r.Stdout, "journalctl", true
		}
		tried = "journalctl"
	}
	return "", tried, false
}

// ScanNVRMMessages scans kernel log text for GSP failures, NVRM
// out-of-memory events, OOM kills and the nvidia_peermem load attempt. It is
// pure so fixtures can drive it.
func ScanNVRMMessages(output string) NVRMMessages {
	var res NVRMMessages
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, nvrmNoMemoryMarker) {
			res.NoMemoryCount++
		}
		if strings.Contains(line, oomKillMarker) {
			res.OOMKillCount++
		}
		if strings.Contains(line, peermemModule) {
			res.PeermemAttempted = true
		}
		for _, marker := range gspFailureMarkers {
			if strings.Contains(line, marker) {
				res.GSPFailureLines = append(res.GSPFailureLines, line)
				break
			}
		}
	}
	if len(res.GSPFailureLines) > maxGSPFailureLines {
		res.GSPFailureLines = res.GSPFailureLines[len(res.GSPFailureLines)-maxGSPFailureLines:]
	}
	return res
}
