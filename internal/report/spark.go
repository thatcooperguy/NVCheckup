package report

// Rendering helpers for the DGX Spark / RTX Spark / unified-memory additions
// (docs/roadmap/spark-support.md sections 5 and 5.1): the Platform block, the
// UNIFIED MEMORY / DGX OS / FIRMWARE / CLUSTER FABRIC sections, the impact
// label next to WARN/CRIT severities and the distinct rendering of next
// steps that start with the word Advisory.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// advisoryRe matches the state-changing steps of spec 5 ("Advisory:" and
// "Advisory: (data loss)" both qualify).
var advisoryRe = regexp.MustCompile(`^Advisory\b`)

// stateChangingRe additionally covers the "Last resort" System Recovery
// steps, which erase the unit and must stay after every read-only step.
var stateChangingRe = regexp.MustCompile(`^(Advisory\b|Last resort\b)`)

// advisoryPrefixRe captures the leading "Advisory" / "Advisory:" token for
// bold rendering in markdown.
var advisoryPrefixRe = regexp.MustCompile(`^Advisory:?`)

// unifiedGPU reports whether a GPU's thermal sample belongs to a
// unified-memory part: the platform flag, or nvidia-smi memory reported as
// not-supported for that GPU (spec 2.1 / 5.1).
func unifiedGPU(report *types.Report, gpu types.GPUInfo) bool {
	return report.Platform.UnifiedMemory || gpu.MemoryReporting == "not-supported"
}

// isAdvisory reports whether a next step is an Advisory (state-changing) step.
func isAdvisory(step string) bool {
	return advisoryRe.MatchString(step)
}

// orderedSteps returns the steps with every read-only step first and the
// Advisory / last-resort steps after them, each group in its original order
// (spec 5: read-only steps always come first).
func orderedSteps(steps []string) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if !stateChangingRe.MatchString(s) {
			out = append(out, s)
		}
	}
	for _, s := range steps {
		if stateChangingRe.MatchString(s) {
			out = append(out, s)
		}
	}
	return out
}

// impactSuffix renders " (impact: persistent)" for WARN/CRIT findings that
// carry an impact; INFO findings and legacy reports without impact print
// nothing extra.
func impactSuffix(f types.Finding) string {
	if f.Impact == "" || (f.Severity != types.SeverityWarn && f.Severity != types.SeverityCrit) {
		return ""
	}
	return " (impact: " + f.Impact + ")"
}

// markdownStep renders one next step for markdown, bolding the Advisory token.
func markdownStep(step string) string {
	if !isAdvisory(step) {
		return step
	}
	m := advisoryPrefixRe.FindString(step)
	return "**" + m + "**" + strings.TrimPrefix(step, m)
}

// platformLabel is the human name of a platform class (mirrors the analyzer).
func platformLabel(class string) string {
	switch class {
	case "dgx-spark":
		return "DGX Spark"
	case "rtx-spark":
		return "RTX Spark"
	case "jetson":
		return "Jetson"
	case "grace-hopper":
		return "Grace Hopper"
	case "arm64-dgpu":
		return "arm64 + discrete GPU"
	}
	return class
}

// feOrOEM labels a GB10 unit from the DMI vendor (spec 3.1 row 10).
func feOrOEM(vendor string) string {
	switch {
	case strings.EqualFold(strings.TrimSpace(vendor), "NVIDIA"):
		return "Founders Edition"
	case strings.TrimSpace(vendor) == "":
		return "vendor unknown"
	}
	return "OEM"
}

// gib renders a kB count as "119.7".
func gib(kb int64) string {
	return fmt.Sprintf("%.1f", float64(kb)/(1024*1024))
}

// hasPlatformBlock reports whether the report carries anything the Platform
// block can show.
func hasPlatformBlock(r *types.Report) bool {
	return r.Platform.Class != "" || r.Platform.UnifiedMemory || r.Platform.IsWindowsOnArm ||
		r.UnifiedMemory != nil || r.DGXOS != nil || r.Cluster != nil
}

