// Package analyzer produces actionable diagnostic findings from collected data.
package analyzer

import (
	"fmt"
	"strings"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// Analyze takes a partially-filled report (with collected data) and produces findings.
func Analyze(report *types.Report, mode types.RunMode) {
	var findings []types.Finding

	// Always run universal checks
	findings = append(findings, analyzeGPUPresence(report)...)
	findings = append(findings, analyzeDriverBasics(report)...)
	findings = append(findings, analyzeThermal(report)...)
	findings = append(findings, analyzePCIe(report)...)

	// Mode-specific analysis
	switch mode {
	case types.ModeGaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeDisplay(report)...)
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeLinuxAdvanced(report)...)
		findings = append(findings, analyzeNetwork(report)...)
	case types.ModeStreaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeStreaming(report)...)
		findings = append(findings, analyzeNetwork(report)...)
	case types.ModeAI:
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeSecureBoot(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzePyTorch(report)...)
		findings = append(findings, analyzeTensorFlow(report)...)
		findings = append(findings, analyzeLinuxAdvanced(report)...)
	case types.ModeCreator:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzeDisplay(report)...)
	case types.ModeFull:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeStreaming(report)...)
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeSecureBoot(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzePyTorch(report)...)
		findings = append(findings, analyzeTensorFlow(report)...)
		findings = append(findings, analyzeWSL(report)...)
		findings = append(findings, analyzeVRAM(report)...)
		findings = append(findings, analyzeDisplay(report)...)
		findings = append(findings, analyzeNetwork(report)...)
		findings = append(findings, analyzeLinuxAdvanced(report)...)
	}

	// Sort by severity: CRIT first, then WARN, then INFO
	sortFindings(findings)

	report.Findings = findings
	report.TopIssues = buildTopIssues(findings)
	report.NextSteps = buildNextSteps(findings)
	report.SummaryBlock = buildSummaryBlock(report)
}

// ── GPU Presence ──────────────────────────────────────────────────────

func analyzeGPUPresence(report *types.Report) []types.Finding {
	var findings []types.Finding

	nvidiaCount := 0
	for _, gpu := range report.GPUs {
		if gpu.IsNVIDIA {
			nvidiaCount++
		}
	}

	if nvidiaCount == 0 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "No NVIDIA GPU Detected",
			Evidence:     fmt.Sprintf("Found %d GPU(s) but none identified as NVIDIA.", len(report.GPUs)),
			WhyItMatters: "NVCheckup is designed for NVIDIA GPU diagnostics. Without an NVIDIA GPU detected, most checks cannot provide useful results.",
			NextSteps: []string{
				"Verify your NVIDIA GPU is properly seated in the PCIe slot.",
				"Check Device Manager (Windows) or lspci (Linux) for the GPU.",
				"Ensure the NVIDIA driver is installed.",
			},
			Category:   "gpu",
			Confidence: 95,
		})
	}

	// Check for hybrid GPU setup
	if len(report.GPUs) > 1 {
		hasNvidia := false
		hasIGPU := false
		for _, gpu := range report.GPUs {
			if gpu.IsNVIDIA {
				hasNvidia = true
			}
			if gpu.Vendor == "Intel" || gpu.Vendor == "AMD" {
				hasIGPU = true
			}
		}
		if hasNvidia && hasIGPU {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityInfo,
				Title:        "Hybrid GPU Configuration Detected",
				Evidence:     fmt.Sprintf("Found %d GPUs including NVIDIA + integrated graphics.", len(report.GPUs)),
				WhyItMatters: "Hybrid GPU setups (laptops, some desktops) can sometimes route display output through the iGPU, causing confusion about which GPU is active.",
				NextSteps: []string{
					"If experiencing performance issues, verify your application is using the NVIDIA GPU.",
					"On Windows: Check NVIDIA Control Panel > Manage 3D Settings > Preferred Graphics Processor.",
					"On Linux: Check PRIME offloading status or use __NV_PRIME_RENDER_OFFLOAD=1.",
				},
				Category:   "gpu",
				Confidence: 90,
			})
		}
	}

	return findings
}

// ── Driver Basics ─────────────────────────────────────────────────────

func analyzeDriverBasics(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Driver.Version == "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "NVIDIA Driver Version Not Detected",
			Evidence:     "nvidia-smi did not return a driver version.",
			WhyItMatters: "Without a working NVIDIA driver, GPU acceleration (gaming, CUDA, hardware encoding) will not function.",
			NextSteps: []string{
				"Install the NVIDIA driver from https://www.nvidia.com/drivers or your Linux distribution's package manager.",
				"On Linux: Check if the nvidia kernel module is loaded with 'lsmod | grep nvidia'.",
				"After install, reboot and run NVCheckup again.",
			},
			Category:   "driver",
			Confidence: 95,
		})
	}

	if report.Driver.NvidiaSmiPath == "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "nvidia-smi Not Found in PATH",
			Evidence:     "The nvidia-smi utility was not found.",
			WhyItMatters: "nvidia-smi is the primary tool for querying NVIDIA GPU status. Its absence suggests the driver may not be installed or PATH is misconfigured.",
			NextSteps: []string{
				"Install the NVIDIA driver package.",
				"On Windows: nvidia-smi is typically at C:\\Windows\\System32\\nvidia-smi.exe.",
				"On Linux: Ensure the nvidia-utils package is installed.",
			},
			Category:   "driver",
			Confidence: 90,
		})
	}

	return findings
}

// ── Thermal Analysis ──────────────────────────────────────────────────

