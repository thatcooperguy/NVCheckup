// Package core orchestrates the NVCheckup diagnostic pipeline.
//
// Run is strictly read-only: it collects, analyzes and redacts, but never
// changes system state. Anything that modifies the machine lives behind
// 'nvcheckup fix' in the remediate package.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/analyzer"
	"github.com/thatcooperguy/nvcheckup/internal/collector/ai"
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/collector/wsl"
	"github.com/thatcooperguy/nvcheckup/internal/redact"
	"github.com/thatcooperguy/nvcheckup/internal/report"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const totalPhases = 7

// phaseTracker prints "[n/7] ..." progress lines and, when verbose, the time
// each phase took plus every collector error as soon as it is recorded.
type phaseTracker struct {
	verbose bool
	printFn func(string)
	current int
	started time.Time
	errors  []types.CollectorError
}

func (p *phaseTracker) begin(msg string) {
	p.current++
	p.started = time.Now()
	p.printFn(fmt.Sprintf("[%d/%d] %s", p.current, totalPhases, msg))
}

func (p *phaseTracker) skip(msg string) {
	p.current++
	p.printFn(fmt.Sprintf("[%d/%d] %s", p.current, totalPhases, msg))
}

func (p *phaseTracker) end() {
	if p.verbose {
		p.printFn(fmt.Sprintf("      done in %.1fs", time.Since(p.started).Seconds()))
	}
}

func (p *phaseTracker) addErrors(errs []types.CollectorError) {
	for _, e := range errs {
		p.errors = append(p.errors, e)
		if p.verbose {
			p.printFn(fmt.Sprintf("      note [%s]: %s", e.Collector, e.Error))
		}
	}
}

