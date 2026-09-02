package common

import (
	"regexp"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// GPUQueryFields is the exact --query-gpu field list used by CollectGPUInfo.
// Exported so self-test can verify the driver accepts it. The leading "index"
// lets each row be matched to the GPU nvidia-smi -L listed under that index.
const GPUQueryFields = "index,driver_version,pci.bus_id,memory.total,memory.free,memory.used,temperature.gpu,power.draw"

// GPUCapQueryFields is the separate, tolerant compute-capability query of
// docs/roadmap/spark-support.md section 4. It is deliberately not part of
// GPUQueryFields: older drivers answer 'Field "compute_cap" is not a valid
// field to query.' with exit 2, and folding it into the main list would
// discard every GPU row on those machines. Exported so self-test can run it.
const GPUCapQueryFields = "index,compute_cap"

// noDevicesFoundText is nvidia-smi's own message when the driver is loaded but
// sees no GPU (spec section 3.2 lists it as part of the GB10 GSP failure).
const noDevicesFoundText = "No devices were found"

// gb10NoDevicesExplanation is appended as a note on Linux when lspci still
// lists the GB10 [10de:2e12] while nvidia-smi reports "No devices were found":
// on DGX Spark that pairing is the driver/GSP initialisation failure signature
// of spec section 3.2, not a missing GPU.
const gb10NoDevicesExplanation = "lspci lists the DGX Spark GPU [10de:2e12] but nvidia-smi reports 'No devices were found': " +
	"on GB10 this is the driver/GSP firmware pairing failure (spec 3.2; look for 'Timeout after 6s of waiting for RPC response from GPU0 GSP!', " +
	"'SEC2 secure boot partition timed out' or 'RmInitAdapter failed!' in dmesg), not a missing GPU"

// tableMemoryNotSupported is the Memory-Usage cell nvidia-smi's table prints on
// unified-memory GPUs (spec 2.1: Memory-Usage "Not Supported").
const tableMemoryNotSupported = "Not Supported"

// gpuListRe matches one GPU line of "nvidia-smi -L":
//
//	GPU 0: NVIDIA GeForce RTX 4090 (UUID: GPU-xxxxx)
//
// MIG instance lines ("  MIG 1g.10gb Device 0: (UUID: MIG-...)") on H100/A100
// with MIG enabled do not start with "GPU N:" and are skipped on purpose.
var gpuListRe = regexp.MustCompile(`^GPU (\d+): (.+?)(?:\s*\(UUID:.*\))?$`)

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
	// nvidia-smi -L for GPU list. On an Optimus laptop with the dGPU powered
	// down or in a container without the GPU mapped this fails with a
	// recognisable message even though the driver itself is fine; surface
	// that text once instead of a bare exit status.
	r := util.RunCommand(timeout, "nvidia-smi", "-L")
	if r.Err != nil {
		e := nvidiaSmiQueryError("gpu.nvidia-smi-L", "-L", r)
		e.Fatal = false
		*errs = append(*errs, e)
		return
	}
	if _, _, known := describeNvidiaSmiFailure(r.Stdout + "\n" + r.Stderr); known {
		e := nvidiaSmiQueryError("gpu.nvidia-smi-L", "-L", r)
		e.Fatal = false
		*errs = append(*errs, e)
		return
	}
	*gpus = append(*gpus, parseGPUList(r.Stdout)...)

	// nvidia-smi summary for driver version, bus id, memory
	r = util.RunCommand(timeout, "nvidia-smi",
		"--query-gpu="+GPUQueryFields,
		"--format=csv,noheader,nounits")
	if r.Err == nil {
		applyGPUQueryRows(*gpus, driver, r.Stdout)
	} else {
		e := nvidiaSmiQueryError("gpu.query", "GPU query", r)
		e.Fatal = false
		*errs = append(*errs, e)
	}

	// Compute capability is a separate query so that a driver rejecting the
	// field (older releases) costs a note, never the GPU rows above.
	r = util.RunCommand(timeout, "nvidia-smi", "--query-gpu="+GPUCapQueryFields, "--format=csv,noheader")
	if r.Err == nil {
		applyGPUCapRows(*gpus, r.Stdout)
	} else {
		*errs = append(*errs, types.CollectorError{
			Collector: "gpu.compute_cap",
			Error:     "nvidia-smi compute_cap query not accepted by this driver (compute capability unavailable): " + commandFailureDetail(r),
		})
	}

	// Get CUDA version from nvidia-smi header
	r = util.RunCommand(timeout, "nvidia-smi")
	if r.Err == nil {
		// The Processes section lists every program using the GPU by name,
		// which is private information; keep only the GPU table above it.
		driver.NvidiaSmiOutput = stripProcessSection(r.Stdout)
		cudaRe := regexp.MustCompile(`CUDA Version:\s*([\d.]+)`)
		if m := cudaRe.FindStringSubmatch(r.Stdout); m != nil {
			driver.CUDAVersion = m[1]
		}
		applyTableMemoryReporting(*gpus, r.Stdout)
	}
}