func analyzeThermal(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Thermal == nil {
		return findings
	}

	t := report.Thermal

	// Critical: active thermal throttling
	if t.ThermalThrottle || t.SlowdownActive {
		reason := "GPU temperature is critically high"
		if t.SlowdownReason != "" {
			reason = t.SlowdownReason
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "GPU Thermal Throttling Active",
			Evidence:     fmt.Sprintf("Temperature: %d°C. Throttle active: %v. Reason: %s.", t.TemperatureC, t.SlowdownActive, reason),
			WhyItMatters: "The GPU is actively reducing performance to prevent heat damage. This causes frame drops, stutter, and reduced compute throughput.",
			NextSteps: []string{
				"Check that case airflow is adequate and intake fans are working.",
				"Clean dust from the GPU heatsink and fans.",
				"Verify thermal paste condition if GPU is older than 3 years.",
				"If overclocked, reduce clocks to stock settings.",
				"Consider adding case fans or improving ventilation.",
			},
			Category:   "performance",
			Confidence: 95,
		})
	} else if t.TemperatureC >= 75 && t.TemperatureC < 85 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "GPU Running Hot",
			Evidence:     fmt.Sprintf("GPU temperature: %d°C (elevated but not throttling yet).", t.TemperatureC),
			WhyItMatters: "While not critically high, sustained temperatures above 75°C reduce GPU lifespan and may lead to throttling under sustained load.",
			NextSteps: []string{
				"Monitor temperatures during extended gaming/compute sessions.",
				"Ensure GPU fans are spinning and case airflow is adequate.",
				"Consider adjusting fan curves to be more aggressive.",
			},
			Category:   "performance",
			Confidence: 80,
		})
	}

	// Fan not spinning at elevated temp
	if t.FanSpeedPct == 0 && t.TemperatureC > 60 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "GPU Fan Not Spinning at Elevated Temperature",
			Evidence:     fmt.Sprintf("Fan speed: 0%% while temperature is %d°C.", t.TemperatureC),
			WhyItMatters: "The GPU fan should be spinning at temperatures above 60°C. This may indicate a fan failure or aggressive zero-RPM fan curve.",
			NextSteps: []string{
				"Check if the GPU uses a zero-RPM fan mode (some cards stop fans below 60°C).",
				"If temperature continues to rise without fan activity, the fan may be faulty.",
				"Use MSI Afterburner or similar to set a manual fan curve.",
			},
			Category:   "hardware",
			Confidence: 70,
		})
	}

	// Power state analysis
	if t.PowerState != "" && t.PowerState != "P0" && t.PowerState != "P1" && t.PowerState != "P2" {
		// Only flag if GPU should be under load (we check if clock is well below max)
		if t.MaxClockMHz > 0 && t.CurrentClockMHz > 0 {
			ratio := float64(t.CurrentClockMHz) / float64(t.MaxClockMHz)
			if ratio < 0.5 {
				findings = append(findings, types.Finding{
					Severity:     types.SeverityWarn,
					Title:        "GPU Power State Not Reaching Maximum Performance",
					Evidence:     fmt.Sprintf("Power state: %s. Clock: %d MHz / %d MHz max (%.0f%%).", t.PowerState, t.CurrentClockMHz, t.MaxClockMHz, ratio*100),
					WhyItMatters: "The GPU is not running at full performance. This may be normal at idle, but if under load it indicates a power management issue.",
					NextSteps: []string{
						"Check if this reading was taken under load or at idle.",
						"On Windows: Set power plan to High Performance.",
						"Set NVIDIA Control Panel > Power Management Mode to 'Prefer Maximum Performance'.",
						"Check for PCIe power cable connections to the GPU.",
					},
					Category:   "performance",
					Confidence: 60,
				})
			}
		}
	}

	return findings
}

// ── PCIe Analysis ─────────────────────────────────────────────────────

func analyzePCIe(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.PCIe == nil {
		return findings
	}

	p := report.PCIe

	if p.Downshifted {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "PCIe Link Downshifted",
			Evidence:     fmt.Sprintf("Current: %s %s. Maximum: %s %s.", p.CurrentSpeed, p.CurrentWidth, p.MaxSpeed, p.MaxWidth),
			WhyItMatters: "The GPU PCIe link is running below its maximum capability. This can reduce GPU bandwidth and cause performance degradation in GPU-bound workloads.",
			NextSteps: []string{
				"Reseat the GPU in the PCIe slot.",
				"Check for bent or dirty PCIe slot pins.",
				"Try a different PCIe slot if available.",
				"Update motherboard BIOS/UEFI.",
				"Note: PCIe link may power-save at idle — recheck under GPU load.",
			},
			Category:   "performance",
			Confidence: 90,
		})
	}

	// Check for legacy PCIe speed
	if p.CurrentSpeed == "Gen1" || p.CurrentSpeed == "Gen2" {
		confidence := 85
		if p.MaxSpeed == p.CurrentSpeed {
			confidence = 50 // Might just be an old slot/GPU
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "PCIe Running at Legacy Speed",
			Evidence:     fmt.Sprintf("Link speed: %s %s.", p.CurrentSpeed, p.CurrentWidth),
			WhyItMatters: "Gen1/Gen2 PCIe speeds significantly limit bandwidth for modern GPUs. This may be normal for older hardware or indicate a configuration issue.",
			NextSteps: []string{
				"Verify the GPU is in a PCIe 3.0 or 4.0 x16 slot.",
				"Check BIOS PCIe settings (some BIOSes default to Gen2 for compatibility).",
				"Ensure no riser cables or adapters are limiting link speed.",
			},
			Category:   "performance",
			Confidence: confidence,
		})
	}

	return findings
}

// ── Display Analysis ──────────────────────────────────────────────────

func analyzeDisplay(report *types.Report) []types.Finding {
	var findings []types.Finding

	if len(report.Displays) < 2 {
		return findings
	}

	// Check mixed refresh rates
	refreshRates := map[int]bool{}
	for _, d := range report.Displays {
		if d.RefreshHz > 0 {
			refreshRates[d.RefreshHz] = true
		}
	}
	if len(refreshRates) > 1 {
		var rates []string
		for r := range refreshRates {
			rates = append(rates, fmt.Sprintf("%dHz", r))
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Mixed Refresh Rate Multi-Monitor Setup",
			Evidence:     fmt.Sprintf("%d monitors with different refresh rates: %s.", len(report.Displays), strings.Join(rates, ", ")),
			WhyItMatters: "Mixed refresh rates across monitors can cause frame pacing issues, stutter, and micro-lag in some applications and desktop compositors.",
			NextSteps: []string{
				"If experiencing stutter, try disabling hardware acceleration in browser/apps on the secondary monitor.",
				"On Windows: Ensure both monitors use the correct refresh rate in Display Settings.",
				"Consider closing secondary monitor apps during competitive gaming.",
			},
			Category:   "display",
			Confidence: 65,
		})
	}

	// High display chain complexity (3+ monitors on same GPU)
	if len(report.Displays) >= 3 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "High Display Chain Complexity",
			Evidence:     fmt.Sprintf("%d displays connected.", len(report.Displays)),
			WhyItMatters: "Running 3 or more displays from a single GPU increases GPU compositor load and may reduce gaming performance by a few percent.",
			NextSteps: []string{
				"If experiencing performance issues, try disconnecting unused monitors during demanding workloads.",
				"Consider using the iGPU for secondary displays if available.",
			},
			Category:   "display",
			Confidence: 50,
		})
	}

	return findings
}

