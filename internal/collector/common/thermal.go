package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// ThermalQueryFields is the exact --query-gpu field list used by CollectThermalInfo.
// It is exported so self-test can verify the driver accepts it. Note that the
// power-state field is named "pstate" (the older "power.state" spelling is
// rejected by nvidia-smi and made this collector fail on every machine).
const ThermalQueryFields = "temperature.gpu,pstate,clocks.current.graphics,clocks.max.graphics,power.limit,power.draw,fan.speed,utilization.gpu"

// ThermalEventQueryFields is the clock-event bitmask field on R535+ drivers.
const ThermalEventQueryFields = "clocks_event_reasons.active"

// ThermalEventQueryFieldsLegacy is the pre-R535 spelling of the same bitmask.
const ThermalEventQueryFieldsLegacy = "clocks_throttle_reasons.active"

// NVML clock event reason bits (nvmlClocksEventReasons / nvmlClocksThrottleReasons).
const (
	throttleGPUIdle              uint64 = 0x1
	throttleAppClocksSetting     uint64 = 0x2
	throttleSWPowerCap           uint64 = 0x4
	throttleHWSlowdown           uint64 = 0x8
	throttleSyncBoost            uint64 = 0x10
	throttleSWThermalSlowdown    uint64 = 0x20
	throttleHWThermalSlowdown    uint64 = 0x40
	throttleHWPowerBrakeSlowdown uint64 = 0x80
	throttleDisplayClockSetting  uint64 = 0x100
)

// throttleReasonNames maps each NVML bit to its canonical name, in bit order so
// ThrottleReasons is deterministic.
var throttleReasonNames = []struct {
	bit  uint64
	name string
}{
	{throttleGPUIdle, "gpu_idle"},
	{throttleAppClocksSetting, "applications_clocks_setting"},
	{throttleSWPowerCap, "sw_power_cap"},
	{throttleHWSlowdown, "hw_slowdown"},
	{throttleSyncBoost, "sync_boost"},
	{throttleSWThermalSlowdown, "sw_thermal_slowdown"},
	{throttleHWThermalSlowdown, "hw_thermal_slowdown"},
	{throttleHWPowerBrakeSlowdown, "hw_power_brake_slowdown"},
	{throttleDisplayClockSetting, "display_clock_setting"},
}

// slowdownBits are the reasons that mean the GPU is being held back by power,
// thermal, or hardware protection. gpu_idle (0x1) is deliberately excluded: an
// idle GPU at P8 with low clocks is the normal power-saving state, not a problem,
// and treating it as a slowdown produced false positives on every idle desktop.
const slowdownBits = throttleSWPowerCap | throttleHWSlowdown | throttleSWThermalSlowdown |
	throttleHWThermalSlowdown | throttleHWPowerBrakeSlowdown

// thermalBits are the reasons that are unambiguously thermal.
const thermalBits = throttleSWThermalSlowdown | throttleHWThermalSlowdown

// hwSlowdownThermalTempC is the temperature at or above which a generic
// hw_slowdown (0x8) is attributed to heat. hw_slowdown also fires for power
// brake and external events, so it is only thermal when the die is hot.
const hwSlowdownThermalTempC = 83

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

	r := util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu="+ThermalQueryFields,
		"--format=csv,noheader,nounits")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "thermal.query",
			Error:     fmt.Sprintf("nvidia-smi thermal query failed: %v (%s)", r.Err, strings.TrimSpace(r.Stderr)),
			Fatal:     true,
		})
		return info, errs
	}

	line := firstLine(r.Stdout)
	if line == "" {
		errs = append(errs, types.CollectorError{
			Collector: "thermal.parse",
			Error:     "nvidia-smi thermal query returned empty output",
			Fatal:     true,
		})
		return info, errs
	}

	info, parseErrs := parseThermalCSV(line)
	errs = append(errs, parseErrs...)

	// Clock event reasons: try the current field name first, then the legacy
	// spelling used by drivers older than R535.
	raw, ok := queryThrottleMask(timeout, &errs)
	if ok {
		info.SlowdownReason = raw
		mask, err := parseThrottleMask(raw)
		if err != nil {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.slowdown",
				Error:     fmt.Sprintf("could not decode clock event reasons %q: %v", raw, err),
			})
		} else {
			applyThrottleMask(&info, mask)
		}
	}

	return info, errs
}