// platformRows returns the label/value pairs of the Platform block, shared by
// the text and markdown renderers.
func platformRows(r *types.Report) [][2]string {
	var rows [][2]string
	add := func(k, v string) { rows = append(rows, [2]string{k, v}) }
	p := r.Platform
	if p.Class != "" {
		add("Class", fmt.Sprintf("%s (%s)", platformLabel(p.Class), p.Class))
	} else {
		add("Class", "not classified")
	}
	if model := strings.TrimSpace(p.Vendor + " " + p.Model); model != "" {
		v := model
		if p.Class == "dgx-spark" {
			v += " (" + feOrOEM(p.Vendor) + ")"
		}
		var extra []string
		if p.ProductVersion != "" {
			extra = append(extra, "version "+p.ProductVersion)
		}
		if p.BIOSVersion != "" {
			extra = append(extra, "BIOS "+p.BIOSVersion)
		}
		if len(extra) > 0 {
			v += " [" + strings.Join(extra, ", ") + "]"
		}
		add("Vendor/Model", v)
	}
	if p.GPUSoC != "" || p.ComputeCap != "" {
		v := valueOrNA(p.GPUSoC)
		if p.ComputeCap != "" {
			v += " (compute capability " + p.ComputeCap + ")"
		}
		add("GPU SoC", v)
	}
	if p.UnifiedMemory {
		add("Unified memory", "yes (nvidia-smi memory, fan, power limit and PCIe fields are [N/A] by design)")
	} else if p.Class != "" {
		add("Unified memory", "no")
	}
	if r.DGXOS != nil {
		v := valueOrNA(r.DGXOS.SWBuildVersion)
		if r.DGXOS.OTAName != "" || r.DGXOS.OTAVersion != "" {
			v += " / OTA " + valueOrNA(r.DGXOS.OTAName)
			if r.DGXOS.OTAVersion != "" {
				v += " (" + r.DGXOS.OTAVersion + ")"
			}
		}
		add("DGX OS", v)
	}
	if um := r.UnifiedMemory; um != nil {
		add("Memory pool", fmt.Sprintf("%s GiB total, %s GiB available, %s GiB allocatable (MemAvailable + SwapFree; HugePages override, spec 3.3)",
			gib(um.MemTotalKB), gib(um.MemAvailableKB), gib(um.AllocatableKB)))
	}
	if c := r.Cluster; c != nil && len(c.Ports) > 0 {
		parts := make([]string, 0, len(c.Ports))
		for _, port := range c.Ports {
			name := port.Netdev
			if name == "" {
				name = port.RDMADev
			}
			state := port.State
			if state == "" {
				state = port.PhysState
			}
			parts = append(parts, fmt.Sprintf("%s %s %d Mb/s", valueOrNA(name), valueOrNA(state), port.SpeedMbps))
		}
		add("Cluster fabric", fmt.Sprintf("%d ConnectX-7 port(s): %s", len(c.Ports), strings.Join(parts, "; ")))
	}
	if p.IsWindowsOnArm {
		v := "yes (native " + valueOrNA(p.NativeMachine) + ", NVCheckup emulated: "
		if p.ProcessEmulated {
			v += "yes)"
		} else {
			v += "no)"
		}
		add("Windows on Arm", v)
	}
	return rows
}

// unifiedMemoryRows lists the /proc/meminfo picture.
func unifiedMemoryRows(um *types.UnifiedMemoryInfo) [][2]string {
	swapUsed := um.SwapTotalKB - um.SwapFreeKB
	rows := [][2]string{
		{"MemTotal", gib(um.MemTotalKB) + " GiB"},
		{"MemAvailable", gib(um.MemAvailableKB) + " GiB"},
		{"MemFree", gib(um.MemFreeKB) + " GiB"},
		{"Buffers + Cached", gib(um.BuffersKB+um.CachedKB) + " GiB (reclaimable)"},
		{"Swap", fmt.Sprintf("%s of %s GiB used (%s)", gib(swapUsed), gib(um.SwapTotalKB), valueOrNA(strings.Join(um.SwapDevices, ", ")))},
		{"Swappiness", fmt.Sprintf("%d", um.Swappiness)},
		{"Allocatable", gib(um.AllocatableKB) + " GiB"},
		{"PSI memory", fmt.Sprintf("some avg10 %.2f, full avg10 %.2f", um.PSISomeAvg10, um.PSIFullAvg10)},
		{"GPU processes", fmt.Sprintf("%d", um.GPUProcesses)},
		{"OOM events", fmt.Sprintf("%d OOM-killer, %d NVRM no-memory", um.OOMKills, um.NVRMNoMemory)},
	}
	if um.HugePagesTotal > 0 {
		rows = append(rows, [2]string{"HugePages", fmt.Sprintf("%d total, %d free, %d kB each", um.HugePagesTotal, um.HugePagesFree, um.HugepagesizeKB)})
	}
	return rows
}