// applyGPUCapRows fills GPUInfo.ComputeCap from "index, compute_cap" rows.
// "[N/A]" (some virtual GPUs) leaves the field empty.
func applyGPUCapRows(gpus []types.GPUInfo, out string) {
	rows, _ := csvRows(out)
	for i, row := range rows {
		idx, fields := parseRowIndex(splitCSV(row), i)
		if len(fields) == 0 {
			continue
		}
		g := gpuByIndex(gpus, idx)
		if g == nil || isNotAvailable(fields[0]) || fields[0] == "" {
			continue
		}
		g.ComputeCap = fields[0]
	}
}

// applyTableMemoryReporting marks GPUs whose memory query never answered as
// not-supported when the nvidia-smi table shows "Not Supported" in the
// Memory-Usage column (spec 3.1 flag rule B accepts either signal). GPUs that
// already have a MemoryReporting value keep it.
func applyTableMemoryReporting(gpus []types.GPUInfo, table string) {
	if !tableShowsMemoryNotSupported(table) {
		return
	}
	for i := range gpus {
		if gpus[i].IsNVIDIA && gpus[i].MemoryReporting == "" {
			gpus[i].MemoryReporting = MemoryReportingNotSupported
		}
	}
}

// tableShowsMemoryNotSupported reports whether a GPU status row of the
// nvidia-smi table prints "Not Supported" in the Memory-Usage cell, where
// "usedMiB / totalMiB" belongs (spec 2.1). Only the middle cell (index 2 after
// splitting on '|') of the second row of each GPU block is inspected - that row
// is recognised by the Pwr:Usage/Cap "x / y" in its first cell - so a "Not
// Supported" in the Volatile Uncorr. ECC column of the first row, header lines
// and the Processes section cannot trigger it.
func tableShowsMemoryNotSupported(table string) bool {
	for _, l := range strings.Split(table, "\n") {
		if strings.Contains(l, "Processes:") {
			return false
		}
		if !strings.HasPrefix(strings.TrimSpace(l), "|") || strings.Contains(l, "Memory-Usage") {
			continue
		}
		cells := strings.Split(l, "|")
		if len(cells) < 4 {
			continue
		}
		// Status row: first cell holds "Fan Temp Perf Pwr:Usage/Cap" with a '/'.
		if !strings.Contains(cells[1], "/") {
			continue
		}
		mem := cells[2]
		if strings.Contains(mem, tableMemoryNotSupported) && !strings.Contains(mem, "MiB") {
			return true
		}
	}
	return false
}

// parseGPUList parses "nvidia-smi -L" output into one GPUInfo per GPU line.
// It is a pure function so it can be unit-tested with captured output.
func parseGPUList(out string) []types.GPUInfo {
	var gpus []types.GPUInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := gpuListRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		gpus = append(gpus, types.GPUInfo{
			Index:    int(parseIntSafe(m[1])),
			Name:     strings.TrimSpace(m[2]),
			Vendor:   "NVIDIA",
			IsNVIDIA: true,
		})
	}
	return gpus
}

