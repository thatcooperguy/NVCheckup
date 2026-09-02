// Package common provides cross-platform system information collectors.
package common

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
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

	info.Timezone = formatTimezone(time.Now())

	if util.IsWindows() {
		collectWindowsSystem(&info, &errs, timeout)
	} else if util.IsLinux() {
		collectLinuxSystem(&info, &errs, timeout)
	}

	return info, errs
}

// formatTimezone renders e.g. "Local (CDT, UTC-05:00)". Location().String()
// is the literal "Local" for the system zone on every platform, so on its own
// it identifies nothing; the zone abbreviation and numeric offset are what
// actually place the machine.
func formatTimezone(now time.Time) string {
	abbr, offset := now.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	utcOffset := fmt.Sprintf("UTC%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
	name := now.Location().String()
	if abbr != "" && abbr != name {
		return fmt.Sprintf("%s (%s, %s)", name, abbr, utcOffset)
	}
	return fmt.Sprintf("%s (%s)", name, utcOffset)
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

	collectWindowsBoot(info, errs, timeout)
}

// secureBootScript reports three independent sources on separate lines.
// Confirm-SecureBootUEFI needs elevation ("Unable to set proper privileges"),
// so for a normal user the registry value written by the boot loader
// (UEFISecureBootEnabled: 1/0, absent on legacy BIOS) and the firmware_type
// environment variable (UEFI/Legacy) are the non-elevated fallbacks.
const secureBootScript = `$ErrorActionPreference = 'SilentlyContinue'; ` +
	`$sb = 'Unknown'; try { $sb = [string](Confirm-SecureBootUEFI -ErrorAction Stop) } catch { $sb = 'Unknown' }; ` +
	`$reg = (Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecureBoot\State' -ErrorAction SilentlyContinue).UEFISecureBootEnabled; ` +
	`if ($null -eq $reg) { $reg = '__ABSENT__' }; ` +
	`"cmdlet=$sb"; "registry=$reg"; "firmware=$env:firmware_type"; exit 0`

func collectWindowsBoot(info *types.SystemInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", secureBootScript)
	if r.Err != nil {
		info.SecureBoot, info.BootMode = "Unknown", "Unknown"
		*errs = append(*errs, types.CollectorError{Collector: "system.secureboot", Error: r.Err.Error()})
		return
	}
	info.SecureBoot, info.BootMode = parseSecureBootProbe(r.Stdout)
}

// parseSecureBootProbe combines the three sources printed by secureBootScript.
// Order of trust: the cmdlet when it ran, then the registry value, then
// firmware_type for the boot mode alone.
func parseSecureBootProbe(out string) (secureBoot, bootMode string) {
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v := util.ParseKeyValue(line, "="); k != "" {
			fields[k] = v
		}
	}

	secureBoot, bootMode = "Unknown", "Unknown"
	switch fields["cmdlet"] {
	case "True":
		secureBoot, bootMode = "Enabled", "UEFI"
	case "False":
		secureBoot, bootMode = "Disabled", "UEFI"
	}
	if secureBoot == "Unknown" {
		switch fields["registry"] {
		case "1":
			secureBoot, bootMode = "Enabled", "UEFI"
		case "0":
			secureBoot, bootMode = "Disabled", "UEFI"
		case "__ABSENT__":
			// The boot loader only writes this key on UEFI firmware.
			secureBoot = "Not supported/Legacy"
		}
	}
	switch strings.ToUpper(fields["firmware"]) {
	case "UEFI":
		bootMode = "UEFI"
	case "LEGACY":
		bootMode = "Legacy/BIOS"
	}
	return secureBoot, bootMode
}

// Linux system files read by collectLinuxSystem, all through SimPath so the
// simulated GB10 scenario (spec section 10) can inject fixtures.
const (
	osReleasePath = "/etc/os-release"
	cpuinfoPath   = "/proc/cpuinfo"
	meminfoPath   = "/proc/meminfo"
	efiDirPath    = "/sys/firmware/efi"
)

