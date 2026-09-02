package report

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// GenerateMarkdown produces a GitHub/Reddit-ready markdown report.
func GenerateMarkdown(report *types.Report) string {
	var sb strings.Builder
	w := func(format string, args ...interface{}) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}
	row := func(k, v string) { w("| %s | %s |\n", mdCell(k), mdCell(v)) }
	table := func() { w("| Property | Value |\n|----------|-------|\n") }

	w("# NVCheckup Diagnostic Report\n\n")
	w("> %s\n\n", types.Disclaimer)
	w("**Generated:** %s | **Mode:** %s | **Platform:** %s | **Runtime:** %.1fs\n\n",
		report.Metadata.Timestamp.Format("2006-01-02 15:04:05"),
		report.Metadata.Mode, report.Metadata.Platform, report.Metadata.RuntimeSeconds)

	// Summary
	w("## Summary\n\n")
	w("```\n%s```\n\n", ensureTrailingNewline(report.SummaryBlock))

	// System
	w("## System\n\n")
	table()
	os := report.System.OSName + " " + report.System.OSVersion
	if report.System.OSBuild != "" {
		os += " (Build " + report.System.OSBuild + ")"
	}
	row("OS", os)
	if report.System.KernelVersion != "" {
		row("Kernel", report.System.KernelVersion)
	}
	row("Architecture", report.System.Architecture)
	row("CPU", report.System.CPUModel)
	row("RAM", fmt.Sprintf("%d MB", report.System.RAMTotalMB))
	row("Boot Mode", valueOrNA(report.System.BootMode))
	row("Secure Boot", valueOrNA(report.System.SecureBoot))
	if report.System.Uptime != "" {
		row("Uptime", report.System.Uptime)
	}
	w("\n")

	// GPUs
	w("## GPUs\n\n")
	if len(report.GPUs) == 0 {
		w("No GPUs detected.\n\n")
	}
	perGPU := perGPULines(report)
	for _, gpu := range report.GPUs {
		w("### GPU %d: %s\n\n", gpu.Index, mdCell(gpu.Name))
		table()
		row("Vendor", gpu.Vendor)
		row("Driver", valueOrNA(gpu.DriverVersion))
		if gpu.VRAMTotalMB > 0 {
			row("VRAM", fmt.Sprintf("%d MB total / %d MB free", gpu.VRAMTotalMB, gpu.VRAMFreeMB))
		}
		if gpu.Temperature > 0 {
			row("Temperature", fmt.Sprintf("%d°C", gpu.Temperature))
		}
		if gpu.WDDMVersion != "" {
			row("WDDM", gpu.WDDMVersion)
		}
		if perGPU {
			if p := pcieFor(report.GPUPCIe, gpu); p != nil {
				row("PCIe", pcieSummaryFor(report.Findings, p))
			}
			if t := thermalFor(report.GPUThermal, gpu); t != nil {
				row("Thermal", thermalSummary(t))
			}
		}
		w("\n")
	}
	// Samples for indexes the inventory does not know are still shown.
	if perGPU {
		for _, idx := range unmatchedSampleIndexes(report) {
			w("### GPU %d: (not in inventory)\n\n", idx)
			table()
			if p := pcieAt(report.GPUPCIe, idx); p != nil {
				row("PCIe", pcieSummaryFor(report.Findings, p))
			}
			if t := thermalAt(report.GPUThermal, idx); t != nil {
				row("Thermal", thermalSummary(t))
			}
			w("\n")
		}
	}

	w("**NVIDIA Driver:** %s | **CUDA (driver):** %s\n\n", valueOrNA(report.Driver.Version), valueOrNA(report.Driver.CUDAVersion))
	if !perGPU && report.PCIe != nil {
		w("**PCIe:** %s\n\n", pcieSummary(report))
	}
	if !perGPU && report.Thermal != nil {
		w("**Thermal:** %s\n\n", thermalSummary(report.Thermal))
	}

	// Windows details
	if report.Windows != nil {
		win := report.Windows
		w("## Windows\n\n")
		table()
		row("HAGS", valueOrNA(win.HAGSEnabled))
		row("Game Mode", valueOrNA(win.GameMode))
		row("Power Plan", valueOrNA(win.PowerPlan))
		row("Driver resets (4101)", eventCountMD(report.CollectorErrors, "windows.event4101", len(win.DriverResetEvents)))
		row("nvlddmkm errors", eventCountMD(report.CollectorErrors, "windows.nvlddmkm", len(win.NvlddmkmErrors)))
		row("WHEA errors", eventCountMD(report.CollectorErrors, "windows.whea", len(win.WHEAErrors)))
		if win.NVIDIAAppVersion != "" {
			row("NVIDIA App", "v"+win.NVIDIAAppVersion)
		}
		if win.GFEVersion != "" {
			row("GeForce Experience", "v"+win.GFEVersion)
		}
		if len(win.OverlaySoftware) > 0 {
			row("Overlays", strings.Join(win.OverlaySoftware, ", "))
		}
		w("\n")

		monitors := 0
		for _, m := range win.Monitors {
			if m.Resolution != "" {
				monitors++
			}
		}
		if monitors > 0 {
			w("| Monitor | Resolution | Refresh |\n|---------|------------|---------|\n")
			for _, m := range win.Monitors {
				if m.Resolution == "" {
					continue // WMI placeholder rows with no data are noise in a forum post
				}
				w("| %s | %s | %s |\n", mdCell(valueOrNA(m.Name)), mdCell(m.Resolution), mdCell(valueOrNA(m.RefreshRate)))
			}
			w("\n")
		}
		if len(win.RecentKBs) > 0 {
			w("**Recent Windows updates (60 days):** ")
			ids := make([]string, 0, len(win.RecentKBs))
			for _, kb := range win.RecentKBs {
				ids = append(ids, kb.KBID)
			}
			w("%s\n\n", strings.Join(ids, ", "))
		}
	}

	// Linux details
	if report.Linux != nil {
		l := report.Linux
		w("## Linux\n\n")
		table()
		row("Distro", strings.TrimSpace(l.Distro+" "+l.DistroVersion))
		row("Session", valueOrNA(l.SessionType))
		row("Secure Boot", valueOrNA(l.SecureBootState))
		row("DKMS", valueOrNA(l.DKMSStatus))
		row("libcuda.so", valueOrNA(l.LibCudaPath))
		row("PRIME", valueOrNA(l.PRIMEStatus))
		if l.GLRenderer != "" {
			row("GL Renderer", l.GLRenderer)
		}
		if len(l.DevNvidiaNodes) > 0 {
			row("/dev/nvidia*", strings.Join(l.DevNvidiaNodes, ", "))
		} else {
			row("/dev/nvidia*", "NONE")
		}
		row("Xid errors", fmt.Sprintf("%d", len(l.XidErrors)))
		w("\n")
	}

	// AI / CUDA
	if report.AI != nil {
		ai := report.AI
		w("## AI / CUDA Environment\n\n")
		table()
		row("CUDA Toolkit (nvcc)", valueOrNA(ai.CUDAToolkitVersion))
		row("cuDNN", valueOrNA(ai.CuDNNVersion))
		row("Conda", fmt.Sprintf("%v", ai.CondaPresent))
		for _, p := range ai.PythonVersions {
			row("Python", strings.TrimSpace(p.Version+" "+p.Path))
		}
		if ai.PyTorchInfo != nil {
			if ai.PyTorchInfo.Error != "" {
				row("PyTorch", "import failed: "+ai.PyTorchInfo.Error)
			} else {
				row("PyTorch", fmt.Sprintf("%s (CUDA %s, available=%v, device=%s)",
					ai.PyTorchInfo.Version, valueOrNA(ai.PyTorchInfo.CUDAVersion),
					ai.PyTorchInfo.CUDAAvailable, valueOrNA(ai.PyTorchInfo.DeviceName)))
			}
		}
		if ai.TensorFlowInfo != nil {
			if ai.TensorFlowInfo.Error != "" {
				row("TensorFlow", "import failed: "+ai.TensorFlowInfo.Error)
			} else {
				row("TensorFlow", fmt.Sprintf("%s (%d GPU(s))", ai.TensorFlowInfo.Version, len(ai.TensorFlowInfo.GPUs)))
			}
		}
		for _, pkg := range ai.KeyPackages {
			row(pkg.Name, pkg.Version)
		}
		w("\n")
	}

	// Network
	if report.Network != nil {
		n := report.Network
		w("## Network\n\n")
		table()
		row("Interface", fmt.Sprintf("%s (%s)", valueOrNA(n.InterfaceName), valueOrNA(n.InterfaceType)))
		if n.InterfaceType == "wifi" && n.WifiBand != "" {
			row("WiFi", fmt.Sprintf("%s, %d dBm", n.WifiBand, n.WifiSignalDBM))
		}
		row("Latency / Jitter", fmt.Sprintf("%.1f ms / %.1f ms", n.LatencyMs, n.JitterMs))
		row("Packet Loss", fmt.Sprintf("%.1f%%", n.PacketLossPct))
		row("DNS Time", fmt.Sprintf("%.1f ms", n.DNSTimeMs))
		row("Traceroute hops", fmt.Sprintf("%d", len(n.Hops)))
		w("\n")
	}

	// Findings
	w("## Findings\n\n")
	if len(report.Findings) == 0 {
		w("No issues detected.\n\n")
	} else {
		w("| Severity | Finding | Evidence | Next Step |\n")
		w("|----------|---------|----------|-----------|\n")
		for _, f := range report.Findings {
			nextStep := "—"
			if len(f.NextSteps) > 0 {
				nextStep = f.NextSteps[0]
			}
			w("| **%s** | %s | %s | %s |\n",
				f.Severity, mdCell(f.Title),
				mdCell(truncate(f.Evidence, 80)),
				mdCell(truncate(nextStep, 80)))
		}
		w("\n")

		// Detailed findings
		w("### Details\n\n")
		for i, f := range report.Findings {
			w("<details>\n<summary><b>[%s] #%d: %s</b></summary>\n\n", f.Severity, i+1, f.Title)
			if f.ID != "" {
				w("**ID:** `%s`\n\n", f.ID)
			}
			w("**Evidence:** %s\n\n", f.Evidence)
			w("**Why it matters:** %s\n\n", f.WhyItMatters)
			w("**Next steps:**\n")
			for _, step := range f.NextSteps {
				w("- %s\n", step)
			}
			if f.Remediation != nil {
				w("\n**Fix:** `nvcheckup fix --id %s`\n", f.Remediation.ID)
			}
			w("\n</details>\n\n")
		}
	}

	// Top Issues & Next Steps
	w("## Top Issues\n\n")
	for i, issue := range report.TopIssues {
		w("%d. %s\n", i+1, issue)
	}
	w("\n")

	w("## Recommended Next Steps\n\n")
	for i, step := range report.NextSteps {
		w("%d. %s\n", i+1, step)
	}
	w("\n")

	// Collector errors
	if len(report.CollectorErrors) > 0 {
		w("## Collector Notes\n\n")
		for _, ce := range report.CollectorErrors {
			w("- **%s:** %s\n", ce.Collector, ce.Error)
		}
		w("\n")
	}

	// Privacy
	w("---\n\n")
	for _, l := range footerLines(report.Metadata) {
		w("*%s*  \n", l)
	}
	w("*%s*\n", types.Disclaimer)

	return sb.String()
}

// eventCountMD is the markdown counterpart of eventCount.
func eventCountMD(errs []types.CollectorError, collector string, n int) string {
	if collectorFailed(errs, collector) {
		return "not readable (see Collector Notes)"
	}
	return fmt.Sprintf("%d in last 30 days", n)
}

// mdCell makes a string safe for a markdown table cell: pipes are escaped and
// line breaks collapsed, otherwise a multi-line evidence string would break
// the table on GitHub and Reddit.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to at most maxLen runes, appending "..." when cut.
// Counting runes rather than bytes keeps multi-byte characters such as the
// degree sign or an em dash from being split into invalid UTF-8.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string([]rune(s)[:maxLen])
	}
	return string([]rune(s)[:maxLen-3]) + "..."
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
