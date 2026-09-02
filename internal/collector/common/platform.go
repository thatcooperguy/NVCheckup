package common

import (
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Platform classes, the closed set of docs/roadmap/spark-support.md section 3.1
// (also the values allowed in a rule's platforms[] list, section 5).
const (
	ClassDGXSpark    = "dgx-spark"
	ClassRTXSpark    = "rtx-spark"
	ClassJetson      = "jetson"
	ClassGraceHopper = "grace-hopper"
	ClassArm64DGPU   = "arm64-dgpu"
)

// MemoryReporting values for GPUInfo.MemoryReporting (spec section 4).
const (
	MemoryReportingDedicated    = "dedicated"     // nvidia-smi memory.total is a number
	MemoryReportingNotSupported = "not-supported" // memory.total is [N/A] / table Memory-Usage "Not Supported" (unified memory)
)

// Exact strings of spec section 3.1 / 3.2. Everything below is quoted from the
// spec; nothing is guessed.
const (
	// Row 4: /etc/dgx-release keys and values.
	dgxReleasePath      = "/etc/dgx-release"
	dgxReleaseName      = "DGX Spark"        // DGX_NAME="DGX Spark"
	dgxReleasePretty    = "NVIDIA DGX Spark" // DGX_PRETTY_NAME="NVIDIA DGX Spark"
	fastosReleasePath   = "/etc/fastos-release"
	fastosReleaseName   = "DGX SPARK FASTOS" // NAME="DGX SPARK FASTOS"
	deviceTreeModelFile = "/proc/device-tree/model"

	// Rows 5 and 6: PCI ids (vendor 10de). 2e12 is GB10 (captured in S23 and
	// S12); 2e03 / 2e06 are the RTX Spark N1X parts (pci-ids registry, S50).
	pciVendorNVIDIA = "10de"
	pciDeviceGB10   = "2e12"
	pciDeviceN1X6k  = "2e03" // 6,144-core N1X
	pciDeviceN1X5k  = "2e06" // 5,120-core N1X

	// Row 5: nvidia-smi -L name on DGX Spark (spec 2.1).
	nvidiaSmiNameGB10 = "NVIDIA GB10"

	// Row 2: RTX Spark adapter name token, INF and WDDM DriverVersion suffix.
	rtxSparkNameToken = "RTX Spark N1X"
	rtxSparkINF       = "nv_surface_woa.inf"
	rtxSparkWDDMTail  = "16.1600" // only the trailing part is confirmed (spec 2.2)

	// Row 9: the compute capability of GB10 (sm_121, spec 2.1). N1X is
	// inferred 12.1 too (spec 2.2), which is why row 9 never asserts a class.
	computeCapGB10 = "12.1"

	// Row 10: Founders Edition DMI strings.
	dmiVendorNVIDIA    = "NVIDIA"
	dmiProductDGXSpark = "NVIDIA_DGX_Spark"
	dmiVersionA7       = "A.7"

	// GPUSoC labels (spec section 4 comment: GB10 | N1X | GH200) plus the
	// row-9 marker.
	socGB10         = "GB10"
	socN1X          = "N1X"
	socUnknownCC121 = "unknown-cc12.1"

	// Row 1: Win32_Processor.Architecture values.
	windowsArchARM64 = 12
	windowsArchAMD64 = 9
)

// feBIOSVersions are the Founders Edition BIOS strings of row 10.
var feBIOSVersions = []string{"5.36_0ACUM018", "5.36_0ACUM023"}

// oemGB10Vendors are the OEM GB10 system vendors named in row 10 (compared
// case-insensitively because DMI casing varies between boards).
var oemGB10Vendors = []string{"ASUS", "HP", "LENOVO", "Dell", "MSI", "Acer", "GIGABYTE"}

// graceHopperNames are the nvidia-smi name tokens of row 7.
var graceHopperNames = []string{"GH200", "GB200", "GB300"}

// nvidiaKernelRe is the row 11 regex for Canonical's linux-nvidia kernel
// (-64k and -lowlatency variants accepted).
var nvidiaKernelRe = regexp.MustCompile(`^\d+\.\d+\.\d+-\d+-nvidia(-64k|-lowlatency)?$`)

// lspciIDRe finds the "[vvvv:dddd]" id pair on an lspci -nn line.
var lspciIDRe = regexp.MustCompile(`\[([0-9a-fA-F]{4}):([0-9a-fA-F]{4})\]`)

// gb10NameRe matches the exact row 5 nvidia-smi name "NVIDIA GB10" as a whole
// token (spec 2.1 quotes the name verbatim), so a "NVIDIA GB100 ..." datacenter
// part is not mistaken for the Spark SoC.
var gb10NameRe = regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(nvidiaSmiNameGB10) + `(\s|$)`)