// ── Network Analysis ──────────────────────────────────────────────────

func analyzeNetwork(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Network == nil {
		return findings
	}

	n := report.Network
	hasIssue := false

	// High jitter
	if n.JitterMs > 15 {
		hasIssue = true
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "High Network Jitter Detected",
			Evidence:     fmt.Sprintf("Jitter: %.1f ms (threshold: 15 ms). Interface: %s (%s).", n.JitterMs, n.InterfaceName, n.InterfaceType),
			WhyItMatters: "High jitter causes inconsistent latency, leading to lag spikes and stutter in online games and real-time applications.",
			NextSteps: []string{
				"If on Wi-Fi, switch to ethernet for lower and more consistent latency.",
				"Check for background downloads or streaming on the network.",
				"If on ethernet, check cable quality and switch/router condition.",
			},
			Category:   "network",
			Confidence: 85,
		})
	}

	// Packet loss
	if n.PacketLossPct > 0 {
		hasIssue = true
		sev := types.SeverityWarn
		confidence := 90
		if n.PacketLossPct > 5 {
			sev = types.SeverityCrit
			confidence = 95
		}
		findings = append(findings, types.Finding{
			Severity:     sev,
			Title:        "Packet Loss Detected",
			Evidence:     fmt.Sprintf("Packet loss: %.1f%%. Interface: %s (%s).", n.PacketLossPct, n.InterfaceName, n.InterfaceType),
			WhyItMatters: "Packet loss causes disconnections, rubber-banding in games, and degraded streaming quality. This is a significant network quality issue.",
			NextSteps: []string{
				"If on Wi-Fi, switch to ethernet.",
				"Restart your router/modem.",
				"Contact your ISP if packet loss persists on ethernet.",
				"Check for failing network hardware (cable, switch, NIC).",
			},
			Category:   "network",
			Confidence: confidence,
		})
	}

	// Wi-Fi congestion
	if n.InterfaceType == "wifi" && n.WifiBand == "2.4GHz" {
		hasIssue = true
		confidence := 60
		if n.WifiSignalDBM < -70 {
			confidence = 75
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Wi-Fi Congestion Likely",
			Evidence:     fmt.Sprintf("Connected on %s Wi-Fi. Signal: %d dBm.", n.WifiBand, n.WifiSignalDBM),
			WhyItMatters: "2.4 GHz Wi-Fi is more susceptible to congestion from nearby networks, microwaves, and other devices. This can cause latency spikes.",
			NextSteps: []string{
				"Switch to 5 GHz or 6 GHz Wi-Fi band if available.",
				"Use ethernet for the most reliable connection.",
				"Move closer to the router or remove obstructions.",
			},
			Category:   "network",
			Confidence: confidence,
		})
	}

	// DNS slow
	if n.DNSTimeMs > 100 {
		hasIssue = true
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Slow DNS Resolution",
			Evidence:     fmt.Sprintf("DNS resolution time: %.0f ms.", n.DNSTimeMs),
			WhyItMatters: "Slow DNS adds latency to the initial connection to servers. While it doesn't affect ongoing connections, it delays matchmaking and page loads.",
			NextSteps: []string{
				"Consider switching to a faster DNS provider (1.1.1.1, 8.8.8.8, or 9.9.9.9).",
				"Check if your router's DNS settings are optimal.",
			},
			Category:   "network",
			Confidence: 70,
		})
	}

	// Network healthy
	if !hasIssue {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Network Appears Healthy",
			Evidence:     fmt.Sprintf("Latency: %.1f ms. Jitter: %.1f ms. Packet loss: %.1f%%. DNS: %.0f ms.", n.LatencyMs, n.JitterMs, n.PacketLossPct, n.DNSTimeMs),
			WhyItMatters: "Local network and LAN diagnostics look good. If you are experiencing online issues, they are likely upstream or service-side.",
			NextSteps:    []string{"No network action needed. Issue may be external to your network."},
			Category:     "network",
			Confidence:   80,
		})
	}

	return findings
}

// ── Linux Advanced (Xid, llvmpipe, Wayland) ───────────────────────────

