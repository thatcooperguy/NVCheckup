package windows

// RTX Spark (N1X) adapter and toolkit facts on Windows on Arm, spec
// docs/roadmap/spark-support.md 3.1 row 2, 3.2 "RTX Spark WMI" and section 8.
// This file has no build tag: its parsers are pure and unit-tested on every
// OS, and the WMI query only runs when woa.go (Windows-only) calls it.

import (
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// PE machine types (IMAGE_FILE_MACHINE_*), shared by IsWow64Process2 and the
// nvcc.exe header check (spec 3.1 row 1, section 8).
const (
	machineUnknown uint16 = 0x0
	machineI386    uint16 = 0x14c
	machineARMNT   uint16 = 0x1c4
	machineAMD64   uint16 = 0x8664
	machineARM64   uint16 = 0xAA64
)

// rtxSparkDeviceIDs are the N1X PCI device ids (spec 2.2, S50) with their
// core counts (spec 2.2 driver names).
var rtxSparkDeviceIDs = map[string]string{
	"2E03": "6144-core",
	"2E06": "5120-core",
}

const (
	// rtxSparkNameMarker is the adapter-name test of spec 3.1 row 2.
	rtxSparkNameMarker = "RTX Spark N1X"
	// rtxSparkINF is the 616.00 Developer Preview INF (spec 2.2, 3.2).
	rtxSparkINF = "nv_surface_woa.inf"
	// developerPreviewDriverSuffix: WDDM DriverVersion ending 16.1600 = 616.00
	// (spec 3.1 row 2: the 32.0 prefix is unconfirmed, match the tail only).
	developerPreviewDriverSuffix = "16.1600"
	// win32ProcessorArchitectureARM64 is Win32_Processor.Architecture on Arm
	// (spec 3.2: 12).
	win32ProcessorArchitectureARM64 = "12"
)

// machineName maps a PE machine constant to the report label.
func machineName(m uint16) string {
	switch m {
	case machineARM64:
		return "ARM64"
	case machineAMD64:
		return "AMD64"
	case machineI386:
		return "I386"
	case machineARMNT:
		return "ARM"
	case machineUnknown:
		return ""
	default:
		return fmt.Sprintf("0x%04X", m)
	}
}

// applyWow64 fills the row-1 facts from IsWow64Process2: native machine,
// Windows-on-Arm when the host is ARM64, and ProcessEmulated when the process
// machine is not IMAGE_FILE_MACHINE_UNKNOWN (spec 3.1 row 1, S73).
func applyWow64(p *types.PlatformInfo, processMachine, nativeMachine uint16) {
	p.NativeMachine = machineName(nativeMachine)
	if nativeMachine == machineARM64 {
		p.IsWindowsOnArm = true
	}
	p.ProcessEmulated = processMachine != machineUnknown
}

// applyArchEnv is the fallback when IsWow64Process2 is unavailable:
// PROCESSOR_ARCHITECTURE / PROCESSOR_ARCHITEW6432 (spec section 8). ARM64 in
// either marks Windows on Arm; ARCHITEW6432 being set at all means this
// process runs under WOW emulation.
func applyArchEnv(p *types.PlatformInfo, arch, archW6432 string) {
	arch = strings.ToUpper(strings.TrimSpace(arch))
	archW6432 = strings.ToUpper(strings.TrimSpace(archW6432))
	native := archW6432
	if native == "" {
		native = arch
	}
	if native != "" && p.NativeMachine == "" {
		p.NativeMachine = native
	}
	if arch == "ARM64" || archW6432 == "ARM64" {
		p.IsWindowsOnArm = true
	}
	if archW6432 != "" {
		p.ProcessEmulated = true
	}
}

// wmiVideoAdapter is one Win32_VideoController row of the RTX Spark query.
type wmiVideoAdapter struct {
	Name          string
	PNPDeviceID   string
	DriverVersion string
	InfFilename   string
}

// videoAdapterScript prints "Name|PNPDeviceID|DriverVersion|InfFilename"
// per adapter (spec 3.2 field list).
const videoAdapterScript = `$ErrorActionPreference = 'SilentlyContinue'; Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.Name)|$($_.PNPDeviceID)|$($_.DriverVersion)|$($_.InfFilename)" }; exit 0`

// parseVideoAdapterRows parses the pipe-separated rows of videoAdapterScript.
func parseVideoAdapterRows(out string) []wmiVideoAdapter {
	var rows []wmiVideoAdapter
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		a := wmiVideoAdapter{Name: strings.TrimSpace(parts[0]), PNPDeviceID: strings.TrimSpace(parts[1])}
		if len(parts) > 2 {
			a.DriverVersion = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			a.InfFilename = strings.TrimSpace(parts[3])
		}
		rows = append(rows, a)
	}
	return rows
}

// rtxSparkDeviceID returns the N1X device id ("2E03"/"2E06") when the PNP id
// is PCI\VEN_10DE&DEV_2E03/2E06, or "" otherwise (spec 3.1 row 2).
func rtxSparkDeviceID(pnpDeviceID string) string {
	upper := strings.ToUpper(pnpDeviceID)
	if !strings.Contains(upper, "VEN_10DE&") {
		return ""
	}
	for id := range rtxSparkDeviceIDs {
		if strings.Contains(upper, "&DEV_"+id) {
			return id
		}
	}
	return ""
}