// dmiSysfsDir holds the world-readable DMI strings on Linux.
const dmiSysfsDir = "/sys/class/dmi/id"

// dmiKeys maps the sysfs file name to the dmidecode -s keyword used as a
// fallback when sysfs is unavailable.
var dmiKeys = []struct{ sysfs, dmidecode string }{
	{"sys_vendor", "system-manufacturer"},
	{"product_name", "system-product-name"},
	{"product_version", "system-version"},
	{"bios_version", "bios-version"},
	{"bios_date", "bios-release-date"},
}

// windowsAdapter is one Win32_VideoController row as printed by
// windowsPlatformScript (row 2 inputs).
type windowsAdapter struct {
	Name, PNPDeviceID, InfFilename, DriverVersion string
}

// platformInputs is everything the phase-1 classifier looks at. It is filled
// from files, lspci, DMI and the kernel by DetectPlatform and by tests
// directly, so the decision table is exercised without hardware.
type platformInputs struct {
	GOOS, GOARCH string

	// Linux files (rows 3, 4).
	TegraReleasePresent bool
	DeviceTreeModel     string
	DGXRelease          string // content of /etc/dgx-release, "" when absent
	FastOSRelease       string // content of /etc/fastos-release

	// lspci -nn output (rows 5, 6); "" when lspci is unavailable.
	Lspci string

	// DMI (row 10) keyed by sysfs name.
	DMI map[string]string

	// Kernel release (row 11).
	Kernel string

	// Windows (rows 1, 2).
	ProcessorArchitecture int // Win32_Processor.Architecture, -1 when unknown
	ArchitEW6432          string
	Adapters              []windowsAdapter
	ComputerManufacturer  string
	ComputerProductName   string
	ComputerVersion       string
	BIOSVersion           string
	BIOSDate              string
}

// DetectPlatform classifies the machine from files, lspci, DMI and the kernel
// only (rows 1-4, 6, 10, 11 of spec table 3.1; the lspci half of row 5 also
// needs no GPU data and runs here). It runs in phase 1 right after
// CollectSystemInfo. Rows that need nvidia-smi data (5 by name, 7, 8, 9) and
// flag rules A-C are applied later by ApplyPlatformFlags.
func DetectPlatform(timeout int) (types.PlatformInfo, []types.CollectorError) {
	var errs []types.CollectorError
	in := platformInputs{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, ProcessorArchitecture: -1}
	switch {
	case util.IsWindows():
		gatherWindowsInputs(&in, timeout, &errs)
	case util.IsLinux():
		gatherLinuxInputs(&in, timeout, &errs)
	}
	return classifyPhase1(in), errs
}

// gatherLinuxInputs reads the row 3/4/10/11 files through SimPath and runs
// lspci -nn (rows 5/6) and, when sysfs has no DMI, dmidecode.
func gatherLinuxInputs(in *platformInputs, timeout int, errs *[]types.CollectorError) {
	in.TegraReleasePresent = SimFileExists(tegraReleasePath)
	in.DeviceTreeModel = readSimString(deviceTreeModelFile)
	if data, err := ReadSimFile(dgxReleasePath); err == nil {
		in.DGXRelease = string(data)
	}
	if data, err := ReadSimFile(fastosReleasePath); err == nil {
		in.FastOSRelease = string(data)
	}

	if util.CommandExists("lspci") {
		r := util.RunCommand(timeout, "lspci", "-nn")
		if r.Err == nil {
			in.Lspci = r.Stdout
		} else {
			*errs = append(*errs, types.CollectorError{Collector: "platform.lspci", Error: "lspci -nn failed: " + commandFailureDetail(r)})
		}
	}

	in.DMI = readDMI(timeout)
	in.Kernel = readKernelRelease(timeout)
}