func analyzeLinuxAdvanced(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Linux == nil {
		return findings
	}

	// Xid errors
	if len(report.Linux.XidErrors) > 0 {
		totalCount := 0
		var codes []string
		for _, xid := range report.Linux.XidErrors {
			totalCount += xid.Count
			codes = append(codes, fmt.Sprintf("Xid %d (%s) x%d", xid.Code, xid.Message, xid.Count))
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "NVIDIA Xid Errors Detected",
			Evidence:     fmt.Sprintf("%d Xid error(s) found: %s.", totalCount, strings.Join(codes, "; ")),
			WhyItMatters: "Xid errors are GPU hardware/driver fault reports from the NVIDIA kernel module. They indicate serious issues ranging from memory faults to the GPU falling off the PCIe bus.",
			NextSteps: []string{
				"Update to the latest NVIDIA driver.",
				"If Xid 79 (fallen off bus): Check PCIe power connections and slot seating.",
				"If Xid 48/63 (ECC/remapper): GPU VRAM may be degrading — consider RMA.",
				"If overclocked, revert to stock clocks.",
				"Run a GPU stress test (e.g., furmark) while monitoring for new Xid errors.",
			},
			Category:   "hardware",
			Confidence: 95,
		})
	}

	// llvmpipe fallback
	if report.Linux.LlvmpipeFallback {
		renderer := report.Linux.GLRenderer
		if renderer == "" {
			renderer = "llvmpipe (software)"
		}
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "Software Rendering (llvmpipe) Active",
			Evidence:     fmt.Sprintf("OpenGL renderer: %s.", renderer),
			WhyItMatters: "The system is using CPU-based software rendering instead of the NVIDIA GPU. All graphics and CUDA workloads will be extremely slow.",
			NextSteps: []string{
				"Ensure the NVIDIA driver is installed and the nvidia kernel module is loaded.",
				"Check that LIBGL_ALWAYS_SOFTWARE is not set to 1.",
				"Verify /dev/nvidia* device nodes exist.",
				"If using Wayland, ensure the correct EGL driver is being selected.",
			},
			Category:   "driver",
			Confidence: 95,
		})
	}

	// Wayland + NVIDIA
	if report.Linux.SessionType == "wayland" {
		// Check if driver is loaded
		nvidiaLoaded := false
		if loaded, exists := report.Linux.LoadedModules["nvidia"]; exists && loaded {
			nvidiaLoaded = true
		}
		if nvidiaLoaded {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityWarn,
				Title:        "Wayland Session with NVIDIA — Known Compatibility Issues",
				Evidence:     fmt.Sprintf("Session type: Wayland. NVIDIA driver: %s.", report.Driver.Version),
				WhyItMatters: "While NVIDIA Wayland support has improved significantly, some applications and compositors may still exhibit screen tearing, window glitches, or reduced performance compared to X11.",
				NextSteps: []string{
					"Ensure you are using driver 535+ with explicit sync support.",
					"If experiencing issues, test with X11 session to compare.",
					"Check if your compositor supports direct scanout and explicit sync.",
				},
				Category:   "driver",
				Confidence: 70,
			})
		}
	}

	return findings
}

// ── Windows Gaming ────────────────────────────────────────────────────

func analyzeWindowsGaming(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Windows == nil {
		return findings
	}
	w := report.Windows

	// Driver Reset Detection (Event ID 4101)
	if len(w.DriverResetEvents) > 0 {
		count := len(w.DriverResetEvents)
		lastEvent := w.DriverResetEvents[0] // Most recent first

		sev := types.SeverityWarn
		confidence := 85
		if count >= 3 {
			sev = types.SeverityCrit
			confidence = 92
		}

		findings = append(findings, types.Finding{
			Severity: sev,
			Title:    "Display Driver Resets Detected (Event ID 4101)",
			Evidence: fmt.Sprintf("%d driver reset event(s) in the last 30 days. Most recent: %s.",
				count, lastEvent.Time.Format("2006-01-02 15:04")),
			WhyItMatters: "Event ID 4101 indicates the display driver stopped responding and was recovered by Windows. Frequent occurrences cause black screens, freezes, and application crashes.",
			NextSteps: []string{
				"Update to the latest NVIDIA driver (clean install recommended).",
				"Check GPU temperatures — overheating can trigger driver resets.",
				"If overclocked, revert GPU clocks to stock settings.",
				"Test with Hardware-Accelerated GPU Scheduling (HAGS) toggled off.",
				"If recent Windows Update coincides with issues, consider testing a rollback (understand security implications first).",
			},
			Category:   "driver",
			Confidence: confidence,
		})
	}

	// nvlddmkm errors
	if len(w.NvlddmkmErrors) > 0 {
		count := len(w.NvlddmkmErrors)
		sev := types.SeverityWarn
		confidence := 85
		if count >= 5 {
			sev = types.SeverityCrit
			confidence = 92
		}

		findings = append(findings, types.Finding{
			Severity:     sev,
			Title:        "nvlddmkm Driver Errors Detected",
			Evidence:     fmt.Sprintf("%d nvlddmkm error(s) in the last 30 days.", count),
			WhyItMatters: "nvlddmkm is the NVIDIA Windows kernel-mode driver. Errors here often correlate with crashes, BSODs, or display instability.",
			NextSteps: []string{
				"Perform a clean driver reinstall using the NVIDIA installer's 'Clean Install' option.",
				"If persistent, consider using DDU (Display Driver Uninstaller) in Safe Mode before reinstalling.",
				"Check for BIOS/UEFI updates for your motherboard.",
				"Test GPU in another PCIe slot if available.",
			},
			Category:   "driver",
			Confidence: confidence,
		})
	}

	// WHEA errors
	if len(w.WHEAErrors) > 0 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "Hardware Errors (WHEA) Detected",
			Evidence:     fmt.Sprintf("%d WHEA hardware error(s) in the last 30 days.", len(w.WHEAErrors)),
			WhyItMatters: "WHEA (Windows Hardware Error Architecture) errors indicate hardware-level issues. These can be CPU, memory, or PCIe related and may contribute to system instability.",
			NextSteps: []string{
				"Run Windows Memory Diagnostic (mdsched.exe) to test RAM.",
				"If CPU is overclocked, test at stock speeds.",
				"Check PCIe slot seating and power connections.",
				"Update motherboard BIOS/UEFI to latest version.",
			},
			Category:   "hardware",
			Confidence: 75,
		})
	}

	// Power plan
	if w.PowerPlan != "" && !strings.Contains(strings.ToLower(w.PowerPlan), "high performance") && !strings.Contains(strings.ToLower(w.PowerPlan), "ultimate") {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Power Plan Not Set to High Performance",
			Evidence:     fmt.Sprintf("Active power plan: %s.", w.PowerPlan),
			WhyItMatters: "Balanced or Power Saver plans may throttle CPU/GPU performance. For gaming or CUDA workloads, High Performance is generally recommended.",
			NextSteps: []string{
				"Open Power Options and switch to 'High Performance' for testing.",
				"This is a reversible change with no risk.",
			},
			Category:   "performance",
			Confidence: 40,
			Remediation: &types.RemediationAction{
				ID:          "set-high-performance",
				Title:       "Switch Power Plan to High Performance",
				Risk:        types.RiskLow,
				Description: "Changes the Windows power plan to High Performance using powercfg.",
				DryRunDesc:  "Would run: powercfg /setactive 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c",
				UndoDesc:    "Restore the previous power plan.",
				Platform:    "windows",
				NeedsReboot: false,
				NeedsAdmin:  true,
			},
		})
	}

	// HAGS info
	if w.HAGSEnabled == "Enabled" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Hardware-Accelerated GPU Scheduling (HAGS) is Enabled",
			Evidence:     "HAGS is currently enabled.",
			WhyItMatters: "HAGS can improve performance in some scenarios but has been reported to cause stuttering or instability in certain games or driver versions.",
			NextSteps: []string{
				"If experiencing stutter or instability, try disabling HAGS in Settings > System > Display > Graphics > Change default graphics settings.",
				"This is a reversible change.",
			},
			Category:   "performance",
			Confidence: 45,
			Remediation: &types.RemediationAction{
				ID:          "disable-hags",
				Title:       "Disable Hardware-Accelerated GPU Scheduling",
				Risk:        types.RiskLow,
				Description: "Sets the HAGS registry key to Disabled.",
				DryRunDesc:  "Would set registry HwSchMode to 1 (Disabled).",
				UndoDesc:    "Restore HwSchMode to 2 (Enabled). Requires reboot.",
				Platform:    "windows",
				NeedsReboot: true,
				NeedsAdmin:  true,
			},
		})
	}

	// Recent updates correlation
	if len(w.RecentKBs) > 0 && len(w.DriverResetEvents) > 0 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Recent Windows Updates Installed",
			Evidence:     fmt.Sprintf("%d Windows Update(s) installed in the last 60 days.", len(w.RecentKBs)),
			WhyItMatters: "Windows Updates can occasionally introduce driver compatibility issues. If issues started after a specific update, it may be worth investigating.",
			NextSteps: []string{
				"Check if driver issues correlate with a specific KB installation date.",
				"Rollback specific updates only if you understand the security implications.",
				"Prefer updating NVIDIA drivers over rolling back Windows updates.",
			},
			Category:   "updates",
			Confidence: 35,
		})
	}

	return findings
}

