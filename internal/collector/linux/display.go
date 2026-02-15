//go:build linux

package linux

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectDisplayInfo gathers display/monitor information on Linux by parsing
// xrandr output. Falls back to wlr-randr for Wayland sessions.
func CollectDisplayInfo(timeout int) ([]types.DisplayInfo, []types.CollectorError) {
	var displays []types.DisplayInfo
	var errs []types.CollectorError

	// Try xrandr first (works on X11 and some Wayland compositors via XWayland)
	if util.CommandExists("xrandr") {
		d, e := parseXrandr(timeout)
		displays = append(displays, d...)
		errs = append(errs, e...)
	}

	// If xrandr yielded nothing, try wlr-randr as a Wayland fallback
	if len(displays) == 0 && util.CommandExists("wlr-randr") {
		d, e := parseWlrRandr(timeout)
		displays = append(displays, d...)
		errs = append(errs, e...)
	}

	// If neither tool is available, report an error
	if !util.CommandExists("xrandr") && !util.CommandExists("wlr-randr") {
		errs = append(errs, types.CollectorError{
			Collector: "linux.display",
			Error:     "Neither xrandr nor wlr-randr found; cannot collect display info",
		})
	}

	// Mark the first display as primary if none are marked yet
	hasPrimary := false
	for _, d := range displays {
		if d.Primary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary && len(displays) > 0 {
		displays[0].Primary = true
	}

	return displays, errs
}

// parseXrandr runs xrandr --query and parses connected outputs.
func parseXrandr(timeout int) ([]types.DisplayInfo, []types.CollectorError) {
	var displays []types.DisplayInfo
	var errs []types.CollectorError

	r := util.RunCommand(timeout, "xrandr", "--query")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "linux.display.xrandr",
			Error:     "xrandr --query failed: " + r.Err.Error(),
		})
		return displays, errs
	}

	lines := strings.Split(r.Stdout, "\n")

	// Regex for connected output line, e.g.:
	//   DP-0 connected primary 2560x1440+0+0 (normal left inverted ...)
	//   HDMI-1 connected 1920x1080+2560+0 (normal left inverted ...)
	//   eDP-1 connected primary 1920x1080+0+0 (...)
	connectedRe := regexp.MustCompile(`^(\S+)\s+connected\s+(primary\s+)?(\d+x\d+\+\d+\+\d+)?`)

	// Regex for mode line, e.g.:
	//   2560x1440     143.86*+  120.00    59.95
	// The asterisk (*) marks the current mode, plus (+) marks the preferred mode.
	modeRe := regexp.MustCompile(`^\s+(\d+)x(\d+)\s+([\d.]+)\*`)

	var current *types.DisplayInfo

	for _, line := range lines {
		if m := connectedRe.FindStringSubmatch(line); m != nil {
			// Finish previous display if any
			if current != nil {
				displays = append(displays, *current)
			}

			outputName := m[1]
			isPrimary := strings.TrimSpace(m[2]) == "primary"
			resolution := ""
			if m[3] != "" {
				// Extract WxH from WxH+X+Y
				parts := strings.SplitN(m[3], "+", 2)
				if len(parts) >= 1 {
					resolution = parts[0]
				}
			}

			current = &types.DisplayInfo{
				Name:       outputName,
				Resolution: resolution,
				Primary:    isPrimary,
				OutputType: inferOutputTypeFromPortName(outputName),
			}
			continue
		}

		// Parse mode lines only when we have a current display and haven't set refresh yet
		if current != nil && current.RefreshHz == 0 {
			if m := modeRe.FindStringSubmatch(line); m != nil {
				// This is the currently active mode (has asterisk)
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])

				// Update resolution from the mode line if not already set from the header
				if current.Resolution == "" && w > 0 && h > 0 {
					current.Resolution = strconv.Itoa(w) + "x" + strconv.Itoa(h)
				}

				// Parse refresh rate
				rateStr := m[3]
				rate, err := strconv.ParseFloat(rateStr, 64)
				if err == nil {
					current.RefreshHz = int(rate + 0.5) // round to nearest int
				}
			}
		}
	}

	// Don't forget the last display
	if current != nil {
		displays = append(displays, *current)
	}

	return displays, errs
}

// parseWlrRandr runs wlr-randr and parses its output as a fallback for
// Wayland compositors that support the wlr-output-management protocol.
func parseWlrRandr(timeout int) ([]types.DisplayInfo, []types.CollectorError) {
	var displays []types.DisplayInfo
	var errs []types.CollectorError

	r := util.RunCommand(timeout, "wlr-randr")
	if r.Err != nil {
		errs = append(errs, types.CollectorError{
			Collector: "linux.display.wlr-randr",
			Error:     "wlr-randr failed: " + r.Err.Error(),
		})
		return displays, errs
	}

	lines := strings.Split(r.Stdout, "\n")

	// wlr-randr output format:
	//   DP-1 "Monitor Name (DP-1)"
	//     Enabled: yes
	//     Modes:
	//       2560x1440 px, 143.860001 Hz (preferred, current)
	//       1920x1080 px, 60.000000 Hz
	outputRe := regexp.MustCompile(`^(\S+)\s`)
	modeCurrentRe := regexp.MustCompile(`^\s+(\d+)x(\d+)\s+px,\s+([\d.]+)\s+Hz\s+.*current`)

	var current *types.DisplayInfo

	for _, line := range lines {
		// Check for output header line (not indented)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.TrimSpace(line) != "" {
			if m := outputRe.FindStringSubmatch(line); m != nil {
				if current != nil {
					displays = append(displays, *current)
				}
				outputName := m[1]
				current = &types.DisplayInfo{
					Name:       outputName,
					OutputType: inferOutputTypeFromPortName(outputName),
				}
			}
			continue
		}

		// Parse current mode
		if current != nil && current.RefreshHz == 0 {
			if m := modeCurrentRe.FindStringSubmatch(line); m != nil {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				if w > 0 && h > 0 {
					current.Resolution = strconv.Itoa(w) + "x" + strconv.Itoa(h)
				}
				rate, err := strconv.ParseFloat(m[3], 64)
				if err == nil {
					current.RefreshHz = int(rate + 0.5)
				}
			}
		}
	}

	if current != nil {
		displays = append(displays, *current)
	}

	return displays, errs
}

// inferOutputTypeFromPortName determines the display output type from the
// port/connector name used by xrandr or wlr-randr.
func inferOutputTypeFromPortName(name string) string {
	upper := strings.ToUpper(name)

	switch {
	case strings.HasPrefix(upper, "DP-") || strings.HasPrefix(upper, "DP"):
		return "DP"
	case strings.HasPrefix(upper, "HDMI-") || strings.HasPrefix(upper, "HDMI"):
		return "HDMI"
	case strings.HasPrefix(upper, "EDP-") || strings.HasPrefix(upper, "EDP"):
		return "eDP"
	case strings.HasPrefix(upper, "DVI-") || strings.HasPrefix(upper, "DVI"):
		return "DVI"
	case strings.HasPrefix(upper, "VGA-") || strings.HasPrefix(upper, "VGA"):
		return "VGA"
	case strings.HasPrefix(upper, "USB-") || strings.Contains(upper, "USB"):
		return "USB-C"
	case strings.HasPrefix(upper, "VIRTUAL") || strings.HasPrefix(upper, "DUMMY"):
		return "Virtual"
	default:
		return "Unknown"
	}
}
