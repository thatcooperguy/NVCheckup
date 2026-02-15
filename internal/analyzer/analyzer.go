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

	// Mode-specific analysis
	switch mode {
	case types.ModeGaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeOverlays(report)...)
	case types.ModeStreaming:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeOverlays(report)...)
		findings = append(findings, analyzeStreaming(report)...)
	case types.ModeAI:
		findings = append(findings, analyzeLinuxModules(report)...)
		findings = append(findings, analyzeSecureBoot(report)...)
		findings = append(findings, analyzeCUDA(report)...)
		findings = append(findings, analyzePyTorch(report)...)
		findings = append(findings, analyzeTensorFlow(report)...)
	case types.ModeCreator:
		findings = append(findings, analyzeWindowsGaming(report)...)
		findings = append(findings, analyzeCUDA(report)...)
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
			Category: "gpu",
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
				Category: "gpu",
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
			WhyItMatters: "Without a working NVIDIA driver, GPU acceleration (gaming, CUDA, NVENC) will not function.",
			NextSteps: []string{
				"Install the NVIDIA driver from https://www.nvidia.com/drivers or your Linux distribution's package manager.",
				"On Linux: Check if the nvidia kernel module is loaded with 'lsmod | grep nvidia'.",
				"After install, reboot and run NVCheckup again.",
			},
			Category: "driver",
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
			Category: "driver",
		})
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
		if count >= 3 {
			sev = types.SeverityCrit
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
			Category: "driver",
		})
	}

	// nvlddmkm errors
	if len(w.NvlddmkmErrors) > 0 {
		count := len(w.NvlddmkmErrors)
		sev := types.SeverityWarn
		if count >= 5 {
			sev = types.SeverityCrit
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
			Category: "driver",
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
			Category: "hardware",
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
			Category: "performance",
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
			Category: "performance",
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
			Category: "updates",
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
			Category: "overlay",
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
			Category: "overlay",
		})
	}

	return findings
}

// ── Streaming / NVENC ─────────────────────────────────────────────────

