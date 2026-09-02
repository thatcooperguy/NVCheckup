package analyzer

// RTX Spark (N1X) and Windows-on-Arm rules (docs/roadmap/spark-support.md
// sections 5 and 8) plus the two rules that follow the N1X onto other
// operating systems: wsl-linux-driver-installed (platforms: all) and
// rtx-spark-linux-unsupported. All of them run in every mode except the WSL
// rule, which rides with the WSL collector (ai and full).
//
// Producer: windows.CollectWoA fills PlatformInfo.IsWindowsOnArm,
// ProcessEmulated, NativeMachine and, on an Arm host with the N1X adapter,
// PlatformInfo.WoA (adapter name, PNP id, WDDM DriverVersion, INF,
// DeveloperPreview, nvcc.exe PE machine). Driver.Source and
// GPUInfo.DriverVersion stay as fallbacks for reports that predate it.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const (
	// rtx-spark-driver-developer-preview (spec 5 / 2.2): first Arm64 driver
	// 616.00 Developer Preview; its WDDM DriverVersion is expected to end in
	// 16.1600 (the 32.0. prefix is unconfirmed, so only the suffix is matched).
	rtxSparkDPDriver      = "616.00"
	rtxSparkDPWDDMSuffix  = "16.1600"
	rtxSparkDPINF         = "nv_surface_woa.inf"
	rtxSparkDPPackageName = "DeveloperPreview"
	// woa-cuda-toolkit-not-native (spec 5 / 2.2): CUDA 13.4 DP is the first
	// native Windows Arm64 toolkit, so <= 13.3 is x86_64 under Prism.
	woaFirstNativeCUDA = "13.4"
	// woa-windows-build-too-old (spec 5, S74): 24H2 is build 26100.
	woaMinBuild = 26100
	// nvidiaReleaseMinMajor: NVIDIA driver release strings (as nvidia-smi
	// prints them, e.g. 580.159.03 or 616.00) have a three-digit leading
	// integer; a four-part WDDM DriverVersion such as 32.0.16.1600 does not
	// and must never be read as "below 616.00".
	nvidiaReleaseMinMajor = 100
)

// wslDriverPackageRe matches the Linux driver packages that must never be
// installed inside WSL (spec 5, wsl-linux-driver-installed).
var wslDriverPackageRe = regexp.MustCompile(`^(nvidia-driver-\d+|nvidia-dkms-\d+)`)

// pnpDeviceRe extracts the PCI device id from a PNPDeviceID (DEV_2E03).
var pnpDeviceRe = regexp.MustCompile(`DEV_([0-9A-Fa-f]{4})`)

// coreCountRe extracts "6144-core" from the adapter name.
var coreCountRe = regexp.MustCompile(`(\d+)-core`)

// windowsBuild parses SystemInfo.OSBuild ("26100" or "10.0.26100") into the
// build number, 0 when unknown.
func windowsBuild(build string) int {
	v := versionInts(build)
	switch len(v) {
	case 0:
		return 0
	case 1, 2:
		return v[0]
	default:
		return v[2]
	}
}