// applyGPUQueryRows fills driver version, bus id, memory, temperature and
// power from the CSV rows produced by GPUQueryFields. Rows are matched to
// GPUs by the leading index field (falling back to row order), so a rig
// whose -L order and query order differ still lands each row on the right
// GPU. "[N/A]" fields (MIG, some virtual GPUs) are left at their zero value.
func applyGPUQueryRows(gpus []types.GPUInfo, driver *types.DriverInfo, out string) {
	rows, _ := csvRows(out)
	for i, row := range rows {
		idx, fields := parseRowIndex(splitCSV(row), i)
		get := func(n int) string {
			if n < len(fields) {
				return fields[n]
			}
			return ""
		}
		version := get(0)
		if isNotAvailable(version) {
			version = ""
		}
		if driver.Version == "" && version != "" {
			driver.Version = version
		}
		g := gpuByIndex(gpus, idx)
		if g == nil {
			continue
		}
		if version != "" {
			g.DriverVersion = version
		}
		if s := get(1); s != "" && !isNotAvailable(s) {
			g.PCIBusID = s
		}
		// memory.total decides MemoryReporting (spec 3.1 flag rule B): a
		// number is dedicated VRAM, "[N/A]" is a unified-memory GPU whose
		// memory NVML cannot report (GB10: "[N/A], [N/A], [N/A]").
		if s := get(2); s != "" {
			if isNotAvailable(s) {
				g.MemoryReporting = MemoryReportingNotSupported
			} else {
				g.VRAMTotalMB = parseIntSafe(s)
				g.MemoryReporting = MemoryReportingDedicated
			}
		}
		if s := get(3); s != "" && !isNotAvailable(s) {
			g.VRAMFreeMB = parseIntSafe(s)
		}
		if s := get(4); s != "" && !isNotAvailable(s) {
			g.VRAMUsedMB = parseIntSafe(s)
		}
		if s := get(5); s != "" && !isNotAvailable(s) {
			g.Temperature = int(parseIntSafe(s))
		}
		if s := get(6); s != "" && !isNotAvailable(s) {
			g.PowerDraw = s
		}
	}
}

// gpuByIndex returns the NVIDIA GPU with the given nvidia-smi index, or nil.
func gpuByIndex(gpus []types.GPUInfo, idx int) *types.GPUInfo {
	for i := range gpus {
		if gpus[i].IsNVIDIA && gpus[i].Index == idx {
			return &gpus[i]
		}
	}
	return nil
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

	*gpus = append(*gpus, parseWMIVideoControllers(r.Stdout, *gpus)...)

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

var pnpPCIRe = regexp.MustCompile(`VEN_([0-9A-Fa-f]+)&DEV_([0-9A-Fa-f]+)`)

// parseWMIVideoControllers parses the "Name|DriverVersion|AdapterRAM|PNPDeviceID"
// lines printed by the Win32_VideoController query. Adapters whose name was
// already reported by nvidia-smi are skipped so the NVIDIA dGPU of a hybrid
// laptop is not listed twice; the iGPU (Intel/AMD) is appended after them.
func parseWMIVideoControllers(out string, existing []types.GPUInfo) []types.GPUInfo {
	// Names already reported by nvidia-smi are skipped so the same card is not
	// listed twice. WMI rows themselves are de-duplicated by PNPDeviceID, never
	// by name: a rig with two identical cards (2x RTX 4090, 4x A6000) emits two
	// Win32_VideoController instances with the same Name, and both are real.
	existingNames := make(map[string]bool)
	for _, g := range existing {
		existingNames[g.Name] = true
	}
	seenPNP := make(map[string]bool)

	var added []types.GPUInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || existingNames[name] {
			continue // Already have from nvidia-smi
		}
		if len(parts) >= 4 {
			pnp := strings.ToUpper(strings.TrimSpace(parts[3]))
			if pnp != "" {
				if seenPNP[pnp] {
					continue // WMI listed the same physical device twice
				}
				seenPNP[pnp] = true
			}
		}

		gpu := types.GPUInfo{
			Index:         len(existing) + len(added),
			Name:          name,
			DriverVersion: strings.TrimSpace(parts[1]),
			Vendor:        vendorFromName(name),
		}
		gpu.IsNVIDIA = gpu.Vendor == "NVIDIA"

		// Parse PCI IDs from PNP Device ID
		if len(parts) >= 4 {
			if m := pnpPCIRe.FindStringSubmatch(parts[3]); m != nil {
				gpu.PCIVendorID = m[1]
				gpu.PCIDeviceID = m[2]
			}
		}

		added = append(added, gpu)
	}
	return added
}

