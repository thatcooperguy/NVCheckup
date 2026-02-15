// Package snapshot creates and compares timestamped system snapshots.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/collector/ai"
	"github.com/nicholasgasior/nvcheckup/internal/collector/common"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// Create generates a timestamped JSON snapshot of the current system state.
func Create(outDir string, timeout int) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create output directory: %w", err)
	}

	snap := types.Snapshot{
		Metadata: types.ReportMetadata{
			ToolVersion: types.Version,
			Timestamp:   time.Now(),
			Mode:        types.ModeFull,
		},
	}

	// Collect system info
	sysInfo, _ := common.CollectSystemInfo(timeout)
	snap.System = sysInfo

	// Collect GPU info
	gpus, driver, _ := common.CollectGPUInfo(timeout)
	snap.GPUs = gpus
	snap.Driver = driver

	// Collect AI info
	aiInfo, _ := ai.CollectAIInfo(timeout)
	snap.AI = &aiInfo

	snap.Metadata.RuntimeSeconds = time.Since(snap.Metadata.Timestamp).Seconds()

	// Write snapshot
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal snapshot: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("nvcheckup-snapshot-%s.json", timestamp)
	path := filepath.Join(outDir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("cannot write snapshot: %w", err)
	}

	return path, nil
}

// Compare reads two snapshot files and outputs their differences.
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
		SnapshotA:  filepath.Base(pathA),
		SnapshotB:  filepath.Base(pathB),
		TimestampA: snapA.Metadata.Timestamp,
		TimestampB: snapB.Metadata.Timestamp,
	}

	// Compare fields
	addDiff := func(field, a, b, sev string) {
		if a != b {
			result.Differences = append(result.Differences, types.Difference{
				Field:    field,
				ValueA:   a,
				ValueB:   b,
				Severity: sev,
			})
		}
	}

	addDiff("OS Version", snapA.System.OSVersion, snapB.System.OSVersion, "INFO")
	addDiff("Kernel", snapA.System.KernelVersion, snapB.System.KernelVersion, "WARN")
	addDiff("Driver Version", snapA.Driver.Version, snapB.Driver.Version, "WARN")
	addDiff("CUDA Version", snapA.Driver.CUDAVersion, snapB.Driver.CUDAVersion, "WARN")

	// Compare GPU count
	addDiff("GPU Count", fmt.Sprintf("%d", len(snapA.GPUs)), fmt.Sprintf("%d", len(snapB.GPUs)), "CRIT")

	// Compare each GPU
	minGPUs := len(snapA.GPUs)
	if len(snapB.GPUs) < minGPUs {
		minGPUs = len(snapB.GPUs)
	}
	for i := 0; i < minGPUs; i++ {
		prefix := fmt.Sprintf("GPU[%d]", i)
		addDiff(prefix+" Name", snapA.GPUs[i].Name, snapB.GPUs[i].Name, "WARN")
		addDiff(prefix+" Driver", snapA.GPUs[i].DriverVersion, snapB.GPUs[i].DriverVersion, "WARN")
		addDiff(prefix+" VRAM Total",
			fmt.Sprintf("%d MB", snapA.GPUs[i].VRAMTotalMB),
			fmt.Sprintf("%d MB", snapB.GPUs[i].VRAMTotalMB), "INFO")
	}

	// AI info comparison
	if snapA.AI != nil && snapB.AI != nil {
		addDiff("CUDA Toolkit", snapA.AI.CUDAToolkitVersion, snapB.AI.CUDAToolkitVersion, "WARN")
		addDiff("cuDNN", snapA.AI.CuDNNVersion, snapB.AI.CuDNNVersion, "INFO")

		if snapA.AI.PyTorchInfo != nil && snapB.AI.PyTorchInfo != nil {
			addDiff("PyTorch Version", snapA.AI.PyTorchInfo.Version, snapB.AI.PyTorchInfo.Version, "INFO")
			addDiff("PyTorch CUDA", snapA.AI.PyTorchInfo.CUDAVersion, snapB.AI.PyTorchInfo.CUDAVersion, "WARN")
			addDiff("PyTorch CUDA Available",
				fmt.Sprintf("%v", snapA.AI.PyTorchInfo.CUDAAvailable),
				fmt.Sprintf("%v", snapB.AI.PyTorchInfo.CUDAAvailable), "CRIT")
		}
	}

	// Output results
	output := formatComparison(result, markdown)
	fmt.Println(output)

	// Optionally write to file
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0755); err == nil {
			ext := ".txt"
			if markdown {
				ext = ".md"
			}
			outPath := filepath.Join(outDir, "comparison"+ext)
			os.WriteFile(outPath, []byte(output), 0644)
			fmt.Printf("\nComparison written to: %s\n", outPath)
		}
	}

	return nil
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
				sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", d.Field, d.ValueA, d.ValueB, d.Severity))
			}
		}
	} else {
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
				sb.WriteString(fmt.Sprintf("    A: %s\n", d.ValueA))
				sb.WriteString(fmt.Sprintf("    B: %s\n\n", d.ValueB))
			}
		}
	}

	return sb.String()
}