// analyzeWoA runs the rtx-spark-* and woa-* rules.
func analyzeWoA(r *types.Report) []types.Finding {
	var findings []types.Finding
	woa := r.Platform.IsWindowsOnArm

	// Rule row woa-nvcheckup-emulated (spec 5), WARN.
	if woa && r.Platform.ProcessEmulated {
		findings = append(findings, sparkFinding("woa-nvcheckup-emulated", fmt.Sprintf("NVCheckup runs under Prism (process %s, host %s); CPU data reflects the emulated processor.",
			orNA(r.System.Architecture), orNA(r.Platform.NativeMachine))))
	}

	// Rule row woa-windows-build-too-old (spec 5), WARN.
	if woa {
		if b := windowsBuild(r.System.OSBuild); b > 0 && b < woaMinBuild {
			findings = append(findings, sparkFinding("woa-windows-build-too-old", fmt.Sprintf("Windows build %d predates 24H2 (%d); RTX Spark devices are expected to ship 26H1 (build 28000, device-scoped, S74) or newer per the launch announcement (S27) - unconfirmed.", b, woaMinBuild)))
		}
	}

	// Rule row rtx-spark-linux-unsupported (spec 5): N1X PCI ids seen on Linux.
	if isRTXSpark(r) && !isWindowsReport(r) {
		pciid, distro := "10de:2e03/2e06", "n/a"
		if g := firstNVIDIAGPU(r); g != nil && g.PCIDeviceID != "" {
			pciid = "10de:" + strings.ToLower(strings.TrimPrefix(g.PCIDeviceID, "0x"))
		}
		if r.Linux != nil {
			distro = strings.TrimSpace(r.Linux.Distro + " " + r.Linux.DistroVersion)
		}
		f := sparkFinding("rtx-spark-linux-unsupported", fmt.Sprintf("RTX Spark N1X [%s] under Linux (%s, kernel %s, driver %s); NVIDIA has not announced Linux support for RTX Spark as of %s (S76 reports the absence of an announcement).",
			pciid, orNA(distro), orNA(r.System.KernelVersion), orNA(r.Driver.Version), r.Metadata.Timestamp.Format("2006-01-02")))
		f.GPUIndexes = nvidiaGPUIndexes(r)
		findings = append(findings, f)
		return findings
	}

	if !isRTXSpark(r) || !isWindowsReport(r) {
		return findings
	}
	woaInfo := r.Platform.WoA

	// Rule row rtx-spark-driver-developer-preview (spec 5), WARN: the 616.00
	// DP package, its INF / WDDM suffix (PlatformInfo.WoA, spec 8), or an
	// older (extracted) build on Arm64.
	driver := strings.TrimSpace(r.Driver.Version)
	dp := driver == rtxSparkDPDriver || strings.HasPrefix(driver, rtxSparkDPDriver)
	inf := "INF not collected"
	if woaInfo != nil {
		if woaInfo.DeveloperPreview {
			dp = true
		}
		var facts []string
		if woaInfo.InfFilename != "" {
			facts = append(facts, woaInfo.InfFilename)
			if strings.EqualFold(strings.TrimSpace(woaInfo.InfFilename), rtxSparkDPINF) {
				dp = true
			}
		}
		if woaInfo.DriverVersion != "" {
			facts = append(facts, "WDDM "+woaInfo.DriverVersion)
			if strings.HasSuffix(strings.TrimSpace(woaInfo.DriverVersion), rtxSparkDPWDDMSuffix) {
				dp = true
			}
		}
		if len(facts) > 0 {
			inf = strings.Join(facts, ", ")
		}
		if driver == "" {
			driver = woaInfo.DriverVersion
		}
	}
	woaFacts := woaInfo != nil && (woaInfo.InfFilename != "" || woaInfo.DriverVersion != "")
	for _, g := range r.GPUs {
		if !g.IsNVIDIA {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(g.DriverVersion), rtxSparkDPWDDMSuffix) {
			dp = true
			if !woaFacts {
				inf = "WDDM " + g.DriverVersion
			}
		}
		if driver == "" {
			driver = g.DriverVersion
		}
	}
	if strings.Contains(strings.ToLower(r.Driver.Source), strings.ToLower(rtxSparkDPINF)) || strings.Contains(r.Driver.Source, rtxSparkDPPackageName) {
		dp = true
		if !woaFacts {
			inf = r.Driver.Source
		}
	}
	// "below 616.00 on Arm64": only NVIDIA release strings (NNN.NN[.NN])
	// qualify; four-part WDDM strings are not release numbers.
	old := false
	if v := versionInts(r.Driver.Version); len(v) > 0 && len(v) <= 3 && v[0] >= nvidiaReleaseMinMajor && v[0] < versionMajor(rtxSparkDPDriver) {
		old = true
	}
	if dp || old {
		evidence := fmt.Sprintf("Driver %s (%s) is the RTX Spark Developer Preview branch (R616, 2026-07-16, S24); below 616.00 on Arm64 is an extracted/unofficial build.", orNA(driver), inf)
		if old && !dp {
			evidence = fmt.Sprintf("Driver %s (%s) is below the 616.00 Developer Preview on Arm64: an extracted/unofficial build (S24).", orNA(driver), inf)
		}
		f := sparkFinding("rtx-spark-driver-developer-preview", evidence)
		f.GPUIndexes = nvidiaGPUIndexes(r)
		findings = append(findings, f)
	}

	// Rule row woa-cuda-toolkit-not-native (spec 5), WARN. The nvcc.exe PE
	// machine (PlatformInfo.WoA.NvccMachine) decides when it was collected:
	// AMD64 / I386 run under Prism, ARM64 is native whatever the version.
	// Without it the version clause applies: toolkits <= 13.3 predate the
	// native Arm64 build (13.4 Developer Preview).
	toolkit, nvccPath, machine := "", "", ""
	if r.AI != nil {
		toolkit, nvccPath = r.AI.CUDAToolkitVersion, r.AI.NvccPath
	}
	if woaInfo != nil {
		machine = strings.ToUpper(strings.TrimSpace(woaInfo.NvccMachine))
		if nvccPath == "" {
			nvccPath = woaInfo.NvccPath
		}
	}
	var emulated bool
	switch machine {
	case "AMD64", "I386":
		emulated = true
	case "ARM64":
		emulated = false
	default:
		emulated = toolkit != "" && versionLess(toolkit, woaFirstNativeCUDA)
	}
	if emulated && (toolkit != "" || nvccPath != "") {
		why := "version <= 13.3"
		if machine != "" {
			why = "nvcc.exe PE machine " + machine
		}
		findings = append(findings, sparkFinding("woa-cuda-toolkit-not-native", fmt.Sprintf("CUDA %s at %s is x86_64 under Prism (%s); the first native Windows Arm64 toolkit is 13.4 Developer Preview.",
			orNA(toolkit), orNA(nvccPath), why)))
	}
	return findings
}