// readDMI returns the DMI strings of row 10 from /sys/class/dmi/id, falling
// back to dmidecode -s (root only; failures are silent because the strings are
// a refinement, not a classification input).
func readDMI(timeout int) map[string]string {
	dmi := map[string]string{}
	for _, k := range dmiKeys {
		if v := readSimString(dmiSysfsDir + "/" + k.sysfs); v != "" {
			dmi[k.sysfs] = v
		}
	}
	if len(dmi) > 0 || !util.CommandExists("dmidecode") {
		return dmi
	}
	for _, k := range dmiKeys {
		r := util.RunCommand(timeout, "dmidecode", "-s", k.dmidecode)
		if r.Err == nil {
			if v := firstValueLine(r.Stdout); v != "" {
				dmi[k.sysfs] = v
			}
		}
	}
	return dmi
}

// firstValueLine returns the first non-empty line of dmidecode -s output that
// is not a comment. dmidecode prints "# SMBIOS implementations newer than
// version 3.x are not fully supported." (one or more '#' lines) before the
// value, so taking the first line would drop the value.
func firstValueLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return t
	}
	return ""
}

// readKernelRelease prefers /proc/sys/kernel/osrelease (so NVC_SIM_ROOT can
// inject a kernel string) and falls back to uname -r.
func readKernelRelease(timeout int) string {
	if v := readSimString("/proc/sys/kernel/osrelease"); v != "" {
		return v
	}
	r := util.RunCommand(timeout, "uname", "-r")
	if r.Err == nil {
		return strings.TrimSpace(r.Stdout)
	}
	return ""
}

// windowsPlatformScript prints the row 1/2/10 inputs as prefixed lines from a
// single PowerShell invocation: the processor architecture (12 = ARM64), the
// SMBIOS system and BIOS strings, and every video controller as
// "adapter=Name|PNPDeviceID|InfFilename|DriverVersion".
const windowsPlatformScript = `$ErrorActionPreference = 'SilentlyContinue'; ` +
	`"arch=$((Get-CimInstance Win32_Processor | Select-Object -First 1).Architecture)"; ` +
	`$cs = Get-CimInstance Win32_ComputerSystem; "manufacturer=$($cs.Manufacturer)"; ` +
	`$csp = Get-CimInstance Win32_ComputerSystemProduct; "product=$($csp.Name)"; "version=$($csp.Version)"; ` +
	`$b = Get-CimInstance Win32_BIOS; "bios=$($b.SMBIOSBIOSVersion)"; "bios_date=$($b.ReleaseDate)"; ` +
	`Get-CimInstance Win32_VideoController | ForEach-Object { "adapter=$($_.Name)|$($_.PNPDeviceID)|$($_.InfFilename)|$($_.DriverVersion)" }; exit 0`

func gatherWindowsInputs(in *platformInputs, timeout int, errs *[]types.CollectorError) {
	in.ArchitEW6432 = os.Getenv("PROCESSOR_ARCHITEW6432")
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", windowsPlatformScript)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "platform.wmi", Error: "WMI platform query failed: " + commandFailureDetail(r)})
		return
	}
	parseWindowsPlatformOutput(in, r.Stdout)
}

// parseWindowsPlatformOutput fills the Windows inputs from windowsPlatformScript output.
func parseWindowsPlatformOutput(in *platformInputs, out string) {
	for _, line := range strings.Split(out, "\n") {
		k, v := util.ParseKeyValue(line, "=")
		v = strings.TrimSpace(v)
		switch k {
		case "arch":
			if n, ok := parseSmallInt(v); ok {
				in.ProcessorArchitecture = n
			}
		case "manufacturer":
			in.ComputerManufacturer = v
		case "product":
			in.ComputerProductName = v
		case "version":
			in.ComputerVersion = v
		case "bios":
			in.BIOSVersion = v
		case "bios_date":
			in.BIOSDate = v
		case "adapter":
			parts := strings.SplitN(v, "|", 4)
			for len(parts) < 4 {
				parts = append(parts, "")
			}
			if strings.TrimSpace(parts[0]) == "" {
				continue
			}
			in.Adapters = append(in.Adapters, windowsAdapter{
				Name: strings.TrimSpace(parts[0]), PNPDeviceID: strings.TrimSpace(parts[1]),
				InfFilename: strings.TrimSpace(parts[2]), DriverVersion: strings.TrimSpace(parts[3]),
			})
		}
	}
}

