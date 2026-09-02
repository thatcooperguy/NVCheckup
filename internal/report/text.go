// Package report generates human-readable and machine-readable reports.
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Footer sentences shared by the text and markdown renderers. The wording is
// part of the documented CLI contract; change it together with the docs.
const (
	footerLocal    = "This report was generated locally. No diagnostic data was transmitted."
	footerProbes   = "Network probes were run at your request (ICMP ping and traceroute to 1.1.1.1, DNS lookup of google.com)."
	footerReadOnly = "The run command did not modify your system. Changes are made only by 'nvcheckup fix' after explicit confirmation."
)

// footerLines builds the privacy footer from the report metadata so the
// statements are always true for the run that produced the report.
func footerLines(meta types.ReportMetadata) []string {
	lines := []string{footerLocal}
	if meta.NetworkProbes {
		lines = append(lines, footerProbes)
	}
	if meta.RedactionEnabled {
		lines = append(lines, "Redaction was applied to remove usernames, hostnames, home paths and IP addresses.")
	} else {
		lines = append(lines, "Redaction was DISABLED. This report may contain identifying information.")
	}
	lines = append(lines, footerReadOnly)
	return lines
}

// pcieSummary renders the PCIe link state on one line, deciding from the
// analyzer's findings exactly like the summary block does: "DOWNSHIFTED" is
// printed only when a pcie-downshift or pcie-width-reduced WARN fired.
// Otherwise a link below its maximum generation is labelled idle (a GPU at P8
// with Gen1 negotiated is normal power saving) and a link at maximum gets no
// annotation. PCIeInfo.Downshifted is deliberately not consulted: the
// collector sets it without knowing whether the sample was taken at idle.
func pcieSummary(report *types.Report) string {
	return pcieLine(report.PCIe, pcieWarned(report.Findings))
}

// pcieSummaryFor renders one GPU's PCIe line for a multi-GPU report, deciding
// "DOWNSHIFTED" from the findings attributed to that GPU only.
func pcieSummaryFor(findings []types.Finding, p *types.PCIeInfo) string {
	return pcieLine(p, pcieWarnedFor(findings, p.GPUIndex))
}

// pcieLine formats a PCIe sample; warned selects the DOWNSHIFTED annotation.
func pcieLine(p *types.PCIeInfo, warned bool) string {
	cur := strings.TrimSpace(valueOrNA(p.CurrentSpeed) + " " + p.CurrentWidth)
	switch {
	case warned:
		return fmt.Sprintf("%s (DOWNSHIFTED, max %s %s)", cur, valueOrNA(p.MaxSpeed), p.MaxWidth)
	case pcieGen(p.CurrentSpeed) > 0 && pcieGen(p.CurrentSpeed) < pcieGen(p.MaxSpeed):
		return fmt.Sprintf("%s (idle, max %s)", cur, valueOrNA(p.MaxSpeed))
	default:
		return cur
	}
}

// pcieWarned reports whether the analyzer raised a PCIe link WARN.
func pcieWarned(findings []types.Finding) bool {
	for _, f := range findings {
		if (f.ID == "pcie-downshift" || f.ID == "pcie-width-reduced") && f.Severity == types.SeverityWarn {
			return true
		}
	}
	return false
}

// pcieWarnedFor reports whether a PCIe link WARN fired for the given GPU. A
// finding without GPU attribution counts for every GPU.
func pcieWarnedFor(findings []types.Finding, gpuIndex int) bool {
	for _, f := range findings {
		if (f.ID != "pcie-downshift" && f.ID != "pcie-width-reduced") || f.Severity != types.SeverityWarn {
			continue
		}
		if len(f.GPUIndexes) == 0 {
			return true
		}
		for _, idx := range f.GPUIndexes {
			if idx == gpuIndex {
				return true
			}
		}
	}
	return false
}