func collectLinuxSystem(info *types.SystemInfo, errs *[]types.CollectorError, timeout int) {
	// Parse /etc/os-release
	if data, err := ReadSimFile(osReleasePath); err == nil {
		info.OSName, info.OSVersion = parseOSRelease(string(data))
	} else {
		*errs = append(*errs, types.CollectorError{Collector: "system.os-release", Error: err.Error()})
	}

	// Kernel version (/proc/sys/kernel/osrelease, then uname -r)
	info.KernelVersion = readKernelRelease(timeout)

	// CPU model: "model name" from /proc/cpuinfo on x86; arm64 cpuinfo carries
	// only MIDR fields, so lscpu "Model name:" lines and then a MIDR decode are
	// the fallbacks (spec 3.1, "CPU model on arm64").
	info.CPUModel = readLinuxCPUModel(timeout)

	// RAM
	if data, err := ReadSimFile(meminfoPath); err == nil {
		if kb, ok := ParseMeminfo(string(data))["MemTotal"]; ok {
			info.RAMTotalMB = kb / 1024
		}
	}

	// Storage
	r := util.RunCommand(timeout, "sh", "-c", `df -m / | tail -1 | awk '{print $4}'`)
	if r.Err == nil {
		info.StorageFreeMB = parseIntSafe(r.Stdout)
	}

	// Uptime
	r = util.RunCommand(timeout, "uptime", "-p")
	if r.Err == nil {
		info.Uptime = strings.TrimSpace(r.Stdout)
	}

	// Boot mode
	if SimFileExists(efiDirPath) {
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

	// Jetson / Tegra: the GPU is integrated, not on PCIe, and nvidia-smi does
	// not ship with JetPack, so the rest of the pipeline needs to know this is
	// a healthy board rather than a desktop with a missing driver.
	info.IsJetson, info.JetsonRelease = DetectJetson()
}

// tegraReleasePath is written by JetPack / L4T on every Jetson board.
const tegraReleasePath = "/etc/nv_tegra_release"

// deviceTreeModelPath holds the board model string on ARM device-tree
// systems, e.g. "NVIDIA Jetson AGX Orin Developer Kit".
const deviceTreeModelPath = "/proc/device-tree/model"

// DetectJetson reports whether this host is an NVIDIA Jetson / Tegra board
// and, when available, the L4T release line from /etc/nv_tegra_release. It
// only reads two files (through SimPath), so it is safe to call from any
// collector on any OS (both files are absent everywhere but Jetson).
func DetectJetson() (isJetson bool, release string) {
	return detectJetsonFrom(SimPath(tegraReleasePath), SimPath(deviceTreeModelPath))
}

// parseOSRelease returns NAME (or PRETTY_NAME) and VERSION_ID of /etc/os-release.
func parseOSRelease(content string) (name, version string) {
	pretty := ""
	for _, line := range strings.Split(content, "\n") {
		k, v := util.ParseKeyValue(line, "=")
		v = strings.Trim(strings.TrimSpace(v), "\"")
		switch k {
		case "NAME":
			name = v
		case "VERSION_ID":
			version = v
		case "PRETTY_NAME":
			pretty = v
		}
	}
	if name == "" {
		name = pretty
	}
	return name, version
}

// ParseMeminfo parses /proc/meminfo into a map of field name to value in kB
// (HugePages_* counts are plain numbers and are returned as-is).
func ParseMeminfo(content string) map[string]int64 {
	m := map[string]int64{}
	for _, line := range strings.Split(content, "\n") {
		k, v := util.ParseKeyValue(line, ":")
		if k == "" {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		m[k] = n
	}
	return m
}

// readLinuxCPUModel returns the CPU model: /proc/cpuinfo "model name" (x86),
// else lscpu "Model name:" lines joined with " / " (GB10: "Cortex-X925 /
// Cortex-A725"), else a MIDR decode of "CPU implementer" / "CPU part".
func readLinuxCPUModel(timeout int) string {
	cpuinfo := ""
	if data, err := ReadSimFile(cpuinfoPath); err == nil {
		cpuinfo = string(data)
	}
	if m := parseCPUInfoModelName(cpuinfo); m != "" {
		return m
	}
	if util.CommandExists("lscpu") {
		r := util.RunCommand(timeout, "lscpu")
		if r.Err == nil {
			if m := parseLscpuModelNames(r.Stdout); m != "" {
				return m
			}
		}
	}
	return decodeMIDR(cpuinfo)
}

// parseCPUInfoModelName returns the first "model name" value of /proc/cpuinfo.
func parseCPUInfoModelName(cpuinfo string) string {
	for _, line := range strings.Split(cpuinfo, "\n") {
		k, v := util.ParseKeyValue(line, ":")
		if strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseLscpuModelNames joins the distinct "Model name:" values of lscpu output
// in order of appearance (big.LITTLE parts list one per cluster).
func parseLscpuModelNames(out string) string {
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		k, v := util.ParseKeyValue(line, ":")
		if strings.TrimSpace(k) != "Model name" {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || v == "-" || seen[v] {
			continue
		}
		seen[v] = true
		names = append(names, v)
	}
	return strings.Join(names, " / ")
}

// midrParts maps Arm "CPU part" ids (implementer 0x41 = Arm Ltd) to core
// names; the three entries are the ones spec 3.1 names (0xd85 X925, 0xd87
// A725, 0xd4f Neoverse V2).
var midrParts = map[string]string{
	"0xd85": "Cortex-X925",
	"0xd87": "Cortex-A725",
	"0xd4f": "Neoverse-V2",
}

// midrImplementerArm is the "CPU implementer" value for Arm Ltd (spec 3.1).
const midrImplementerArm = "0x41"

// decodeMIDR names the distinct cores of a MIDR-only /proc/cpuinfo (arm64).
// Unknown parts are reported as "ARM part 0x...." so the field is never empty
// when the file had MIDR data at all.
func decodeMIDR(cpuinfo string) string {
	implementer := ""
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(cpuinfo, "\n") {
		k, v := util.ParseKeyValue(line, ":")
		k, v = strings.TrimSpace(k), strings.ToLower(strings.TrimSpace(v))
		switch k {
		case "CPU implementer":
			implementer = v
		case "CPU part":
			if seen[v] {
				continue
			}
			seen[v] = true
			if implementer == midrImplementerArm || implementer == "" {
				if name, ok := midrParts[v]; ok {
					names = append(names, name)
					continue
				}
			}
			names = append(names, "ARM part "+v)
		}
	}
	return strings.Join(names, " / ")
}

// detectJetsonFrom is DetectJetson with injectable paths for tests.
func detectJetsonFrom(releasePath, modelPath string) (isJetson bool, release string) {
	if data, err := os.ReadFile(releasePath); err == nil {
		isJetson = true
		release = parseTegraRelease(string(data))
	}
	if data, err := os.ReadFile(modelPath); err == nil && isJetsonModel(data) {
		isJetson = true
	}
	return isJetson, release
}

// parseTegraRelease returns the first non-empty line of /etc/nv_tegra_release,
// trimmed, e.g. "# R36 (release), REVISION: 4.3, GCID: 38968081, BOARD: generic,
// EABI: aarch64, DATE: Wed Jan 8 01:49:37 UTC 2025".
func parseTegraRelease(content string) string {
	for _, l := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(strings.Trim(l, "\x00")); t != "" {
			return t
		}
	}
	return ""
}

// isJetsonModel reports whether a /proc/device-tree/model value (NUL
// terminated) names an NVIDIA Jetson board.
func isJetsonModel(model []byte) bool {
	s := strings.TrimSpace(strings.Trim(string(model), "\x00"))
	return strings.Contains(s, "NVIDIA Jetson")
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
