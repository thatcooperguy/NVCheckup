//go:build windows

package windows

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// wmiVideoController represents a Win32_VideoController WMI object (one per
// graphics adapter, not per monitor).
type wmiVideoController struct {
	Name                        string
	CurrentHorizontalResolution int
	CurrentVerticalResolution   int
	CurrentRefreshRate          int
	AdapterCompatibility        string
}

// screenInfo is one entry from System.Windows.Forms.Screen.AllScreens: a
// logical monitor as the desktop sees it. Width/Height are physical pixels
// because the probe calls SetProcessDPIAware() first (see screensScript).
// RefreshHz is the per-monitor rate from EnumDisplaySettings(DeviceName,
// ENUM_CURRENT_SETTINGS); it is 0 when that P/Invoke failed, in which case
// the callers fall back to the adapter's WMI CurrentRefreshRate.
type screenInfo struct {
	DeviceName string `json:"DeviceName"`
	Primary    bool   `json:"Primary"`
	Width      int    `json:"W"`
	Height     int    `json:"H"`
	RefreshHz  int    `json:"Hz"`
}

// monitorIdentity is the EDID-derived identity of a connected monitor from
// root\wmi WmiMonitorID joined with WmiMonitorConnectionParams. It carries no
// PnP instance path on purpose: the instance path
// (DISPLAY\DEL41B3\5&1babaec3&0&UID266501_0) is machine-identifying.
type monitorIdentity struct {
	Manufacturer string // three-letter PnP vendor code, e.g. "DEL"
	FriendlyName string // EDID descriptor, e.g. "DELL U2720Q"
	Active       bool
	OutputType   string // "DP", "HDMI", ... from VideoOutputTechnology; "" if unknown
}

// screensScript enumerates monitors with WinForms. SetProcessDPIAware is
// P/Invoked first because without it Screen.Bounds is reported in
// DPI-scaled logical pixels (3072x1728 for a 4K panel at 125 %), which would
// mislead any resolution-based rule. The same Add-Type block declares DEVMODE
// and EnumDisplaySettings so each screen's current refresh rate is read per
// \\.\DISPLAYn device (Win32_VideoController only knows one rate per adapter,
// which hid mixed-refresh setups). RefreshHz returns 0 when the call fails so
// the Go side can fall back to the adapter value.
const screensScript = `$ErrorActionPreference = 'SilentlyContinue'; ` +
	`Add-Type -TypeDefinition '` + nvcDpiTypeDefinition + `'; ` +
	`[void][NvcDpi]::SetProcessDPIAware(); ` +
	`Add-Type -AssemblyName System.Windows.Forms; ` +
	`[System.Windows.Forms.Screen]::AllScreens | Select-Object DeviceName,Primary,@{n='W';e={$_.Bounds.Width}},@{n='H';e={$_.Bounds.Height}},@{n='Hz';e={[NvcDpi]::RefreshHz($_.DeviceName)}} | ConvertTo-Json -Compress; ` +
	`exit 0`

// nvcDpiTypeDefinition is the C# compiled by screensScript. It must not
// contain single quotes (it is embedded in a single-quoted PowerShell
// literal). DEVMODE uses the display-device union layout (dmPosition,
// dmDisplayOrientation, dmDisplayFixedOutput); Marshal.SizeOf yields 220
// bytes for the Unicode form, which EnumDisplaySettings requires in dmSize.
const nvcDpiTypeDefinition = `using System; using System.Runtime.InteropServices; ` +
	`public static class NvcDpi { ` +
	`[DllImport("user32.dll")] public static extern bool SetProcessDPIAware(); ` +
	`[StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] public struct DEVMODE { ` +
	`[MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string dmDeviceName; ` +
	`public ushort dmSpecVersion; public ushort dmDriverVersion; public ushort dmSize; public ushort dmDriverExtra; public uint dmFields; ` +
	`public int dmPositionX; public int dmPositionY; public uint dmDisplayOrientation; public uint dmDisplayFixedOutput; ` +
	`public short dmColor; public short dmDuplex; public short dmYResolution; public short dmTTOption; public short dmCollate; ` +
	`[MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string dmFormName; ` +
	`public ushort dmLogPixels; public uint dmBitsPerPel; public uint dmPelsWidth; public uint dmPelsHeight; public uint dmDisplayFlags; public uint dmDisplayFrequency; ` +
	`public uint dmICMMethod; public uint dmICMIntent; public uint dmMediaType; public uint dmDitherType; public uint dmReserved1; public uint dmReserved2; public uint dmPanningWidth; public uint dmPanningHeight; } ` +
	`[DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern bool EnumDisplaySettings(string deviceName, int modeNum, ref DEVMODE devMode); ` +
	`public static int RefreshHz(string deviceName) { DEVMODE dm = new DEVMODE(); dm.dmSize = (ushort)Marshal.SizeOf(typeof(DEVMODE)); ` +
	`if (EnumDisplaySettings(deviceName, -1, ref dm)) { return (int)dm.dmDisplayFrequency; } return 0; } }`

