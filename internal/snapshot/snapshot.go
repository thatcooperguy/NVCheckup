// Package snapshot creates and compares timestamped system snapshots.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/collector/ai"
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	linuxCollector "github.com/thatcooperguy/nvcheckup/internal/collector/linux"
	"github.com/thatcooperguy/nvcheckup/internal/redact"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Create generates a timestamped, redacted JSON snapshot of the current
// system state. It is equivalent to CreateWithOptions(outDir, timeout, true).
func Create(outDir string, timeout int) (string, error) {
	return CreateWithOptions(outDir, timeout, true)
}

// CreateWithOptions generates a timestamped JSON snapshot. When redactEnabled
// is true the hostname, username and home-directory paths are replaced with
// the standard redaction tokens before the file is written, so a snapshot is
// safe to attach to a forum post by default.
func CreateWithOptions(outDir string, timeout int, redactEnabled bool) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create output directory: %w", err)
	}

	snap := collect(timeout, redactEnabled)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal snapshot: %w", err)
	}

	filename := fmt.Sprintf("nvcheckup-snapshot-%s.json", snap.Metadata.Timestamp.Format("20060102-150405"))
	path := filepath.Join(outDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("cannot write snapshot: %w", err)
	}
	return path, nil
}

// collect runs the snapshot collectors and applies redaction. Collector errors
// are deliberately dropped: a snapshot is a best-effort record for later
// comparison, not a diagnostic report.
func collect(timeout int, redactEnabled bool) types.Snapshot {
	start := time.Now()
	snap := types.Snapshot{
		Metadata: types.ReportMetadata{
			ToolVersion:      types.Version,
			Timestamp:        start,
			Mode:             types.ModeFull,
			Platform:         runtime.GOOS,
			RedactionEnabled: redactEnabled,
			SchemaVersion:    types.SchemaVersion,
		},
	}

	sysInfo, _ := common.CollectSystemInfo(timeout)
	snap.System = sysInfo

	gpus, driver, _ := common.CollectGPUInfo(timeout)
	snap.GPUs = gpus
	snap.Driver = driver

	// Platform class and the unified-memory / on-package flags, derived the
	// same way the run pipeline does (spec 3.1: phase-1 rows, then the
	// GPU-dependent rows and flag rules over the inventory above).
	platform, _ := common.DetectPlatform(timeout)
	tmp := &types.Report{System: sysInfo, GPUs: gpus, Platform: platform}
	tmp.Metadata.Platform = runtime.GOOS
	common.ApplyPlatformFlags(tmp)
	snap.GPUs = tmp.GPUs
	snap.Platform = &tmp.Platform
	if tmp.Platform.UnifiedMemory {
		um, _ := common.CollectUnifiedMemory(timeout)
		snap.UnifiedMemory = &um
	}
	if tmp.Platform.Class == common.ClassDGXSpark {
		collectDGXSpark(timeout, &snap, &tmp.Platform)
	}

	aiInfo, _ := ai.CollectAIInfo(timeout)
	snap.AI = &aiInfo

	snap.Metadata.RuntimeSeconds = time.Since(start).Seconds()

	redact.ApplyToSnapshot(&snap, redact.New(redactEnabled))
	return snap
}

// The DGX OS collectors are untagged pure-Go parsers shared with the runner
// (which reaches them through the linux build tag); calling them here without
// a runtime.GOOS gate is intentional so the parser-only tests run on every OS,
// and dgx-spark is never classified off Linux.
// collectDGXSpark fills the DGX OS facts of a dgx-spark snapshot so Diff can
// compare them (spark-work-packages.md WP1 item 13): the release files
// (/etc/dgx-release, /etc/fastos-release) merged with the OTA, package and
// unit state of linux.CollectDGXOS, and the host facts of
// linux.CollectDGXHostState (fwupdmgr firmware table -> Platform.Firmware,
// boot classification, pstore, acpitz, GDM sleep policy, suspend markers).
// Read-only; p is the PlatformInfo the snapshot points at. Collector errors
// are dropped like everywhere else in a snapshot.
func collectDGXSpark(timeout int, snap *types.Snapshot, p *types.PlatformInfo) {
	base, _ := common.CollectDGXRelease()
	extra, _ := linuxCollector.CollectDGXOS(timeout)
	snap.DGXOS = linuxCollector.MergeDGXOS(base, extra)
	linuxCollector.CollectDGXHostState(timeout, p)
}

// Compare reads two snapshot files, prints their differences and optionally
// writes them to outDir as comparison.txt or comparison.md.
func Compare(pathA, pathB, outDir string, markdown bool) error {
	snapA, err := loadSnapshot(pathA)
	if err != nil {
		return fmt.Errorf("cannot load snapshot A: %w", err)
	}
	snapB, err := loadSnapshot(pathB)
	if err != nil {
		return fmt.Errorf("cannot load snapshot B: %w", err)
	}

	result := types.ComparisonResult{
		SnapshotA:   filepath.Base(pathA),
		SnapshotB:   filepath.Base(pathB),
		TimestampA:  snapA.Metadata.Timestamp,
		TimestampB:  snapB.Metadata.Timestamp,
		Differences: Diff(snapA, snapB),
	}

	output := formatComparison(result, markdown)
	fmt.Println(output)

	// An empty outDir means console only; "." is the current directory and
	// is a real destination, not "write nothing".
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("cannot create output directory: %w", err)
		}
		ext := ".txt"
		if markdown {
			ext = ".md"
		}
		outPath := filepath.Join(outDir, "comparison"+ext)
		if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
			return fmt.Errorf("cannot write comparison: %w", err)
		}
		if abs, err := filepath.Abs(outPath); err == nil {
			outPath = abs
		}
		fmt.Printf("\nComparison written to: %s\n", outPath)
	}

	return nil
}