// ── Overlay Analysis ──────────────────────────────────────────────────

func analyzeOverlays(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Windows == nil {
		return findings
	}

	// NVIDIA App / GFE overlay
	if report.Windows.NVIDIAAppVersion != "" || report.Windows.GFEVersion != "" {
		appName := "NVIDIA App"
		version := report.Windows.NVIDIAAppVersion
		if version == "" {
			appName = "GeForce Experience"
			version = report.Windows.GFEVersion
		}

		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        fmt.Sprintf("%s Detected (v%s)", appName, version),
			Evidence:     fmt.Sprintf("%s version %s is installed.", appName, version),
			WhyItMatters: "The in-game overlay, Game Filters, and Photo Mode features can occasionally impact performance or cause alt-tab issues in some games.",
			NextSteps: []string{
				"If experiencing performance drops or alt-tab bugs, try disabling the in-game overlay temporarily.",
				"This does not require uninstalling — just toggle the overlay feature off in settings.",
			},
			Category:   "overlay",
			Confidence: 50,
		})
	}

	// Other overlays
	if len(report.Windows.OverlaySoftware) > 0 {
		overlayList := strings.Join(report.Windows.OverlaySoftware, ", ")
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "Overlay/Recording Software Detected",
			Evidence:     fmt.Sprintf("Detected: %s.", overlayList),
			WhyItMatters: "Multiple active overlays can compete for resources and cause frame pacing issues, stutter, or input lag. This is informational — these tools are commonly used and are not inherently problematic.",
			NextSteps: []string{
				"If experiencing stutter, try disabling overlays one at a time to isolate the cause.",
				"Ensure only one overlay/recording tool is active during gaming.",
			},
			Category:   "overlay",
			Confidence: 50,
		})
	}

	return findings
}

// ── Streaming ─────────────────────────────────────────────────────────

func analyzeStreaming(report *types.Report) []types.Finding {
	var findings []types.Finding

	hasNvidiaGPU := false
	for _, gpu := range report.GPUs {
		if gpu.IsNVIDIA {
			hasNvidiaGPU = true
			break
		}
	}

	if !hasNvidiaGPU {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "No NVIDIA GPU Available for Hardware Encoding",
			Evidence:     "No NVIDIA GPU detected — hardware encoding is not available.",
			WhyItMatters: "NVIDIA hardware encoding is used by OBS, Shadowplay, and other streaming/recording tools. Without an NVIDIA GPU, software encoding must be used instead.",
			NextSteps: []string{
				"Ensure the NVIDIA GPU is properly installed and detected.",
				"Install the NVIDIA driver.",
			},
			Category:   "gpu",
			Confidence: 95,
		})
	}

	return findings
}

// ── Linux Modules ─────────────────────────────────────────────────────