// perGPULines reports whether the report carries thermal or PCIe samples for
// more than one GPU. In that case the samples are printed inside each GPU's
// inventory block instead of as the single "PCIe:" / "Thermal:" lines, which
// only ever described GPU 0.
func perGPULines(report *types.Report) bool {
	return len(report.GPUPCIe) > 1 || len(report.GPUThermal) > 1
}

// pcieFor returns the PCIe sample for an inventory entry, or nil.
func pcieFor(samples []types.PCIeInfo, gpu types.GPUInfo) *types.PCIeInfo {
	if !gpu.IsNVIDIA {
		return nil
	}
	for i := range samples {
		if samples[i].GPUIndex == gpu.Index {
			return &samples[i]
		}
	}
	return nil
}

// thermalFor returns the thermal sample for an inventory entry, or nil.
func thermalFor(samples []types.ThermalInfo, gpu types.GPUInfo) *types.ThermalInfo {
	if !gpu.IsNVIDIA {
		return nil
	}
	for i := range samples {
		if samples[i].GPUIndex == gpu.Index {
			return &samples[i]
		}
	}
	return nil
}

// pcieGen parses "Gen4" (or "4") into 4; 0 when unknown.
func pcieGen(s string) int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(s), "gen"))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// collectorFailed reports whether the named collector recorded an error, i.e.
// its data is missing rather than verified empty.
func collectorFailed(errs []types.CollectorError, name string) bool {
	for _, e := range errs {
		if e.Collector == name {
			return true
		}
	}
	return false
}

// eventCount renders an event-log count, or explains that the log could not
// be read so "0 event(s)" is never shown for a failed query.
func eventCount(errs []types.CollectorError, collector string, n int) string {
	if collectorFailed(errs, collector) {
		return "not readable (see Collector Notes)"
	}
	return fmt.Sprintf("%d event(s)", n)
}

// thermalSummary renders temperature, pstate and fan on one line.
func thermalSummary(t *types.ThermalInfo) string {
	parts := []string{fmt.Sprintf("%d°C", t.TemperatureC)}
	if t.PowerState != "" {
		parts = append(parts, t.PowerState)
	}
	if t.FanSupported {
		parts = append(parts, fmt.Sprintf("fan %d%%", t.FanSpeedPct))
	} else {
		parts = append(parts, "fan N/A")
	}
	if t.PowerDrawW != "" && t.PowerLimitW != "" {
		parts = append(parts, fmt.Sprintf("%s / %s W", t.PowerDrawW, t.PowerLimitW))
	}
	if t.SlowdownActive {
		parts = append(parts, "SLOWDOWN: "+strings.Join(t.ThrottleReasons, ","))
	}
	return strings.Join(parts, ", ")
}

