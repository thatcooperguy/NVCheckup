package wsl

import (
	"os"
	"runtime"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// DetectWSL checks if we're running inside WSL and gathers WSL-specific info.
func DetectWSL(timeout int) (types.WSLInfo, []types.CollectorError) {
	var info types.WSLInfo
	var errs []types.CollectorError

	if runtime.GOOS != "linux" {
		// On Windows, check if WSL is available
		if runtime.GOOS == "windows" {
			r := util.RunCommand(timeout, "wsl", "--status")
			if r.Err == nil {
				info.IsWSL = false // We're on the host side
			}
		}
		return info, errs
	}

	// On Linux, check if we're inside WSL
	// Check /proc/version for Microsoft/WSL indicators
	r := util.RunCommand(timeout, "cat", "/proc/version")
	if r.Err == nil {
		version := strings.ToLower(r.Stdout)
		if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
			info.IsWSL = true
			info.KernelVersion = strings.TrimSpace(r.Stdout)
		}
	}

	if !info.IsWSL {
		return info, errs
	}

	// WSL version detection
	r = util.RunCommand(timeout, "cat", "/proc/sys/fs/binfmt_misc/WSLInterop")
	if r.Err == nil {
		info.WSLVersion = "2" // WSL2 if binfmt_misc exists
	} else {
		info.WSLVersion = "1"
	}

	// Distro info
	r = util.RunCommand(timeout, "sh", "-c", `grep ^NAME /etc/os-release | cut -d= -f2 | tr -d '"'`)
	if r.Err == nil {
		info.Distro = strings.TrimSpace(r.Stdout)
	}

	// Check /dev/dxg (WSL2 GPU paravirtualization device)
	if _, err := os.Stat("/dev/dxg"); err == nil {
		info.DevDxgExists = true
	}

	// Check nvidia-smi inside WSL
	if util.CommandExists("nvidia-smi") {
		r = util.RunCommand(timeout, "nvidia-smi", "-L")
		if r.Err == nil {
			info.NvidiaSmiOK = true
		}
	}

	return info, errs
}