func analyzeLinuxModules(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Linux == nil {
		return findings
	}

	mods := report.Linux.LoadedModules

	// Check for nouveau
	if loaded, exists := mods["nouveau"]; exists && loaded {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "Nouveau Driver is Active (Instead of NVIDIA)",
			Evidence:     "The open-source 'nouveau' kernel module is loaded instead of the proprietary NVIDIA driver.",
			WhyItMatters: "Nouveau does not support CUDA or Vulkan performance comparable to the NVIDIA driver. GPU acceleration will be severely limited.",
			NextSteps: []string{
				"Install the proprietary NVIDIA driver for your distribution.",
				"Blacklist nouveau: add 'blacklist nouveau' and 'options nouveau modeset=0' to /etc/modprobe.d/blacklist-nouveau.conf.",
				"Rebuild initramfs and reboot.",
				"Debian/Ubuntu: sudo apt install nvidia-driver-XXX",
				"Fedora: sudo dnf install akmod-nvidia",
				"Arch: sudo pacman -S nvidia",
			},
			Category:   "driver",
			Confidence: 95,
			Remediation: &types.RemediationAction{
				ID:          "blacklist-nouveau",
				Title:       "Blacklist Nouveau Driver",
				Risk:        types.RiskMedium,
				Description: "Creates /etc/modprobe.d/blacklist-nouveau.conf to prevent nouveau from loading.",
				DryRunDesc:  "Would create /etc/modprobe.d/blacklist-nouveau.conf with blacklist entries.",
				UndoDesc:    "Remove /etc/modprobe.d/blacklist-nouveau.conf and rebuild initramfs.",
				Platform:    "linux",
				NeedsReboot: true,
				NeedsAdmin:  true,
			},
		})
	}

	// Check nvidia module not loaded
	nvidiaLoaded := false
	if loaded, exists := mods["nvidia"]; exists && loaded {
		nvidiaLoaded = true
	}

	if !nvidiaLoaded && report.Driver.Version == "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "NVIDIA Kernel Module Not Loaded",
			Evidence:     "The 'nvidia' kernel module is not loaded. nvidia-smi will fail.",
			WhyItMatters: "Without the NVIDIA kernel module, the GPU cannot be used for any accelerated workload.",
			NextSteps: []string{
				"Check if the module exists: modinfo nvidia",
				"Try loading manually: sudo modprobe nvidia",
				"Check dmesg for load errors: dmesg | grep -i nvidia",
				"If Secure Boot is enabled, the module may need to be signed (see Secure Boot finding).",
				"If using DKMS, check dkms status for build failures.",
			},
			Category:   "driver",
			Confidence: 95,
		})
	}

	// /dev/nvidia* nodes
	if nvidiaLoaded && len(report.Linux.DevNvidiaNodes) == 0 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "No /dev/nvidia* Device Nodes Found",
			Evidence:     "NVIDIA module appears loaded but /dev/nvidia* device nodes are missing.",
			WhyItMatters: "Applications need /dev/nvidia0, /dev/nvidiactl, etc. to communicate with the GPU.",
			NextSteps: []string{
				"Try running: sudo nvidia-smi (this can create device nodes).",
				"Check if nvidia-persistenced is running.",
				"Ensure nvidia_uvm module is loaded: sudo modprobe nvidia_uvm.",
			},
			Category:   "driver",
			Confidence: 85,
		})
	}

	// libcuda.so
	if report.Linux.LibCudaPath == "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "libcuda.so Not Found",
			Evidence:     "libcuda.so could not be located via ldconfig or common paths.",
			WhyItMatters: "CUDA applications link against libcuda.so. If missing, frameworks like PyTorch and TensorFlow cannot access the GPU.",
			NextSteps: []string{
				"Install the NVIDIA driver package (which provides libcuda.so).",
				"Run 'sudo ldconfig' to update the library cache.",
				"Check LD_LIBRARY_PATH if using a non-standard installation.",
			},
			Category:   "cuda",
			Confidence: 85,
			Remediation: &types.RemediationAction{
				ID:          "update-ldconfig",
				Title:       "Refresh Library Cache (ldconfig)",
				Risk:        types.RiskLow,
				Description: "Runs ldconfig to refresh the shared library cache.",
				DryRunDesc:  "Would run: sudo ldconfig",
				UndoDesc:    "No undo needed — ldconfig only refreshes the cache.",
				Platform:    "linux",
				NeedsReboot: false,
				NeedsAdmin:  true,
			},
		})
	}

	// DKMS failures
	if report.Linux.DKMSErrors != "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "DKMS Build Failure Detected",
			Evidence:     "DKMS reports errors for NVIDIA modules. The driver may not be built for the current kernel.",
			WhyItMatters: "If DKMS fails to build the NVIDIA module for your running kernel (e.g., after a kernel update), the GPU will not function.",
			NextSteps: []string{
				"Run 'sudo dkms autoinstall' to retry building modules.",
				"Ensure kernel headers are installed for your current kernel.",
				"Debian/Ubuntu: sudo apt install linux-headers-$(uname -r)",
				"Fedora: sudo dnf install kernel-devel-$(uname -r)",
				"Check 'dkms status' output for specific error details.",
			},
			Category:   "driver",
			Confidence: 90,
		})
	}

	return findings
}

// ── Secure Boot ───────────────────────────────────────────────────────

func analyzeSecureBoot(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Linux == nil {
		return findings
	}

	if report.Linux.SecureBootState == "Enabled" {
		nvidiaLoaded := false
		if loaded, exists := report.Linux.LoadedModules["nvidia"]; exists && loaded {
			nvidiaLoaded = true
		}

		if !nvidiaLoaded {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityCrit,
				Title:        "Secure Boot Enabled — NVIDIA Module May Be Blocked",
				Evidence:     "Secure Boot is enabled and the NVIDIA kernel module is not loaded.",
				WhyItMatters: "Secure Boot requires kernel modules to be signed with an enrolled key. Unsigned NVIDIA modules will be rejected by the kernel.",
				NextSteps: []string{
					"Option A (Recommended): Sign the NVIDIA module and enroll the key with MOK.",
					"Option B: Disable Secure Boot in BIOS/UEFI (reduces system security).",
					"Some distributions (Ubuntu) handle signing automatically with DKMS.",
				},
				Category:   "secureboot",
				Confidence: 85,
			})
		} else {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityInfo,
				Title:        "Secure Boot Enabled — NVIDIA Module is Loading Successfully",
				Evidence:     "Secure Boot is enabled and the NVIDIA module is loaded. Module signing appears to be properly configured.",
				WhyItMatters: "This is the ideal configuration — security is maintained while NVIDIA drivers function correctly.",
				NextSteps:    []string{"No action needed."},
				Category:     "secureboot",
				Confidence:   95,
			})
		}
	}

	return findings
}

// ── CUDA / AI Analysis ────────────────────────────────────────────────

func analyzeCUDA(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.AI == nil {
		return findings
	}

	// CUDA toolkit vs driver mismatch
	if report.AI.CUDAToolkitVersion != "" && report.Driver.CUDAVersion != "" {
		toolkitMajor := majorVersion(report.AI.CUDAToolkitVersion)
		driverMajor := majorVersion(report.Driver.CUDAVersion)

		if toolkitMajor != "" && driverMajor != "" && toolkitMajor != driverMajor {
			findings = append(findings, types.Finding{
				Severity: types.SeverityWarn,
				Title:    "CUDA Toolkit / Driver Version Mismatch",
				Evidence: fmt.Sprintf("CUDA Toolkit: %s, Driver CUDA runtime: %s.",
					report.AI.CUDAToolkitVersion, report.Driver.CUDAVersion),
				WhyItMatters: "Major version mismatches between the CUDA toolkit and driver can cause compilation or runtime failures. The driver's CUDA runtime must be >= the toolkit version.",
				NextSteps: []string{
					"Update the NVIDIA driver to support CUDA " + report.AI.CUDAToolkitVersion + ".",
					"Or install a CUDA toolkit version matching the driver's supported CUDA version.",
					"Check compatibility at: https://docs.nvidia.com/cuda/cuda-toolkit-release-notes/",
				},
				Category:   "cuda",
				Confidence: 80,
			})
		}
	}

	return findings
}