func analyzeStreaming(report *types.Report) []types.Finding {
	var findings []types.Finding

	// Check for NVENC capability (basic: is NVIDIA GPU present with driver)
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
			Title:        "No NVIDIA GPU Available for NVENC",
			Evidence:     "No NVIDIA GPU detected — NVENC hardware encoding is not available.",
			WhyItMatters: "NVENC is NVIDIA's hardware video encoder used by OBS, Shadowplay, and other streaming/recording tools. Without an NVIDIA GPU, software encoding must be used instead.",
			NextSteps: []string{
				"Ensure the NVIDIA GPU is properly installed and detected.",
				"Install the NVIDIA driver.",
			},
			Category: "streaming",
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
			WhyItMatters: "Nouveau does not support CUDA, NVENC, or Vulkan performance comparable to the NVIDIA driver. GPU acceleration will be severely limited.",
			NextSteps: []string{
				"Install the proprietary NVIDIA driver for your distribution.",
				"Blacklist nouveau: add 'blacklist nouveau' and 'options nouveau modeset=0' to /etc/modprobe.d/blacklist-nouveau.conf.",
				"Rebuild initramfs and reboot.",
				"Debian/Ubuntu: sudo apt install nvidia-driver-XXX",
				"Fedora: sudo dnf install akmod-nvidia",
				"Arch: sudo pacman -S nvidia",
			},
			Category: "driver",
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
			WhyItMatters: "Without the NVIDIA kernel module, the GPU cannot be used for any accelerated workload (display, CUDA, NVENC).",
			NextSteps: []string{
				"Check if the module exists: modinfo nvidia",
				"Try loading manually: sudo modprobe nvidia",
				"Check dmesg for load errors: dmesg | grep -i nvidia",
				"If Secure Boot is enabled, the module may need to be signed (see Secure Boot finding).",
				"If using DKMS, check dkms status for build failures.",
			},
			Category: "driver",
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
			Category: "driver",
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
			Category: "cuda",
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
			Category: "driver",
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
		// Check if nvidia module is actually loading
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
					"Option A (Recommended): Sign the NVIDIA module and enroll the key with MOK (Machine Owner Key).",
					"  - Generate a signing key: openssl req -new -x509 -newkey rsa:2048 -keyout MOK.priv -outform DER -out MOK.der -nodes -days 36500 -subj '/CN=NVIDIA Module Signing/'",
					"  - Enroll: sudo mokutil --import MOK.der (reboot and confirm in UEFI)",
					"  - Sign: sudo /usr/src/linux-headers-$(uname -r)/scripts/sign-file sha256 MOK.priv MOK.der /path/to/nvidia.ko",
					"Option B: Disable Secure Boot in BIOS/UEFI (reduces system security — understand the tradeoff).",
					"Some distributions (Ubuntu) handle signing automatically with DKMS — check if a MOK enrollment prompt appeared during driver install.",
				},
				Category: "secureboot",
			})
		} else {
			findings = append(findings, types.Finding{
				Severity:     types.SeverityInfo,
				Title:        "Secure Boot Enabled — NVIDIA Module is Loading Successfully",
				Evidence:     "Secure Boot is enabled and the NVIDIA module is loaded. Module signing appears to be properly configured.",
				WhyItMatters: "This is the ideal configuration — security is maintained while NVIDIA drivers function correctly.",
				NextSteps:    []string{"No action needed."},
				Category:     "secureboot",
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
				Category: "cuda",
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
			Category: "ai",
		})
		return findings
	}

	if !pt.CUDAAvailable {
		if pt.CUDAVersion == "" {
			findings = append(findings, types.Finding{
				Severity: types.SeverityWarn,
				Title:    "PyTorch Installed Without CUDA Support",
				Evidence: fmt.Sprintf("PyTorch %s is installed but torch.version.cuda is empty — this is a CPU-only build.", pt.Version),
				WhyItMatters: "A CPU-only PyTorch wheel was installed. torch.cuda.is_available() returns False because the CUDA runtime is not compiled in.",
				NextSteps: []string{
					"Uninstall the current PyTorch: pip uninstall torch torchvision torchaudio",
					"Reinstall with CUDA support from https://pytorch.org/get-started/locally/",
					"Example: pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu121",
					"Make sure to select the correct CUDA version matching your driver.",
				},
				Category: "ai",
			})
		} else {
			findings = append(findings, types.Finding{
				Severity: types.SeverityWarn,
				Title:    "PyTorch CUDA Available but GPU Not Accessible",
				Evidence: fmt.Sprintf("PyTorch %s has CUDA %s compiled in, but torch.cuda.is_available() is False.", pt.Version, pt.CUDAVersion),
				WhyItMatters: "PyTorch was built with CUDA support but cannot access the GPU. This usually indicates a driver issue or environment mismatch.",
				NextSteps: []string{
					"Ensure nvidia-smi works and shows your GPU.",
					"Check that the NVIDIA driver version supports CUDA " + pt.CUDAVersion + ".",
					"If using conda, ensure you're in the correct environment.",
					"Check LD_LIBRARY_PATH (Linux) or PATH (Windows) includes CUDA libraries.",
				},
				Category: "ai",
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
			Category: "ai",
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
			Category: "ai",
		})
	} else {
		findings = append(findings, types.Finding{
			Severity:     types.SeverityInfo,
			Title:        "TensorFlow GPU is Working",
			Evidence:     fmt.Sprintf("TensorFlow %s detected %d GPU(s): %s.", tf.Version, len(tf.GPUs), strings.Join(tf.GPUs, ", ")),
			WhyItMatters: "GPU acceleration is available for TensorFlow workloads.",
			NextSteps:    []string{"No action needed."},
			Category:     "ai",
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
			Category: "wsl",
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
			Category: "wsl",
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
				Severity: types.SeverityInfo,
				Title:    fmt.Sprintf("Low VRAM Detected: %s (%d MB)", gpu.Name, gpu.VRAMTotalMB),
				Evidence: fmt.Sprintf("GPU %s has %d MB of VRAM.", gpu.Name, gpu.VRAMTotalMB),
				WhyItMatters: "Less than 4 GB of VRAM may limit performance in modern games and prevent loading larger AI models.",
				NextSteps: []string{
					"For AI workloads: use smaller model variants, reduce batch sizes, or enable gradient checkpointing.",
					"For gaming: lower texture quality and resolution settings.",
				},
				Category: "hardware",
			})
		}
	}

	return findings
}

// ── Helpers ───────────────────────────────────────────────────────────

func sortFindings(findings []types.Finding) {
	// Simple bubble sort by severity (CRIT > WARN > INFO)
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
			issues = append(issues, fmt.Sprintf("[%s] %s", f.Severity, f.Title))
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

	critCount, warnCount := 0, 0
	for _, f := range report.Findings {
		switch f.Severity {
		case types.SeverityCrit:
			critCount++
		case types.SeverityWarn:
			warnCount++
		}
	}
	sb.WriteString(fmt.Sprintf("Findings: %d CRITICAL, %d WARNING, %d total\n", critCount, warnCount, len(report.Findings)))

	return sb.String()
}

func majorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