// classifyPhase1 evaluates rows 1-4, the lspci half of 5, 6, 10 and 11 of
// spec table 3.1 (first match wins for Class). It is a pure function.
func classifyPhase1(in platformInputs) types.PlatformInfo {
	var p types.PlatformInfo

	if in.GOOS == "windows" {
		classifyWindows(in, &p)
		return p
	}

	// Row 3: Jetson files (Thor included; it ships nvidia-smi, spec 2.3).
	if in.TegraReleasePresent || strings.Contains(in.DeviceTreeModel, "NVIDIA Jetson") {
		p.Class = ClassJetson
	}

	// Row 4: stock DGX OS on DGX Spark.
	dgx := ParseDGXRelease(in.DGXRelease)
	if p.Class == "" && (dgx.Name == dgxReleaseName || dgx.PrettyName == dgxReleasePretty || parseFastOSName(in.FastOSRelease) == fastosReleaseName) {
		p.Class = ClassDGXSpark
		p.GPUSoC = socGB10
	}

	// Rows 5 and 6 (PCI ids), evaluated before any compute-capability
	// heuristic so an N1X running Linux is never classed dgx-spark.
	ids := lspciNVIDIADeviceIDs(in.Lspci)
	if p.Class == "" && ids[pciDeviceGB10] {
		p.Class = ClassDGXSpark // row 5: GB10 hardware even when nvidia-smi is dead or DGX OS absent
		p.GPUSoC = socGB10
	}
	if p.Class == "" && (ids[pciDeviceN1X6k] || ids[pciDeviceN1X5k]) {
		p.Class = ClassRTXSpark // row 6: rtx-spark on Linux (unsupported)
		p.GPUSoC = socN1X
	}

	// Row 10: DMI strings are recorded for every class; on dgx-spark they
	// tell Founders Edition (NVIDIA / NVIDIA_DGX_Spark / A.7) from OEM boxes.
	p.Vendor = in.DMI["sys_vendor"]
	p.Model = in.DMI["product_name"]
	p.ProductVersion = in.DMI["product_version"]
	p.BIOSVersion = in.DMI["bios_version"]
	p.BIOSDate = in.DMI["bios_date"]
	if p.Model == "" && in.DeviceTreeModel != "" {
		p.Model = in.DeviceTreeModel
	}

	// Row 11: Canonical linux-nvidia kernel flavour (informational).
	p.NvidiaKernelFlavour = IsNvidiaKernelFlavour(in.Kernel)
	return p
}

// classifyWindows evaluates rows 1 and 2 plus the WMI equivalents of row 10.
func classifyWindows(in platformInputs, p *types.PlatformInfo) {
	// Row 1: Windows on Arm. A native arm64 build knows from GOARCH; an x64
	// build learns it from Win32_Processor.Architecture == 12 (or the
	// PROCESSOR_ARCHITEW6432 variable Prism sets), and is then emulated.
	switch {
	case in.GOARCH == "arm64":
		p.IsWindowsOnArm = true
		p.NativeMachine = "ARM64"
	case in.ProcessorArchitecture == windowsArchARM64 || strings.EqualFold(in.ArchitEW6432, "ARM64"):
		p.IsWindowsOnArm = true
		p.ProcessEmulated = true
		p.NativeMachine = "ARM64"
	case in.ProcessorArchitecture == windowsArchAMD64:
		p.NativeMachine = "AMD64"
	}

	// Row 2: RTX Spark N1X adapter. The PNP id (DEV_2E03 / DEV_2E06) and the
	// name token are sufficient on their own; the INF name and the WDDM
	// DriverVersion suffix only corroborate on a Windows-on-Arm host (row 1),
	// because spark-rules.json rtx-spark-detected triggers on "WoA + PNP" and a
	// 616.00-series WDDM version on an x64 desktop must not class a discrete GPU.
	for _, a := range in.Adapters {
		if IsRTXSparkAdapter(a.Name, a.PNPDeviceID, a.InfFilename, a.DriverVersion) ||
			(p.IsWindowsOnArm && IsRTXSparkAdapterCorroborated(a.PNPDeviceID, a.InfFilename, a.DriverVersion)) {
			p.Class = ClassRTXSpark
			p.GPUSoC = socN1X
			break
		}
	}

	p.Vendor = in.ComputerManufacturer
	p.Model = in.ComputerProductName
	p.ProductVersion = in.ComputerVersion
	p.BIOSVersion = in.BIOSVersion
	p.BIOSDate = in.BIOSDate
}