// isRTXSparkAdapter applies the spec 3.1 row-2 test: PNP DEV_2E03/2E06 or an
// adapter name containing "RTX Spark N1X".
func isRTXSparkAdapter(a wmiVideoAdapter) bool {
	return rtxSparkDeviceID(a.PNPDeviceID) != "" || strings.Contains(a.Name, rtxSparkNameMarker)
}

// isDeveloperPreviewDriver reports the 616.00 Developer Preview: a WDDM
// DriverVersion ending in 16.1600 (only the tail is matched, spec 3.1 row 2)
// or the nv_surface_woa.inf INF (spec 2.2).
func isDeveloperPreviewDriver(driverVersion, inf string) bool {
	v := strings.TrimSpace(driverVersion)
	if v != "" && strings.HasSuffix(v, developerPreviewDriverSuffix) {
		// Guard against a bare "16.1600" style tail without a separator being
		// part of a longer number (e.g. "...116.1600" is still a 16.1600 tail
		// in WDDM terms, so only require the suffix).
		return true
	}
	return strings.EqualFold(strings.TrimSpace(filepath.Base(inf)), rtxSparkINF)
}

// applyRTXSparkAdapter picks the N1X adapter out of the WMI rows and fills
// PlatformInfo: Class rtx-spark, GPUSoC N1X and the WoA adapter facts. It
// returns false when no row matches.
func applyRTXSparkAdapter(p *types.PlatformInfo, rows []wmiVideoAdapter) bool {
	for _, a := range rows {
		if !isRTXSparkAdapter(a) {
			continue
		}
		p.Class = "rtx-spark"
		p.GPUSoC = "N1X"
		if p.WoA == nil {
			p.WoA = &types.WoAInfo{}
		}
		p.WoA.AdapterName = a.Name
		p.WoA.PNPDeviceID = a.PNPDeviceID
		p.WoA.DriverVersion = a.DriverVersion
		p.WoA.InfFilename = a.InfFilename
		p.WoA.DeveloperPreview = isDeveloperPreviewDriver(a.DriverVersion, a.InfFilename)
		return true
	}
	return false
}

// collectRTXSparkAdapter runs the Win32_VideoController query and applies it.
func collectRTXSparkAdapter(timeout int, p *types.PlatformInfo, errs *[]types.CollectorError) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", videoAdapterScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "windows.woa.adapter", Error: "Win32_VideoController query failed: " + r.Err.Error()})
		return
	}
	applyRTXSparkAdapter(p, parseVideoAdapterRows(r.Stdout))
}

// peMachine returns the machine type of a PE executable's file header.
func peMachine(path string) (uint16, error) {
	f, err := pe.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.FileHeader.Machine, nil
}

// findNvcc locates nvcc.exe: %CUDA_PATH%\bin first, then PATH.
func findNvcc() string {
	if cudaPath := os.Getenv("CUDA_PATH"); cudaPath != "" {
		candidate := filepath.Join(cudaPath, "bin", "nvcc.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("nvcc"); err == nil {
		return p
	}
	return ""
}

// collectNvccMachine records the PE machine of nvcc.exe (rule
// woa-cuda-toolkit-not-native: AMD64 means the toolkit runs under Prism).
func collectNvccMachine(p *types.PlatformInfo) {
	path := findNvcc()
	if path == "" {
		return
	}
	m, err := peMachine(path)
	if err != nil {
		return
	}
	if p.WoA == nil {
		p.WoA = &types.WoAInfo{}
	}
	p.WoA.NvccPath = path
	p.WoA.NvccMachine = machineName(m)
}

// systemProductScript prints "Architecture|ProductName|ProductVendor|Manufacturer|SystemType"
// (spec 3.2: Win32_Processor.Architecture 12, Win32_ComputerSystemProduct.Name).
const systemProductScript = `$ErrorActionPreference = 'SilentlyContinue'; $p = Get-CimInstance Win32_Processor | Select-Object -First 1; $c = Get-CimInstance Win32_ComputerSystemProduct; $s = Get-CimInstance Win32_ComputerSystem; "$($p.Architecture)|$($c.Name)|$($c.Vendor)|$($s.Manufacturer)|$($s.SystemType)"; exit 0`

// systemProduct is the parsed systemProductScript row.
type systemProduct struct {
	Architecture string
	ProductName  string
	Vendor       string
	Manufacturer string
	SystemType   string
}

// parseSystemProduct parses the single pipe-separated row.
func parseSystemProduct(out string) systemProduct {
	parts := strings.Split(strings.TrimSpace(firstLineOfText(out)), "|")
	var sp systemProduct
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	sp.Architecture = get(0)
	sp.ProductName = get(1)
	sp.Vendor = get(2)
	sp.Manufacturer = get(3)
	sp.SystemType = get(4)
	return sp
}

// applySystemProduct fills Vendor/Model (when empty) and the WMI arm test:
// Win32_Processor.Architecture == 12 or SystemType "ARM64-based PC".
func applySystemProduct(p *types.PlatformInfo, sp systemProduct) {
	if sp.Architecture == win32ProcessorArchitectureARM64 || strings.Contains(strings.ToUpper(sp.SystemType), "ARM64") {
		p.IsWindowsOnArm = true
		if p.NativeMachine == "" {
			p.NativeMachine = "ARM64"
		}
	}
	if p.Model == "" && sp.ProductName != "" {
		p.Model = sp.ProductName
	}
	if p.Vendor == "" {
		p.Vendor = util.FirstNonEmpty(sp.Manufacturer, sp.Vendor)
	}
}

// firstLineOfText returns the first line of s, trimmed (windows.go has
// firstLine under a build tag; this untagged file needs its own).
func firstLineOfText(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