// Diff returns the scalar differences between two snapshots. Field names are
// stable snake_case identifiers so scripts can key on them. A driver version
// change is reported once as "driver_version"; per-GPU driver strings are only
// compared when the top-level driver did not change, to avoid reporting the
// same upgrade N+1 times on multi-GPU machines.
func Diff(a, b *types.Snapshot) []types.Difference {
	var diffs []types.Difference
	add := func(field, va, vb, sev string) {
		if va != vb {
			diffs = append(diffs, types.Difference{Field: field, ValueA: va, ValueB: vb, Severity: sev})
		}
	}
	itoa := func(n int64) string { return fmt.Sprintf("%d", n) }
	btoa := func(v bool) string { return fmt.Sprintf("%v", v) }

	add("os_version", a.System.OSVersion, b.System.OSVersion, "INFO")
	add("os_build", a.System.OSBuild, b.System.OSBuild, "INFO")
	add("kernel_version", a.System.KernelVersion, b.System.KernelVersion, "WARN")
	add("architecture", a.System.Architecture, b.System.Architecture, "WARN")
	add("cpu_model", a.System.CPUModel, b.System.CPUModel, "INFO")
	add("ram_total_mb", itoa(a.System.RAMTotalMB), itoa(b.System.RAMTotalMB), "INFO")
	add("boot_mode", a.System.BootMode, b.System.BootMode, "INFO")
	add("secure_boot", a.System.SecureBoot, b.System.SecureBoot, "WARN")

	driverChanged := a.Driver.Version != b.Driver.Version
	add("driver_version", a.Driver.Version, b.Driver.Version, "WARN")
	add("cuda_driver_version", a.Driver.CUDAVersion, b.Driver.CUDAVersion, "WARN")

	add("gpu_count", fmt.Sprintf("%d", len(a.GPUs)), fmt.Sprintf("%d", len(b.GPUs)), "CRIT")

	n := len(a.GPUs)
	if len(b.GPUs) < n {
		n = len(b.GPUs)
	}
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("gpu[%d].", i)
		add(p+"name", a.GPUs[i].Name, b.GPUs[i].Name, "WARN")
		if !driverChanged {
			add(p+"driver_version", a.GPUs[i].DriverVersion, b.GPUs[i].DriverVersion, "WARN")
		}
		add(p+"vram_total_mb", itoa(a.GPUs[i].VRAMTotalMB), itoa(b.GPUs[i].VRAMTotalMB), "INFO")
		add(p+"wddm_version", a.GPUs[i].WDDMVersion, b.GPUs[i].WDDMVersion, "INFO")
		add(p+"pcie_link_speed", a.GPUs[i].PCIeLinkSpeed, b.GPUs[i].PCIeLinkSpeed, "INFO")
		add(p+"pcie_link_width", a.GPUs[i].PCIeLinkWidth, b.GPUs[i].PCIeLinkWidth, "INFO")
		add(p+"compute_cap", a.GPUs[i].ComputeCap, b.GPUs[i].ComputeCap, "INFO")
		add(p+"memory_reporting", a.GPUs[i].MemoryReporting, b.GPUs[i].MemoryReporting, "WARN")
	}

	// Platform, DGX OS, unified memory and firmware (spark-work-packages.md
	// WP1 item 13). Snapshots written before these fields existed carry nil
	// pointers and are simply not compared.
	if a.Platform != nil && b.Platform != nil {
		add("platform.class", a.Platform.Class, b.Platform.Class, "WARN")
		add("platform.gpu_soc", a.Platform.GPUSoC, b.Platform.GPUSoC, "INFO")
		add("platform.unified_memory", btoa(a.Platform.UnifiedMemory), btoa(b.Platform.UnifiedMemory), "WARN")
		add("platform.bios_version", a.Platform.BIOSVersion, b.Platform.BIOSVersion, "INFO")
		add("platform.nvidia_kernel_flavour", btoa(a.Platform.NvidiaKernelFlavour), btoa(b.Platform.NvidiaKernelFlavour), "WARN")
		fa, fb := firmwareVersions(a.Platform.Firmware), firmwareVersions(b.Platform.Firmware)
		for name, va := range fa {
			add("platform.firmware["+name+"].version", va, fb[name], "INFO")
		}
		for name, vb := range fb {
			if _, seen := fa[name]; !seen {
				add("platform.firmware["+name+"].version", "", vb, "INFO")
			}
		}
	}
	if a.DGXOS != nil && b.DGXOS != nil {
		add("dgx_os.ota_version", a.DGXOS.OTAVersion, b.DGXOS.OTAVersion, "WARN")
		add("dgx_os.ota_name", a.DGXOS.OTAName, b.DGXOS.OTAName, "INFO")
		add("dgx_os.sw_build_version", a.DGXOS.SWBuildVersion, b.DGXOS.SWBuildVersion, "INFO")
		add("dgx_os.fast_os_version", a.DGXOS.FastOSVersion, b.DGXOS.FastOSVersion, "INFO")
		add("dgx_os.driver_pkg_version", a.DGXOS.DriverPkgVersion, b.DGXOS.DriverPkgVersion, "WARN")
		add("dgx_os.firmware_pkg_version", a.DGXOS.FirmwarePkgVersion, b.DGXOS.FirmwarePkgVersion, "WARN")
	}
	if a.UnifiedMemory != nil && b.UnifiedMemory != nil {
		add("unified_memory.mem_total_kb", itoa(a.UnifiedMemory.MemTotalKB), itoa(b.UnifiedMemory.MemTotalKB), "WARN")
		add("unified_memory.swap_total_kb", itoa(a.UnifiedMemory.SwapTotalKB), itoa(b.UnifiedMemory.SwapTotalKB), "INFO")
		add("unified_memory.swappiness", fmt.Sprintf("%d", a.UnifiedMemory.Swappiness), fmt.Sprintf("%d", b.UnifiedMemory.Swappiness), "INFO")
	}

	if a.AI != nil && b.AI != nil {
		add("cuda_toolkit_version", a.AI.CUDAToolkitVersion, b.AI.CUDAToolkitVersion, "WARN")
		add("cudnn_version", a.AI.CuDNNVersion, b.AI.CuDNNVersion, "INFO")
		add("conda_present", btoa(a.AI.CondaPresent), btoa(b.AI.CondaPresent), "INFO")

		if a.AI.PyTorchInfo != nil && b.AI.PyTorchInfo != nil {
			add("pytorch.version", a.AI.PyTorchInfo.Version, b.AI.PyTorchInfo.Version, "INFO")
			add("pytorch.cuda_version", a.AI.PyTorchInfo.CUDAVersion, b.AI.PyTorchInfo.CUDAVersion, "WARN")
			add("pytorch.cuda_available", btoa(a.AI.PyTorchInfo.CUDAAvailable), btoa(b.AI.PyTorchInfo.CUDAAvailable), "CRIT")
		}
		if a.AI.TensorFlowInfo != nil && b.AI.TensorFlowInfo != nil {
			add("tensorflow.version", a.AI.TensorFlowInfo.Version, b.AI.TensorFlowInfo.Version, "INFO")
			add("tensorflow.gpu_count", fmt.Sprintf("%d", len(a.AI.TensorFlowInfo.GPUs)), fmt.Sprintf("%d", len(b.AI.TensorFlowInfo.GPUs)), "WARN")
		}
	}

	return diffs
}