// IsRTXSparkAdapter applies the strong row 2 tests to one Windows display
// adapter: the PNP id (VEN_10DE&DEV_2E03 / DEV_2E06, S50) or the adapter name
// token "RTX Spark N1X" are each sufficient on their own (spec 3.1 row 2). The
// INF name and WDDM version are deliberately not consulted here; see
// IsRTXSparkAdapterCorroborated. The infFilename / driverVersion parameters are
// kept so existing callers compile.
func IsRTXSparkAdapter(name, pnpDeviceID, infFilename, driverVersion string) bool {
	_, _ = infFilename, driverVersion
	pnp := strings.ToUpper(pnpDeviceID)
	if strings.Contains(pnp, "VEN_10DE&DEV_2E03") || strings.Contains(pnp, "VEN_10DE&DEV_2E06") {
		return true
	}
	return strings.Contains(name, rtxSparkNameToken)
}

// IsRTXSparkAdapterCorroborated applies the weak row 2 signals - INF
// nv_surface_woa.inf or a WDDM DriverVersion ending 16.1600 (spec 2.2: only the
// tail is confirmed) - to an NVIDIA (VEN_10DE) adapter. They are corroboration
// only: callers must additionally know the host is Windows on Arm (row 1),
// otherwise a 616.00-series driver on an x64 desktop would class a discrete
// GPU as rtx-spark (spark-rules.json rtx-spark-detected: "WoA + PNP").
func IsRTXSparkAdapterCorroborated(pnpDeviceID, infFilename, driverVersion string) bool {
	pnp := strings.ToUpper(pnpDeviceID)
	if !strings.Contains(pnp, "VEN_10DE") {
		return false
	}
	return strings.EqualFold(infFilename, rtxSparkINF) || strings.HasSuffix(driverVersion, rtxSparkWDDMTail)
}

// IsNvidiaKernelFlavour reports whether a kernel release string names
// Canonical's linux-nvidia kernel (spec row 11 regex).
func IsNvidiaKernelFlavour(kernel string) bool {
	return nvidiaKernelRe.MatchString(strings.TrimSpace(kernel))
}

// lspciNVIDIADeviceIDs returns the set of lower-case device ids of every
// vendor 10de function in lspci -nn output.
func lspciNVIDIADeviceIDs(out string) map[string]bool {
	ids := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		for _, m := range lspciIDRe.FindAllStringSubmatch(line, -1) {
			if strings.ToLower(m[1]) == pciVendorNVIDIA {
				ids[strings.ToLower(m[2])] = true
			}
		}
	}
	return ids
}

// LspciHasGB10 reports whether lspci -nn output lists the GB10 GPU
// [10de:2e12] (row 5), used to explain "No devices were found" on DGX Spark.
func LspciHasGB10(out string) bool {
	return lspciNVIDIADeviceIDs(out)[pciDeviceGB10]
}

// ParseDGXRelease parses /etc/dgx-release (row 4, keys per S104) into a
// DGXOSInfo. Unknown keys are ignored; values are unquoted.
func ParseDGXRelease(content string) types.DGXOSInfo {
	var d types.DGXOSInfo
	for _, line := range strings.Split(content, "\n") {
		k, v := util.ParseKeyValue(line, "=")
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch k {
		case "DGX_NAME":
			d.Name = v
		case "DGX_PRETTY_NAME":
			d.PrettyName = v
		case "DGX_SWBUILD_VERSION":
			d.SWBuildVersion = v
		case "DGX_SWBUILD_DATE":
			d.SWBuildDate = v
		case "DGX_OTA_VERSION":
			d.OTAVersion = v
		case "DGX_OTA_DATE":
			d.OTADate = v
		case "DGX_PLATFORM":
			d.Platform = v
		case "DGX_COMMIT_ID":
			d.CommitID = v
		case "DGX_SERIAL_NUMBER":
			d.SerialNumber = v // redacted to <serial> by internal/redact
		}
	}
	return d
}

// parseFastOSName returns the NAME value of /etc/fastos-release.
func parseFastOSName(content string) string {
	name, _ := parseFastOSRelease(content)
	return name
}