// GenerateText produces a human-readable report.txt
func GenerateText(report *types.Report) string {
	var sb strings.Builder
	w := func(format string, args ...interface{}) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}
	line := func() { sb.WriteString(strings.Repeat("─", 72) + "\n") }

	// Header
	line()
	w("  NVCheckup v%s — NVIDIA Diagnostic Report\n", report.Metadata.ToolVersion)
	w("  %s\n", types.Disclaimer)
	line()
	w("  Generated: %s\n", report.Metadata.Timestamp.Format("2006-01-02 15:04:05 MST"))
	w("  Mode:      %s\n", report.Metadata.Mode)
	w("  Platform:  %s\n", report.Metadata.Platform)
	w("  Runtime:   %.1fs\n", report.Metadata.RuntimeSeconds)
	if report.Metadata.RedactionEnabled {
		w("  Redaction: ENABLED (PII removed)\n")
	} else {
		w("  Redaction: DISABLED\n")
	}
	line()

	// Summary Block (designed for forum pasting)
	w("\n== SUMMARY (paste this in support threads) ==\n\n")
	w("%s\n", report.SummaryBlock)
	line()

	// System Info
	w("\n== SYSTEM INFO ==\n\n")
	w("  OS:           %s %s", report.System.OSName, report.System.OSVersion)
	if report.System.OSBuild != "" {
		w(" (Build %s)", report.System.OSBuild)
	}
	w("\n")
	if report.System.KernelVersion != "" {
		w("  Kernel:       %s\n", report.System.KernelVersion)
	}
	w("  Architecture: %s\n", report.System.Architecture)
	w("  CPU:          %s\n", report.System.CPUModel)
	w("  RAM:          %d MB\n", report.System.RAMTotalMB)
	if report.System.StorageFreeMB > 0 {
		w("  Storage Free: %d MB\n", report.System.StorageFreeMB)
	}
	w("  Uptime:       %s\n", report.System.Uptime)
	w("  Boot Mode:    %s\n", report.System.BootMode)
	w("  Secure Boot:  %s\n", report.System.SecureBoot)
	line()

	// GPU Info
	w("\n== GPU INVENTORY ==\n\n")
	if len(report.GPUs) == 0 {
		w("  No GPUs detected.\n")
	}
	perGPU := perGPULines(report)
	for _, gpu := range report.GPUs {
		w("  [GPU %d] %s\n", gpu.Index, gpu.Name)
		w("    Vendor:    %s\n", gpu.Vendor)
		w("    Driver:    %s\n", gpu.DriverVersion)
		if gpu.PCIBusID != "" {
			w("    PCI Bus:   %s\n", gpu.PCIBusID)
		}
		if gpu.VRAMTotalMB > 0 {
			w("    VRAM:      %d MB total, %d MB used, %d MB free\n",
				gpu.VRAMTotalMB, gpu.VRAMUsedMB, gpu.VRAMFreeMB)
		}
		if gpu.Temperature > 0 {
			w("    Temp:      %d°C\n", gpu.Temperature)
		}
		if gpu.WDDMVersion != "" {
			w("    WDDM:      %s\n", gpu.WDDMVersion)
		}
		if perGPU {
			if p := pcieFor(report.GPUPCIe, gpu); p != nil {
				w("    PCIe:      %s\n", pcieSummaryFor(report.Findings, p))
			}
			if t := thermalFor(report.GPUThermal, gpu); t != nil {
				w("    Thermal:   %s\n", thermalSummary(t))
			}
		}
		w("\n")
	}

	// Driver Info. With a single GPU the PCIe and Thermal lines keep their
	// long-standing place and format here; multi-GPU reports print them per
	// GPU above instead.
	w("  NVIDIA Driver: %s\n", valueOrNA(report.Driver.Version))
	w("  CUDA (driver): %s\n", valueOrNA(report.Driver.CUDAVersion))
	if !perGPU && report.PCIe != nil {
		w("  PCIe:          %s\n", pcieSummary(report))
	}
	if !perGPU && report.Thermal != nil {
		w("  Thermal:       %s\n", thermalSummary(report.Thermal))
	}
	line()

	// Platform-specific sections
	if report.Windows != nil {
		writeWindowsSection(&sb, report.Windows, report.CollectorErrors)
		line()
	}

	if report.Linux != nil {
		writeLinuxSection(&sb, report.Linux)
		line()
	}

	if report.WSL != nil && report.WSL.IsWSL {
		writeWSLSection(&sb, report.WSL)
		line()
	}

	if report.AI != nil {
		writeAISection(&sb, report.AI)
		line()
	}

	if report.Network != nil {
		writeNetworkSection(&sb, report.Network)
		line()
	}

	// Findings
	w("\n== FINDINGS ==\n\n")
	if len(report.Findings) == 0 {
		w("  No issues detected.\n")
	}

	critCount, warnCount, infoCount := 0, 0, 0
	for _, f := range report.Findings {
		switch f.Severity {
		case types.SeverityCrit:
			critCount++
		case types.SeverityWarn:
			warnCount++
		case types.SeverityInfo:
			infoCount++
		}
	}
	w("  Total: %d CRITICAL, %d WARNING, %d INFO\n\n", critCount, warnCount, infoCount)

	for i, f := range report.Findings {
		if f.ID != "" {
			w("  [%s] #%d: %s (%s)\n", f.Severity, i+1, f.Title, f.ID)
		} else {
			w("  [%s] #%d: %s\n", f.Severity, i+1, f.Title)
		}
		w("    Evidence:     %s\n", f.Evidence)
		w("    Why:          %s\n", f.WhyItMatters)
		w("    Next Steps:\n")
		for _, step := range f.NextSteps {
			w("      • %s\n", step)
		}
		if f.Remediation != nil {
			w("    Fix:          nvcheckup fix --id %s\n", f.Remediation.ID)
		}
		w("\n")
	}
	line()

	// Top Issues
	w("\n== TOP ISSUES ==\n\n")
	for i, issue := range report.TopIssues {
		w("  %d. %s\n", i+1, issue)
	}
	w("\n")

	// Next Steps
	w("== RECOMMENDED NEXT STEPS ==\n\n")
	for i, step := range report.NextSteps {
		w("  %d. %s\n", i+1, step)
	}
	w("\n")
	line()

	// Collector Errors
	if len(report.CollectorErrors) > 0 {
		w("\n== COLLECTOR NOTES ==\n\n")
		for _, ce := range report.CollectorErrors {
			w("  [%s] %s\n", ce.Collector, ce.Error)
		}
		w("\n")
		line()
	}

	// Privacy
	w("\n== PRIVACY & DATA ==\n\n")
	for _, l := range footerLines(report.Metadata) {
		w("  %s\n", l)
	}
	w("\n")
	line()
	w("  %s\n", types.Disclaimer)
	line()

	return sb.String()
}

