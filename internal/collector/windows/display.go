//go:build windows

package windows

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// wmiVideoController represents a Win32_VideoController WMI object.
type wmiVideoController struct {
	Name                       string
	CurrentHorizontalResolution int
	CurrentVerticalResolution   int
	CurrentRefreshRate          int
	AdapterCompatibility       string
}

// CollectDisplayInfo gathers display/monitor information on Windows via WMI
// and registry queries for HDR and G-Sync/VRR status.
func CollectDisplayInfo(timeout int) ([]types.DisplayInfo, []types.CollectorError) {
	var displays []types.DisplayInfo
	var errs []types.CollectorError

	// Query video controllers via WMI
	controllers := queryVideoControllers(timeout, &errs)

	// Query HDR status from registry
	hdrEnabled := queryHDRStatus(timeout, &errs)

	// Query G-Sync/VRR status from registry
	vrrEnabled := queryGSyncStatus(timeout, &errs)

	// Build DisplayInfo entries from controllers
	for i, ctl := range controllers {
		di := types.DisplayInfo{
			Name:       ctl.Name,
			GPUIndex:   i,
			Primary:    i == 0,
			VRREnabled: vrrEnabled,
			HDREnabled: hdrEnabled,
		}

		if ctl.CurrentHorizontalResolution > 0 && ctl.CurrentVerticalResolution > 0 {
			di.Resolution = strconv.Itoa(ctl.CurrentHorizontalResolution) + "x" + strconv.Itoa(ctl.CurrentVerticalResolution)
		}

		if ctl.CurrentRefreshRate > 0 {
			di.RefreshHz = ctl.CurrentRefreshRate
		}

		// Determine output type heuristic based on adapter name
		di.OutputType = inferOutputTypeWindows(ctl.Name, ctl.AdapterCompatibility)

		displays = append(displays, di)
	}

	return displays, errs
}

// queryVideoControllers runs a PowerShell WMI query for Win32_VideoController
// and parses the JSON output into a slice of controller structs.
func queryVideoControllers(timeout int, errs *[]types.CollectorError) []wmiVideoController {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-CimInstance -ClassName Win32_VideoController | Select-Object Name, CurrentHorizontalResolution, CurrentVerticalResolution, CurrentRefreshRate, AdapterCompatibility | ConvertTo-Json`)

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display",
			Error:     "Failed to query Win32_VideoController: " + r.Err.Error(),
		})
		return nil
	}

	output := strings.TrimSpace(r.Stdout)
	if output == "" {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display",
			Error:     "Win32_VideoController returned empty output",
		})
		return nil
	}

	return parseVideoControllerJSON(output)
}

// parseVideoControllerJSON parses the PowerShell ConvertTo-Json output.
// The output may be a single JSON object (one GPU) or an array (multiple GPUs).
// We parse manually to avoid importing encoding/json, using only the standard
// library string/regexp/strconv.
func parseVideoControllerJSON(raw string) []wmiVideoController {
	var controllers []wmiVideoController

	// Split into individual object blocks by looking for { ... } pairs
	// This handles both single objects and arrays.
	blocks := extractJSONObjects(raw)

	for _, block := range blocks {
		ctl := wmiVideoController{}
		ctl.Name = extractJSONStringField(block, "Name")
		ctl.CurrentHorizontalResolution = extractJSONIntField(block, "CurrentHorizontalResolution")
		ctl.CurrentVerticalResolution = extractJSONIntField(block, "CurrentVerticalResolution")
		ctl.CurrentRefreshRate = extractJSONIntField(block, "CurrentRefreshRate")
		ctl.AdapterCompatibility = extractJSONStringField(block, "AdapterCompatibility")
		controllers = append(controllers, ctl)
	}

	return controllers
}

// extractJSONObjects splits a JSON string (single object or array) into
// individual object strings.
func extractJSONObjects(raw string) []string {
	var objects []string
	depth := 0
	start := -1

	for i, ch := range raw {
		switch ch {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, raw[start:i+1])
				start = -1
			}
		}
	}

	return objects
}

// extractJSONStringField extracts a string value for a given key from a JSON
// object string using regexp.
func extractJSONStringField(obj, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]*)"`)
	m := re.FindStringSubmatch(obj)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// extractJSONIntField extracts an integer value for a given key from a JSON
// object string using regexp.
func extractJSONIntField(obj, key string) int {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*(\d+)`)
	m := re.FindStringSubmatch(obj)
	if len(m) >= 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return n
		}
	}
	return 0
}

// queryHDRStatus checks the Windows registry for HDR enablement.
func queryHDRStatus(timeout int, errs *[]types.CollectorError) bool {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`try { (Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\GraphicsDrivers' -Name EnableHDR -ErrorAction Stop).EnableHDR } catch { "NotFound" }`)

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display.hdr",
			Error:     "Failed to query HDR registry key: " + r.Err.Error(),
		})
		return false
	}

	val := strings.TrimSpace(r.Stdout)
	return val == "1"
}

// queryGSyncStatus checks the Windows registry for G-Sync/VRR enablement.
func queryGSyncStatus(timeout int, errs *[]types.CollectorError) bool {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`try { (Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\nvlddmkm\Global\GSync' -ErrorAction Stop) | Out-String } catch { "NotFound" }`)

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display.gsync",
			Error:     "Failed to query G-Sync registry key: " + r.Err.Error(),
		})
		return false
	}

	val := strings.TrimSpace(r.Stdout)
	// If the key exists and is not "NotFound", G-Sync support is present.
	// Look for a value indicating enablement.
	if val == "NotFound" || val == "" {
		return false
	}

	// Check if output contains an enabled indicator
	lower := strings.ToLower(val)
	return strings.Contains(lower, "1") || strings.Contains(lower, "true") || strings.Contains(lower, "enabled")
}

// inferOutputTypeWindows attempts to determine the display output type from
// the adapter name and compatibility string.
func inferOutputTypeWindows(name, adapterCompat string) string {
	combined := strings.ToLower(name + " " + adapterCompat)

	if strings.Contains(combined, "displayport") || strings.Contains(combined, " dp") {
		return "DP"
	}
	if strings.Contains(combined, "hdmi") {
		return "HDMI"
	}
	if strings.Contains(combined, "usb-c") || strings.Contains(combined, "usb c") {
		return "USB-C"
	}
	if strings.Contains(combined, "dvi") {
		return "DVI"
	}
	if strings.Contains(combined, "vga") {
		return "VGA"
	}

	return "Unknown"
}
