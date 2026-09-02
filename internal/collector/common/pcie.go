package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// PCIeQueryFields is the exact --query-gpu field list used by CollectPCIeInfo.
// Exported so self-test can verify the driver accepts it.
const PCIeQueryFields = "pcie.link.gen.current,pcie.link.gen.max,pcie.link.width.current,pcie.link.width.max,pstate,utilization.gpu"

// idleUtilizationPct is the utilization below which the GPU is considered idle
// for PCIe purposes. An idle GPU is allowed (expected, even) to drop its link
// generation to save power, so a Gen1 link at 5% load is not a fault.
const idleUtilizationPct = 20

// CollectPCIeInfo gathers PCIe link state data via nvidia-smi.
func CollectPCIeInfo(timeout int) (types.PCIeInfo, []types.CollectorError) {
	var info types.PCIeInfo
	var errs []types.CollectorError

	if !util.CommandExists("nvidia-smi") {
		errs = append(errs, types.CollectorError{
			Collector: "pcie",
			Error:     "nvidia-smi not found in PATH",
			Fatal:     true,
		})
		return info, errs
	}

	r := util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu="+PCIeQueryFields,
		"--format=csv,noheader,nounits")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "pcie.query",
			Error:     fmt.Sprintf("nvidia-smi PCIe query failed: %v (%s)", r.Err, strings.TrimSpace(r.Stderr)),
			Fatal:     true,
		})
		return info, errs
	}

	// One CSV row per GPU; report the first GPU only.
	line := firstLine(r.Stdout)
	if line == "" {
		errs = append(errs, types.CollectorError{
			Collector: "pcie.parse",
			Error:     "nvidia-smi PCIe query returned empty output",
			Fatal:     true,
		})
		return info, errs
	}

	return parsePCIeCSV(line)
}

// parsePCIeCSV parses one nvidia-smi CSV line produced by PCIeQueryFields
// (current gen, max gen, current width, max width, pstate, utilization).
// "[N/A]" fields are left empty rather than recorded as 0. It is a pure
// function so it can be unit-tested with captured output.
func parsePCIeCSV(line string) (types.PCIeInfo, []types.CollectorError) {
	var info types.PCIeInfo
	var errs []types.CollectorError

	fields := splitCSV(line)
	get := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}
		return ""
	}
	// parseInt returns ok=false for missing or "[N/A]" fields without an error,
	// and records a CollectorError for values that are present but malformed.
	parseInt := func(i int, collector, label string) (int, bool) {
		s := get(i)
		if s == "" || isNotAvailable(s) {
			return 0, false
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			errs = append(errs, types.CollectorError{
				Collector: collector,
				Error:     fmt.Sprintf("failed to parse %s: %s", label, s),
			})
			return 0, false
		}
		return v, true
	}

	currentGen, haveCurrentGen := parseInt(0, "pcie.current_gen", "current PCIe gen")
	if haveCurrentGen {
		info.CurrentSpeed = formatPCIeGen(currentGen)
	}
	maxGen, haveMaxGen := parseInt(1, "pcie.max_gen", "max PCIe gen")
	if haveMaxGen {
		info.MaxSpeed = formatPCIeGen(maxGen)
	}
	currentWidth, haveCurrentWidth := parseInt(2, "pcie.current_width", "current PCIe width")
	if haveCurrentWidth {
		info.CurrentWidth = formatPCIeWidth(currentWidth)
	}
	maxWidth, haveMaxWidth := parseInt(3, "pcie.max_width", "max PCIe width")
	if haveMaxWidth {
		info.MaxWidth = formatPCIeWidth(maxWidth)
	}

	if s := get(4); s != "" && !isNotAvailable(s) {
		info.PowerState = s
	}
	utilPct, haveUtil := parseInt(5, "pcie.utilization", "utilization")
	if haveUtil {
		info.UtilizationPct = utilPct
	}

	// Idle detection: P5..P12 are power-saving states, and low utilization
	// means the driver may have negotiated the link down on purpose. Only a
	// parsed utilization counts; an unknown value must not read as "0% = idle".
	info.IdleLikely = isIdlePState(info.PowerState) || (haveUtil && utilPct < idleUtilizationPct)

	// Width below max is always a fault (a bent pin, bad riser, or wrong slot
	// cannot be explained by power saving). Gen below max only matters when the
	// GPU is actually busy; idle GPUs routinely sit at Gen1.
	widthDownshift := haveCurrentWidth && haveMaxWidth && currentWidth > 0 && maxWidth > 0 && currentWidth < maxWidth
	genDownshift := haveCurrentGen && haveMaxGen && currentGen > 0 && maxGen > 0 && currentGen < maxGen
	info.Downshifted = widthDownshift || (genDownshift && !info.IdleLikely)

	return info, errs
}

// isIdlePState reports whether an nvidia-smi pstate string (P0..P12) is one of
// the power-saving states P5 and above.
func isIdlePState(pstate string) bool {
	p := strings.ToUpper(strings.TrimSpace(pstate))
	if !strings.HasPrefix(p, "P") {
		return false
	}
	n, err := strconv.Atoi(p[1:])
	if err != nil {
		return false
	}
	return n >= 5 && n <= 12
}

// formatPCIeGen formats a PCIe generation number as "GenN".
func formatPCIeGen(gen int) string {
	return fmt.Sprintf("Gen%d", gen)
}

// formatPCIeWidth formats a PCIe lane width as "xN".
func formatPCIeWidth(width int) string {
	return fmt.Sprintf("x%d", width)
}