func writeWindowsSection(sb *strings.Builder, w *types.WindowsInfo, errs []types.CollectorError) {
	fmt.Fprintf(sb, "\n== WINDOWS DETAILS ==\n\n")
	fmt.Fprintf(sb, "  HAGS:           %s\n", valueOrNA(w.HAGSEnabled))
	fmt.Fprintf(sb, "  Game Mode:      %s\n", valueOrNA(w.GameMode))
	fmt.Fprintf(sb, "  Power Plan:     %s\n", valueOrNA(w.PowerPlan))

	if len(w.Monitors) > 0 {
		fmt.Fprintf(sb, "\n  Monitors:\n")
		for _, m := range w.Monitors {
			if m.Resolution == "" {
				continue // placeholder entries from WMI carry no useful data
			}
			fmt.Fprintf(sb, "    - %s: %s @ %s\n", valueOrNA(m.Name), m.Resolution, valueOrNA(m.RefreshRate))
		}
	}

	if w.NVIDIAAppVersion != "" {
		fmt.Fprintf(sb, "\n  NVIDIA App:     v%s\n", w.NVIDIAAppVersion)
	}
	if w.GFEVersion != "" {
		fmt.Fprintf(sb, "  GeForce Exp:    v%s\n", w.GFEVersion)
	}

	if len(w.OverlaySoftware) > 0 {
		fmt.Fprintf(sb, "\n  Overlay Software Detected:\n")
		for _, o := range w.OverlaySoftware {
			fmt.Fprintf(sb, "    - %s\n", o)
		}
	}

	fmt.Fprintf(sb, "\n  Event Log Summary (last 30 days):\n")
	fmt.Fprintf(sb, "    Driver Resets (4101):  %s\n", eventCount(errs, "windows.event4101", len(w.DriverResetEvents)))
	fmt.Fprintf(sb, "    nvlddmkm Errors:       %s\n", eventCount(errs, "windows.nvlddmkm", len(w.NvlddmkmErrors)))
	fmt.Fprintf(sb, "    WHEA Errors:           %s\n", eventCount(errs, "windows.whea", len(w.WHEAErrors)))

	if len(w.RecentKBs) > 0 {
		fmt.Fprintf(sb, "\n  Recent Windows Updates (last 60 days):\n")
		for _, kb := range w.RecentKBs {
			fmt.Fprintf(sb, "    - %s: %s (%s)\n", kb.KBID, kb.Title, kb.InstalledOn.Format("2006-01-02"))
		}
	}
}

