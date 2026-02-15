package common

import (
	"regexp"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectGPUInfo gathers GPU and NVIDIA driver information.
func CollectGPUInfo(timeout int) ([]types.GPUInfo, types.DriverInfo, []types.CollectorError) {
	var gpus []types.GPUInfo
	var driver types.DriverInfo
	var errs []types.CollectorError

	// Try nvidia-smi first (cross-platform)
	if util.CommandExists("nvidia-smi") {
		driver.NvidiaSmiPath = "nvidia-smi"
		collectFromNvidiaSmi(&gpus, &driver, &errs, timeout)
	} else {
		errs = append(errs, types.CollectorError{
			Collector: "gpu.nvidia-smi",
			Error:     "nvidia-smi not found in PATH; NVIDIA driver may not be installed",
		})
	}

	// Platform-specific GPU enumeration
	if util.IsWindows() {
		collectGPUsWindows(&gpus, &driver, &errs, timeout)
	} else if util.IsLinux() {
		collectGPUsLinux(&gpus, &errs, timeout)
	}

	return gpus, driver, errs
}

func collectFromNvidiaSmi(gpus *[]types.GPUInfo, driver *types.DriverInfo, errs *[]types.CollectorError, timeout int) {
	// nvidia-smi -L for GPU list
	r := util.RunCommand(timeout, "nvidia-smi", "-L")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "gpu.nvidia-smi-L",
			Error:     "nvidia-smi -L failed: " + r.Err.Error(),
		})
		return
	}

	// Parse GPU list: "GPU 0: NVIDIA GeForce RTX 4090 (UUID: GPU-xxxxx)"
	gpuRe := regexp.MustCompile(`GPU (\d+): (.+?)(?:\s*\(UUID:.*\))?$`)
	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if m := gpuRe.FindStringSubmatch(line); m != nil {
			idx := int(parseIntSafe(m[1]))
			gpu := types.GPUInfo{
				Index:    idx,
				Name:     strings.TrimSpace(m[2]),
				Vendor:   "NVIDIA",
				IsNVIDIA: true,
			}
			*gpus = append(*gpus, gpu)
		}
	}

	// nvidia-smi summary for driver version, CUDA version, memory
	r = util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu=driver_version,pci.bus_id,memory.total,memory.free,memory.used,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits")
	if r.Err == nil {
		lines := strings.Split(r.Stdout, "\n")
		for i, line := range lines {
			fields := strings.Split(line, ", ")
			if len(fields) >= 2 {
				if i == 0 && len(fields) >= 1 {
					driver.Version = strings.TrimSpace(fields[0])
				}
				if i < len(*gpus) {
					if len(fields) >= 2 {
						(*gpus)[i].PCIBusID = strings.TrimSpace(fields[1])
						(*gpus)[i].DriverVersion = driver.Version
					}
					if len(fields) >= 3 {
						(*gpus)[i].VRAMTotalMB = parseIntSafe(fields[2])
					}
					if len(fields) >= 4 {
						(*gpus)[i].VRAMFreeMB = parseIntSafe(fields[3])
					}
					if len(fields) >= 5 {
						(*gpus)[i].VRAMUsedMB = parseIntSafe(fields[4])
					}
					if len(fields) >= 6 {
						(*gpus)[i].Temperature = int(parseIntSafe(fields[5]))
					}
					if len(fields) >= 7 {
						(*gpus)[i].PowerDraw = strings.TrimSpace(fields[6])
					}
				}
			}
		}
	}

	// Get CUDA version from nvidia-smi header
	r = util.RunCommand(timeout, "nvidia-smi")
	if r.Err == nil {
		driver.NvidiaSmiOutput = r.Stdout
		cudaRe := regexp.MustCompile(`CUDA Version:\s*([\d.]+)`)
		if m := cudaRe.FindStringSubmatch(r.Stdout); m != nil {
			driver.CUDAVersion = m[1]
		}
	}
}

func collectGPUsWindows(gpus *[]types.GPUInfo, driver *types.DriverInfo, errs *[]types.CollectorError, timeout int) {
	// Use WMI to enumerate all display adapters (includes iGPU)
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.Name)|$($_.DriverVersion)|$($_.AdapterRAM)|$($_.PNPDeviceID)" }`)
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "gpu.wmi",
			Error:     "WMI GPU enumeration failed: " + r.Err.Error(),
		})
		return
	}

	existingNames := make(map[string]bool)
	for _, g := range *gpus {
		existingNames[g.Name] = true
	}

	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if existingNames[name] {
			continue // Already have from nvidia-smi
		}

		gpu := types.GPUInfo{
			Index:         len(*gpus),
			Name:          name,
			DriverVersion: strings.TrimSpace(parts[1]),
		}

		if strings.Contains(strings.ToLower(name), "nvidia") {
			gpu.Vendor = "NVIDIA"
			gpu.IsNVIDIA = true
		} else if strings.Contains(strings.ToLower(name), "intel") {
			gpu.Vendor = "Intel"
		} else if strings.Contains(strings.ToLower(name), "amd") || strings.Contains(strings.ToLower(name), "radeon") {
			gpu.Vendor = "AMD"
		} else {
			gpu.Vendor = "Unknown"
		}

		// Parse PCI IDs from PNP Device ID
		if len(parts) >= 4 {
			pnp := parts[3]
			pciRe := regexp.MustCompile(`VEN_([0-9A-Fa-f]+)&DEV_([0-9A-Fa-f]+)`)
			if m := pciRe.FindStringSubmatch(pnp); m != nil {
				gpu.PCIVendorID = m[1]
				gpu.PCIDeviceID = m[2]
			}
		}

		*gpus = append(*gpus, gpu)
	}

	// Try to get WDDM version
	r = util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\DirectX").Version`)
	if r.Err == nil && r.Stdout != "" {
		for i := range *gpus {
			if (*gpus)[i].IsNVIDIA {
				(*gpus)[i].WDDMVersion = strings.TrimSpace(r.Stdout)
			}
		}
	}
}

func collectGPUsLinux(gpus *[]types.GPUInfo, errs *[]types.CollectorError, timeout int) {
	// Use lspci for GPU enumeration if available
	if !util.CommandExists("lspci") {
		return
	}

	r := util.RunCommand(timeout, "lspci", "-nn")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "gpu.lspci",
			Error:     r.Err.Error(),
		})
		return
	}

	existingBusIDs := make(map[string]bool)
	for _, g := range *gpus {
		existingBusIDs[g.PCIBusID] = true
	}

	vgaRe := regexp.MustCompile(`^([0-9a-f:.]+)\s+(?:VGA|3D|Display).*?:\s+(.+?)\s*\[([0-9a-f]{4}):([0-9a-f]{4})\]`)
	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		m := vgaRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		busID := m[1]
		name := m[2]
		vendorID := m[3]
		deviceID := m[4]

		if existingBusIDs[busID] {
			continue
		}

		gpu := types.GPUInfo{
			Index:       len(*gpus),
			Name:        name,
			PCIBusID:    busID,
			PCIVendorID: vendorID,
			PCIDeviceID: deviceID,
		}

		switch strings.ToLower(vendorID) {
		case "10de":
			gpu.Vendor = "NVIDIA"
			gpu.IsNVIDIA = true
		case "8086":
			gpu.Vendor = "Intel"
		case "1002":
			gpu.Vendor = "AMD"
		default:
			gpu.Vendor = "Unknown"
		}

		*gpus = append(*gpus, gpu)
	}
}