// vendorFromName classifies a display adapter by its marketing name.
func vendorFromName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "nvidia"):
		return "NVIDIA"
	case strings.Contains(lower, "intel"):
		return "Intel"
	case strings.Contains(lower, "amd"), strings.Contains(lower, "radeon"):
		return "AMD"
	default:
		return "Unknown"
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

	*gpus = append(*gpus, parseLspciGPUs(r.Stdout, *gpus)...)

	// DGX Spark whose driver/GSP pairing failed: nvidia-smi says "No devices
	// were found" while the GB10 is still on the bus. Explain it once so the
	// note is not read as "no GPU installed" (spec 3.2).
	if LspciHasGB10(r.Stdout) && errorsMention(*errs, noDevicesFoundText) {
		*errs = append(*errs, types.CollectorError{Collector: "gpu.gb10", Error: gb10NoDevicesExplanation})
	}
}

// errorsMention reports whether any collector error text contains needle.
func errorsMention(errs []types.CollectorError, needle string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error, needle) {
			return true
		}
	}
	return false
}

var lspciGPURe = regexp.MustCompile(`^([0-9a-f:.]+)\s+(?:VGA|3D|Display).*?:\s+(.+?)\s*\[([0-9a-f]{4}):([0-9a-f]{4})\]`)

// parseLspciGPUs parses "lspci -nn" output for VGA/3D/Display devices. Devices
// whose bus id nvidia-smi already reported are skipped. nvidia-smi prints bus
// ids as "00000000:01:00.0" while lspci prints "01:00.0", so the comparison
// is on the domain-less form.
func parseLspciGPUs(out string, existing []types.GPUInfo) []types.GPUInfo {
	existingBusIDs := make(map[string]bool)
	for _, g := range existing {
		if g.PCIBusID != "" {
			existingBusIDs[shortBusID(g.PCIBusID)] = true
		}
	}

	var added []types.GPUInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := lspciGPURe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		busID := m[1]
		name := m[2]
		vendorID := m[3]
		deviceID := m[4]

		if existingBusIDs[shortBusID(busID)] {
			continue
		}
		existingBusIDs[shortBusID(busID)] = true

		gpu := types.GPUInfo{
			Index:       len(existing) + len(added),
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

		added = append(added, gpu)
	}
	return added
}

// shortBusID normalises a PCI bus id to lower-case "bb:dd.f" by dropping the
// domain prefix nvidia-smi adds ("00000000:01:00.0" -> "01:00.0").
func shortBusID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if parts := strings.Split(id, ":"); len(parts) == 3 {
		return parts[1] + ":" + parts[2]
	}
	return id
}

// processSectionOmittedNote replaces the Processes table in stored nvidia-smi output.
const processSectionOmittedNote = "(Processes section omitted: process names are private)"

// stripProcessSection returns the nvidia-smi table with everything from the
// "Processes:" section onward removed, including the box border that opens
// that section. Output without a Processes section is returned unchanged.
func stripProcessSection(output string) string {
	lines := strings.Split(output, "\n")
	cut := -1
	for i, l := range lines {
		if strings.Contains(l, "Processes:") {
			cut = i
			break
		}
	}
	if cut < 0 {
		return output
	}
	kept := lines[:cut]
	// The Processes box opens with a "+----+" border on the line before the
	// header; drop it (and any blank lines) so the kept table ends cleanly.
	kept = trimTrailingBlank(kept)
	if len(kept) >= 2 && isTableBorder(kept[len(kept)-1]) && strings.TrimSpace(kept[len(kept)-2]) == "" {
		kept = trimTrailingBlank(kept[:len(kept)-1])
	}
	return strings.Join(kept, "\n") + "\n" + processSectionOmittedNote
}

// trimTrailingBlank drops trailing whitespace-only lines.
func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// isTableBorder reports whether a line is an nvidia-smi box border like "+-----+".
func isTableBorder(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 2 || t[0] != '+' {
		return false
	}
	return strings.Trim(t, "+-=") == ""
}
