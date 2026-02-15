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
	"github.com/nicholasgasior/nvcheckup/internal/collector/common"
	"github.com/nicholasgasior/nvcheckup/internal/core"
	"github.com/nicholasgasior/nvcheckup/internal/doctor"
	"github.com/nicholasgasior/nvcheckup/internal/remediate"
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
	case "fix":
		fixCmd(os.Args[2:])
	case "undo":
		undoCmd(os.Args[2:])
	case "network-test":
		networkTestCmd(os.Args[2:])
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

func fixCmd(args []string) {
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	id := fs.String("id", "", "Remediation action ID to apply")
	dryRun := fs.Bool("dry-run", false, "Preview changes without applying")
	outDir := fs.String("out", ".", "Directory for change journal")
	all := fs.Bool("all", false, "Preview all available fixes")
	fs.Parse(args)

	printBanner()

	engine := remediate.NewEngine(nil, *outDir, *dryRun)
	actions := engine.ListAvailable()

	if len(actions) == 0 {
		fmt.Println("No remediation actions available for this platform.")
		return
	}

	// List mode
	if *id == "" || *all {
		fmt.Println("Available remediation actions:")
		fmt.Println()
		for _, a := range actions {
			fmt.Printf("  %-25s [%s risk] %s\n", a.ID, a.Risk, a.Title)
			if *all || *dryRun {
				fmt.Printf("    %s\n", a.Description)
				if a.NeedsAdmin {
					fmt.Printf("    Requires: elevated/admin privileges\n")
				}
				if a.NeedsReboot {
					fmt.Printf("    Note: reboot required after applying\n")
				}
				fmt.Println()
			}
		}
		if !*all {
			fmt.Println()
			fmt.Println("Use: nvcheckup fix --id <action-id> to apply a fix")
			fmt.Println("     nvcheckup fix --id <action-id> --dry-run to preview")
		}
		return
	}

	// Find the requested action
	var target *types.RemediationAction
	for _, a := range actions {
		a := a
		if a.ID == *id {
			target = &a
			break
		}
	}

	if target == nil {
		fmt.Fprintf(os.Stderr, "Unknown action ID: %s\n", *id)
		fmt.Fprintln(os.Stderr, "Run 'nvcheckup fix' to see available actions.")
		os.Exit(types.ExitError)
	}

	fmt.Println(engine.Preview(*target))

	if *dryRun {
		fmt.Println("[DRY RUN] No changes were made.")
		return
	}

	fmt.Print("Apply this fix? (yes/no): ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "yes" && answer != "y" {
		fmt.Println("Aborted.")
		return
	}

	result, err := engine.Apply(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}

	if result.Success {
		fmt.Printf("Applied: %s\n", result.Output)
		if target.NeedsReboot {
			fmt.Println("  A reboot is required for this change to take effect.")
		}
	} else {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", result.Output)
		os.Exit(types.ExitError)
	}
}

func undoCmd(args []string) {
	fs := flag.NewFlagSet("undo", flag.ExitOnError)
	id := fs.String("id", "", "Action ID to undo")
	outDir := fs.String("out", ".", "Directory containing change journal")
	fs.Parse(args)

	printBanner()

	engine := remediate.NewEngine(nil, *outDir, false)
	journal := remediate.NewJournal(*outDir)
	entries, err := journal.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading change journal: %v\n", err)
		os.Exit(types.ExitError)
	}

	if len(entries) == 0 {
		fmt.Println("No changes recorded in the journal.")
		return
	}

	// List mode
	if *id == "" {
		fmt.Println("Change journal entries:")
		fmt.Println()
		for i, e := range entries {
			status := "applied"
			if !e.Success {
				status = "FAILED"
			}
			if !e.UndoneAt.IsZero() {
				if e.UndoSuccess {
					status = "undone"
				} else {
					status = "undo FAILED"
				}
			}
			fmt.Printf("  %d. [%s] %-25s %s (%s)\n", i+1, status, e.ActionID, e.Title, e.AppliedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		fmt.Println("Use: nvcheckup undo --id <action-id> to reverse a change")
		return
	}

	// Find the entry to undo
	var target *types.ChangeJournalEntry
	for i := range entries {
		if entries[i].ActionID == *id && entries[i].Success && entries[i].UndoneAt.IsZero() {
			target = &entries[i]
			break
		}
	}

	if target == nil {
		fmt.Fprintf(os.Stderr, "No undoable entry found for action: %s\n", *id)
		os.Exit(types.ExitError)
	}

	fmt.Printf("Undoing: %s (applied %s)\n", target.Title, target.AppliedAt.Format("2006-01-02 15:04:05"))
	fmt.Print("Proceed? (yes/no): ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "yes" && answer != "y" {
		fmt.Println("Aborted.")
		return
	}

	if err := engine.Undo(*target); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}

	fmt.Println("Successfully undone.")
}