// dgxOSRows lists the DGX OS release, pairing and service state.
func dgxOSRows(d *types.DGXOSInfo) [][2]string {
	state := func(b bool) string {
		if b {
			return "active"
		}
		return "inactive"
	}
	torn := "n/a"
	if d.OTATorn != nil {
		torn = fmt.Sprintf("%d", *d.OTATorn)
	}
	modules := "missing"
	if d.ModulesForKernel {
		modules = "present"
	}
	port := "closed"
	if d.DashboardPortOpen {
		port = "open"
	}
	rows := [][2]string{
		{"Release", strings.TrimSpace(valueOrNA(d.PrettyName) + " " + d.SWBuildVersion)},
		{"OTA", strings.TrimSpace(valueOrNA(d.OTAName) + " " + d.OTAVersion + " " + d.OTADate)},
		{"OTA check", fmt.Sprintf("torn=%s, failed: %s", torn, valueOrNA(strings.Join(d.OTAFailed, ", ")))},
		{"Driver package", valueOrNA(d.DriverPkgVersion)},
		{"Firmware package", valueOrNA(d.FirmwarePkgVersion)},
		{"Modules for kernel", modules},
		{"Dashboard", fmt.Sprintf("dgx-dashboard %s, dgx-dashboard-admin %s, port 11000 %s", state(d.DashboardActive), state(d.DashboardAdminActive), port)},
		{"fwupd", state(d.FwupdActive)},
		{"Persistenced", state(d.PersistencedActive)},
	}
	if d.FwupdError != "" {
		rows = append(rows, [2]string{"fwupd error", d.FwupdError})
	}
	if d.AptSourceCorrupt != "" {
		rows = append(rows, [2]string{"apt source", d.AptSourceCorrupt + " (fails to parse)"})
	}
	return rows
}

// firmwareRows lists fwupdmgr components.
func firmwareRows(fw []types.FirmwareComponent) [][2]string {
	rows := make([][2]string, 0, len(fw))
	for _, c := range fw {
		v := valueOrNA(c.Version)
		if c.Pending != "" {
			v += " (pending " + c.Pending + ")"
		}
		rows = append(rows, [2]string{c.Name, v})
	}
	return rows
}

// clusterRows lists the ConnectX-7 fabric state.
func clusterRows(c *types.ClusterInfo) [][2]string {
	var rows [][2]string
	for _, p := range c.Ports {
		name := p.Netdev
		if p.RDMADev != "" {
			name += " (" + p.RDMADev + ")"
		}
		v := fmt.Sprintf("cage %d, %s, %d Mb/s, MTU %d, IPv4 %s", p.Cage, valueOrNA(p.State), p.SpeedMbps, p.MTU, valueOrNA(strings.Join(p.IPv4, ", ")))
		if p.Bond != "" {
			v += ", bond " + p.Bond
		}
		if p.Persistent {
			v += ", persistent"
		}
		rows = append(rows, [2]string{strings.TrimSpace(name), v})
	}
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	rows = append(rows,
		[2]string{"cx7-hotplug-enabled", yesNo(c.HotplugFileEnabled)},
		[2]string{"netplan MTU", fmt.Sprintf("%d", c.NetplanMTU)},
	)
	if len(c.NCCLEnv) > 0 {
		keys := make([]string, 0, len(c.NCCLEnv))
		for k := range c.NCCLEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+c.NCCLEnv[k])
		}
		rows = append(rows, [2]string{"NCCL env", strings.Join(parts, " ")})
	}
	if c.NCCLVersion != "" || c.NCCLPluginLib != "" {
		rows = append(rows, [2]string{"NCCL", strings.TrimSpace(valueOrNA(c.NCCLVersion) + " plugin " + valueOrNA(c.NCCLPluginLib))})
	}
	rows = append(rows,
		[2]string{"nvidia-peermem", "load attempted: " + yesNo(c.PeermemAttempted)},
		[2]string{"avahi", fmt.Sprintf("%s, %d hostname conflict(s)", map[bool]string{true: "active", false: "inactive"}[c.AvahiActive], c.AvahiConflicts)},
		[2]string{"ufw", map[bool]string{true: "enabled", false: "disabled"}[c.UfwEnabled]},
	)
	if len(c.RDMATools) > 0 {
		rows = append(rows, [2]string{"rdma tools", strings.Join(c.RDMATools, ", ")})
	}
	return rows
}