// firmwareVersions maps fwupdmgr component names to their versions; the name
// is the stable key because GUIDs differ between FE and OEM boards.
func firmwareVersions(fw []types.FirmwareComponent) map[string]string {
	m := map[string]string{}
	for _, f := range fw {
		if f.Name != "" {
			m[f.Name] = f.Version
		}
	}
	return m
}

func loadSnapshot(path string) (*types.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap types.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func formatComparison(result types.ComparisonResult, markdown bool) string {
	var sb strings.Builder
	na := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}

	if markdown {
		sb.WriteString("# NVCheckup Snapshot Comparison\n\n")
		sb.WriteString(fmt.Sprintf("**Snapshot A:** %s (%s)\n\n", result.SnapshotA, result.TimestampA.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("**Snapshot B:** %s (%s)\n\n", result.SnapshotB, result.TimestampB.Format("2006-01-02 15:04:05")))

		if len(result.Differences) == 0 {
			sb.WriteString("No differences found.\n")
		} else {
			sb.WriteString("| Field | Snapshot A | Snapshot B | Severity |\n")
			sb.WriteString("|-------|-----------|-----------|----------|\n")
			for _, d := range result.Differences {
				sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", d.Field, na(d.ValueA), na(d.ValueB), d.Severity))
			}
		}
		return sb.String()
	}

	sb.WriteString("NVCheckup Snapshot Comparison\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Snapshot A: %s (%s)\n", result.SnapshotA, result.TimestampA.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Snapshot B: %s (%s)\n", result.SnapshotB, result.TimestampB.Format("2006-01-02 15:04:05")))
	sb.WriteString(strings.Repeat("─", 60) + "\n\n")

	if len(result.Differences) == 0 {
		sb.WriteString("No differences found.\n")
	} else {
		sb.WriteString(fmt.Sprintf("Found %d difference(s):\n\n", len(result.Differences)))
		for _, d := range result.Differences {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", d.Severity, d.Field))
			sb.WriteString(fmt.Sprintf("    A: %s\n", na(d.ValueA)))
			sb.WriteString(fmt.Sprintf("    B: %s\n\n", na(d.ValueB)))
		}
	}
	return sb.String()
}
