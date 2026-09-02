package analyzer

// RTX Spark (N1X) and Windows-on-Arm rules (docs/roadmap/spark-support.md
// sections 5 and 8) plus the two rules that follow the N1X onto other
// operating systems: wsl-linux-driver-installed (platforms: all) and
// rtx-spark-linux-unsupported. All of them run in every mode except the WSL
// rule, which rides with the WSL collector (ai and full).

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
)

// wslDriverPackageRe matches the Linux driver packages that must never be
// installed inside WSL (spec 5, wsl-linux-driver-installed).
var wslDriverPackageRe = regexp.MustCompile(`^(nvidia-driver-\d+|nvidia-dkms-\d+)`)

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

	// Rule row rtx-spark-driver-developer-preview (spec 5), WARN: the 616.00
	// DP package, its WDDM suffix, or an older (extracted) build on Arm64.
	driver := strings.TrimSpace(r.Driver.Version)
	dp := driver == rtxSparkDPDriver || strings.HasPrefix(driver, rtxSparkDPDriver)
	inf := "INF not collected"
	for _, g := range r.GPUs {
		if !g.IsNVIDIA {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(g.DriverVersion), rtxSparkDPWDDMSuffix) {
			dp = true
			inf = "WDDM " + g.DriverVersion
		}
		if driver == "" {
			driver = g.DriverVersion
		}
	}
	if strings.Contains(strings.ToLower(r.Driver.Source), strings.ToLower(rtxSparkDPINF)) || strings.Contains(r.Driver.Source, rtxSparkDPPackageName) {
		dp = true
		inf = r.Driver.Source
	}
	old := false
	if major := versionMajor(r.Driver.Version); major > 0 && major < versionMajor(rtxSparkDPDriver) {
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

	// Rule row woa-cuda-toolkit-not-native (spec 5), WARN: toolkit <= 13.3
	// (the nvcc PE machine type is not part of the collected types).
	if r.AI != nil && r.AI.CUDAToolkitVersion != "" && versionLess(r.AI.CUDAToolkitVersion, woaFirstNativeCUDA) {
		findings = append(findings, sparkFinding("woa-cuda-toolkit-not-native", fmt.Sprintf("CUDA %s at %s is x86_64 under Prism; the first native Windows Arm64 toolkit is 13.4 Developer Preview.",
			r.AI.CUDAToolkitVersion, orNA(r.AI.NvccPath))))
	}
	return findings
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