// poolSource names where the unified-memory pool figures come from (spec 3.3
// on Linux, Win32_OperatingSystem on Windows on Arm, spec 8).
func poolSource(r *types.Report) string {
	if r.Platform.IsWindowsOnArm || r.Metadata.Platform == "windows" {
		return "Win32_OperatingSystem TotalVisibleMemorySize / FreePhysicalMemory"
	}
	return "/proc/meminfo is the only truthful VRAM source"
}

// writeRows prints aligned "  Label:  value" lines for the text report.
func writeRows(sb *strings.Builder, rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(sb, "  %-*s %s\n", width+1, r[0]+":", r[1])
	}
}

// writePlatformSections writes the Platform block and the optional detail
// sections of the text report.
func writePlatformSections(sb *strings.Builder, r *types.Report, line func()) {
	if !hasPlatformBlock(r) {
		return
	}
	fmt.Fprintf(sb, "\n== PLATFORM ==\n\n")
	writeRows(sb, platformRows(r))
	line()
	if r.UnifiedMemory != nil {
		fmt.Fprintf(sb, "\n== UNIFIED MEMORY (%s) ==\n\n", poolSource(r))
		writeRows(sb, unifiedMemoryRows(r.UnifiedMemory))
		line()
	}
	if r.DGXOS != nil {
		fmt.Fprintf(sb, "\n== DGX OS ==\n\n")
		writeRows(sb, dgxOSRows(r.DGXOS))
		line()
	}
	if len(r.Platform.Firmware) > 0 {
		fmt.Fprintf(sb, "\n== FIRMWARE (fwupdmgr get-devices) ==\n\n")
		writeRows(sb, firmwareRows(r.Platform.Firmware))
		line()
	}
	if r.Cluster != nil {
		fmt.Fprintf(sb, "\n== CLUSTER FABRIC (ConnectX-7) ==\n\n")
		writeRows(sb, clusterRows(r.Cluster))
		line()
	}
}

// writePlatformSectionsMarkdown is the markdown counterpart.
func writePlatformSectionsMarkdown(sb *strings.Builder, r *types.Report) {
	if !hasPlatformBlock(r) {
		return
	}
	table := func(title string, rows [][2]string) {
		fmt.Fprintf(sb, "## %s\n\n| Property | Value |\n|----------|-------|\n", title)
		for _, row := range rows {
			fmt.Fprintf(sb, "| %s | %s |\n", mdCell(row[0]), mdCell(row[1]))
		}
		sb.WriteString("\n")
	}
	table("Platform", platformRows(r))
	if r.UnifiedMemory != nil {
		table("Unified Memory", unifiedMemoryRows(r.UnifiedMemory))
	}
	if r.DGXOS != nil {
		table("DGX OS", dgxOSRows(r.DGXOS))
	}
	if len(r.Platform.Firmware) > 0 {
		table("Firmware", firmwareRows(r.Platform.Firmware))
	}
	if r.Cluster != nil {
		table("Cluster Fabric (ConnectX-7)", clusterRows(r.Cluster))
	}
}
