//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectLinuxInfo gathers Linux-specific diagnostic data.
func CollectLinuxInfo(timeout int, includeLogs bool) (types.LinuxInfo, []types.CollectorError) {
	var info types.LinuxInfo
	var errs []types.CollectorError

	collectDistroInfo(&info, &errs, timeout)
	collectPackageManager(&info, &errs, timeout)
	collectNVIDIAPackages(&info, &errs, timeout)
	collectKernelModules(&info, &errs, timeout)
	collectDevNodes(&info, &errs, timeout)
	collectLibCuda(&info, &errs, timeout)
	collectDKMS(&info, &errs, timeout)
	collectSecureBoot(&info, &errs, timeout)
	collectSessionType(&info, &errs, timeout)
	collectPRIME(&info, &errs, timeout)
	collectContainerRuntime(&info, &errs, timeout)

	if includeLogs {
		collectJournalSnippets(&info, &errs, timeout)
		collectDmesgSnippets(&info, &errs, timeout)
	}

	return info, errs
}

func collectDistroInfo(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "cat", "/etc/os-release")
	if r.Err == nil {
		for _, line := range strings.Split(r.Stdout, "\n") {
			k, v := util.ParseKeyValue(line, "=")
			v = strings.Trim(v, "\"")
			switch k {
			case "NAME":
				info.Distro = v
			case "VERSION_ID":
				info.DistroVersion = v
			}
		}
	}
}

func collectPackageManager(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if util.CommandExists("apt") {
		info.PackageManager = "apt"
	} else if util.CommandExists("dnf") {
		info.PackageManager = "dnf"
	} else if util.CommandExists("yum") {
		info.PackageManager = "yum"
	} else if util.CommandExists("pacman") {
		info.PackageManager = "pacman"
	} else if util.CommandExists("zypper") {
		info.PackageManager = "zypper"
	} else {
		info.PackageManager = "unknown"
	}
}

func collectNVIDIAPackages(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	var r util.CommandResult
	switch info.PackageManager {
	case "apt":
		r = util.RunCommand(timeout, "sh", "-c", `dpkg -l | grep -i nvidia | awk '{print $2 " " $3}'`)
	case "dnf", "yum":
		r = util.RunCommand(timeout, "sh", "-c", `rpm -qa | grep -i nvidia`)
	case "pacman":
		r = util.RunCommand(timeout, "sh", "-c", `pacman -Q | grep -i nvidia`)
	default:
		return
	}

	if r.Err == nil && r.Stdout != "" {
		for _, line := range strings.Split(r.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				info.NVIDIAPackages = append(info.NVIDIAPackages, line)
			}
		}
	}
}

func collectKernelModules(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	info.LoadedModules = make(map[string]bool)

	r := util.RunCommand(timeout, "sh", "-c", `lsmod | grep -E "^(nvidia|nouveau)" | awk '{print $1}'`)
	if r.Err == nil {
		for _, line := range strings.Split(r.Stdout, "\n") {
			mod := strings.TrimSpace(line)
			if mod != "" {
				info.LoadedModules[mod] = true
			}
		}
	} else {
		*errs = append(*errs, types.CollectorError{
			Collector: "linux.modules",
			Error:     "Could not list kernel modules: " + r.Err.Error(),
		})
	}

	// Check for key modules explicitly
	for _, mod := range []string{"nvidia", "nvidia_drm", "nvidia_modeset", "nvidia_uvm", "nouveau"} {
		if _, found := info.LoadedModules[mod]; !found {
			// Check if module exists but isn't loaded
			r = util.RunCommand(timeout, "modinfo", mod)
			if r.Err == nil {
				info.LoadedModules[mod] = false // exists but not loaded
			}
			// If modinfo fails, module doesn't exist at all - don't add
		}
	}
}

func collectDevNodes(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	matches, err := filepath.Glob("/dev/nvidia*")
	if err == nil {
		info.DevNvidiaNodes = matches
	}
}

func collectLibCuda(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "sh", "-c", `ldconfig -p 2>/dev/null | grep libcuda.so | head -1 | awk '{print $NF}'`)
	if r.Err == nil && r.Stdout != "" {
		info.LibCudaPath = strings.TrimSpace(r.Stdout)
	}

	// Also check common locations
	if info.LibCudaPath == "" {
		for _, path := range []string{
			"/usr/lib/x86_64-linux-gnu/libcuda.so",
			"/usr/lib64/libcuda.so",
			"/usr/lib/aarch64-linux-gnu/libcuda.so",
			"/usr/local/cuda/lib64/libcuda.so",
		} {
			if _, err := os.Stat(path); err == nil {
				info.LibCudaPath = path
				break
			}
		}
	}
}

