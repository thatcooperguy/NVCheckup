package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// ThermalQueryFields is the exact --query-gpu field list used by the thermal
// collector. It is exported so self-test can verify the driver accepts it.
// The leading "index" makes every CSV row self-identifying so multi-GPU rigs
// map each row to the right GPU. Note that the power-state field is named
// "pstate" (the older "power.state" spelling is rejected by nvidia-smi and
// made this collector fail on every machine).
const ThermalQueryFields = "index,temperature.gpu,pstate,clocks.current.graphics,clocks.max.graphics,power.limit,power.draw,fan.speed,utilization.gpu"

// ThermalEventQueryFields is the clock-event bitmask field on R535+ drivers.
const ThermalEventQueryFields = "clocks_event_reasons.active"

// ThermalEventQueryFieldsLegacy is the pre-R535 spelling of the same bitmask.
const ThermalEventQueryFieldsLegacy = "clocks_throttle_reasons.active"

// ClockEventQuery returns the --query-gpu list the collector uses for a clock
// event field: the GPU index followed by the field, so rows are per-GPU.
func ClockEventQuery(field string) string {
	return "index," + field
}

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

// CollectThermalInfo gathers GPU thermal, power state, and clock data for the
// first NVIDIA GPU via nvidia-smi. It is a thin wrapper over CollectThermalAll
// kept for callers that only understand a single GPU; the zero value is
// returned when no GPU row was parsed.
func CollectThermalInfo(timeout int) (types.ThermalInfo, []types.CollectorError) {
	all, errs := CollectThermalAll(timeout)
	if len(all) == 0 {
		return types.ThermalInfo{}, errs
	}
	return all[0], errs
}

// CollectThermalAll gathers thermal, power state and clock data for every
// NVIDIA GPU nvidia-smi reports, one entry per GPU in nvidia-smi index order.
func CollectThermalAll(timeout int) ([]types.ThermalInfo, []types.CollectorError) {
	var errs []types.CollectorError

	if !util.CommandExists("nvidia-smi") {
		if e := missingNvidiaSmiError("thermal", isJetsonHost()); e != nil {
			errs = append(errs, *e)
		}
		return nil, errs
	}

	rows, qerr, ok := nvidiaSmiRows(timeout, "thermal", "thermal", ThermalQueryFields, true)
	if !ok {
		return nil, append(errs, qerr)
	}

	infos, parseErrs := parseThermalRows(rows)
	errs = append(errs, parseErrs...)
	if len(infos) == 0 {
		return nil, errs
	}

	// Clock event reasons: try the current field name first, then the legacy
	// spelling used by drivers older than R535. Rows are keyed by GPU index.
	masks, ok := queryThrottleMasks(timeout, &errs)
	if ok {
		applyThrottleMasks(infos, masks, &errs)
	}

	return infos, errs
}

// applyThrottleMasks decodes each GPU's raw clock event mask into its
// ThermalInfo entry, matching rows by GPU index.
func applyThrottleMasks(infos []types.ThermalInfo, masks map[int]string, errs *[]types.CollectorError) {
	for i := range infos {
		raw, have := masks[infos[i].GPUIndex]
		if !have {
			continue
		}
		infos[i].SlowdownReason = raw
		mask, err := parseThrottleMask(raw)
		if err != nil {
			*errs = append(*errs, types.CollectorError{
				Collector: "thermal.slowdown",
				Error:     fmt.Sprintf("GPU %d: could not decode clock event reasons %q: %v", infos[i].GPUIndex, raw, err),
			})
			continue
		}
		applyThrottleMask(&infos[i], mask)
	}
}

// parseThermalRows parses every CSV row produced by ThermalQueryFields into
// one ThermalInfo per GPU. Rows that carry no usable index fall back to their
// ordinal position.
func parseThermalRows(rows []string) ([]types.ThermalInfo, []types.CollectorError) {
	var infos []types.ThermalInfo
	var errs []types.CollectorError
	for i, row := range rows {
		info, rowErrs := parseThermalRow(row, i)
		infos = append(infos, info)
		errs = append(errs, rowErrs...)
	}
	return infos, errs
}

// queryThrottleMasks returns the raw clock event reason value per GPU index,
// falling back to the legacy field name when the driver rejects the modern one.
func queryThrottleMasks(timeout int, errs *[]types.CollectorError) (map[int]string, bool) {
	fields := []string{ThermalEventQueryFields, ThermalEventQueryFieldsLegacy}
	var lastErr string
	for _, f := range fields {
		r := util.RunCommand(timeout, "nvidia-smi", "--query-gpu="+ClockEventQuery(f), "--format=csv,noheader")
		if r.Err == nil {
			return parseThrottleRows(r.Stdout), true
		}
		lastErr = f + ": " + commandFailureDetail(r)
		if r.TimedOut {
			break
		}
	}
	*errs = append(*errs, types.CollectorError{
		Collector: "thermal.slowdown",
		Error:     "nvidia-smi clock event reasons query failed: " + lastErr,
	})
	return nil, false
}

// parseThrottleRows maps "index, mask" CSV rows to raw mask strings keyed by
// GPU index. Output without a comma (a single mask column, as produced when
// the query is run without "index") is keyed by ordinal position instead.
func parseThrottleRows(out string) map[int]string {
	masks := map[int]string{}
	rows, other := csvRows(out)
	if len(rows) == 0 {
		for i, t := range other {
			masks[i] = t
		}
		return masks
	}
	for i, row := range rows {
		idx, rest := parseRowIndex(splitCSV(row), i)
		if len(rest) > 0 {
			masks[idx] = rest[0]
		}
	}
	return masks
}

// parseThermalCSV parses one nvidia-smi CSV line produced by ThermalQueryFields
// (index, temperature, pstate, current clock, max clock, power limit, power
// draw, fan, utilization). It is a pure function so it can be unit-tested with
// captured output. A row without a parsable index is assigned index 0.
func parseThermalCSV(line string) (types.ThermalInfo, []types.CollectorError) {
	return parseThermalRow(line, 0)
}

// parseThermalRow is parseThermalCSV with an explicit fallback index for rows
// whose leading index field is missing or unreadable.
func parseThermalRow(line string, ordinal int) (types.ThermalInfo, []types.CollectorError) {
	var info types.ThermalInfo
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

	// Fan: passive (data-center and Tesla), water-cooled, and most laptop GPUs
	// report "[N/A]" or "[Not Supported]". Storing 0 there would look like a
	// stalled fan, so we record FanSupported=false and leave the percentage
	// untouched.
	if fan := get(6); fan != "" {
		if isNotAvailable(fan) {
			info.FanSupported = false
		} else if v, err := strconv.Atoi(fan); err == nil {
			info.FanSpeedPct = v
			info.FanSupported = true
		} else {
			errs = append(errs, types.CollectorError{
				Collector: "thermal.fan_speed",
				Error:     fmt.Sprintf("GPU %d: failed to parse fan speed: %s", idx, fan),
			})
		}
	}

	// Utilization is "[N/A]" on MIG-enabled GPUs and some virtual GPUs; it
	// stays 0 there and the analyzer treats 0 as "no load evidence".
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

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// commandFailureDetail returns a one-line reason for a failed command: the
// first non-empty of trimmed stderr, the first line of stdout, and the Go
// error. nvidia-smi prints the "not a valid field to query" message to
// STDOUT with exit 2 and an empty stderr, so stderr alone loses the reason.
func commandFailureDetail(r util.CommandResult) string {
	if s := firstLine(r.Stderr); s != "" {
		return s
	}
	if s := firstLine(r.Stdout); s != "" {
		return s
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
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