func writeLinuxSection(sb *strings.Builder, l *types.LinuxInfo) {
	fmt.Fprintf(sb, "\n== LINUX DETAILS ==\n\n")
	fmt.Fprintf(sb, "  Distro:         %s %s\n", l.Distro, l.DistroVersion)
	fmt.Fprintf(sb, "  Package Mgr:    %s\n", l.PackageManager)
	fmt.Fprintf(sb, "  Session Type:   %s\n", valueOrNA(l.SessionType))
	fmt.Fprintf(sb, "  Secure Boot:    %s\n", valueOrNA(l.SecureBootState))

	if l.LoadedModules != nil {
		fmt.Fprintf(sb, "\n  Kernel Modules:\n")
		// Sorted so two reports from the same machine diff cleanly.
		mods := make([]string, 0, len(l.LoadedModules))
		for mod := range l.LoadedModules {
			mods = append(mods, mod)
		}
		sort.Strings(mods)
		for _, mod := range mods {
			status := "loaded"
			if !l.LoadedModules[mod] {
				status = "NOT loaded (exists but inactive)"
			}
			fmt.Fprintf(sb, "    - %-20s %s\n", mod, status)
		}
	}

	if len(l.DevNvidiaNodes) > 0 {
		fmt.Fprintf(sb, "\n  /dev/nvidia* nodes: %s\n", strings.Join(l.DevNvidiaNodes, ", "))
	} else {
		fmt.Fprintf(sb, "\n  /dev/nvidia* nodes: NONE\n")
	}

	fmt.Fprintf(sb, "  libcuda.so:     %s\n", valueOrNA(l.LibCudaPath))
	fmt.Fprintf(sb, "  DKMS Status:    %s\n", valueOrNA(l.DKMSStatus))
	fmt.Fprintf(sb, "  PRIME:          %s\n", valueOrNA(l.PRIMEStatus))
	if l.GLRenderer != "" {
		fmt.Fprintf(sb, "  GL Renderer:    %s\n", l.GLRenderer)
	}

	if l.ContainerRuntime != "" {
		fmt.Fprintf(sb, "  Container:      %s\n", l.ContainerRuntime)
		fmt.Fprintf(sb, "  NV Container:   %s\n", valueOrNA(l.NVContainerToolkit))
	}

	if len(l.XidErrors) > 0 {
		fmt.Fprintf(sb, "\n  Xid Errors:\n")
		for _, x := range l.XidErrors {
			fmt.Fprintf(sb, "    - Xid %d x%d: %s\n", x.Code, x.Count, x.Message)
		}
	}

	if len(l.NVIDIAPackages) > 0 {
		fmt.Fprintf(sb, "\n  NVIDIA Packages Installed:\n")
		for _, pkg := range l.NVIDIAPackages {
			fmt.Fprintf(sb, "    - %s\n", pkg)
		}
	}
}

func writeWSLSection(sb *strings.Builder, w *types.WSLInfo) {
	fmt.Fprintf(sb, "\n== WSL2 DETAILS ==\n\n")
	fmt.Fprintf(sb, "  WSL Version:    %s\n", w.WSLVersion)
	fmt.Fprintf(sb, "  Distro:         %s\n", valueOrNA(w.Distro))
	fmt.Fprintf(sb, "  /dev/dxg:       %v\n", w.DevDxgExists)
	fmt.Fprintf(sb, "  nvidia-smi OK:  %v\n", w.NvidiaSmiOK)
}