// monitorIdentityScript decodes the uint16 EDID strings of WmiMonitorID and
// joins the connector type from WmiMonitorConnectionParams by InstanceName.
// The InstanceName is used only as the join key inside PowerShell and is not
// printed.
const monitorIdentityScript = `$ErrorActionPreference = 'SilentlyContinue'; ` +
	`$conn = @{}; ` +
	`Get-CimInstance -Namespace root\wmi -ClassName WmiMonitorConnectionParams | ForEach-Object { $conn[$_.InstanceName] = $_.VideoOutputTechnology }; ` +
	`Get-CimInstance -Namespace root\wmi -ClassName WmiMonitorID | ForEach-Object { ` +
	`$m = -join ($_.ManufacturerName | Where-Object { $_ -ne 0 } | ForEach-Object { [char]$_ }); ` +
	`$n = -join ($_.UserFriendlyName | Where-Object { $_ -ne 0 } | ForEach-Object { [char]$_ }); ` +
	`"$m|$n|$($_.Active)|$($conn[$_.InstanceName])" }; ` +
	`exit 0`

const videoControllerScript = `Get-CimInstance -ClassName Win32_VideoController | Select-Object Name, CurrentHorizontalResolution, CurrentVerticalResolution, CurrentRefreshRate, AdapterCompatibility | ConvertTo-Json -Compress`

// displayProbeCache memoizes the three PowerShell display probes. Both
// CollectWindowsInfo (Windows.Monitors) and CollectDisplayInfo (Displays) need
// the same data and each PowerShell spawn costs one to two seconds, so the
// probes run once per process. Collector errors are handed to the first
// caller only, so they appear once in the report.
type displayProbeCache struct {
	mu      sync.Mutex
	loaded  bool
	screens []screenInfo
	idents  []monitorIdentity
	ctls    []wmiVideoController
	errs    []types.CollectorError
}

var displayProbes displayProbeCache

func (c *displayProbeCache) load(timeout int) ([]screenInfo, []monitorIdentity, []wmiVideoController, []types.CollectorError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.screens, c.idents, c.ctls, nil
	}
	c.loaded = true
	c.ctls = queryVideoControllers(timeout, &c.errs)
	c.screens = queryScreens(timeout, &c.errs)
	c.idents = queryMonitorIdentities(timeout, &c.errs)
	return c.screens, c.idents, c.ctls, c.errs
}

// CollectDisplayInfo returns one DisplayInfo per connected monitor.
//
// HDREnabled/VRREnabled/HDRCapable/ColorDepth are deliberately left at their
// zero values: the registry keys previously consulted (GraphicsDrivers\EnableHDR
// and nvlddmkm\Global\GSync) are not per-monitor state and are absent on most
// systems, so reporting them as "false" claimed a check that never happened.
// Reading real HDR/VRR state needs DXGI or NVAPI, which stdlib Go cannot reach
// without cgo.
func CollectDisplayInfo(timeout int) ([]types.DisplayInfo, []types.CollectorError) {
	screens, idents, ctls, errs := displayProbes.load(timeout)
	return buildDisplays(screens, idents, ctls), errs
}