// Run executes the full diagnostic pipeline and returns the completed report.
// Network probes (ping/traceroute/DNS) run only when cfg.NetworkTest is set,
// regardless of mode, because they contact external hosts and take 30-60 s.
func Run(cfg types.RunConfig, verbose bool, printFn func(string)) (*types.Report, error) {
	startTime := time.Now()
	if printFn == nil {
		printFn = func(string) {}
	}

	r := &types.Report{
		Metadata: types.ReportMetadata{
			ToolVersion:      types.Version,
			Timestamp:        startTime,
			Mode:             cfg.Mode,
			RedactionEnabled: cfg.Redact,
			Platform:         runtime.GOOS,
			SchemaVersion:    types.SchemaVersion,
		},
	}

	redactor := redact.New(cfg.Redact)
	ph := &phaseTracker{verbose: verbose || cfg.Verbose, printFn: printFn}

	// Phase 1: system info
	ph.begin("Collecting system information...")
	sysInfo, sysErrs := common.CollectSystemInfo(cfg.Timeout)
	r.System = sysInfo
	ph.addErrors(sysErrs)
	// Platform class from files, lspci, DMI and the kernel (spec 3.1 rows
	// 1-4, 6, 10, 11); the GPU-dependent rows follow after phase 3.
	platform, platformErrs := common.DetectPlatform(cfg.Timeout)
	r.Platform = platform
	ph.addErrors(platformErrs)
	ph.end()

	// Phase 2: GPUs and driver
	ph.begin("Detecting GPUs and drivers...")
	gpus, driver, gpuErrs := common.CollectGPUInfo(cfg.Timeout)
	r.GPUs = gpus
	r.Driver = driver
	ph.addErrors(gpuErrs)
	ph.end()

	// Phase 3: thermal + PCIe
	ph.begin("Collecting GPU thermal and PCIe data...")
	// One entry per NVIDIA GPU lands in GPUThermal / GPUPCIe; the single
	// Thermal / PCIe pointers keep pointing at GPU 0 for existing consumers.
	thermals, thermalErrs := common.CollectThermalAll(cfg.Timeout)
	if len(thermals) > 0 {
		r.GPUThermal = thermals
		if thermals[0].TemperatureC > 0 || thermals[0].PowerState != "" {
			r.Thermal = &r.GPUThermal[0]
		}
	}
	ph.addErrors(thermalErrs)

	pcies, pcieErrs := common.CollectPCIeAll(cfg.Timeout)
	if len(pcies) > 0 {
		r.GPUPCIe = pcies
		if pcies[0].CurrentSpeed != "" || pcies[0].MaxSpeed != "" {
			r.PCIe = &r.GPUPCIe[0]
		}
	}
	ph.addErrors(pcieErrs)
	// Rows 5, 7, 8, 9 and flag rules A-C of spec 3.1 need the GPU and PCIe
	// data above and must land before the platform collectors and analysis.
	common.ApplyPlatformFlags(r)
	ph.end()

	// Phase 4: platform-specific (Windows/Linux), then the Spark /
	// unified-memory collectors gated on r.Platform.
	ph.begin("Running platform-specific checks...")
	ph.addErrors(collectPlatformSpecific(r, cfg))
	ph.addErrors(collectPlatformExtras(r, cfg))
	ph.end()

	// Phase 5: AI/CUDA (ai, creator and full modes)
	if cfg.Mode == types.ModeAI || cfg.Mode == types.ModeFull || cfg.Mode == types.ModeCreator {
		ph.begin("Checking AI/CUDA environment...")
		aiInfo, aiErrs := ai.CollectAIInfo(cfg.Timeout)
		r.AI = &aiInfo
		ph.addErrors(aiErrs)
		ph.end()
	} else {
		ph.skip("Skipping AI checks (not selected)...")
	}

	// WSL detection rides along with phase 5 (full or AI mode)
	if cfg.Mode == types.ModeFull || cfg.Mode == types.ModeAI {
		wslInfo, wslErrs := wsl.DetectWSL(cfg.Timeout)
		if wslInfo.IsWSL {
			r.WSL = &wslInfo
		}
		ph.addErrors(wslErrs)
	}

	// Phase 6: network probes, opt-in only
	if cfg.NetworkTest {
		printFn("      Network probes take 30-60 s (ping/traceroute to 1.1.1.1, DNS lookup of google.com).")
		ph.begin("Running network diagnostics...")
		netInfo, netErrs := common.CollectNetworkInfo(cfg.Timeout)
		r.Metadata.NetworkProbes = true
		if netInfo.InterfaceName != "" || netInfo.LatencyMs > 0 || len(netInfo.Hops) > 0 {
			r.Network = &netInfo
		}
		ph.addErrors(netErrs)
		ph.end()
	} else {
		ph.skip("Skipping network probes (use --network to enable)...")
	}

	r.CollectorErrors = ph.errors

	// Phase 7: analysis
	ph.begin("Analyzing results...")
	analyzer.Analyze(r, cfg.Mode)
	ph.end()

	r.Metadata.RuntimeSeconds = time.Since(startTime).Seconds()

	redact.ApplyToReport(r, redactor)

	return r, nil
}

// ExitCodeFor maps a report's findings to the CLI exit code contract shared by
// 'run' and 'doctor': 0 clean, 1 warnings, 2 at least one critical finding.
func ExitCodeFor(r *types.Report) int {
	code := types.ExitOK
	if r == nil {
		return code
	}
	for _, f := range r.Findings {
		switch f.Severity {
		case types.SeverityCrit:
			return types.ExitCritical
		case types.SeverityWarn:
			code = types.ExitWarnings
		}
	}
	return code
}

// WriteReport writes the report to the output directory in all requested formats.
func WriteReport(r *types.Report, cfg types.RunConfig) ([]string, error) {
	var outputFiles []string

	outDir := cfg.OutDir
	if outDir == "" || outDir == "." {
		outDir, _ = os.Getwd()
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}

	// report.txt is always produced
	txtPath := filepath.Join(outDir, "report.txt")
	if err := os.WriteFile(txtPath, []byte(report.GenerateText(r)), 0644); err != nil {
		return nil, fmt.Errorf("cannot write report.txt: %w", err)
	}
	outputFiles = append(outputFiles, txtPath)

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

	if cfg.Markdown {
		mdPath := filepath.Join(outDir, "report.md")
		if err := os.WriteFile(mdPath, []byte(report.GenerateMarkdown(r)), 0644); err != nil {
			return outputFiles, fmt.Errorf("cannot write report.md: %w", err)
		}
		outputFiles = append(outputFiles, mdPath)
	}

	return outputFiles, nil
}
