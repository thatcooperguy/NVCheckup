package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectThermalInfo gathers GPU thermal, power state, and clock data via nvidia-smi.
func CollectThermalInfo(timeout int) (types.ThermalInfo, []types.CollectorError) {
	var info types.ThermalInfo
	var errs []types.CollectorError

	if !util.CommandExists("nvidia-smi") {
		errs = append(errs, types.CollectorError{
			Collector: "thermal",
			Error:     "nvidia-smi not found in PATH",
			Fatal:     true,
		})
		return info, errs
	}

	// Query thermal, power state, clocks, power limits, fan speed
	r := util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu=temperature.gpu,power.state,clocks.current.graphics,clocks.max.graphics,power.limit,power.draw,fan.speed",
		"--format=csv,noheader,nounits")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "thermal.query",
			Error:     fmt.Sprintf("nvidia-smi thermal query failed: %v", r.Err),
			Fatal:     true,
		})
		return info, errs
	}

	// Parse first line of CSV output (first GPU)
	line := strings.TrimSpace(r.Stdout)
	if line == "" {
		errs = append(errs, types.CollectorError{
			Collector: "thermal.parse",
			Error:     "nvidia-smi thermal query returned empty output",
			Fatal:     true,
		})
		return info, errs
	}

	// Take only the first GPU line if there are multiple
	lines := strings.Split(line, "\n")
	fields := strings.Split(lines[0], ", ")

	if len(fields) >= 1 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[0])); err == nil {
			info.TemperatureC = v
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.temperature",
				Error:     fmt.Sprintf("failed to parse temperature: %s", fields[0]),
			})
		}
	}

	if len(fields) >= 2 {
		info.PowerState = strings.TrimSpace(fields[1])
	}

	if len(fields) >= 3 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[2])); err == nil {
			info.CurrentClockMHz = v
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.current_clock",
				Error:     fmt.Sprintf("failed to parse current clock: %s", fields[2]),
			})
		}
	}

	if len(fields) >= 4 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[3])); err == nil {
			info.MaxClockMHz = v
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.max_clock",
				Error:     fmt.Sprintf("failed to parse max clock: %s", fields[3]),
			})
		}
	}

	if len(fields) >= 5 {
		info.PowerLimitW = strings.TrimSpace(fields[4])
	}

	if len(fields) >= 6 {
		info.PowerDrawW = strings.TrimSpace(fields[5])
	}

	if len(fields) >= 7 {
		if v, err := strconv.Atoi(strings.TrimSpace(fields[6])); err == nil {
			info.FanSpeedPct = v
		} else {
			// Fan speed may report "[Not Supported]" on some GPUs (e.g., laptop)
			errs = append(errs, types.CollectorError{
				Collector: "thermal.fan_speed",
				Error:     fmt.Sprintf("failed to parse fan speed: %s", fields[6]),
			})
		}
	}

	// Query clock event reasons for slowdown detection
	r = util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu=clocks_event_reasons.active",
		"--format=csv,noheader")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "thermal.slowdown",
			Error:     fmt.Sprintf("nvidia-smi clocks_event_reasons query failed: %v", r.Err),
		})
	} else {
		reason := strings.TrimSpace(r.Stdout)
		// Take only first GPU line
		if idx := strings.Index(reason, "\n"); idx >= 0 {
			reason = strings.TrimSpace(reason[:idx])
		}
		info.SlowdownReason = reason

		// Any non-zero / non-"Not Active" / non-empty value indicates throttling
		reasonLower := strings.ToLower(reason)
		if reason != "" && reason != "0" && reason != "0x0000000000000000" &&
			!strings.Contains(reasonLower, "not active") &&
			reasonLower != "none" {
			info.SlowdownActive = true
		}

		// Determine thermal throttle: temp >= 85 or slowdown reason mentions thermal
		if info.TemperatureC >= 85 || strings.Contains(reasonLower, "thermal") {
			info.ThermalThrottle = true
		}
	}

	// If we could not query slowdown reasons, still check temperature threshold
	if info.TemperatureC >= 85 {
		info.ThermalThrottle = true
	}

	return info, errs
}
