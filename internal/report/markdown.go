package report

import (
	"fmt"
	"strings"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// GenerateMarkdown produces a GitHub/Reddit-ready markdown report.
func GenerateMarkdown(report *types.Report) string {
	var sb strings.Builder
	w := func(format string, args ...interface{}) {
		sb.WriteString(fmt.Sprintf(format, args...))
	}

	w("# NVCheckup Diagnostic Report\n\n")
	w("> %s\n\n", types.Disclaimer)
	w("**Generated:** %s | **Mode:** %s | **Platform:** %s\n\n",
		report.Metadata.Timestamp.Format("2006-01-02 15:04:05"),
		report.Metadata.Mode, report.Metadata.Platform)

	// Summary
	w("## Summary\n\n")
	w("```\n%s```\n\n", report.SummaryBlock)

	// System
	w("## System\n\n")
	w("| Property | Value |\n")
	w("|----------|-------|\n")
	w("| OS | %s %s |\n", report.System.OSName, report.System.OSVersion)
	if report.System.KernelVersion != "" {
		w("| Kernel | %s |\n", report.System.KernelVersion)
	}
	w("| Architecture | %s |\n", report.System.Architecture)
	w("| CPU | %s |\n", report.System.CPUModel)
	w("| RAM | %d MB |\n", report.System.RAMTotalMB)
	w("| Boot Mode | %s |\n", report.System.BootMode)
	w("| Secure Boot | %s |\n", report.System.SecureBoot)
	w("\n")

	// GPUs
	w("## GPUs\n\n")
	for _, gpu := range report.GPUs {
		w("### GPU %d: %s\n\n", gpu.Index, gpu.Name)
		w("| Property | Value |\n")
		w("|----------|-------|\n")
		w("| Vendor | %s |\n", gpu.Vendor)
		w("| Driver | %s |\n", gpu.DriverVersion)
		if gpu.VRAMTotalMB > 0 {
			w("| VRAM | %d MB total / %d MB free |\n", gpu.VRAMTotalMB, gpu.VRAMFreeMB)
		}
		if gpu.Temperature > 0 {
			w("| Temperature | %d°C |\n", gpu.Temperature)
		}
		w("\n")
	}

	w("**NVIDIA Driver:** %s | **CUDA:** %s\n\n", valueOrNA(report.Driver.Version), valueOrNA(report.Driver.CUDAVersion))

	// Findings
	w("## Findings\n\n")
	if len(report.Findings) == 0 {
		w("No issues detected.\n\n")
	} else {
		w("| Severity | Finding | Evidence | Next Step |\n")
		w("|----------|---------|----------|-----------|\n")
		for _, f := range report.Findings {
			nextStep := "—"
			if len(f.NextSteps) > 0 {
				nextStep = f.NextSteps[0]
			}
			w("| **%s** | %s | %s | %s |\n",
				f.Severity, f.Title,
				truncate(f.Evidence, 80),
				truncate(nextStep, 80))
		}
		w("\n")

		// Detailed findings
		w("### Details\n\n")
		for i, f := range report.Findings {
			w("<details>\n<summary><b>[%s] #%d: %s</b></summary>\n\n", f.Severity, i+1, f.Title)
			w("**Evidence:** %s\n\n", f.Evidence)
			w("**Why it matters:** %s\n\n", f.WhyItMatters)
			w("**Next steps:**\n")
			for _, step := range f.NextSteps {
				w("- %s\n", step)
			}
			w("\n</details>\n\n")
		}
	}

	// Top Issues & Next Steps
	w("## Top Issues\n\n")
	for i, issue := range report.TopIssues {
		w("%d. %s\n", i+1, issue)
	}
	w("\n")

	w("## Recommended Next Steps\n\n")
	for i, step := range report.NextSteps {
		w("%d. %s\n", i+1, step)
	}
	w("\n")

	// Collector errors
	if len(report.CollectorErrors) > 0 {
		w("## Collector Notes\n\n")
		for _, ce := range report.CollectorErrors {
			w("- **%s:** %s\n", ce.Collector, ce.Error)
		}
		w("\n")
	}

	// Privacy
	w("---\n\n")
	w("*This report was generated locally. No data was transmitted. %s*\n", types.Disclaimer)

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