// parseFastOSRelease returns NAME and VERSION of /etc/fastos-release.
func parseFastOSRelease(content string) (name, version string) {
	for _, line := range strings.Split(content, "\n") {
		k, v := util.ParseKeyValue(line, "=")
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch k {
		case "NAME":
			name = v
		case "VERSION":
			version = v
		}
	}
	return name, version
}

// CollectDGXRelease reads /etc/dgx-release and /etc/fastos-release (row 4 of
// spec 3.1) into the release-file part of DGXOSInfo. It is the read-only
// half of the DGX OS collector; package state, OTA checker, systemd and
// fwupd state are added by linux.CollectDGXOS (work package 1b). nil is
// returned when neither file exists.
func CollectDGXRelease() (*types.DGXOSInfo, []types.CollectorError) {
	var errs []types.CollectorError
	var d types.DGXOSInfo
	found := false
	if data, err := ReadSimFile(dgxReleasePath); err == nil {
		d = ParseDGXRelease(string(data))
		found = true
	} else if !os.IsNotExist(err) {
		errs = append(errs, types.CollectorError{Collector: "dgxos.release", Error: dgxReleasePath + ": " + err.Error()})
	}
	if data, err := ReadSimFile(fastosReleasePath); err == nil {
		if name, version := parseFastOSRelease(string(data)); name == fastosReleaseName {
			d.FastOSVersion = version
			found = true
		}
	}
	if !found {
		return nil, errs
	}
	return &d, errs
}

// ApplyPlatformFlags runs after phase 3 (GPU and PCIe collected) and before
// phase 4. It evaluates the GPU-dependent rows 5 (nvidia-smi name), 7, 8 and 9
// of spec table 3.1 when phase 1 left Class empty, records the compute
// capability, and then applies flag rules A-C to every GPUInfo and PCIeInfo,
// whatever row matched. The CC 12.1 + [N/A] fallback (row 9) never asserts a
// class (spec: "the heuristic no longer asserts dgx-spark").
func ApplyPlatformFlags(r *types.Report) {
	if r == nil {
		return
	}
	p := &r.Platform

	anyNotSupported := false
	for i := range r.GPUs {
		g := &r.GPUs[i]
		if !g.IsNVIDIA {
			continue
		}
		if g.MemoryReporting == MemoryReportingNotSupported {
			anyNotSupported = true
		}
		if p.ComputeCap == "" && g.ComputeCap != "" {
			p.ComputeCap = g.ComputeCap
		}
	}

	if p.Class == "" {
		classifyFromGPUs(r, p)
	}

	// Row 2 fallback on Windows: an RTX Spark adapter that phase 1 could not
	// see (WMI unavailable) is still recognisable from the GPU inventory.
	if p.Class == "" && r.Metadata.Platform == "windows" {
		for _, g := range r.GPUs {
			pnp := "PCI\\VEN_" + strings.ToUpper(g.PCIVendorID) + "&DEV_" + strings.ToUpper(g.PCIDeviceID)
			if g.IsNVIDIA && IsRTXSparkAdapter(g.Name, pnp, "", "") {
				p.Class = ClassRTXSpark
				p.GPUSoC = socN1X
				break
			}
		}
	}

	// GPUSoC for the Spark classes when phase 1 did not set it.
	if p.GPUSoC == "" {
		switch p.Class {
		case ClassDGXSpark:
			p.GPUSoC = socGB10
		case ClassRTXSpark:
			p.GPUSoC = socN1X
		}
	}

	// Flag rule A (class-derived).
	switch p.Class {
	case ClassDGXSpark, ClassRTXSpark:
		p.UnifiedMemory = true
		setOnPackage(r, true)
	case ClassJetson:
		p.UnifiedMemory = true
	}

	// Flag rule B (platform-independent): any NVIDIA GPU with memory.total
	// [N/A] (or table Memory-Usage "Not Supported") is unified memory /
	// on-package, even when Class is empty (Thor without
	// /etc/nv_tegra_release, a future iGPU, row 9).
	if anyNotSupported && p.Class != ClassGraceHopper {
		p.UnifiedMemory = true
		for i := range r.GPUs {
			g := &r.GPUs[i]
			if g.IsNVIDIA && g.MemoryReporting == MemoryReportingNotSupported {
				g.OnPackage = true
				setPCIeOnPackage(r, g.Index, true)
			}
		}
	}

	// Flag rule C: grace-hopper (numeric HBM memory) is never unified / on-package.
	if p.Class == ClassGraceHopper {
		p.UnifiedMemory = false
		setOnPackage(r, false)
	}
}

