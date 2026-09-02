package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// PCIeQueryFields is the exact --query-gpu field list used by the PCIe
// collector. Exported so self-test can verify the driver accepts it. The
// leading "index" makes every CSV row self-identifying on multi-GPU rigs.
const PCIeQueryFields = "index,pcie.link.gen.current,pcie.link.gen.max,pcie.link.width.current,pcie.link.width.max,pstate,utilization.gpu"

// idleUtilizationPct is the utilization below which the GPU is considered idle
// for PCIe purposes. An idle GPU is allowed (expected, even) to drop its link
// generation to save power, so a Gen1 link at 5% load is not a fault.
const idleUtilizationPct = 20

// maxPState is the highest NVML performance state (nvmlPstates_t defines
// P0..P15). Older code capped this at P12 and would have treated a P13-P15
// reading as unknown rather than idle.
const maxPState = 15

// CollectPCIeInfo gathers PCIe link state data for the first NVIDIA GPU via
// nvidia-smi. It is a thin wrapper over CollectPCIeAll kept for callers that
// only understand a single GPU; the zero value is returned when no GPU row
// was parsed.
func CollectPCIeInfo(timeout int) (types.PCIeInfo, []types.CollectorError) {
	all, errs := CollectPCIeAll(timeout)
	if len(all) == 0 {
		return types.PCIeInfo{}, errs
	}
	return all[0], errs
}

// CollectPCIeAll gathers PCIe link state for every NVIDIA GPU nvidia-smi
// reports, one entry per GPU in nvidia-smi index order.
func CollectPCIeAll(timeout int) ([]types.PCIeInfo, []types.CollectorError) {
	var errs []types.CollectorError

	if !util.CommandExists("nvidia-smi") {
		errs = append(errs, types.CollectorError{
			Collector: "pcie",
			Error:     "nvidia-smi not found in PATH",
			Fatal:     true,
		})
		return nil, errs
	}

	rows, qerr, ok := nvidiaSmiRows(timeout, "pcie", "PCIe", PCIeQueryFields, true)
	if !ok {
		return nil, append(errs, qerr)
	}

	infos, parseErrs := parsePCIeRows(rows)
	return infos, append(errs, parseErrs...)
}

// parsePCIeRows parses every CSV row produced by PCIeQueryFields into one
// PCIeInfo per GPU. Rows that carry no usable index fall back to their
// ordinal position.
func parsePCIeRows(rows []string) ([]types.PCIeInfo, []types.CollectorError) {
	var infos []types.PCIeInfo
	var errs []types.CollectorError
	for i, row := range rows {
		info, rowErrs := parsePCIeRow(row, i)
		infos = append(infos, info)
		errs = append(errs, rowErrs...)
	}
	return infos, errs
}

// parsePCIeCSV parses one nvidia-smi CSV line produced by PCIeQueryFields
// (index, current gen, max gen, current width, max width, pstate,
// utilization). "[N/A]" fields are left empty rather than recorded as 0. It
// is a pure function so it can be unit-tested with captured output. A row
// without a parsable index is assigned index 0.
func parsePCIeCSV(line string) (types.PCIeInfo, []types.CollectorError) {
	return parsePCIeRow(line, 0)
}

// parsePCIeRow is parsePCIeCSV with an explicit fallback index for rows whose
// leading index field is missing or unreadable.
func parsePCIeRow(line string, ordinal int) (types.PCIeInfo, []types.CollectorError) {
	var info types.PCIeInfo
	var errs []types.CollectorError

	idx, fields := parseRowIndex(splitCSV(line), ordinal)
	info.GPUIndex = idx
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
				Error:     fmt.Sprintf("GPU %d: failed to parse %s: %s", idx, label, s),
			})
			return 0, false
		}
		return v, true
	}

	// Generation and width are formatted from whatever number the driver
	// reports; there is deliberately no ceiling, so Gen5/Gen6 and future
	// widths pass through untouched.
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

	// Idle detection: P5..P15 are power-saving states, and low utilization
	// means the driver may have negotiated the link down on purpose. Only a
	// parsed utilization counts; an unknown value must not read as "0% = idle".
	info.IdleLikely = isIdlePState(info.PowerState) || (haveUtil && utilPct < idleUtilizationPct)

	// Load detection needs positive evidence: an active P-state (P0..P4) or a
	// parsed utilization at or above the idle threshold. When both fields are
	// [N/A] we know nothing about load, and "not idle" must not be read as
	// "busy". This mirrors the analyzer's pcieUnderLoad rule.
	underLoad := isActivePState(info.PowerState) || (haveUtil && utilPct >= idleUtilizationPct)

	// Width below max is always a fault (a bent pin, bad riser, or wrong slot
	// cannot be explained by power saving). The comparison is against the
	// card's own maximum, so a laptop dGPU wired x8 of x8 is at full width.
	// Gen below max only matters when the GPU is demonstrably busy; idle GPUs
	// routinely sit at Gen1, and an unknown state is not evidence either way.
	widthDownshift := haveCurrentWidth && haveMaxWidth && currentWidth > 0 && maxWidth > 0 && currentWidth < maxWidth
	genDownshift := haveCurrentGen && haveMaxGen && currentGen > 0 && maxGen > 0 && currentGen < maxGen
	info.Downshifted = widthDownshift || (genDownshift && underLoad && !info.IdleLikely)

	return info, errs
}

// parsePState parses an nvidia-smi pstate string (P0..P15) into its number.
// ok is false for empty, "[N/A]", or malformed values.
func parsePState(pstate string) (int, bool) {
	p := strings.ToUpper(strings.TrimSpace(pstate))
	if !strings.HasPrefix(p, "P") {
		return 0, false
	}
	n, err := strconv.Atoi(p[1:])
	if err != nil || n < 0 || n > maxPState {
		return 0, false
	}
	return n, true
}

// isIdlePState reports whether an nvidia-smi pstate string (P0..P15) is one of
// the power-saving states P5 and above.
func isIdlePState(pstate string) bool {
	n, ok := parsePState(pstate)
	return ok && n >= 5
}

// isActivePState reports whether an nvidia-smi pstate string is one of the
// performance states P0..P4, which is positive evidence that the GPU was
// busy when the link was sampled (some mobile parts and video workloads sit
// in P3/P4 under load).
func isActivePState(pstate string) bool {
	n, ok := parsePState(pstate)
	return ok && n <= 4
}

// formatPCIeGen formats a PCIe generation number as "GenN".
func formatPCIeGen(gen int) string {
	return fmt.Sprintf("Gen%d", gen)
}

// formatPCIeWidth formats a PCIe lane width as "xN".
func formatPCIeWidth(width int) string {
	return fmt.Sprintf("x%d", width)
}
