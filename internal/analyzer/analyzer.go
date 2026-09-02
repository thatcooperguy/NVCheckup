// Package analyzer produces actionable diagnostic findings from collected data.
//
// Every finding carries a stable kebab-case ID (see knowledge/rules.json) so
// that reports can be diffed across runs and so 'nvcheckup fix --id' can refer
// to the rule that produced a remediation.
package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Analyze takes a partially-filled report (with collected data) and produces findings.
//
// The dispatch table below mirrors the runner's collection matrix. Running an
// analyzer in a mode where its data is never collected is harmless (every
// analyzer nil-guards) but misleading, so the two are kept in sync here:
//
//	Windows info  : gaming, streaming, creator, full
//	Displays      : gaming, streaming, full
//	AI / CUDA     : ai, creator, full
//	WSL           : ai, full
//	Linux info    : every mode (Xid / llvmpipe only in gaming, ai, full)
//	Network       : only when --network was passed (report.Network is nil otherwise)
func Analyze(report *types.Report, mode types.RunMode) {
	var findings []types.Finding

	// Universal checks: GPU, driver, thermal and PCIe are collected in every mode.
	findings = append(findings, analyzeGPUPresence(report)...)
	findings = append(findings, analyzeDriverBasics(report)...)
	findings = append(findings, analyzeThermal(report)...)
	findings = append(findings, analyzePCIe(report)...)
	// Network probes are opt-in (--network) and independent of mode; the
	// analyzer simply does nothing when no probe data is present.
	findings = append(findings, analyzeNetwork(report)...)

	switch mode {
	case types.ModeGaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeWindowsPerfSettings(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeDisplay(report)...)
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeLinuxAdvanced(report)...)
	case types.ModeStreaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeWindowsPerfSettings(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeStreaming(report)...)
		findings = append(findings, analyzeDisplay(report)...)
	case types.ModeAI:
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeSecureBoot(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzePyTorch(report)...)
		findings = append(findings, analyzeTensorFlow(report)...)
		findings = append(findings, analyzeWSL(report)...)
		findings = append(findings, analyzeLinuxAdvanced(report)...)
	case types.ModeCreator:
		// Creator collects Windows info and AI info but not displays or WSL.
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzePyTorch(report)...)
		findings = append(findings, analyzeTensorFlow(report)...)
	case types.ModeFull:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeWindowsPerfSettings(report)...)
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
		findings = append(findings, analyzeLinuxAdvanced(report)...)
	}

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
			ID:           "no-nvidia-gpu",
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
				ID:           "hybrid-gpu",
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
			ID:           "driver-not-detected",
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
			ID:           "nvidia-smi-missing",
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

// Bits of nvidia-smi's clocks_event_reasons.active bitmask (historically
// named clocks_throttle_reasons). Only a subset of these are real slowdowns.
const (
	throttleGPUIdle       uint64 = 0x1   // clocks lowered because nothing is running: NOT a slowdown
	throttleAppClocks     uint64 = 0x2   // user-configured application clocks: informational
	throttleSWPowerCap    uint64 = 0x4   // power limit reached
	throttleHWSlowdown    uint64 = 0x8   // external hardware brake (power connector, hot spot)
	throttleSyncBoost     uint64 = 0x10  // multi-GPU sync boost: informational
	throttleSWThermal     uint64 = 0x20  // driver thermal slowdown
	throttleHWThermal     uint64 = 0x40  // hardware thermal slowdown
	throttleHWPowerBrake  uint64 = 0x80  // hardware power brake
	throttleDisplayClocks uint64 = 0x100 // display clock setting: informational
)

// throttleThermalBits are the bits that mean "the GPU is too hot".
const throttleThermalBits = throttleSWThermal | throttleHWThermal

// throttleSlowdownBits are every bit that represents a genuine performance
// reduction. gpu_idle, application clocks, sync boost and display clocks
// are deliberately excluded: an idle GPU parked at low clocks is healthy.
const throttleSlowdownBits = throttleSWPowerCap | throttleHWSlowdown |
	throttleSWThermal | throttleHWThermal | throttleHWPowerBrake

var throttleBitNames = []struct {
	bit  uint64
	name string
}{
	{throttleGPUIdle, "gpu_idle"},
	{throttleAppClocks, "applications_clocks_setting"},
	{throttleSWPowerCap, "sw_power_cap"},
	{throttleHWSlowdown, "hw_slowdown"},
	{throttleSyncBoost, "sync_boost"},
	{throttleSWThermal, "sw_thermal_slowdown"},
	{throttleHWThermal, "hw_thermal_slowdown"},
	{throttleHWPowerBrake, "hw_power_brake_slowdown"},
	{throttleDisplayClocks, "display_clock_setting"},
}

