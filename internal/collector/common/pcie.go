package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

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
		"--query-gpu=pcie.link.gen.current,pcie.link.gen.max,pcie.link.width.current,pcie.link.width.max",
		"--format=csv,noheader,nounits")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "pcie.query",
			Error:     fmt.Sprintf("nvidia-smi PCIe query failed: %v", r.Err),
			Fatal:     true,
		})
		return info, errs
	}

	line := strings.TrimSpace(r.Stdout)
	if line == "" {
		errs = append(errs, types.CollectorError{
			Collector: "pcie.parse",
			Error:     "nvidia-smi PCIe query returned empty output",
			Fatal:     true,
		})
		return info, errs
	}

	// Take only the first GPU line if there are multiple
	lines := strings.Split(line, "\n")
	fields := strings.Split(lines[0], ", ")

	var currentGen, maxGen, currentWidth, maxWidth int

	if len(fields) >= 1 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[0])); err == nil {
			currentGen = v
			info.CurrentSpeed = formatPCIeGen(v)
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "pcie.current_gen",
				Error:     fmt.Sprintf("failed to parse current PCIe gen: %s", fields[0]),
			})
		}
	}

	if len(fields) >= 2 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[1])); err == nil {
			maxGen = v
			info.MaxSpeed = formatPCIeGen(v)
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "pcie.max_gen",
				Error:     fmt.Sprintf("failed to parse max PCIe gen: %s", fields[1]),
			})
		}
	}

	if len(fields) >= 3 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[2])); err == nil {
			currentWidth = v
			info.CurrentWidth = formatPCIeWidth(v)
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "pcie.current_width",
				Error:     fmt.Sprintf("failed to parse current PCIe width: %s", fields[2]),
			})
		}
	}

	if len(fields) >= 4 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[3])); err == nil {
			maxWidth = v
			info.MaxWidth = formatPCIeWidth(v)
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "pcie.max_width",
				Error:     fmt.Sprintf("failed to parse max PCIe width: %s", fields[3]),
			})
		}
	}

	// Downshifted if current < max on either speed or width
	if (currentGen > 0 && maxGen > 0 && currentGen < maxGen) ||
		(currentWidth > 0 && maxWidth > 0 && currentWidth < maxWidth) {
		info.Downshifted = true
	}

	return info, errs
}

// formatPCIeGen formats a PCIe generation number as "GenN".
func formatPCIeGen(gen int) string {
	return fmt.Sprintf("Gen%d", gen)
}

// formatPCIeWidth formats a PCIe lane width as "xN".
func formatPCIeWidth(width int) string {
	return fmt.Sprintf("x%d", width)
}
