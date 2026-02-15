// NVCheckup — Cross-platform NVIDIA diagnostic CLI tool.
// Unofficial community tool, not affiliated with NVIDIA Corporation.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/bundle"
	"github.com/nicholasgasior/nvcheckup/internal/core"
	"github.com/nicholasgasior/nvcheckup/internal/doctor"
	"github.com/nicholasgasior/nvcheckup/internal/selftest"
	"github.com/nicholasgasior/nvcheckup/internal/snapshot"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "snapshot":
		snapshotCmd(os.Args[2:])
	case "compare":
		compareCmd(os.Args[2:])
	case "doctor":
		doctorCmd(os.Args[2:])
	case "self-test":
		selfTestCmd(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("NVCheckup v%s\n", types.Version)
		fmt.Println(types.Disclaimer)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(types.ExitError)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	mode := fs.String("mode", "full", "Diagnostic mode: gaming, ai, creator, streaming, full")
	outDir := fs.String("out", ".", "Output directory for reports")
	doZip := fs.Bool("zip", false, "Create a zip bundle of the report and logs")
	doJSON := fs.Bool("json", false, "Generate report.json (structured output)")
	doMD := fs.Bool("md", false, "Generate report.md (GitHub/Reddit-ready)")
	verbose := fs.Bool("verbose", false, "Enable verbose output")
	noAdmin := fs.Bool("no-admin", false, "Skip checks requiring admin/root")
	timeout := fs.Int("timeout", 30, "Timeout in seconds for each system command")
	redactFlag := fs.Bool("redact", true, "Enable PII redaction (default: true)")
	noRedact := fs.Bool("no-redact", false, "Disable PII redaction (not recommended for sharing)")
	includeLogs := fs.Bool("include-logs", false, "Include extended logs in the report/bundle")

	fs.Parse(args)

	// Validate mode
	m := types.RunMode(strings.ToLower(*mode))
	switch m {
	case types.ModeGaming, types.ModeAI, types.ModeCreator, types.ModeStreaming, types.ModeFull:
		// ok
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Use: gaming, ai, creator, streaming, full\n", *mode)
		os.Exit(types.ExitError)
	}

	// Handle redaction flags
	redact := *redactFlag
	if *noRedact {
		redact = false
	}

	cfg := types.RunConfig{
		Mode:        m,
		OutDir:      *outDir,
		Zip:         *doZip,
		JSON:        *doJSON,
		Markdown:    *doMD,
		Verbose:     *verbose,
		NoAdmin:     *noAdmin,
		Timeout:     *timeout,
		Redact:      redact,
		IncludeLogs: *includeLogs,
	}

	printBanner()

	report, err := core.Run(cfg, *verbose, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}

	// Write outputs
	files, err := core.WriteReport(report, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		os.Exit(types.ExitError)
	}

	fmt.Println()
	for _, f := range files {
		fmt.Printf("  Written: %s\n", f)
	}

	// Zip if requested
	if cfg.Zip {
		zipPath, err := bundle.CreateZip(cfg.OutDir, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating zip: %v\n", err)
		} else {
			fmt.Printf("  Bundle:  %s\n", zipPath)
		}
	}

	fmt.Println()

	// Print summary to console
	fmt.Println(report.SummaryBlock)

	if len(report.TopIssues) > 0 {
		fmt.Println("Top Issues:")
		for i, issue := range report.TopIssues {
			fmt.Printf("  %d. %s\n", i+1, issue)
		}
		fmt.Println()
	}

	// Determine exit code
	exitCode := types.ExitOK
	for _, f := range report.Findings {
		switch f.Severity {
		case types.SeverityCrit:
			exitCode = types.ExitCritical
		case types.SeverityWarn:
			if exitCode < types.ExitWarnings {
				exitCode = types.ExitWarnings
			}
		}
	}
	os.Exit(exitCode)
}

func snapshotCmd(args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	outDir := fs.String("out", ".", "Output directory")
	timeout := fs.Int("timeout", 30, "Command timeout in seconds")
	fs.Parse(args)

	printBanner()
	fmt.Println("Creating snapshot...")

	path, err := snapshot.Create(*outDir, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}
	fmt.Printf("Snapshot saved: %s\n", path)
}

func compareCmd(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	outDir := fs.String("out", ".", "Output directory")
	doMD := fs.Bool("md", false, "Output as markdown")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: nvcheckup compare <snapshotA.json> <snapshotB.json> [--out DIR] [--md]")
		os.Exit(types.ExitError)
	}

	printBanner()
	err := snapshot.Compare(remaining[0], remaining[1], *outDir, *doMD)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}
}

func doctorCmd(args []string) {
	printBanner()
	doctor.RunInteractive()
}

func selfTestCmd(args []string) {
	printBanner()
	exitCode := selftest.Run()
	os.Exit(exitCode)
}

func printBanner() {
	fmt.Println()
	fmt.Printf("  NVCheckup v%s\n", types.Version)
	fmt.Printf("  %s\n", types.Disclaimer)
	fmt.Printf("  %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	fmt.Println()
}

func printUsage() {
	fmt.Printf(`NVCheckup v%s — Cross-platform NVIDIA Diagnostic Tool
%s

Usage:
  nvcheckup <command> [flags]

Commands:
  run         Run diagnostics and generate a report
  snapshot    Create a timestamped JSON snapshot
  compare     Compare two snapshots
  doctor      Interactive guided diagnostic mode
  self-test   Verify environment, dependencies, and permissions
  version     Show version information

Run Flags:
  --mode      Diagnostic mode: gaming, ai, creator, streaming, full (default: full)
  --out       Output directory (default: current directory)
  --zip       Create a zip bundle of reports and logs
  --json      Generate structured JSON report
  --md        Generate markdown report (GitHub/Reddit-ready)
  --verbose   Enable verbose output
  --no-admin  Skip checks requiring elevated permissions
  --timeout   Command timeout in seconds (default: 30)
  --redact    Enable PII redaction (default: true)
  --no-redact Disable PII redaction
  --include-logs  Include extended system logs in the bundle

Examples:
  nvcheckup run --mode gaming --zip
  nvcheckup run --mode ai --json --md
  nvcheckup run --mode full --zip --json --out ./reports
  nvcheckup snapshot --out ./snapshots
  nvcheckup compare snap1.json snap2.json
  nvcheckup doctor
  nvcheckup self-test
`, types.Version, types.Disclaimer)
}