// parseThrottleMask decodes the raw clocks_event_reasons.active value
// ("0x0000000000000004", "4", "Not Active", "None"). ok is false when the
// value is empty or not understood, in which case callers should fall back
// to the collector's boolean flags.
func parseThrottleMask(raw string) (mask uint64, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "":
		return 0, false
	case s == "none", strings.Contains(s, "not active"), strings.Contains(s, "n/a"):
		return 0, true
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// decodeThrottleMask returns the human-readable names of every set bit.
func decodeThrottleMask(mask uint64) []string {
	var names []string
	for _, b := range throttleBitNames {
		if mask&b.bit != 0 {
			names = append(names, b.name)
		}
	}
	return names
}

// parsePState turns "P8" into 8. ok is false for empty/unknown values.
func parsePState(s string) (int, bool) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// thermalState resolves the collector's flags against the raw bitmask. When
// the mask is available it wins, because older collectors set SlowdownActive
// for gpu_idle and ThermalThrottle for any temperature >= 85C.
func thermalState(t *types.ThermalInfo) (thermal, slowdown bool, reasons []string) {
	thermal = t.ThermalThrottle
	slowdown = t.SlowdownActive
	reasons = t.ThrottleReasons

	if mask, ok := parseThrottleMask(t.SlowdownReason); ok {
		thermal = mask&throttleThermalBits != 0
		slowdown = mask&throttleSlowdownBits != 0
		if len(reasons) == 0 {
			reasons = decodeThrottleMask(mask & throttleSlowdownBits)
		}
	}
	return thermal, slowdown, reasons
}

func analyzeThermal(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Thermal == nil {
		return findings
	}

	t := report.Thermal
	thermal, slowdown, reasons := thermalState(t)
	reasonText := strings.Join(reasons, ", ")
	if reasonText == "" {
		reasonText = "not reported"
	}

	switch {
	case thermal:
		findings = append(findings, types.Finding{
			ID:       "thermal-throttling",
			Severity: types.SeverityCrit,
			Title:    "GPU Thermal Throttling Active",
			Evidence: fmt.Sprintf("Temperature: %d°C. Active thermal slowdown bits set. Reasons: %s. Clock: %d MHz / %d MHz max.",
				t.TemperatureC, reasonText, t.CurrentClockMHz, t.MaxClockMHz),
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
	case t.TemperatureC >= 93:
		findings = append(findings, types.Finding{
			ID:           "gpu-running-hot",
			Severity:     types.SeverityCrit,
			Title:        "GPU Running Hot",
			Evidence:     fmt.Sprintf("GPU temperature: %d°C (critical; thermal slowdown is imminent).", t.TemperatureC),
			WhyItMatters: "Temperatures at or above 93°C are at the edge of the GPU's thermal limit. Sustained operation here shortens component life and will trigger throttling.",
			NextSteps: []string{
				"Stop the current workload and let the GPU cool before continuing.",
				"Ensure GPU fans are spinning and case airflow is adequate.",
				"Clean dust from the GPU heatsink and check thermal paste condition.",
			},
			Category:   "performance",
			Confidence: 90,
		})
	case t.TemperatureC >= 85:
		findings = append(findings, types.Finding{
			ID:           "gpu-running-hot",
			Severity:     types.SeverityWarn,
			Title:        "GPU Running Hot",
			Evidence:     fmt.Sprintf("GPU temperature: %d°C (elevated but not throttling yet).", t.TemperatureC),
			WhyItMatters: "While not critically high, sustained temperatures above 85°C reduce GPU lifespan and may lead to throttling under sustained load.",
			NextSteps: []string{
				"Monitor temperatures during extended gaming/compute sessions.",
				"Ensure GPU fans are spinning and case airflow is adequate.",
				"Consider adjusting fan curves to be more aggressive.",
			},
			Category:   "performance",
			Confidence: 80,
		})
	}

	// Non-thermal slowdown (power cap, hardware brake). Reported separately
	// because the fix is different: power limits and cabling, not cooling.
	if slowdown && !thermal {
		findings = append(findings, types.Finding{
			ID:       "gpu-clock-slowdown",
			Severity: types.SeverityWarn,
			Title:    "GPU Clock Slowdown Active",
			Evidence: fmt.Sprintf("Active slowdown reasons: %s. Power draw: %s / limit %s. Clock: %d MHz / %d MHz max. Temperature: %d°C.",
				reasonText, valueOrUnknown(t.PowerDrawW), valueOrUnknown(t.PowerLimitW), t.CurrentClockMHz, t.MaxClockMHz, t.TemperatureC),
			WhyItMatters: "The GPU is lowering its clocks for a non-thermal reason such as hitting its power limit or an external hardware brake. Under load this caps performance below what the card can deliver.",
			NextSteps: []string{
				"If sw_power_cap is listed, this is normal at the power limit; raise the limit only if your PSU and cooling allow.",
				"If hw_slowdown or hw_power_brake_slowdown is listed, check the PCIe power cables and PSU capacity.",
				"Re-run under a sustained GPU load to confirm the reason persists.",
			},
			Category:   "performance",
			Confidence: 70,
		})
	}

	// Fan not spinning at elevated temperature. Passive and water-cooled
	// cards report [N/A], which the collector maps to FanSupported=false.
	if t.FanSupported && t.FanSpeedPct == 0 && t.TemperatureC > 60 {
		findings = append(findings, types.Finding{
			ID:           "fan-not-spinning",
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

	// Low power state is only a problem when the GPU is actually busy. At
	// idle P8 is exactly what a healthy GPU does, so require real utilization.
	if ps, ok := parsePState(t.PowerState); ok && ps >= 5 && t.UtilizationPct >= 60 {
		findings = append(findings, types.Finding{
			ID:       "gpu-power-state-stuck",
			Severity: types.SeverityWarn,
			Title:    "GPU Stuck in Low Power State Under Load",
			Evidence: fmt.Sprintf("Power state: %s at %d%% GPU utilization. Clock: %d MHz / %d MHz max.",
				t.PowerState, t.UtilizationPct, t.CurrentClockMHz, t.MaxClockMHz),
			WhyItMatters: "A busy GPU should be in P0-P2. Staying in P5 or lower under load means the driver or power management is holding clocks down, which shows up as low frame rates or slow compute.",
			NextSteps: []string{
				"On Windows: set the power plan to High Performance.",
				"Set NVIDIA Control Panel > Power Management Mode to 'Prefer Maximum Performance'.",
				"Check that all PCIe power cables are connected to the GPU.",
				"Re-run under load to confirm the P-state does not rise.",
			},
			Category:   "performance",
			Confidence: 75,
		})
	}

	return findings
}

// ── PCIe Analysis ─────────────────────────────────────────────────────

// parsePCIeGen turns "Gen4" (or "4") into 4; 0 when unknown.
func parsePCIeGen(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "gen")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// parsePCIeWidth turns "x16" (or "16") into 16; 0 when unknown.
func parsePCIeWidth(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "x")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// pcieUnderLoad reports whether there is positive evidence the GPU was busy
// when the link was sampled. A reduced generation is only a fault under
// load; at idle the link drops to Gen1 by design, so without evidence of
// load we deliberately do NOT warn. The collector's IdleLikely flag is
// authoritative; otherwise a P0-P2 power state counts as loaded, and when no
// P-state was captured at all (older collectors) utilization is the only clue.
func pcieUnderLoad(p *types.PCIeInfo) bool {
	if p.IdleLikely {
		return false
	}
	if ps, ok := parsePState(p.PowerState); ok {
		return ps <= 2
	}
	return p.UtilizationPct >= 20
}

func analyzePCIe(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.PCIe == nil {
		return findings
	}

	p := report.PCIe
	curGen, maxGen := parsePCIeGen(p.CurrentSpeed), parsePCIeGen(p.MaxSpeed)
	curWidth, maxWidth := parsePCIeWidth(p.CurrentWidth), parsePCIeWidth(p.MaxWidth)
	widthReduced := curWidth > 0 && maxWidth > 0 && curWidth < maxWidth
	genReduced := curGen > 0 && maxGen > 0 && curGen < maxGen

	evidence := fmt.Sprintf("Current: %s %s. Maximum: %s %s. P-state: %s. GPU utilization: %d%%.",
		p.CurrentSpeed, p.CurrentWidth, p.MaxSpeed, p.MaxWidth, valueOrUnknown(p.PowerState), p.UtilizationPct)

	switch {
	case widthReduced:
		// Lane width never drops for power saving; a narrow link is physical.
		findings = append(findings, types.Finding{
			ID:           "pcie-width-reduced",
			Severity:     types.SeverityWarn,
			Title:        "PCIe Link Width Reduced",
			Evidence:     evidence,
			WhyItMatters: "The GPU is negotiating fewer PCIe lanes than the slot and card support. Unlike link speed, lane width does not drop at idle, so this points to a seating, slot or riser problem and halves (or worse) available bandwidth.",
			NextSteps: []string{
				"Power off and reseat the GPU firmly in the PCIe slot.",
				"Check the slot for debris or bent pins and confirm it is a full x16 electrical slot.",
				"If using a riser cable or adapter, test without it.",
			},
			Category:   "performance",
			Confidence: 85,
		})
	case genReduced && pcieUnderLoad(p):
		findings = append(findings, types.Finding{
			ID:           "pcie-downshift",
			Severity:     types.SeverityWarn,
			Title:        "PCIe Link Speed Downshifted Under Load",
			Evidence:     evidence,
			WhyItMatters: "The GPU was busy but the PCIe link stayed below its maximum generation. This reduces bandwidth to the GPU and can cause performance degradation in bandwidth-bound workloads.",
			NextSteps: []string{
				"Reseat the GPU in the PCIe slot.",
				"Check BIOS/UEFI PCIe settings (some boards default to Gen2/Gen3 for compatibility).",
				"Try a different PCIe slot or remove any riser cable.",
				"Update motherboard BIOS/UEFI.",
			},
			Category:   "performance",
			Confidence: 85,
		})
	case genReduced:
		// At idle (P8) the link drops to Gen1 by design (ASPM / link power
		// management). This is expected and NOT a fault. This branch also
		// covers samples with no load evidence at all, where the honest
		// answer is "re-check under load", not a warning.
		findings = append(findings, types.Finding{
			ID:           "pcie-idle-power-saving",
			Severity:     types.SeverityInfo,
			Title:        "PCIe Link Power-Saving at Idle (expected)",
			Evidence:     evidence,
			WhyItMatters: "Modern GPUs drop the PCIe link to Gen1 when idle to save power and raise it again under load. This reading was taken at idle, so it does not indicate a problem.",
			NextSteps: []string{
				fmt.Sprintf("Re-run under GPU load to verify the link reaches %s.", p.MaxSpeed),
			},
			Category:   "performance",
			Confidence: 40,
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
		sort.Strings(rates)
		findings = append(findings, types.Finding{
			ID:           "mixed-refresh-rate",
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
			ID:           "display-chain-complex",
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

// networkHasSamples reports whether the probes actually produced data. A
// zero latency with zero loss and no hops means ping/traceroute never ran
// or failed entirely, and saying "healthy" about that would be a lie.
func networkHasSamples(n *types.NetworkInfo) bool {
	return n.LatencyMs != 0 || n.PacketLossPct != 0 || len(n.Hops) != 0
}

func analyzeNetwork(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Network == nil || !networkHasSamples(report.Network) {
		return findings
	}

	n := report.Network
	hasIssue := false

	// High jitter
	if n.JitterMs > 15 {
		hasIssue = true
		findings = append(findings, types.Finding{
			ID:           "high-jitter",
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
			ID:           "packet-loss",
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

	// Wi-Fi congestion. A signal of exactly 0 dBm means the collector could
	// not read it, so the rule stays quiet rather than guessing.
	if n.InterfaceType == "wifi" && n.WifiSignalDBM != 0 && n.WifiBand == "2.4GHz" {
		hasIssue = true
		confidence := 60
		if n.WifiSignalDBM < -70 {
			confidence = 75
		}
		findings = append(findings, types.Finding{
			ID:           "wifi-congestion",
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

	// DNS slow (measured in-process by the collector)
	if n.DNSTimeMs > 100 {
		hasIssue = true
		findings = append(findings, types.Finding{
			ID:           "dns-slow",
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
			ID:           "network-healthy",
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
			ID:           "xid-errors",
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
			ID:           "llvmpipe-fallback",
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
	if report.Linux.SessionType == "wayland" && report.Linux.LoadedModules["nvidia"] {
		findings = append(findings, types.Finding{
			ID:           "wayland-nvidia-issue",
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

	return findings
}

// ── Windows Event Log (driver resets, nvlddmkm, WHEA) ─────────────────

// wheaClass separates corrected (recoverable, usually benign) hardware
// errors from uncorrected ones that crash or corrupt.
type wheaClass int

const (
	wheaCorrected wheaClass = iota
	wheaUncorrected
)

// classifyWHEA maps a WHEA-Logger event onto corrected/uncorrected.
//
//	17 = corrected PCIe error, 19 = corrected machine check, 47 = corrected memory error
//	18 = fatal machine check, 20 = fatal PCIe error, 41 = fatal (bugcheck), 46 = fatal memory error
//
// Unknown IDs fall back to the event level. An entry with neither ID nor
// level (older collectors) is treated as uncorrected so it is not silently
// downgraded to INFO.
func classifyWHEA(e types.EventLogEntry) wheaClass {
	switch e.EventID {
	case 17, 19, 47:
		return wheaCorrected
	case 18, 20, 41, 46:
		return wheaUncorrected
	}
	switch strings.ToLower(strings.TrimSpace(e.Level)) {
	case "error", "critical":
		return wheaUncorrected
	case "":
		if e.EventID == 0 {
			return wheaUncorrected
		}
	}
	return wheaCorrected
}

// wheaMentionsNVIDIA reports whether a WHEA message names the NVIDIA PCI
// vendor (VEN_10DE) or one of the GPUs' bus IDs.
func wheaMentionsNVIDIA(msg string, gpus []types.GPUInfo) bool {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "ven_10de") {
		return true
	}
	for _, gpu := range gpus {
		bus := strings.ToLower(strings.TrimSpace(gpu.PCIBusID))
		if bus == "" {
			continue
		}
		if strings.Contains(lower, bus) {
			return true
		}
		// nvidia-smi reports "00000000:01:00.0"; event logs often use "01:00.0".
		if idx := strings.Index(bus, ":"); idx >= 0 && len(bus) > idx+1 {
			if short := bus[idx+1:]; strings.Contains(lower, short) {
				return true
			}
		}
	}
	return false
}

// wheaExcerpt pulls the device/component lines out of a WHEA message so the
// user can see what failed without reading the whole event.
func wheaExcerpt(msg string) string {
	const maxLen = 200
	var parts []string
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line != "" && (strings.Contains(lower, "component") || strings.Contains(lower, "device") ||
			strings.Contains(lower, "bus:") || strings.Contains(lower, "vendor") || strings.Contains(lower, "error source")) {
			parts = append(parts, line)
		}
	}
	excerpt := strings.Join(parts, "; ")
	if excerpt == "" {
		excerpt = strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])
	}
	if excerpt == "" {
		excerpt = "(no message text)"
	}
	return truncateRunes(excerpt, maxLen)
}

func analyzeWHEA(report *types.Report, entries []types.EventLogEntry) []types.Finding {
	var findings []types.Finding
	if len(entries) == 0 {
		return findings
	}

	var corrected, uncorrected []types.EventLogEntry
	for _, e := range entries {
		if classifyWHEA(e) == wheaUncorrected {
			uncorrected = append(uncorrected, e)
		} else {
			corrected = append(corrected, e)
		}
	}

	if len(corrected) > 0 {
		sev := types.SeverityInfo
		confidence := 60
		nvidiaHit := false
		for _, e := range corrected {
			if wheaMentionsNVIDIA(e.Message, report.GPUs) {
				nvidiaHit = true
				break
			}
		}
		why := "Corrected errors were detected and fixed by the hardware (ECC, PCIe retry). They are usually benign unless they occur frequently or point at the GPU."
		if nvidiaHit {
			sev = types.SeverityWarn
			confidence = 75
			why = "These corrected errors name the NVIDIA GPU or its PCIe bus. Even when recovered, repeated PCIe errors on the GPU link point at seating, riser or slot problems."
		} else if len(corrected) >= 50 {
			sev = types.SeverityWarn
			confidence = 70
			why = "Corrected errors are individually harmless, but this many in 30 days suggests a marginal component that may eventually produce an uncorrected fault."
		}
		findings = append(findings, types.Finding{
			ID:       "whea-corrected",
			Severity: sev,
			Title:    "Corrected Hardware Errors Logged (WHEA)",
			Evidence: fmt.Sprintf("%d corrected WHEA event(s) in the last 30 days. Most recent: %s.",
				len(corrected), wheaExcerpt(corrected[0].Message)),
			WhyItMatters: why,
			NextSteps: []string{
				"Identify the device named in the event and update its driver and firmware.",
				"If the errors name the GPU, reseat it and check the PCIe slot and any riser.",
				"Monitor the count over time; a rising trend is worth investigating.",
			},
			Category:   "hardware",
			Confidence: confidence,
		})
	}

	if len(uncorrected) > 0 {
		sev := types.SeverityWarn
		confidence := 80
		if len(uncorrected) >= 3 {
			sev = types.SeverityCrit
			confidence = 90
		}
		findings = append(findings, types.Finding{
			ID:       "whea-errors",
			Severity: sev,
			Title:    "Uncorrected Hardware Errors (WHEA)",
			Evidence: fmt.Sprintf("%d uncorrected WHEA event(s) in the last 30 days. Most recent: %s.",
				len(uncorrected), wheaExcerpt(uncorrected[0].Message)),
			WhyItMatters: "Uncorrected WHEA errors are hardware faults Windows could not recover from. They cause bugchecks (BSOD), freezes and data corruption and can be CPU, memory or PCIe related.",
			NextSteps: []string{
				"Run Windows Memory Diagnostic (mdsched.exe) to test RAM.",
				"If CPU or GPU is overclocked, test at stock speeds.",
				"Check PCIe slot seating and power connections.",
				"Update motherboard BIOS/UEFI to latest version.",
			},
			Category:   "hardware",
			Confidence: confidence,
		})
	}

	return findings
}

// analyzeWindowsGaming covers the Windows event-log based rules. It runs in
// every mode that collects Windows info (gaming, streaming, creator, full).
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
			ID:       "driver-resets-4101",
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
			ID:           "nvlddmkm-errors",
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

	findings = append(findings, analyzeWHEA(report, w.WHEAErrors)...)

	// Recent updates correlation
	if len(w.RecentKBs) > 0 && len(w.DriverResetEvents) > 0 {
		findings = append(findings, types.Finding{
			ID:           "recent-windows-updates",
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

// powerPlanSuboptimal reports whether the active plan is one that may hold
// back performance. Unknown/unreadable plans never trigger a finding.
func powerPlanSuboptimal(plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	if p == "" || strings.HasPrefix(p, "unknown") || strings.HasPrefix(p, "default") {
		return false
	}
	return !strings.Contains(p, "high performance") && !strings.Contains(p, "ultimate")
}

// analyzeWindowsPerfSettings covers the performance-tuning INFO rules (power
// plan, HAGS). These only matter for interactive/gaming workloads, so they run
// in gaming, streaming and full but not creator.
func analyzeWindowsPerfSettings(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.Windows == nil {
		return findings
	}
	w := report.Windows

	if powerPlanSuboptimal(w.PowerPlan) {
		findings = append(findings, types.Finding{
			ID:           "power-plan-suboptimal",
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

	// HAGS: only an explicit "Enabled" counts. "Default (not configured)"
	// means the registry key is absent and Windows decides, so we say nothing.
	if w.HAGSEnabled == "Enabled" {
		findings = append(findings, types.Finding{
			ID:           "hags-enabled",
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

	// Game Mode: enabled is the Windows 11 default ("Enabled" or "Default (not
	// configured)"), so no finding is produced for any value. The remediation
	// engine still offers disable-game-mode for users who want to test it.

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
			ID:           "nvidia-app-detected",
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
			ID:           "overlay-software",
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
			ID:           "no-nvidia-gpu-encoding",
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
	if mods["nouveau"] {
		findings = append(findings, types.Finding{
			ID:           "nouveau-active",
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

	nvidiaLoaded := mods["nvidia"]

	if !nvidiaLoaded && report.Driver.Version == "" {
		findings = append(findings, types.Finding{
			ID:           "nvidia-module-not-loaded",
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
			ID:           "no-dev-nvidia",
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
			ID:           "libcuda-not-found",
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
			ID:           "dkms-failure",
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

	if report.Linux == nil || report.Linux.SecureBootState != "Enabled" {
		return findings
	}

	if !report.Linux.LoadedModules["nvidia"] {
		findings = append(findings, types.Finding{
			ID:           "secureboot-blocking",
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
			ID:           "secureboot-ok",
			Severity:     types.SeverityInfo,
			Title:        "Secure Boot Enabled — NVIDIA Module is Loading Successfully",
			Evidence:     "Secure Boot is enabled and the NVIDIA module is loaded. Module signing appears to be properly configured.",
			WhyItMatters: "This is the ideal configuration — security is maintained while NVIDIA drivers function correctly.",
			NextSteps:    []string{"No action needed."},
			Category:     "secureboot",
			Confidence:   95,
		})
	}

	return findings
}

// ── CUDA / AI Analysis ────────────────────────────────────────────────

// parseMajorMinor extracts (major, minor) from strings like "12.4", "12.4.1"
// or "13". Callers pass torch.version.cuda / nvcc-style versions, not the
// "cu118" wheel tags.
func parseMajorMinor(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, false
	}
	parts := strings.Split(v, ".")
	major, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	if len(parts) > 1 {
		// Tolerate trailing junk such as "4-rc1".
		digits := strings.TrimSpace(parts[1])
		for i, r := range digits {
			if r < '0' || r > '9' {
				digits = digits[:i]
				break
			}
		}
		if digits != "" {
			minor, _ = strconv.Atoi(digits)
		}
	}
	return major, minor, true
}

// cudaNewerThan reports whether CUDA version a is strictly newer than b,
// comparing major.minor. Unparseable input yields false (no finding).
func cudaNewerThan(a, b string) bool {
	am, an, ok := parseMajorMinor(a)
	if !ok {
		return false
	}
	bm, bn, ok := parseMajorMinor(b)
	if !ok {
		return false
	}
	if am != bm {
		return am > bm
	}
	return an > bn
}

// torchWheelTag turns a driver CUDA version like "12.4" into the PyTorch
// wheel index suffix "cu124". Returns "" when the version is unparseable.
func torchWheelTag(driverCUDA string) string {
	major, minor, ok := parseMajorMinor(driverCUDA)
	if !ok {
		return ""
	}
	return fmt.Sprintf("cu%d%d", major, minor)
}

// torchCUDANewerThanDriver is the actual cause of torch.cuda.is_available()
// returning False on an otherwise working machine: the wheel's CUDA runtime
// is newer than what the installed driver supports.
func torchCUDANewerThanDriver(report *types.Report) bool {
	if report.AI == nil || report.AI.PyTorchInfo == nil || report.AI.PyTorchInfo.Error != "" {
		return false
	}
	return cudaNewerThan(report.AI.PyTorchInfo.CUDAVersion, report.Driver.CUDAVersion)
}

func analyzeCUDA(report *types.Report) []types.Finding {
	var findings []types.Finding

	if report.AI == nil {
		return findings
	}

	// A driver whose CUDA runtime is NEWER than the toolkit is the normal,
	// supported case (drivers are backward compatible). Only the reverse
	// breaks: a toolkit newer than the driver fails at runtime.
	if cudaNewerThan(report.AI.CUDAToolkitVersion, report.Driver.CUDAVersion) {
		findings = append(findings, types.Finding{
			ID:       "cuda-mismatch",
			Severity: types.SeverityWarn,
			Title:    "CUDA Toolkit Newer Than Driver Supports",
			Evidence: fmt.Sprintf("CUDA Toolkit: %s. Driver CUDA runtime: %s.",
				report.AI.CUDAToolkitVersion, report.Driver.CUDAVersion),
			WhyItMatters: "The driver's CUDA runtime must be >= the toolkit version. Programs built with this toolkit will fail with 'CUDA driver version is insufficient for CUDA runtime version'.",
			NextSteps: []string{
				"Update the NVIDIA driver to one that supports CUDA " + report.AI.CUDAToolkitVersion + ".",
				"Or install a CUDA toolkit version no newer than " + report.Driver.CUDAVersion + ".",
				"Check compatibility at: https://docs.nvidia.com/cuda/cuda-toolkit-release-notes/",
			},
			Category:   "cuda",
			Confidence: 80,
		})
	}

	if torchCUDANewerThanDriver(report) {
		pt := report.AI.PyTorchInfo
		hint := "Reinstall a PyTorch wheel built for CUDA " + report.Driver.CUDAVersion + " or older from https://pytorch.org/get-started/locally/"
		if tag := torchWheelTag(report.Driver.CUDAVersion); tag != "" {
			hint = "Reinstall: pip install --force-reinstall torch torchvision torchaudio --index-url https://download.pytorch.org/whl/" + tag
		}
		findings = append(findings, types.Finding{
			ID:       "pytorch-cuda-newer-than-driver",
			Severity: types.SeverityWarn,
			Title:    "PyTorch CUDA Build Newer Than Driver",
			Evidence: fmt.Sprintf("PyTorch %s is built for CUDA %s but the driver only supports CUDA %s. torch.cuda.is_available(): %v.",
				pt.Version, pt.CUDAVersion, report.Driver.CUDAVersion, pt.CUDAAvailable),
			WhyItMatters: "PyTorch bundles its own CUDA runtime. When that runtime is newer than the driver supports, torch.cuda.is_available() returns False even though nvidia-smi works.",
			NextSteps: []string{
				hint,
				"Or update the NVIDIA driver to one that supports CUDA " + pt.CUDAVersion + ".",
			},
			Category:   "ai",
			Confidence: 80,
		})
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
			ID:           "pytorch-import-error",
			Severity:     types.SeverityWarn,
			Title:        "PyTorch Import Error",
			Evidence:     fmt.Sprintf("Error importing PyTorch: %s", pt.Error),
			WhyItMatters: "PyTorch could not be loaded, which will prevent GPU-accelerated training and inference.",
			NextSteps: []string{
				"Check your Python environment and PyTorch installation.",
				"Reinstall PyTorch following https://pytorch.org/get-started/locally/",
			},
			Category:   "ai",
			Confidence: 90,
		})
		return findings
	}

	switch {
	case pt.CUDAAvailable:
		findings = append(findings, types.Finding{
			ID:           "pytorch-cuda-ok",
			Severity:     types.SeverityInfo,
			Title:        "PyTorch CUDA is Working",
			Evidence:     fmt.Sprintf("PyTorch %s with CUDA %s. GPU: %s.", pt.Version, pt.CUDAVersion, pt.DeviceName),
			WhyItMatters: "GPU acceleration is available for PyTorch workloads.",
			NextSteps:    []string{"No action needed."},
			Category:     "ai",
			Confidence:   95,
		})
	case pt.CUDAVersion == "":
		findings = append(findings, types.Finding{
			ID:           "pytorch-cpu-only",
			Severity:     types.SeverityWarn,
			Title:        "PyTorch Installed Without CUDA Support",
			Evidence:     fmt.Sprintf("PyTorch %s is installed but torch.version.cuda is empty — this is a CPU-only build.", pt.Version),
			WhyItMatters: "A CPU-only PyTorch wheel was installed. torch.cuda.is_available() returns False because the CUDA runtime is not compiled in.",
			NextSteps: []string{
				"Uninstall the current PyTorch: pip uninstall torch torchvision torchaudio",
				"Reinstall with CUDA support from https://pytorch.org/get-started/locally/",
				"Make sure to select a CUDA version no newer than your driver's (" + valueOrUnknown(report.Driver.CUDAVersion) + ").",
			},
			Category:   "ai",
			Confidence: 95,
		})
	case torchCUDANewerThanDriver(report):
		// The specific cause is reported by analyzeCUDA; a second generic
		// "GPU not accessible" finding would only add noise.
	default:
		findings = append(findings, types.Finding{
			ID:           "pytorch-cuda-no-gpu",
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
			ID:           "tensorflow-import-error",
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
			ID:           "tensorflow-no-gpu",
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
			ID:           "tensorflow-gpu-ok",
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
			ID:           "wsl-no-dxg",
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
			ID:           "wsl-dxg-smi-fail",
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

// analyzeVRAM emits one aggregated finding (so the ID stays unique per
// report) listing every NVIDIA GPU with less than 4 GB of VRAM.
func analyzeVRAM(report *types.Report) []types.Finding {
	var findings []types.Finding

	var low []string
	for _, gpu := range report.GPUs {
		if gpu.IsNVIDIA && gpu.VRAMTotalMB > 0 && gpu.VRAMTotalMB < 4096 {
			low = append(low, fmt.Sprintf("%s (%d MB)", gpu.Name, gpu.VRAMTotalMB))
		}
	}
	if len(low) == 0 {
		return findings
	}

	findings = append(findings, types.Finding{
		ID:           "low-vram",
		Severity:     types.SeverityInfo,
		Title:        "Low VRAM Detected",
		Evidence:     fmt.Sprintf("GPU(s) with less than 4 GB VRAM: %s.", strings.Join(low, "; ")),
		WhyItMatters: "Less than 4 GB of VRAM may limit performance in modern games and prevent loading larger AI models.",
		NextSteps: []string{
			"For AI workloads: use smaller model variants, reduce batch sizes, or enable gradient checkpointing.",
			"For gaming: lower texture quality and resolution settings.",
		},
		Category:   "hardware",
		Confidence: 90,
	})

	return findings
}

// ── Ordering, summaries and helpers ───────────────────────────────────

var severityRank = map[types.Severity]int{
	types.SeverityCrit: 0,
	types.SeverityWarn: 1,
	types.SeverityInfo: 2,
}

// sortFindings orders CRIT before WARN before INFO, then by descending
// confidence, then by title. The sort is stable so equal findings keep
// their analyzer order, which keeps report diffs quiet between runs.
func sortFindings(findings []types.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		return a.Title < b.Title
	})
}

func isActionable(f types.Finding) bool {
	return f.Severity == types.SeverityCrit || f.Severity == types.SeverityWarn
}

func buildTopIssues(findings []types.Finding) []string {
	var issues []string
	for _, f := range findings {
		if !isActionable(f) {
			continue
		}
		conf := ""
		if f.Confidence > 0 {
			conf = fmt.Sprintf(" (%d%% confidence)", f.Confidence)
		}
		issues = append(issues, fmt.Sprintf("[%s] %s%s", f.Severity, f.Title, conf))
		if len(issues) >= 5 {
			break
		}
	}
	if len(issues) == 0 {
		issues = append(issues, "No significant issues detected.")
	}
	return issues
}

// maxNextSteps caps the consolidated step list so the report stays scannable.
const maxNextSteps = 5

// isPlaceholderStep recognises the "nothing to do" steps that healthy INFO
// findings carry. They belong under the finding itself, not in the
// consolidated action list, where they would displace a real step.
func isPlaceholderStep(lowerStep string) bool {
	return strings.HasPrefix(lowerStep, "no action needed") ||
		strings.HasPrefix(lowerStep, "no network action needed")
}

// buildNextSteps interleaves the findings' steps round-robin: the first step
// of every CRIT/WARN finding, then the second step of each, and so on. This
// way a single verbose finding cannot crowd out the others. INFO findings
// only contribute when there is nothing more serious to act on.
func buildNextSteps(findings []types.Finding) []string {
	var pool []types.Finding
	for _, f := range findings {
		if isActionable(f) {
			pool = append(pool, f)
		}
	}
	if len(pool) == 0 {
		for _, f := range findings {
			if f.Severity == types.SeverityInfo {
				pool = append(pool, f)
			}
		}
	}

	var steps []string
	seen := make(map[string]bool)
	for depth := 0; len(steps) < maxNextSteps; depth++ {
		progressed := false
		for _, f := range pool {
			if depth >= len(f.NextSteps) {
				continue
			}
			progressed = true
			step := strings.TrimSpace(f.NextSteps[depth])
			key := strings.ToLower(step)
			if step == "" || seen[key] || isPlaceholderStep(key) {
				continue
			}
			seen[key] = true
			steps = append(steps, step)
			if len(steps) >= maxNextSteps {
				break
			}
		}
		if !progressed {
			break
		}
	}

	if len(steps) == 0 {
		steps = append(steps, "No immediate action required. System appears healthy.")
	}
	return steps
}

// summaryLineWidth is the widest a summary line may be so the block fits in
// an 80-column terminal with indentation.
const summaryLineWidth = 72

// truncateRunes shortens s to at most n runes, appending "..." when cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

func valueOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

// pcieSummary renders the PCIe line for the summary block. "DOWNSHIFTED" is
// only shown when a PCIe WARN actually fired; an idle Gen1 link is labelled
// as such so users do not chase a non-problem.
func pcieSummary(report *types.Report) string {
	p := report.PCIe
	s := fmt.Sprintf("PCIe: %s %s", p.CurrentSpeed, p.CurrentWidth)
	warned := false
	for _, f := range report.Findings {
		if (f.ID == "pcie-downshift" || f.ID == "pcie-width-reduced") && f.Severity == types.SeverityWarn {
			warned = true
			break
		}
	}
	switch {
	case warned:
		s += fmt.Sprintf(" (DOWNSHIFTED, max %s %s)", p.MaxSpeed, p.MaxWidth)
	case parsePCIeGen(p.CurrentSpeed) > 0 && parsePCIeGen(p.CurrentSpeed) < parsePCIeGen(p.MaxSpeed):
		s += fmt.Sprintf(" (idle, max %s)", p.MaxSpeed)
	}
	return s
}

func buildSummaryBlock(report *types.Report) string {
	var lines []string
	add := func(format string, args ...interface{}) {
		lines = append(lines, truncateRunes(fmt.Sprintf(format, args...), summaryLineWidth))
	}

	add("NVCheckup v%s | %s", report.Metadata.ToolVersion, report.Metadata.Timestamp.Format("2006-01-02 15:04:05"))

	osLine := fmt.Sprintf("OS: %s %s", report.System.OSName, report.System.OSVersion)
	if report.System.KernelVersion != "" {
		osLine += fmt.Sprintf(" | Kernel: %s", report.System.KernelVersion)
	}
	add("%s | Arch: %s", osLine, report.System.Architecture)

	for _, gpu := range report.GPUs {
		if !gpu.IsNVIDIA {
			continue
		}
		line := fmt.Sprintf("GPU: %s | Driver: %s", gpu.Name, gpu.DriverVersion)
		if gpu.VRAMTotalMB > 0 {
			line += fmt.Sprintf(" | VRAM: %d MB", gpu.VRAMTotalMB)
		}
		add("%s", line)
	}

	if report.Driver.CUDAVersion != "" {
		line := fmt.Sprintf("CUDA (driver): %s", report.Driver.CUDAVersion)
		if report.AI != nil && report.AI.CUDAToolkitVersion != "" {
			line += fmt.Sprintf(" | CUDA Toolkit: %s", report.AI.CUDAToolkitVersion)
		}
		add("%s", line)
	}

	if report.AI != nil && report.AI.PyTorchInfo != nil {
		pt := report.AI.PyTorchInfo
		switch {
		case pt.Error != "":
			add("PyTorch: import failed")
		case pt.CUDAAvailable:
			add("PyTorch: %s (CUDA available)", pt.Version)
		default:
			add("PyTorch: %s (CUDA NOT available)", pt.Version)
		}
	}

	if report.Thermal != nil {
		line := fmt.Sprintf("Temp: %d°C", report.Thermal.TemperatureC)
		if report.Thermal.PowerState != "" {
			line += fmt.Sprintf(" | P-State: %s", report.Thermal.PowerState)
		}
		add("%s | Util: %d%%", line, report.Thermal.UtilizationPct)
	}

	if report.PCIe != nil {
		add("%s", pcieSummary(report))
	}

	critCount, warnCount, fixAvailable := 0, 0, 0
	var top []string
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
		if isActionable(f) && len(top) < 2 {
			top = append(top, f.Title)
		}
	}
	line := fmt.Sprintf("Findings: %d CRITICAL, %d WARNING, %d total", critCount, warnCount, len(report.Findings))
	if fixAvailable > 0 {
		line += fmt.Sprintf(" | %d auto-fixable", fixAvailable)
	}
	add("%s", line)
	if len(top) > 0 {
		add("Top: %s", strings.Join(top, "; "))
	}

	return strings.Join(lines, "\n") + "\n"
}