// rtxSparkAdapterFacts returns the core count and PCI device id of the N1X
// adapter from the nvidia-smi inventory or, when nvidia-smi.exe is absent
// (spec 2.2), from the WMI adapter row in PlatformInfo.WoA.
func rtxSparkAdapterFacts(r *types.Report) (cores, devid string) {
	cores, devid = "n/a", "n/a"
	if gpu := firstNVIDIAGPU(r); gpu != nil {
		if m := coreCountRe.FindStringSubmatch(gpu.Name); m != nil {
			cores = m[1]
		}
		devid = orNA(strings.ToUpper(strings.TrimPrefix(gpu.PCIDeviceID, "0x")))
	}
	if w := r.Platform.WoA; w != nil {
		if cores == "n/a" {
			if m := coreCountRe.FindStringSubmatch(w.AdapterName); m != nil {
				cores = m[1]
			}
		}
		if devid == "n/a" {
			if m := pnpDeviceRe.FindStringSubmatch(w.PNPDeviceID); m != nil {
				devid = strings.ToUpper(m[1])
			}
		}
	}
	return cores, devid
}

// analyzeWSLDriverPackages implements wsl-linux-driver-installed (spec 5,
// platforms: all): Linux NVIDIA driver packages inside a WSL2 distro.
func analyzeWSLDriverPackages(r *types.Report) []types.Finding {
	var findings []types.Finding
	if r.WSL == nil || !r.WSL.IsWSL || r.Linux == nil {
		return findings
	}
	var pkgs []string
	for _, pkg := range r.Linux.NVIDIAPackages {
		fields := strings.Fields(pkg)
		if len(fields) > 0 && wslDriverPackageRe.MatchString(fields[0]) {
			pkgs = append(pkgs, fields[0])
		}
	}
	if len(pkgs) == 0 {
		return findings
	}
	libcuda := "not under /usr/lib/wsl/lib"
	if strings.Contains(r.Linux.LibCudaPath, "/usr/lib/wsl/lib") {
		libcuda = "present"
	}
	findings = append(findings, sparkFinding("wsl-linux-driver-installed", fmt.Sprintf("WSL distro %s (%s) has Linux NVIDIA driver packages %s; /usr/lib/wsl/lib/libcuda.so %s.",
		orNA(r.WSL.Distro), orNA(r.System.Architecture), strings.Join(pkgs, ", "), libcuda)))
	return findings
}