func writeAISection(sb *strings.Builder, ai *types.AIInfo) {
	fmt.Fprintf(sb, "\n== AI / CUDA ENVIRONMENT ==\n\n")
	fmt.Fprintf(sb, "  CUDA Toolkit:   %s\n", valueOrNA(ai.CUDAToolkitVersion))
	fmt.Fprintf(sb, "  nvcc Path:      %s\n", valueOrNA(ai.NvccPath))
	fmt.Fprintf(sb, "  cuDNN:          %s\n", valueOrNA(ai.CuDNNVersion))
	fmt.Fprintf(sb, "  Conda:          %v\n", ai.CondaPresent)

	if len(ai.PythonVersions) > 0 {
		fmt.Fprintf(sb, "\n  Python Environments:\n")
		for _, p := range ai.PythonVersions {
			fmt.Fprintf(sb, "    - %s (%s)\n", p.Version, p.Path)
		}
	}

	if ai.PyTorchInfo != nil {
		fmt.Fprintf(sb, "\n  PyTorch:\n")
		if ai.PyTorchInfo.Error != "" {
			fmt.Fprintf(sb, "    Error: %s\n", ai.PyTorchInfo.Error)
		} else {
			fmt.Fprintf(sb, "    Version:        %s\n", ai.PyTorchInfo.Version)
			fmt.Fprintf(sb, "    CUDA Version:   %s\n", valueOrNA(ai.PyTorchInfo.CUDAVersion))
			fmt.Fprintf(sb, "    CUDA Available: %v\n", ai.PyTorchInfo.CUDAAvailable)
			fmt.Fprintf(sb, "    Device:         %s\n", valueOrNA(ai.PyTorchInfo.DeviceName))
		}
	}

	if ai.TensorFlowInfo != nil {
		fmt.Fprintf(sb, "\n  TensorFlow:\n")
		if ai.TensorFlowInfo.Error != "" {
			fmt.Fprintf(sb, "    Error: %s\n", ai.TensorFlowInfo.Error)
		} else {
			fmt.Fprintf(sb, "    Version: %s\n", ai.TensorFlowInfo.Version)
			if len(ai.TensorFlowInfo.GPUs) > 0 {
				fmt.Fprintf(sb, "    GPUs:    %s\n", strings.Join(ai.TensorFlowInfo.GPUs, ", "))
			} else {
				fmt.Fprintf(sb, "    GPUs:    NONE detected\n")
			}
		}
	}

	if len(ai.KeyPackages) > 0 {
		fmt.Fprintf(sb, "\n  Key Packages:\n")
		for _, pkg := range ai.KeyPackages {
			fmt.Fprintf(sb, "    - %-20s %s\n", pkg.Name, pkg.Version)
		}
	}
}

func writeNetworkSection(sb *strings.Builder, n *types.NetworkInfo) {
	fmt.Fprintf(sb, "\n== NETWORK (probes run at your request) ==\n\n")
	fmt.Fprintf(sb, "  Interface:      %s (%s)\n", valueOrNA(n.InterfaceName), valueOrNA(n.InterfaceType))
	if n.InterfaceType == "wifi" {
		if n.WifiBand != "" {
			fmt.Fprintf(sb, "  WiFi Band:      %s\n", n.WifiBand)
		}
		if n.WifiSignalDBM != 0 {
			fmt.Fprintf(sb, "  WiFi Signal:    %d dBm\n", n.WifiSignalDBM)
		}
	}
	fmt.Fprintf(sb, "  Latency:        %.2f ms\n", n.LatencyMs)
	fmt.Fprintf(sb, "  Jitter:         %.2f ms\n", n.JitterMs)
	fmt.Fprintf(sb, "  Packet Loss:    %.1f%%\n", n.PacketLossPct)
	fmt.Fprintf(sb, "  DNS Time:       %.2f ms\n", n.DNSTimeMs)
	if len(n.Hops) > 0 {
		fmt.Fprintf(sb, "\n  Traceroute to 1.1.1.1:\n")
		for _, hop := range n.Hops {
			if hop.Loss {
				fmt.Fprintf(sb, "    %2d. * (timeout)\n", hop.Number)
			} else {
				fmt.Fprintf(sb, "    %2d. %-22s %.2f ms\n", hop.Number, hop.Address, hop.LatencyMs)
			}
		}
	}
}

func valueOrNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
