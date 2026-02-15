// Package common provides cross-platform system information collectors.
package common

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectSystemInfo gathers universal system snapshot data.
func CollectSystemInfo(timeout int) (types.SystemInfo, []types.CollectorError) {
	var info types.SystemInfo
	var errs []types.CollectorError

	info.Architecture = runtime.GOARCH

	hostname, err := os.Hostname()
	if err != nil {
		errs = append(errs, types.CollectorError{Collector: "system.hostname", Error: err.Error()})
	}
	info.Hostname = hostname

	info.Timezone = time.Now().Location().String()

	if util.IsWindows() {
		collectWindowsSystem(&info, &errs, timeout)
	} else if util.IsLinux() {
		collectLinuxSystem(&info, &errs, timeout)
	}

	return info, errs
}

func collectWindowsSystem(info *types.SystemInfo, errs *[]types.CollectorError, timeout int) {
	info.OSName = "Windows"

	// Get OS version via PowerShell
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).Caption")
	if r.Err == nil && r.Stdout != "" {
		info.OSName = strings.TrimSpace(r.Stdout)
	}

	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).Version")
	if r.Err == nil {
		info.OSVersion = strings.TrimSpace(r.Stdout)
	}

	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).BuildNumber")
	if r.Err == nil {
		info.OSBuild = strings.TrimSpace(r.Stdout)
	}

	// CPU
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_Processor).Name")
	if r.Err == nil {
		info.CPUModel = strings.TrimSpace(r.Stdout)
	}

	// RAM (total in MB)
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1MB)")
	if r.Err == nil {
		info.RAMTotalMB = parseIntSafe(r.Stdout)
	}

	// Storage free on system drive
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"[math]::Round((Get-PSDrive C).Free / 1MB)")
	if r.Err == nil {
		info.StorageFreeMB = parseIntSafe(r.Stdout)
	}

	// Uptime
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"$up = (Get-Date) - (Get-CimInstance Win32_OperatingSystem).LastBootUpTime; \"$($up.Days)d $($up.Hours)h $($up.Minutes)m\"")
	if r.Err == nil {
		info.Uptime = strings.TrimSpace(r.Stdout)
	}

	// Boot mode / Secure Boot
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		"try { Confirm-SecureBootUEFI } catch { 'Unknown' }")
	if r.Err == nil {
		val := strings.TrimSpace(r.Stdout)
		if val == "True" {
			info.SecureBoot = "Enabled"
			info.BootMode = "UEFI"
		} else if val == "False" {
			info.SecureBoot = "Disabled"
			info.BootMode = "UEFI"
		} else {
			info.SecureBoot = "Unknown"
			info.BootMode = "Unknown"
		}
	}
}

func collectLinuxSystem(info *types.SystemInfo, errs *[]types.CollectorError, timeout int) {
	// Parse /etc/os-release
	r := util.RunCommand(timeout, "cat", "/etc/os-release")
	if r.Err == nil {
		for _, line := range strings.Split(r.Stdout, "\n") {
			k, v := util.ParseKeyValue(line, "=")
			v = strings.Trim(v, "\"")
			switch k {
			case "NAME":
				info.OSName = v
			case "VERSION_ID":
				info.OSVersion = v
			case "PRETTY_NAME":
				if info.OSName == "" {
					info.OSName = v
				}
			}
		}
	} else {
		*errs = append(*errs, types.CollectorError{Collector: "system.os-release", Error: r.Err.Error()})
	}

	// Kernel version
	r = util.RunCommand(timeout, "uname", "-r")
	if r.Err == nil {
		info.KernelVersion = strings.TrimSpace(r.Stdout)
	}

	// CPU
	r = util.RunCommand(timeout, "sh", "-c", `grep -m1 "model name" /proc/cpuinfo | cut -d: -f2`)
	if r.Err == nil {
		info.CPUModel = strings.TrimSpace(r.Stdout)
	}

	// RAM
	r = util.RunCommand(timeout, "sh", "-c", `grep MemTotal /proc/meminfo | awk '{print int($2/1024)}'`)
	if r.Err == nil {
		info.RAMTotalMB = parseIntSafe(r.Stdout)
	}

	// Storage
	r = util.RunCommand(timeout, "sh", "-c", `df -m / | tail -1 | awk '{print $4}'`)
	if r.Err == nil {
		info.StorageFreeMB = parseIntSafe(r.Stdout)
	}

	// Uptime
	r = util.RunCommand(timeout, "uptime", "-p")
	if r.Err == nil {
		info.Uptime = strings.TrimSpace(r.Stdout)
	}

	// Boot mode
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		info.BootMode = "UEFI"
		// Secure Boot
		r = util.RunCommand(timeout, "sh", "-c",
			`mokutil --sb-state 2>/dev/null || echo "Unknown"`)
		if r.Err == nil {
			out := strings.TrimSpace(r.Stdout)
			if strings.Contains(out, "enabled") {
				info.SecureBoot = "Enabled"
			} else if strings.Contains(out, "disabled") {
				info.SecureBoot = "Disabled"
			} else {
				info.SecureBoot = "Unknown"
			}
		}
	} else {
		info.BootMode = "Legacy/BIOS"
		info.SecureBoot = "N/A"
	}
}

func parseIntSafe(s string) int64 {
	s = strings.TrimSpace(s)
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else {
			break
		}
	}
	return n
}
