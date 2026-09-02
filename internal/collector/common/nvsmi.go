package common

import (
	"fmt"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// nvidiaSmiFailure pairs a substring of nvidia-smi's own failure text with a
// plain-language explanation. nvidia-smi prints these to stdout or stderr,
// sometimes with exit 0, on machines whose driver is perfectly healthy: an
// Optimus laptop whose dGPU is powered down, a container started without the
// GPU mapped, or a GPU that has fallen off the bus. Surfacing the exact text
// plus the likely cause turns a wall of parse errors into one useful note.
var nvidiaSmiFailures = []struct {
	needle      string
	explanation string
}{
	{"Unable to determine the device handle for GPU",
		"the driver is loaded but that GPU cannot be reached; on Optimus laptops this is the dGPU being powered off, on desktops it can mean the GPU fell off the PCIe bus (check dmesg / Event Viewer for Xid 79)"},
	{"No devices were found",
		"the driver is loaded but no NVIDIA GPU is visible to it; common on Optimus laptops with the dGPU powered down, when the GPU is disabled in Device Manager, or in a container/VM without the GPU passed through"},
	{"Failed to initialize NVML",
		"NVML could not start; usually a driver/library version mismatch after an update (reboot), or a container started without the NVIDIA runtime or the GPU mapped"},
	{"couldn't communicate with the NVIDIA driver",
		"the NVIDIA kernel driver is not loaded or not running"},
	{"Driver/library version mismatch",
		"the loaded kernel module and the user-space libraries are from different driver versions; reboot to finish the driver update"},
}

// describeNvidiaSmiFailure returns the nvidia-smi failure line found in out
// and an explanation of its likely cause. known is false when out contains
// none of the recognised messages.
func describeNvidiaSmiFailure(out string) (quoted, explanation string, known bool) {
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		for _, f := range nvidiaSmiFailures {
			if strings.Contains(t, f.needle) {
				return t, f.explanation, true
			}
		}
	}
	return "", "", false
}

// nvidiaSmiQueryError builds the single CollectorError for a failed or
// unusable nvidia-smi invocation. The nvidia-smi text is quoted verbatim and,
// when recognised, followed by the likely cause.
func nvidiaSmiQueryError(collector, what string, r util.CommandResult) types.CollectorError {
	if quoted, why, ok := describeNvidiaSmiFailure(r.Stderr + "\n" + r.Stdout); ok {
		return types.CollectorError{
			Collector: collector,
			Error:     fmt.Sprintf("nvidia-smi %s failed: %q (%s)", what, quoted, why),
			Fatal:     true,
		}
	}
	return types.CollectorError{
		Collector: collector,
		Error:     fmt.Sprintf("nvidia-smi %s failed: %s", what, commandFailureDetail(r)),
		Fatal:     true,
	}
}

// csvRows splits nvidia-smi --format=csv,noheader output into trimmed,
// non-empty rows, one per GPU. Lines without a comma are not CSV rows (they
// are nvidia-smi error text or warnings) and are returned separately so the
// caller can quote them instead of mis-parsing them as a GPU.
func csvRows(out string) (rows []string, other []string) {
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, ",") {
			rows = append(rows, t)
		} else {
			other = append(other, t)
		}
	}
	return rows, other
}

// nvidiaSmiRows runs one --query-gpu list and returns its CSV rows. When the
// command fails, prints a recognised failure message, or yields no CSV rows at
// all, exactly one CollectorError describing why is returned and ok is false.
func nvidiaSmiRows(timeout int, collector, what, fields string, nounits bool) (rows []string, err types.CollectorError, ok bool) {
	format := "--format=csv,noheader"
	if nounits {
		format += ",nounits"
	}
	r := util.RunCommand(timeout, "nvidia-smi", "--query-gpu="+fields, format)
	if r.Err != nil {
		return nil, nvidiaSmiQueryError(collector+".query", what+" query", r), false
	}
	if _, _, known := describeNvidiaSmiFailure(r.Stdout + "\n" + r.Stderr); known {
		return nil, nvidiaSmiQueryError(collector+".query", what+" query", r), false
	}
	rows, other := csvRows(r.Stdout)
	if len(rows) == 0 {
		detail := "nvidia-smi " + what + " query returned no GPU rows"
		if len(other) > 0 {
			detail += ": " + fmt.Sprintf("%q", strings.Join(other, " / "))
		}
		return nil, types.CollectorError{Collector: collector + ".parse", Error: detail, Fatal: true}, false
	}
	return rows, types.CollectorError{}, true
}

// parseRowIndex reads the leading nvidia-smi "index" field of a CSV row and
// returns the remaining fields. When the index is missing or unparsable the
// row's ordinal position is used, so a truncated row still lands on a GPU.
func parseRowIndex(fields []string, ordinal int) (index int, rest []string) {
	if len(fields) == 0 {
		return ordinal, nil
	}
	idx, ok := parseSmallInt(fields[0])
	if !ok {
		return ordinal, fields[1:]
	}
	return idx, fields[1:]
}

// parseSmallInt parses a non-negative decimal integer field; ok is false for
// "[N/A]", empty and malformed values.
func parseSmallInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || isNotAvailable(s) {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return 0, false
		}
	}
	return n, true
}

// missingNvidiaSmiError describes a host without nvidia-smi in PATH for the
// named collector. On Jetson / Tegra nvidia-smi does not ship with JetPack, so
// its absence is the healthy state and nil is returned: the analyzer's
// jetson-detected finding already explains why thermal and PCIe data are
// missing, and a Fatal collector error next to it would contradict that.
func missingNvidiaSmiError(collector string, isJetson bool) *types.CollectorError {
	if isJetson {
		return nil
	}
	return &types.CollectorError{
		Collector: collector,
		Error:     "nvidia-smi not found in PATH",
		Fatal:     true,
	}
}

// isJetsonHost is DetectJetson reduced to the boolean the collectors need.
func isJetsonHost() bool {
	is, _ := DetectJetson()
	return is
}