// buildDisplays pairs enumerated screens with monitor identities and the
// driving adapter. It is pure so it can be unit-tested with captured output.
func buildDisplays(screens []screenInfo, idents []monitorIdentity, ctls []wmiVideoController) []types.DisplayInfo {
	var displays []types.DisplayInfo
	adapter, gpuIndex := pickAdapter(ctls)
	active := activeIdentities(idents)

	if len(screens) == 0 {
		// Screen enumeration failed (headless session, WinForms unavailable).
		// Fall back to the adapter view so the report is not empty, even
		// though this cannot distinguish multiple monitors on one GPU.
		for i, ctl := range ctls {
			if ctl.CurrentHorizontalResolution == 0 || ctl.CurrentVerticalResolution == 0 {
				continue
			}
			displays = append(displays, types.DisplayInfo{
				Name:       identityName(active, i, "Display "+strconv.Itoa(i+1)),
				Resolution: strconv.Itoa(ctl.CurrentHorizontalResolution) + "x" + strconv.Itoa(ctl.CurrentVerticalResolution),
				RefreshHz:  ctl.CurrentRefreshRate,
				OutputType: identityOutputType(active, i),
				GPUIndex:   i,
				Primary:    i == 0,
			})
		}
		return displays
	}

	for i, s := range screens {
		if s.Width == 0 || s.Height == 0 {
			continue
		}
		displays = append(displays, types.DisplayInfo{
			Name:       identityName(active, i, "Display "+strconv.Itoa(i+1)),
			Resolution: strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height),
			RefreshHz:  screenRefresh(s, adapter),
			OutputType: identityOutputType(active, i),
			GPUIndex:   gpuIndex,
			Primary:    s.Primary,
		})
	}
	return displays
}

// screenRefresh prefers the per-monitor rate EnumDisplaySettings reported
// for this screen and uses the adapter's WMI rate only when that P/Invoke
// failed (RefreshHz 0), so mixed-refresh multi-monitor setups stay visible.
func screenRefresh(s screenInfo, adapter wmiVideoController) int {
	if s.RefreshHz > 0 {
		return s.RefreshHz
	}
	return adapter.CurrentRefreshRate
}

// pickAdapter chooses the adapter that supplies GPUIndex and the fallback
// refresh rate: the first NVIDIA adapter, else the first adapter with a mode
// set, else the first adapter at all. The returned index is the ordinal of the
// controller actually chosen in ctls (Win32_VideoController order), so a
// monitor driven by a non-NVIDIA adapter at position 1 is not mis-attributed
// to GPU 0. Win32_VideoController has no link to \\.\DISPLAYn, so on
// multi-GPU systems every monitor is attributed to that one adapter.
func pickAdapter(ctls []wmiVideoController) (wmiVideoController, int) {
	for i, ctl := range ctls {
		if isNvidiaAdapter(ctl) {
			return ctl, i
		}
	}
	for i, ctl := range ctls {
		if ctl.CurrentRefreshRate > 0 {
			return ctl, i
		}
	}
	if len(ctls) > 0 {
		return ctls[0], 0
	}
	return wmiVideoController{}, 0
}

func isNvidiaAdapter(ctl wmiVideoController) bool {
	return strings.Contains(strings.ToLower(ctl.AdapterCompatibility), "nvidia") ||
		strings.Contains(strings.ToLower(ctl.Name), "nvidia")
}

// activeIdentities drops monitors WMI still remembers but that are not
// currently active, so the list lines up with Screen.AllScreens.
func activeIdentities(idents []monitorIdentity) []monitorIdentity {
	var out []monitorIdentity
	for _, id := range idents {
		if id.Active {
			out = append(out, id)
		}
	}
	return out
}

// identityName returns the friendly name for the i-th monitor. Screens and
// WMI identities are paired by ordinal because neither side exposes a shared
// key without EnumDisplayDevices; on mixed-monitor systems this can pair a
// name with a neighbouring screen, which is acceptable for a diagnostic
// listing and never leaks anything machine-specific.
func identityName(idents []monitorIdentity, i int, fallback string) string {
	if i < len(idents) {
		if n := friendlyMonitorName(idents[i]); n != "" {
			return n
		}
	}
	return fallback
}