// queryThrottleMask returns the raw first-GPU clock event reason value, falling
// back to the legacy field name when the driver rejects the modern one.
func queryThrottleMask(timeout int, errs *[]types.CollectorError) (string, bool) {
	fields := []string{ThermalEventQueryFields, ThermalEventQueryFieldsLegacy}
	var lastErr string
	for _, f := range fields {
		r := util.RunCommand(timeout, "nvidia-smi", "--query-gpu="+f, "--format=csv,noheader")
		if r.Err == nil {
			return firstLine(r.Stdout), true
		}
		lastErr = fmt.Sprintf("%s: %v (%s)", f, r.Err, strings.TrimSpace(r.Stderr))
		if r.TimedOut {
			break
		}
	}
	*errs = append(*errs, types.CollectorError{
		Collector: "thermal.slowdown",
		Error:     "nvidia-smi clock event reasons query failed: " + lastErr,
	})
	return "", false
}

// parseThermalCSV parses one nvidia-smi CSV line produced by ThermalQueryFields
// (temperature, pstate, current clock, max clock, power limit, power draw, fan,
// utilization). It is a pure function so it can be unit-tested with captured output.
func parseThermalCSV(line string) (types.ThermalInfo, []types.CollectorError) {
	var info types.ThermalInfo
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

	if v, ok := parseInt(0, "thermal.temperature", "temperature"); ok {
		info.TemperatureC = v
	}
	if s := get(1); s != "" && !isNotAvailable(s) {
		info.PowerState = s
	}
	if v, ok := parseInt(2, "thermal.current_clock", "current clock"); ok {
		info.CurrentClockMHz = v
	}
	if v, ok := parseInt(3, "thermal.max_clock", "max clock"); ok {
		info.MaxClockMHz = v
	}
	if s := get(4); s != "" && !isNotAvailable(s) {
		info.PowerLimitW = s
	}
	if s := get(5); s != "" && !isNotAvailable(s) {
		info.PowerDrawW = s
	}

	// Fan: passive, water-cooled, and most laptop GPUs report "[N/A]" or
	// "[Not Supported]". Storing 0 there would look like a stalled fan, so we
	// record FanSupported=false and leave the percentage untouched.
	if fan := get(6); fan != "" {
		if isNotAvailable(fan) {
			info.FanSupported = false
		} else if v, err := strconv.Atoi(fan); err == nil {
			info.FanSpeedPct = v
			info.FanSupported = true
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.fan_speed",
				Error:     fmt.Sprintf("failed to parse fan speed: %s", fan),
			})
		}
	}

	if v, ok := parseInt(7, "thermal.utilization", "utilization"); ok {
		info.UtilizationPct = v
	}

	return info, errs
}

// parseThrottleMask decodes the clocks_event_reasons.active value reported by
// nvidia-smi. Modern drivers print a 64-bit hex bitmask such as
// "0x0000000000000040"; some older builds print "Not Active" or "None" when no
// bit is set. Returns an error only for values that are neither.
func parseThrottleMask(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	switch {
	case s == "":
		return 0, fmt.Errorf("empty value")
	case lower == "not active", lower == "none", lower == "n/a", lower == "[n/a]", lower == "[not supported]":
		return 0, nil
	}
	if strings.HasPrefix(lower, "0x") {
		return strconv.ParseUint(lower[2:], 16, 64)
	}
	return strconv.ParseUint(lower, 10, 64)
}

// decodeThrottleReasons returns the names of the active bits in mask, in NVML bit order.
func decodeThrottleReasons(mask uint64) []string {
	var names []string
	for _, r := range throttleReasonNames {
		if mask&r.bit != 0 {
			names = append(names, r.name)
		}
	}
	return names
}

// applyThrottleMask fills ThrottleReasons, SlowdownActive, and ThermalThrottle
// from a decoded bitmask, using the temperature already stored in info.
func applyThrottleMask(info *types.ThermalInfo, mask uint64) {
	info.ThrottleReasons = decodeThrottleReasons(mask)
	info.SlowdownActive = mask&slowdownBits != 0
	info.ThermalThrottle = mask&thermalBits != 0 ||
		(mask&throttleHWSlowdown != 0 && info.TemperatureC >= hwSlowdownThermalTempC)
}

// splitCSV splits an nvidia-smi CSV line on commas and trims each field.
func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// firstLine returns the first non-empty line of s, trimmed. nvidia-smi prints
// one CSV row per GPU; the collectors report the first GPU only.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// isNotAvailable reports whether an nvidia-smi field is a placeholder such as
// "[N/A]", "[Not Supported]", "[Unknown Error]", or "N/A".
func isNotAvailable(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return true
	}
	return strings.EqualFold(s, "N/A")
}
