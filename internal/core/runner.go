// Package core orchestrates the NVCheckup diagnostic pipeline.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/analyzer"
	"github.com/nicholasgasior/nvcheckup/internal/collector/ai"
	"github.com/nicholasgasior/nvcheckup/internal/collector/common"
	"github.com/nicholasgasior/nvcheckup/internal/collector/wsl"
	"github.com/nicholasgasior/nvcheckup/internal/redact"
	"github.com/nicholasgasior/nvcheckup/internal/report"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// Run executes the full diagnostic pipeline and returns the completed report.
func Run(cfg types.RunConfig, verbose bool, printFn func(string)) (*types.Report, error) {
	startTime := time.Now()

	r := &types.Report{
		Metadata: types.ReportMetadata{
			ToolVersion:      types.Version,
			Timestamp:        startTime,
			Mode:             cfg.Mode,
			RedactionEnabled: cfg.Redact,
			Platform:         runtime.GOOS,
		},
	}

	redactor := redact.New(cfg.Redact)
	var allErrors []types.CollectorError

	// Phase 1: Collect system info
	printFn("[1/7] Collecting system information...")
	sysInfo, sysErrs := common.CollectSystemInfo(cfg.Timeout)
	r.System = sysInfo
	allErrors = append(allErrors, sysErrs...)

	// Phase 2: Collect GPU info
	printFn("[2/7] Detecting GPUs and drivers...")
	gpus, driver, gpuErrs := common.CollectGPUInfo(cfg.Timeout)
	r.GPUs = gpus
	r.Driver = driver
	allErrors = append(allErrors, gpuErrs...)

	// Phase 3: GPU thermal + PCIe
	printFn("[3/7] Collecting GPU thermal and PCIe data...")
	thermalInfo, thermalErrs := common.CollectThermalInfo(cfg.Timeout)
	if thermalInfo.TemperatureC > 0 || thermalInfo.PowerState != "" {
		r.Thermal = &thermalInfo
	}
	allErrors = append(allErrors, thermalErrs...)

	pcieInfo, pcieErrs := common.CollectPCIeInfo(cfg.Timeout)
	if pcieInfo.CurrentSpeed != "" || pcieInfo.MaxSpeed != "" {
		r.PCIe = &pcieInfo
	}
	allErrors = append(allErrors, pcieErrs...)

	// Phase 4: Platform-specific collection (Windows/Linux)
	printFn("[4/7] Running platform-specific checks...")
	platformErrs := collectPlatformSpecific(r, cfg)
	allErrors = append(allErrors, platformErrs...)

	// Phase 5: AI/CUDA checks (if applicable mode)
	if cfg.Mode == types.ModeAI || cfg.Mode == types.ModeFull || cfg.Mode == types.ModeCreator {
		printFn("[5/7] Checking AI/CUDA environment...")
		aiInfo, aiErrs := ai.CollectAIInfo(cfg.Timeout)
		r.AI = &aiInfo
		allErrors = append(allErrors, aiErrs...)
	} else {
		printFn("[5/7] Skipping AI checks (not selected)...")
	}

	// WSL detection (full mode or AI mode)
	if cfg.Mode == types.ModeFull || cfg.Mode == types.ModeAI {
		wslInfo, wslErrs := wsl.DetectWSL(cfg.Timeout)
		if wslInfo.IsWSL {
			r.WSL = &wslInfo
		}
		allErrors = append(allErrors, wslErrs...)
	}

	// Phase 6: Network diagnostics (if enabled or relevant mode)
	if cfg.NetworkTest || cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeStreaming || cfg.Mode == types.ModeFull {
		printFn("[6/7] Running network diagnostics...")
		netInfo, netErrs := common.CollectNetworkInfo(cfg.Timeout)
		if netInfo.InterfaceName != "" {
			r.Network = &netInfo
		}
		allErrors = append(allErrors, netErrs...)
	} else {
		printFn("[6/7] Skipping network checks...")
	}

	r.CollectorErrors = allErrors

	// Phase 7: Analyze and produce findings
	printFn("[7/7] Analyzing results...")
	analyzer.Analyze(r, cfg.Mode)

	// Calculate runtime
	r.Metadata.RuntimeSeconds = time.Since(startTime).Seconds()

	// Apply redaction
	applyRedaction(r, redactor)

	return r, nil
}

// WriteReport writes the report to the output directory in all requested formats.
func WriteReport(r *types.Report, cfg types.RunConfig) ([]string, error) {
	var outputFiles []string

	outDir := cfg.OutDir
	if outDir == "" || outDir == "." {
		outDir, _ = os.Getwd()
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}

	// Always generate report.txt
	txtPath := filepath.Join(outDir, "report.txt")
	txtContent := report.GenerateText(r)
	if err := os.WriteFile(txtPath, []byte(txtContent), 0644); err != nil {
		return nil, fmt.Errorf("cannot write report.txt: %w", err)
	}
	outputFiles = append(outputFiles, txtPath)

	// JSON if requested
	if cfg.JSON {
		jsonPath := filepath.Join(outDir, "report.json")
		jsonContent, err := report.GenerateJSON(r)
		if err != nil {
			return outputFiles, fmt.Errorf("cannot generate JSON: %w", err)
		}
		if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
			return outputFiles, fmt.Errorf("cannot write report.json: %w", err)
		}
		outputFiles = append(outputFiles, jsonPath)
	}

	// Markdown if requested
	if cfg.Markdown {
		mdPath := filepath.Join(outDir, "report.md")
		mdContent := report.GenerateMarkdown(r)
		if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
			return outputFiles, fmt.Errorf("cannot write report.md: %w", err)
		}
		outputFiles = append(outputFiles, mdPath)
	}

	return outputFiles, nil
}

func applyRedaction(r *types.Report, redactor *redact.Redactor) {
	r.System.Hostname = redactor.RedactHostname(r.System.Hostname)
	r.SummaryBlock = redactor.Redact(r.SummaryBlock)

	// Redact GPU bus IDs paths if needed
	for i := range r.GPUs {
		r.GPUs[i].PCIBusID = redactor.Redact(r.GPUs[i].PCIBusID)
	}

	// Redact nvidia-smi output
	r.Driver.NvidiaSmiOutput = redactor.Redact(r.Driver.NvidiaSmiOutput)
	r.Driver.NvidiaSmiPath = redactor.RedactPath(r.Driver.NvidiaSmiPath)

	// Redact findings evidence
	for i := range r.Findings {
		r.Findings[i].Evidence = redactor.Redact(r.Findings[i].Evidence)
	}

	// Redact collector errors
	for i := range r.CollectorErrors {
		r.CollectorErrors[i].Error = redactor.Redact(r.CollectorErrors[i].Error)
	}

	// Redact Linux-specific paths
	if r.Linux != nil {
		r.Linux.LibCudaPath = redactor.RedactPath(r.Linux.LibCudaPath)
		r.Linux.JournalSnippets = redactor.Redact(r.Linux.JournalSnippets)
		r.Linux.DmesgSnippets = redactor.Redact(r.Linux.DmesgSnippets)
	}

	// Redact AI paths
	if r.AI != nil {
		r.AI.NvccPath = redactor.RedactPath(r.AI.NvccPath)
		for i := range r.AI.PythonVersions {
			r.AI.PythonVersions[i].Path = redactor.RedactPath(r.AI.PythonVersions[i].Path)
		}
	}

	// Redact network hop addresses
	if r.Network != nil {
		for i := range r.Network.Hops {
			r.Network.Hops[i].Address = redactor.Redact(r.Network.Hops[i].Address)
		}
	}
}