func collectDKMS(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if !util.CommandExists("dkms") {
		info.DKMSStatus = "DKMS not installed"
		return
	}

	r := util.RunCommand(timeout, "dkms", "status")
	if r.Err == nil {
		info.DKMSStatus = r.Stdout
		// Check for failures
		if strings.Contains(strings.ToLower(r.Stdout), "error") || strings.Contains(strings.ToLower(r.Stdout), "bad") {
			info.DKMSErrors = r.Stdout
		}
	} else {
		info.DKMSStatus = "Could not query DKMS status"
		*errs = append(*errs, types.CollectorError{Collector: "linux.dkms", Error: r.Err.Error()})
	}
}

func collectSecureBoot(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	// Check if UEFI
	if _, err := os.Stat("/sys/firmware/efi"); err != nil {
		info.SecureBootState = "N/A (Legacy BIOS)"
		return
	}

	r := util.RunCommand(timeout, "mokutil", "--sb-state")
	if r.Err == nil {
		out := strings.TrimSpace(r.Stdout)
		if strings.Contains(strings.ToLower(out), "enabled") {
			info.SecureBootState = "Enabled"
		} else if strings.Contains(strings.ToLower(out), "disabled") {
			info.SecureBootState = "Disabled"
		} else {
			info.SecureBootState = out
		}
	} else {
		info.SecureBootState = "Unknown (mokutil not available)"
	}

	// Check MOK status
	r = util.RunCommand(timeout, "mokutil", "--list-enrolled")
	if r.Err == nil {
		if strings.Contains(r.Stdout, "NVIDIA") || strings.Contains(r.Stdout, "nvidia") {
			info.MOKStatus = "NVIDIA key enrolled"
		} else {
			lines := strings.Split(r.Stdout, "\n")
			count := 0
			for _, l := range lines {
				if strings.Contains(l, "Subject:") {
					count++
				}
			}
			if count > 0 {
				info.MOKStatus = strings.Replace("N enrolled key(s), none appear NVIDIA-specific", "N", string(rune('0'+count)), 1)
			} else {
				info.MOKStatus = "No keys enrolled"
			}
		}
	}
}

func collectSessionType(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	// Check XDG_SESSION_TYPE
	sessionType := os.Getenv("XDG_SESSION_TYPE")
	if sessionType != "" {
		info.SessionType = sessionType
		return
	}

	// Fallback: check loginctl
	r := util.RunCommand(timeout, "sh", "-c", `loginctl show-session $(loginctl | grep $(whoami) | awk '{print $1}') -p Type 2>/dev/null | cut -d= -f2`)
	if r.Err == nil && r.Stdout != "" {
		info.SessionType = strings.TrimSpace(r.Stdout)
	} else {
		info.SessionType = "Unknown"
	}
}

func collectPRIME(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	// Check PRIME offloading status
	r := util.RunCommand(timeout, "sh", "-c", `prime-select query 2>/dev/null || echo "not available"`)
	if r.Err == nil {
		info.PRIMEStatus = strings.TrimSpace(r.Stdout)
	}

	// Also check for __NV_PRIME_RENDER_OFFLOAD
	if os.Getenv("__NV_PRIME_RENDER_OFFLOAD") == "1" {
		if info.PRIMEStatus == "not available" || info.PRIMEStatus == "" {
			info.PRIMEStatus = "PRIME render offload active (env)"
		}
	}
}

func collectContainerRuntime(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if util.CommandExists("docker") {
		info.ContainerRuntime = "docker"
	} else if util.CommandExists("podman") {
		info.ContainerRuntime = "podman"
	}

	// Check nvidia-container-toolkit
	if util.CommandExists("nvidia-container-cli") {
		r := util.RunCommand(timeout, "nvidia-container-cli", "--version")
		if r.Err == nil {
			info.NVContainerToolkit = strings.TrimSpace(r.Stdout)
		} else {
			info.NVContainerToolkit = "installed (version unknown)"
		}
	} else {
		r := util.RunCommand(timeout, "sh", "-c", `dpkg -l nvidia-container-toolkit 2>/dev/null | grep ^ii | awk '{print $3}' || rpm -q nvidia-container-toolkit 2>/dev/null`)
		if r.Err == nil && r.Stdout != "" {
			info.NVContainerToolkit = strings.TrimSpace(r.Stdout)
		}
	}
}

func collectJournalSnippets(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if !util.CommandExists("journalctl") {
		return
	}
	r := util.RunCommand(timeout, "journalctl", "-k", "--no-pager", "-b", "-g", "nvidia|NVRM|gpu", "--lines=100")
	if r.Err == nil {
		info.JournalSnippets = r.Stdout
	}
}

func collectDmesgSnippets(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "sh", "-c", `dmesg 2>/dev/null | grep -i "nvidia\|NVRM\|gpu\|nouveau" | tail -50`)
	if r.Err == nil {
		info.DmesgSnippets = r.Stdout
	}
}
