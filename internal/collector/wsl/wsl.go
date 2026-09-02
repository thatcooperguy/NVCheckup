package wsl

import (
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// wslInteropGlob matches the binfmt_misc handler WSL2 registers for launching
// Windows executables: "WSLInterop" classically, "WSLInterop-late" when the
// distro boots with systemd.
const wslInteropGlob = "/proc/sys/fs/binfmt_misc/WSLInterop*"

// wslVersionFromProcVersion infers the WSL generation from /proc/version.
// "microsoft-standard" (the WSL2 kernel tag) or "WSL2" mean 2; the WSL1
// pico-process kernel identifies itself as "...-Microsoft". Returns "" when
// the string carries no recognisable marker.
func wslVersionFromProcVersion(version string) string {
	v := strings.ToLower(version)
	switch {
	case strings.Contains(v, "microsoft-standard"), strings.Contains(v, "wsl2"):
		return "2"
	case strings.Contains(v, "-microsoft"):
		return "1"
	}
	return ""
}

// DetectWSL checks if we're running inside WSL and gathers WSL-specific info.
func DetectWSL(timeout int) (types.WSLInfo, []types.CollectorError) {
	var info types.WSLInfo
	var errs []types.CollectorError

	// IsWSL means "this process is running inside a WSL distro". A Windows
	// host is by definition not inside WSL, so there is nothing to probe: the
	// previous "wsl --status" call spawned wsl.exe (slow, UTF-16 output,
	// absent on many machines) and then discarded its answer.
	if runtime.GOOS != "linux" {
		return info, errs
	}

	// On Linux, check if we're inside WSL: /proc/version carries the
	// Microsoft/WSL kernel tag. Read through the NVC_SIM_ROOT mapping (spec
	// section 10) so the simulated scenarios control the answer.
	if data, err := common.ReadSimFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
			info.IsWSL = true
			info.KernelVersion = strings.TrimSpace(string(data))
		}
	}

	if !info.IsWSL {
		return info, errs
	}

	// WSL version detection: the kernel string is authoritative (WSL2 ships a
	// real Linux kernel tagged microsoft-standard[-WSL2]; WSL1 reports a
	// "-Microsoft" translation-layer kernel). Fall back to the binfmt_misc
	// interop handler, which WSL2 registers as WSLInterop or, with systemd
	// enabled, WSLInterop-late.
	info.WSLVersion = wslVersionFromProcVersion(info.KernelVersion)
	if info.WSLVersion == "" {
		if matches, _ := common.SimGlob(wslInteropGlob); len(matches) > 0 {
			info.WSLVersion = "2"
		} else {
			info.WSLVersion = "1"
		}
	}

	// Distro info: NAME= from /etc/os-release, parsed in Go (no shell).
	if data, err := common.ReadSimFile("/etc/os-release"); err == nil {
		info.Distro = osReleaseName(string(data))
	}

	// Check /dev/dxg (WSL2 GPU paravirtualization device)
	if common.SimFileExists("/dev/dxg") {
		info.DevDxgExists = true
	}

	// Check nvidia-smi inside WSL
	if util.CommandExists("nvidia-smi") {
		r := util.RunCommand(timeout, "nvidia-smi", "-L")
		if r.Err == nil {
			info.NvidiaSmiOK = true
		}
	}

	return info, errs
}

// osReleaseName returns the unquoted NAME= value of an os-release file, or ""
// (the former grep/cut/tr pipeline; PRETTY_NAME and VERSION_CODENAME lines
// are not NAME and are skipped).
func osReleaseName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "NAME="), `"`)
		}
	}
	return ""
}