func networkTestCmd(args []string) {
	fs := flag.NewFlagSet("network-test", flag.ExitOnError)
	timeout := fs.Int("timeout", 30, "Command timeout in seconds")
	fs.Parse(args)

	printBanner()
	fmt.Println("Running network diagnostics...")
	fmt.Println()

	netInfo, netErrs := common.CollectNetworkInfo(*timeout)

	fmt.Printf("  Interface:    %s (%s)\n", cliValueOrNA(netInfo.InterfaceName), cliValueOrNA(netInfo.InterfaceType))
	if netInfo.InterfaceType == "wifi" {
		if netInfo.WifiBand != "" {
			fmt.Printf("  WiFi Band:    %s\n", netInfo.WifiBand)
		}
		if netInfo.WifiSignalDBM != 0 {
			fmt.Printf("  WiFi Signal:  %d dBm\n", netInfo.WifiSignalDBM)
		}
	}
	fmt.Printf("  Latency:      %.2f ms\n", netInfo.LatencyMs)
	fmt.Printf("  Jitter:       %.2f ms\n", netInfo.JitterMs)
	fmt.Printf("  Packet Loss:  %.1f%%\n", netInfo.PacketLossPct)
	fmt.Printf("  DNS Time:     %.2f ms\n", netInfo.DNSTimeMs)

	if len(netInfo.Hops) > 0 {
		fmt.Println()
		fmt.Println("  Traceroute:")
		for _, hop := range netInfo.Hops {
			if hop.Loss {
				fmt.Printf("    %2d. * (timeout)\n", hop.Number)
			} else {
				fmt.Printf("    %2d. %-16s %.2f ms\n", hop.Number, hop.Address, hop.LatencyMs)
			}
		}
	}

	if len(netErrs) > 0 {
		fmt.Println()
		fmt.Println("  Notes:")
		for _, e := range netErrs {
			fmt.Printf("    [%s] %s\n", e.Collector, e.Error)
		}
	}

	fmt.Println()

	if netInfo.PacketLossPct > 5 {
		fmt.Println("  CRITICAL: High packet loss detected.")
	} else if netInfo.PacketLossPct > 1 {
		fmt.Println("  WARNING: Packet loss detected.")
	}
	if netInfo.JitterMs > 15 {
		fmt.Println("  WARNING: High jitter may cause lag in games/streaming.")
	}
	if netInfo.LatencyMs > 100 {
		fmt.Println("  WARNING: High latency detected.")
	}
	if netInfo.DNSTimeMs > 100 {
		fmt.Println("  INFO: DNS resolution is slow. Consider using 1.1.1.1 or 8.8.8.8.")
	}
	if netInfo.PacketLossPct == 0 && netInfo.JitterMs < 15 && netInfo.LatencyMs < 100 {
		fmt.Println("  Network appears healthy.")
	}
}

func cliValueOrNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
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
  run           Run diagnostics and generate a report
  fix           List and apply safe remediation fixes
  undo          Reverse a previously applied fix
  network-test  Run standalone network diagnostics
  snapshot      Create a timestamped JSON snapshot
  compare       Compare two snapshots
  doctor        Interactive guided diagnostic mode
  self-test     Verify environment, dependencies, and permissions
  version       Show version information

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