func analyzePyTorch(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.AI == nil || report.AI.PyTorchInfo == nil {
		return findings
	}

	pt := report.AI.PyTorchInfo

	if pt.Error != "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "PyTorch Import Error",
			Evidence:     fmt.Sprintf("Error importing PyTorch: %s", pt.Error),
			WhyItMatters: "PyTorch could not be loaded, which will prevent GPU-accelerated training and inference.",
			NextSteps: []string{
				"Check your Python environment and PyTorch installation.",
				"Reinstall PyTorch: pip install torch --index-url https://download.pytorch.org/whl/cu121",
			},
			Category:   "ai",
			Confidence: 90,
		})
		return findings
	}

	if !pt.CUDAAvailable {
		if pt.CUDAVersion == "" {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityWarn,
				Title:        "PyTorch Installed Without CUDA Support",
				Evidence:     fmt.Sprintf("PyTorch %s is installed but torch.version.cuda is empty — this is a CPU-only build.", pt.Version),
				WhyItMatters: "A CPU-only PyTorch wheel was installed. torch.cuda.is_available() returns False because the CUDA runtime is not compiled in.",
				NextSteps: []string{
					"Uninstall the current PyTorch: pip uninstall torch torchvision torchaudio",
					"Reinstall with CUDA support from https://pytorch.org/get-started/locally/",
					"Example: pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu121",
					"Make sure to select the correct CUDA version matching your driver.",
				},
				Category:   "ai",
				Confidence: 95,
			})
		} else {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityWarn,
				Title:        "PyTorch CUDA Available but GPU Not Accessible",
				Evidence:     fmt.Sprintf("PyTorch %s has CUDA %s compiled in, but torch.cuda.is_available() is False.", pt.Version, pt.CUDAVersion),
				WhyItMatters: "PyTorch was built with CUDA support but cannot access the GPU. This usually indicates a driver issue or environment mismatch.",
				NextSteps: []string{
					"Ensure nvidia-smi works and shows your GPU.",
					"Check that the NVIDIA driver version supports CUDA " + pt.CUDAVersion + ".",
					"If using conda, ensure you're in the correct environment.",
					"Check LD_LIBRARY_PATH (Linux) or PATH (Windows) includes CUDA libraries.",
				},
				Category:   "ai",
				Confidence: 80,
			})
		}
	} else {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "PyTorch CUDA is Working",
			Evidence:     fmt.Sprintf("PyTorch %s with CUDA %s. GPU: %s.", pt.Version, pt.CUDAVersion, pt.DeviceName),
			WhyItMatters: "GPU acceleration is available for PyTorch workloads.",
			NextSteps:    []string{"No action needed."},
			Category:     "ai",
			Confidence:   95,
		})
	}

	return findings
}

func analyzeTensorFlow(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.AI == nil || report.AI.TensorFlowInfo == nil {
		return findings
	}

	tf := report.AI.TensorFlowInfo

	if tf.Error != "" {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "TensorFlow Import Error",
			Evidence:     fmt.Sprintf("Error: %s", tf.Error),
			WhyItMatters: "TensorFlow could not be loaded properly.",
			NextSteps: []string{
				"Check your Python environment and TensorFlow installation.",
				"Reinstall: pip install tensorflow[and-cuda]",
			},
			Category:   "ai",
			Confidence: 90,
		})
		return findings
	}

	if len(tf.GPUs) == 0 {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "TensorFlow Cannot See GPU",
			Evidence:     fmt.Sprintf("TensorFlow %s detected no GPU devices.", tf.Version),
			WhyItMatters: "TensorFlow will fall back to CPU-only execution, which is significantly slower for training.",
			NextSteps: []string{
				"Ensure tensorflow[and-cuda] or tensorflow-gpu is installed (not just tensorflow).",
				"Check that CUDA and cuDNN versions are compatible with your TensorFlow version.",
				"Verify nvidia-smi shows your GPU and driver is working.",
				"See https://www.tensorflow.org/install/pip for compatibility matrix.",
			},
			Category:   "ai",
			Confidence: 85,
		})
	} else {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "TensorFlow GPU is Working",
			Evidence:     fmt.Sprintf("TensorFlow %s detected %d GPU(s): %s.", tf.Version, len(tf.GPUs), strings.Join(tf.GPUs, ", ")),
			WhyItMatters: "GPU acceleration is available for TensorFlow workloads.",
			NextSteps:    []string{"No action needed."},
			Category:     "ai",
			Confidence:   95,
		})
	}

	return findings
}

// ── WSL ───────────────────────────────────────────────────────────────

func analyzeWSL(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.WSL == nil || !report.WSL.IsWSL {
		return findings
	}

	if !report.WSL.DevDxgExists {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityCrit,
			Title:        "WSL2 GPU Device (/dev/dxg) Not Found",
			Evidence:     "/dev/dxg does not exist in this WSL2 environment.",
			WhyItMatters: "GPU acceleration in WSL2 requires /dev/dxg, which is provided by the Windows host driver. Without it, CUDA will not work in WSL.",
			NextSteps: []string{
				"Update the Windows NVIDIA driver to the latest version (must support WSL2 GPU).",
				"Ensure you are running WSL2 (not WSL1): wsl --set-version <distro> 2",
				"Update WSL: wsl --update",
				"Restart WSL: wsl --shutdown, then reopen.",
			},
			Category:   "wsl",
			Confidence: 95,
		})
	}

	if report.WSL.DevDxgExists && !report.WSL.NvidiaSmiOK {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityWarn,
			Title:        "WSL2: /dev/dxg Exists but nvidia-smi Fails",
			Evidence:     "/dev/dxg is present but nvidia-smi did not run successfully.",
			WhyItMatters: "The GPU paravirtualization device exists but the NVIDIA tools may not be properly configured in the WSL2 guest.",
			NextSteps: []string{
				"Do NOT install a Linux NVIDIA driver inside WSL2 — use the Windows host driver.",
				"Ensure nvidia-smi is available: it should be provided by the Windows driver.",
				"Try: wsl --shutdown from Windows, then reopen WSL.",
			},
			Category:   "wsl",
			Confidence: 80,
		})
	}

	return findings
}

