//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
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

// collectNVIDIAPackages lists installed packages whose name mentions NVIDIA.
// The package manager is run directly and filtered in Go: the former
// "| grep -i nvidia" pipeline exited 1 on a machine with no NVIDIA packages,
// which is a legitimate answer, not a failure.
func collectNVIDIAPackages(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	var r util.CommandResult
	switch info.PackageManager {
	case "apt":
		r = util.RunCommand(timeout, "dpkg", "-l")
	case "dnf", "yum":
		r = util.RunCommand(timeout, "rpm", "-qa")
	case "pacman":
		r = util.RunCommand(timeout, "pacman", "-Q")
	default:
		return
	}
	if r.Err != nil {
		if detail := toolFailure(r); detail != "" {
			*errs = append(*errs, types.CollectorError{
				Collector: "linux.packages",
				Error:     "Could not list packages: " + detail,
			})
		}
		return
	}
	info.NVIDIAPackages = parseNvidiaPackageList(info.PackageManager, r.Stdout)
}

// parseNvidiaPackageList extracts the NVIDIA-related entries from package
// manager output. dpkg -l rows are reduced to "name version" (the former
// awk '{print $2 " " $3}'); rpm -qa and pacman -Q rows are kept whole. The
// match is case-insensitive on the whole line, as "grep -i nvidia" was.
func parseNvidiaPackageList(packageManager, output string) []string {
	var pkgs []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(strings.ToLower(line), "nvidia") {
			continue
		}
		if packageManager == "apt" {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				line = fields[1] + " " + fields[2]
			}
		}
		pkgs = append(pkgs, line)
	}
	return pkgs
}

// collectKernelModules records which NVIDIA/nouveau modules are loaded (true)
// or merely available to modprobe (false). lsmod is run directly and
// filtered in Go so that "neither module is loaded" is an empty result, not
// the "Could not list kernel modules" error the former grep pipeline raised.
func collectKernelModules(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	info.LoadedModules = make(map[string]bool)

	r := util.RunCommand(timeout, "lsmod")
	if r.Err == nil {
		for _, mod := range parseLsmodGPUModules(r.Stdout) {
			info.LoadedModules[mod] = true
		}
	} else if detail := toolFailure(r); detail != "" {
		*errs = append(*errs, types.CollectorError{
			Collector: "linux.modules",
			Error:     "Could not list kernel modules: " + detail,
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

// parseLsmodGPUModules returns the names of loaded modules that belong to the
// NVIDIA or nouveau drivers (first column of lsmod, names starting with
// "nvidia" or "nouveau"). The header row is skipped.
func parseLsmodGPUModules(output string) []string {
	var mods []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if strings.HasPrefix(name, "nvidia") || strings.HasPrefix(name, "nouveau") {
			mods = append(mods, name)
		}
	}
	return mods
}

// collectLibCuda locates the CUDA driver library via the dynamic linker cache,
// then falls back to well-known paths. ldconfig is run directly; a cache with
// no libcuda entry is an ordinary empty result.
func collectLibCuda(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if util.CommandExists("ldconfig") {
		r := util.RunCommand(timeout, "ldconfig", "-p")
		if r.Err == nil {
			info.LibCudaPath = parseLdconfigLibcuda(r.Stdout)
		}
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

// parseLdconfigLibcuda returns the path of the first libcuda.so entry in
// "ldconfig -p" output (the last whitespace-separated field, after "=>"), or
// "" when the cache has none.
func parseLdconfigLibcuda(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "libcuda.so") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[len(fields)-1]
	}
	return ""
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

// dmesgSnippetKeywords select the kernel log lines worth keeping in the report.
var dmesgSnippetKeywords = []string{"nvidia", "nvrm", "gpu", "nouveau"}

// maxDmesgSnippetLines caps the snippet at the most recent lines (the former
// "| tail -50").
const maxDmesgSnippetLines = 50

// filterDmesgSnippets keeps the last maxDmesgSnippetLines lines of dmesg
// output that mention a GPU-related keyword (case-insensitive).
func filterDmesgSnippets(output string) string {
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		for _, kw := range dmesgSnippetKeywords {
			if strings.Contains(lower, kw) {
				kept = append(kept, line)
				break
			}
		}
	}
	if len(kept) > maxDmesgSnippetLines {
		kept = kept[len(kept)-maxDmesgSnippetLines:]
	}
	return strings.Join(kept, "\n")
}

func collectDmesgSnippets(info *types.LinuxInfo, errs *[]types.CollectorError, timeout int) {
	if !util.CommandExists("dmesg") {
		return
	}
	r := util.RunCommand(timeout, "dmesg")
	if r.Err == nil {
		info.DmesgSnippets = filterDmesgSnippets(r.Stdout)
	}
}