func identityOutputType(idents []monitorIdentity, i int) string {
	if i < len(idents) && idents[i].OutputType != "" {
		return idents[i].OutputType
	}
	return "Unknown"
}

// friendlyMonitorName builds e.g. "DEL DELL U2720Q" from the EDID fields.
func friendlyMonitorName(id monitorIdentity) string {
	return strings.TrimSpace(strings.TrimSpace(id.Manufacturer) + " " + strings.TrimSpace(id.FriendlyName))
}

func queryVideoControllers(timeout int, errs *[]types.CollectorError) []wmiVideoController {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", videoControllerScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display",
			Error:     "Failed to query Win32_VideoController: " + r.Err.Error(),
		})
		return nil
	}
	ctls, err := parseVideoControllerJSON(r.Stdout)
	if err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display",
			Error:     "Could not parse Win32_VideoController output: " + err.Error(),
		})
	}
	return ctls
}

func queryScreens(timeout int, errs *[]types.CollectorError) []screenInfo {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", screensScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display.screens",
			Error:     "Failed to enumerate screens: " + r.Err.Error(),
		})
		return nil
	}
	screens, err := parseScreensJSON(r.Stdout)
	if err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display.screens",
			Error:     "Could not parse screen enumeration: " + err.Error(),
		})
	}
	return screens
}

func queryMonitorIdentities(timeout int, errs *[]types.CollectorError) []monitorIdentity {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", monitorIdentityScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "windows.display.monitorid",
			Error:     "Failed to query WmiMonitorID: " + r.Err.Error(),
		})
		return nil
	}
	return parseMonitorIdentityLines(r.Stdout)
}

// parseVideoControllerJSON parses ConvertTo-Json output, which is a bare
// object for one adapter and an array for several. Null numeric fields
// (headless adapters) decode to zero.
func parseVideoControllerJSON(raw string) ([]wmiVideoController, error) {
	var ctls []wmiVideoController
	if err := unmarshalObjectOrArray(raw, &ctls); err != nil {
		return nil, err
	}
	return ctls, nil
}

// parseScreensJSON parses the AllScreens ConvertTo-Json output (object or
// array). Entries with zero-size bounds are dropped by the callers.
func parseScreensJSON(raw string) ([]screenInfo, error) {
	var screens []screenInfo
	if err := unmarshalObjectOrArray(raw, &screens); err != nil {
		return nil, err
	}
	return screens, nil
}

// parseMonitorIdentityLines parses "MFR|Friendly name|True|10" lines.
func parseMonitorIdentityLines(out string) []monitorIdentity {
	var idents []monitorIdentity
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		id := monitorIdentity{
			Manufacturer: strings.TrimSpace(parts[0]),
			FriendlyName: strings.TrimSpace(parts[1]),
			Active:       true, // older drivers omit Active; assume connected
		}
		if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
			id.Active = strings.EqualFold(strings.TrimSpace(parts[2]), "True")
		}
		if len(parts) >= 4 {
			if code, err := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64); err == nil {
				id.OutputType = videoOutputTechnologyName(code)
			}
		}
		idents = append(idents, id)
	}
	return idents
}

// videoOutputTechnologyName maps D3DKMDT_VIDEO_OUTPUT_TECHNOLOGY values.
// USB-C DisplayPort alt-mode reports as external DisplayPort (10).
func videoOutputTechnologyName(code int64) string {
	switch code {
	case 0:
		return "VGA"
	case 4:
		return "DVI"
	case 5:
		return "HDMI"
	case 6, 11, 13, 0x80000000:
		return "Internal"
	case 10:
		return "DP"
	case 12:
		return "UDI"
	case 15:
		return "Miracast"
	default:
		return ""
	}
}

// unmarshalObjectOrArray accepts either a JSON array or a single JSON object
// (PowerShell's ConvertTo-Json emits a bare object for one-element input).
func unmarshalObjectOrArray[T any](raw string, out *[]T) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		return json.Unmarshal([]byte(raw), out)
	}
	var one T
	if err := json.Unmarshal([]byte(raw), &one); err != nil {
		return err
	}
	*out = append(*out, one)
	return nil
}
