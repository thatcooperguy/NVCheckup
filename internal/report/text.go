// Package report generates human-readable and machine-readable reports.
package report

import (
	"fmt"
	"strings"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

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
		w("\n")
	}

	// Driver Info
	w("  NVIDIA Driver: %s\n", valueOrNA(report.Driver.Version))
	w("  CUDA (driver): %s\n", valueOrNA(report.Driver.CUDAVersion))
	line()

	// Platform-specific sections
	if report.Windows != nil {
		writeWindowsSection(&sb, report.Windows)
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
		w("  [%s] #%d: %s\n", f.Severity, i+1, f.Title)
		w("    Evidence:     %s\n", f.Evidence)
		w("    Why:          %s\n", f.WhyItMatters)
		w("    Next Steps:\n")
		for _, step := range f.NextSteps {
			w("      • %s\n", step)
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
	w("  This report was generated locally. No data was sent anywhere.\n")
	if report.Metadata.RedactionEnabled {
		w("  Redaction was applied to remove usernames, hostnames, and IP addresses.\n")
	} else {
		w("  Redaction was DISABLED. This report may contain identifying information.\n")
	}
	w("  NVCheckup does not modify your system, drivers, or settings.\n")
	w("\n")
	line()
	w("  %s\n", types.Disclaimer)
	line()

	return sb.String()
}

func writeWindowsSection(sb *strings.Builder, w *types.WindowsInfo) {
	fmt.Fprintf(sb, "\n== WINDOWS DETAILS ==\n\n")
	fmt.Fprintf(sb, "  HAGS:           %s\n", valueOrNA(w.HAGSEnabled))
	fmt.Fprintf(sb, "  Game Mode:      %s\n", valueOrNA(w.GameMode))
	fmt.Fprintf(sb, "  Power Plan:     %s\n", valueOrNA(w.PowerPlan))

	if len(w.Monitors) > 0 {
		fmt.Fprintf(sb, "\n  Monitors:\n")
		for _, m := range w.Monitors {
			fmt.Fprintf(sb, "    - %s: %s @ %s\n", m.Name, m.Resolution, m.RefreshRate)
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
	fmt.Fprintf(sb, "    Driver Resets (4101):  %d event(s)\n", len(w.DriverResetEvents))
	fmt.Fprintf(sb, "    nvlddmkm Errors:      %d event(s)\n", len(w.NvlddmkmErrors))
	fmt.Fprintf(sb, "    WHEA Errors:           %d event(s)\n", len(w.WHEAErrors))

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
		for mod, loaded := range l.LoadedModules {
			status := "loaded"
			if !loaded {
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

	if l.ContainerRuntime != "" {
		fmt.Fprintf(sb, "  Container:      %s\n", l.ContainerRuntime)
		fmt.Fprintf(sb, "  NV Container:   %s\n", valueOrNA(l.NVContainerToolkit))
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

func valueOrNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