// classifyFromGPUs evaluates rows 5 (by nvidia-smi name), 7, 8 and 9 in order.
func classifyFromGPUs(r *types.Report, p *types.PlatformInfo) {
	for _, g := range r.GPUs {
		if !g.IsNVIDIA {
			continue
		}
		// Row 5: nvidia-smi -L name "NVIDIA GB10" (whole token, so "NVIDIA
		// GB100" does not match) or lspci device id 2e12.
		if gb10NameRe.MatchString(g.Name) || strings.EqualFold(g.PCIDeviceID, pciDeviceGB10) {
			p.Class = ClassDGXSpark
			p.GPUSoC = socGB10
			return
		}
	}
	for _, g := range r.GPUs {
		if !g.IsNVIDIA {
			continue
		}
		// Row 7: GH200 / GB200 / GB300 with numeric memory.
		for _, tok := range graceHopperNames {
			if strings.Contains(g.Name, tok) && g.MemoryReporting == MemoryReportingDedicated {
				p.Class = ClassGraceHopper
				p.GPUSoC = tok
				return
			}
		}
	}
	if r.System.Architecture == "arm64" {
		for _, g := range r.GPUs {
			// Row 8: aarch64, none of the above, NVIDIA GPU with numeric memory.total.
			if g.IsNVIDIA && g.MemoryReporting == MemoryReportingDedicated {
				p.Class = ClassArm64DGPU
				return
			}
		}
	}
	for _, g := range r.GPUs {
		// Row 9: compute_cap 12.1 with memory.total [N/A] and no PCI-ID
		// match: Class stays empty, GPUSoC marks the heuristic.
		if g.IsNVIDIA && g.ComputeCap == computeCapGB10 && g.MemoryReporting == MemoryReportingNotSupported {
			p.GPUSoC = socUnknownCC121
			return
		}
	}
}

// setOnPackage sets OnPackage on every NVIDIA GPU and every PCIe entry.
func setOnPackage(r *types.Report, v bool) {
	for i := range r.GPUs {
		if r.GPUs[i].IsNVIDIA {
			r.GPUs[i].OnPackage = v
		}
	}
	for i := range r.GPUPCIe {
		r.GPUPCIe[i].OnPackage = v
	}
	if r.PCIe != nil {
		r.PCIe.OnPackage = v
	}
}

// setPCIeOnPackage sets OnPackage on the PCIe entry of one GPU index.
func setPCIeOnPackage(r *types.Report, gpuIndex int, v bool) {
	for i := range r.GPUPCIe {
		if r.GPUPCIe[i].GPUIndex == gpuIndex {
			r.GPUPCIe[i].OnPackage = v
		}
	}
	if r.PCIe != nil && r.PCIe.GPUIndex == gpuIndex {
		r.PCIe.OnPackage = v
	}
}

// IsFoundersEdition reports whether the DMI strings of a dgx-spark match the
// Founders Edition of row 10 (vendor NVIDIA, product NVIDIA_DGX_Spark, version
// A.7, BIOS 5.36_0ACUM018/023); OEM GB10 systems (ASUS, HP, LENOVO, Dell, MSI,
// Acer, GIGABYTE) return false and are told apart by Vendor / Model.
func IsFoundersEdition(p types.PlatformInfo) bool {
	if p.Class != ClassDGXSpark {
		return false
	}
	if strings.EqualFold(p.Vendor, dmiVendorNVIDIA) && (p.Model == dmiProductDGXSpark || p.ProductVersion == dmiVersionA7) {
		return true
	}
	for _, b := range feBIOSVersions {
		if p.BIOSVersion == b {
			return true
		}
	}
	return false
}

// IsOEMGB10Vendor reports whether a DMI sys_vendor is one of the OEM GB10
// system vendors named in row 10.
func IsOEMGB10Vendor(vendor string) bool {
	v := strings.TrimSpace(vendor)
	for _, o := range oemGB10Vendors {
		if strings.EqualFold(v, o) || strings.HasPrefix(strings.ToUpper(v), strings.ToUpper(o)+" ") {
			return true
		}
	}
	return false
}