// ── VRAM ──────────────────────────────────────────────────────────────

func analyzeVRAM(report *types.Report) []types.Finding {
	var findings []types.Finding

	for _, gpu := range report.GPUs {
		if gpu.IsNVIDIA && gpu.VRAMTotalMB > 0 && gpu.VRAMTotalMB < 4096 {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityInfo,
				Title:        fmt.Sprintf("Low VRAM Detected: %s (%d MB)", gpu.Name, gpu.VRAMTotalMB),
				Evidence:     fmt.Sprintf("GPU %s has %d MB of VRAM.", gpu.Name, gpu.VRAMTotalMB),
				WhyItMatters: "Less than 4 GB of VRAM may limit performance in modern games and prevent loading larger AI models.",
				NextSteps: []string{
					"For AI workloads: use smaller model variants, reduce batch sizes, or enable gradient checkpointing.",
					"For gaming: lower texture quality and resolution settings.",
				},
				Category:   "hardware",
				Confidence: 90,
			})
		}
	}

	return findings
}

// ── Helpers ───────────────────────────────────────────────────────────

func sortFindings(findings []types.Finding) {
	sevOrder := map[types.Severity]int{
		types.SeverityCrit: 0,
		types.SeverityWarn: 1,
		types.SeverityInfo: 2,
	}
	for i := 0; i < len(findings); i++ {
		for j := i + 1; j < len(findings); j++ {
			if sevOrder[findings[j].Severity] < sevOrder[findings[i].Severity] {
				findings[i], findings[j] = findings[j], findings[i]
			}
		}
	}
}

func buildTopIssues(findings []types.Finding) []string {
	var issues []string
	count := 0
	for _, f := range findings {
		if f.Severity == types.SeverityCrit || f.Severity == types.SeverityWarn {
			conf := ""
			if f.Confidence > 0 {
				conf = fmt.Sprintf(" (%d%% confidence)", f.Confidence)
			}
			issues = append(issues, fmt.Sprintf("[%s] %s%s", f.Severity, f.Title, conf))
			count++
			if count >= 5 {
				break
			}
		}
	}
	if len(issues) == 0 {
		issues = append(issues, "No significant issues detected.")
	}
	return issues
}

func buildNextSteps(findings []types.Finding) []string {
	var steps []string
	seen := make(map[string]bool)
	count := 0
	for _, f := range findings {
		if f.Severity == types.SeverityInfo {
			continue
		}
		for _, step := range f.NextSteps {
			if !seen[step] && count < 5 {
				steps = append(steps, step)
				seen[step] = true
				count++
			}
			if count >= 5 {
				break
			}
		}
		if count >= 5 {
			break
		}
	}
	if len(steps) == 0 {
		steps = append(steps, "No immediate action required. System appears healthy.")
	}
	return steps
}

func buildSummaryBlock(report *types.Report) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("NVCheckup v%s | %s\n", report.Metadata.ToolVersion, report.Metadata.Timestamp.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("OS: %s %s", report.System.OSName, report.System.OSVersion))
	if report.System.KernelVersion != "" {
		sb.WriteString(fmt.Sprintf(" | Kernel: %s", report.System.KernelVersion))
	}
	sb.WriteString(fmt.Sprintf(" | Arch: %s\n", report.System.Architecture))

	if len(report.GPUs) > 0 {
		for _, gpu := range report.GPUs {
			if gpu.IsNVIDIA {
				sb.WriteString(fmt.Sprintf("GPU: %s | Driver: %s", gpu.Name, gpu.DriverVersion))
				if gpu.VRAMTotalMB > 0 {
					sb.WriteString(fmt.Sprintf(" | VRAM: %d MB", gpu.VRAMTotalMB))
				}
				sb.WriteString("\n")
			}
		}
	}

	if report.Driver.CUDAVersion != "" {
		sb.WriteString(fmt.Sprintf("CUDA (driver): %s", report.Driver.CUDAVersion))
		if report.AI != nil && report.AI.CUDAToolkitVersion != "" {
			sb.WriteString(fmt.Sprintf(" | CUDA Toolkit: %s", report.AI.CUDAToolkitVersion))
		}
		sb.WriteString("\n")
	}

	// Thermal summary
	if report.Thermal != nil {
		sb.WriteString(fmt.Sprintf("Temp: %d°C", report.Thermal.TemperatureC))
		if report.Thermal.PowerState != "" {
			sb.WriteString(fmt.Sprintf(" | P-State: %s", report.Thermal.PowerState))
		}
		sb.WriteString("\n")
	}

	// PCIe summary
	if report.PCIe != nil {
		sb.WriteString(fmt.Sprintf("PCIe: %s %s", report.PCIe.CurrentSpeed, report.PCIe.CurrentWidth))
		if report.PCIe.Downshifted {
			sb.WriteString(" (DOWNSHIFTED)")
		}
		sb.WriteString("\n")
	}

	critCount, warnCount := 0, 0
	fixAvailable := 0
	for _, f := range report.Findings {
		switch f.Severity {
		case types.SeverityCrit:
			critCount++
		case types.SeverityWarn:
			warnCount++
		}
		if f.Remediation != nil {
			fixAvailable++
		}
	}
	sb.WriteString(fmt.Sprintf("Findings: %d CRITICAL, %d WARNING, %d total", critCount, warnCount, len(report.Findings)))
	if fixAvailable > 0 {
		sb.WriteString(fmt.Sprintf(" | %d auto-fixable", fixAvailable))
	}
	sb.WriteString("\n")

	return sb.String()
}

func majorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
