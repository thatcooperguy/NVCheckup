// NVCheckup — Cross-platform NVIDIA diagnostic CLI tool.
// Unofficial community tool, not affiliated with NVIDIA Corporation.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/bundle"
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/core"
	"github.com/thatcooperguy/nvcheckup/internal/doctor"
	"github.com/thatcooperguy/nvcheckup/internal/remediate"
	"github.com/thatcooperguy/nvcheckup/internal/selftest"
	"github.com/thatcooperguy/nvcheckup/internal/snapshot"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const compareUsage = "Usage: nvcheckup compare [--out DIR] [--md] <a.json> <b.json>"

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
	verbose := fs.Bool("verbose", false, "Print per-phase timings and collector notes as they happen")
	network := fs.Bool("network", false, "Run network probes (ping/traceroute to 1.1.1.1, DNS lookup of google.com); off by default")
	timeout := fs.Int("timeout", 30, "Timeout in seconds for each system command")
	redactFlag := fs.Bool("redact", true, "Enable PII redaction (default: true)")
	noRedact := fs.Bool("no-redact", false, "Disable PII redaction (not recommended for sharing)")
	includeLogs := fs.Bool("include-logs", false, "Linux only: include journalctl/dmesg snippets in the report")

	fs.Parse(args)
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Unexpected argument(s): %s\n", strings.Join(fs.Args(), " "))
		fmt.Fprintln(os.Stderr, "Usage: nvcheckup run [--mode MODE] [--out DIR] [--json] [--md] [--zip] [--network] [--verbose] [--timeout N] [--no-redact] [--include-logs]")
		os.Exit(types.ExitError)
	}

	m := types.RunMode(strings.ToLower(*mode))
	switch m {
	case types.ModeGaming, types.ModeAI, types.ModeCreator, types.ModeStreaming, types.ModeFull:
		// ok
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Use: gaming, ai, creator, streaming, full\n", *mode)
		os.Exit(types.ExitError)
	}

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
		Timeout:     *timeout,
		Redact:      redact,
		IncludeLogs: *includeLogs,
		NetworkTest: *network,
	}

	printBanner()

	report, err := core.Run(cfg, *verbose, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}

	files, err := core.WriteReport(report, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		os.Exit(types.ExitError)
	}

	fmt.Println()
	for _, f := range files {
		fmt.Printf("  Written: %s\n", f)
	}

	if cfg.Zip {
		zipPath, err := bundle.CreateZip(cfg.OutDir, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating zip: %v\n", err)
		} else {
			fmt.Printf("  Bundle:  %s\n", zipPath)
		}
	}

	fmt.Println()
	fmt.Println(report.SummaryBlock)

	if len(report.TopIssues) > 0 {
		fmt.Println("Top Issues:")
		for i, issue := range report.TopIssues {
			fmt.Printf("  %d. %s\n", i+1, issue)
		}
		fmt.Println()
	}

	os.Exit(core.ExitCodeFor(report))
}

func snapshotCmd(args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	outDir := fs.String("out", ".", "Output directory")
	timeout := fs.Int("timeout", 30, "Command timeout in seconds")
	noRedact := fs.Bool("no-redact", false, "Disable PII redaction (hostname, username, home paths)")
	fs.Parse(args)

	printBanner()
	if *noRedact {
		fmt.Println("Creating snapshot (redaction DISABLED; do not share publicly)...")
	} else {
		fmt.Println("Creating snapshot...")
	}

	path, err := snapshot.CreateWithOptions(*outDir, *timeout, !*noRedact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}
	fmt.Printf("Snapshot saved: %s\n", path)
}

// compareCmd requires flags BEFORE the two positional paths: Go's flag package
// stops parsing at the first non-flag argument, so "compare a.json b.json --md"
// would silently treat --md as a third file. We reject that instead.
func compareCmd(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, compareUsage)
		fs.PrintDefaults()
	}
	outDir := fs.String("out", ".", "Directory for comparison.txt / comparison.md; the file is written when --md or --out is given (default: current directory)")
	doMD := fs.Bool("md", false, "Output as markdown and write comparison.md into --out")
	fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) != 2 {
		fmt.Fprintln(os.Stderr, compareUsage)
		if len(remaining) > 2 {
			fmt.Fprintln(os.Stderr, "Flags must come before the two snapshot paths.")
		}
		os.Exit(types.ExitError)
	}

	outSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "out" {
			outSet = true
		}
	})

	printBanner()
	if err := snapshot.Compare(remaining[0], remaining[1], compareWriteDir(*outDir, *doMD, outSet), *doMD); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}
}

// compareWriteDir decides where "compare" writes its comparison file. The
// console output is always printed; a file is written only when the user asked
// for markdown or named an output directory, and "." (the flag default) means
// the current directory rather than "write nothing".
func compareWriteDir(outDir string, markdown, outSet bool) string {
	if !markdown && !outSet {
		return ""
	}
	if outDir == "" {
		return "."
	}
	return outDir
}

func doctorCmd(args []string) {
	printBanner()
	os.Exit(doctor.RunInteractive())
}

func selfTestCmd(args []string) {
	printBanner()
	os.Exit(selftest.Run())
}

// defaultJournalDir is <UserConfigDir>/nvcheckup, e.g. %APPDATA%\nvcheckup on
// Windows or ~/.config/nvcheckup on Linux. It is per-user and survives the
// report directory being deleted, which is what an undo journal needs.
//
// On Linux/macOS a fix usually runs as "sudo nvcheckup fix", where
// os.UserConfigDir resolves to root's home. The journal is derived from
// SUDO_USER's home instead so that a later "nvcheckup undo" (or "undo" list)
// by the same user finds the entry. Plain root without SUDO_USER keeps
// os.UserConfigDir. Windows is unchanged.
func defaultJournalDir() string {
	if dir, ok := sudoUserJournalDir(runtime.GOOS, os.Geteuid(), os.Getenv("SUDO_USER"), user.Lookup); ok {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, "nvcheckup")
}

// sudoUserJournalDir returns the journal directory for the invoking sudo user
// when the process is root via sudo on Linux/macOS. ok is false whenever the
// default os.UserConfigDir behaviour should be used instead.
func sudoUserJournalDir(goos string, euid int, sudoUser string, lookup func(string) (*user.User, error)) (string, bool) {
	if goos == "windows" || euid != 0 || sudoUser == "" || sudoUser == "root" {
		return "", false
	}
	u, err := lookup(sudoUser)
	if err != nil || u == nil || u.HomeDir == "" {
		return "", false
	}
	return journalDirForHome(goos, u.HomeDir), true
}

// journalDirForHome mirrors os.UserConfigDir for a given home directory:
// $HOME/.config on Linux (and other Unix) and Library/Application Support on
// macOS.
func journalDirForHome(goos, home string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "nvcheckup")
	}
	return filepath.Join(home, ".config", "nvcheckup")
}

// resolveJournalDir applies precedence --journal > --out (deprecated alias) >
// default. It only computes the path: listing fixes, previewing with
// --dry-run and listing journal entries are read-only and must not leave an
// empty directory behind. ensureJournalDir creates it right before a change
// is applied or undone.
func resolveJournalDir(journalFlag, outFlag string) string {
	dir := journalFlag
	if dir == "" && outFlag != "" {
		fmt.Fprintln(os.Stderr, "Note: --out is deprecated for fix/undo; use --journal DIR.")
		dir = outFlag
	}
	if dir == "" {
		dir = defaultJournalDir()
	}
	return dir
}

// ensureJournalDir creates the journal directory with owner-only permissions.
// It is called immediately before engine.Apply or engine.Undo, never for
// list or dry-run invocations.
func ensureJournalDir(dir string) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create journal directory %s: %v\n", dir, err)
		os.Exit(types.ExitError)
	}
}

// elevationPhrases are the exact phrases that identify an elevation failure:
// the engine's own errNotElevated text, the "Access is denied" that reg.exe and
// powercfg print from a non-elevated terminal, and the POSIX "permission
// denied". Matching is deliberately narrow: a bare "root" or "administrator"
// also appears in ordinary paths such as /root/.config and must not trigger
// the hint.
var elevationPhrases = []string{
	"elevated privileges",
	"access is denied",
	"access denied",
	"permission denied",
	"requires administrator",
	"run as root",
}

// isElevationError recognises an elevation failure so the CLI can give a
// one-line hint instead of a raw error.
func isElevationError(msg string) bool {
	l := strings.ToLower(msg)
	for _, p := range elevationPhrases {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// elevationHint is the platform-specific "how to elevate" line.
func elevationHint(goos, command string) string {
	if goos == "windows" {
		return "Hint: Re-run from an elevated (Administrator) terminal."
	}
	return fmt.Sprintf("Hint: Re-run with sudo (e.g. sudo nvcheckup %s).", command)
}

func printElevationHint(msg string) {
	printElevationHintFor(msg, "fix --id ...")
}

func printElevationHintFor(msg, command string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	fmt.Fprintln(os.Stderr, elevationHint(runtime.GOOS, command))
}

func fixCmd(args []string) {
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	id := fs.String("id", "", "Remediation action ID to apply")
	dryRun := fs.Bool("dry-run", false, "Preview changes without applying")
	journalDir := fs.String("journal", "", "Directory for the change journal (default: <UserConfigDir>/nvcheckup)")
	outDir := fs.String("out", "", "Deprecated alias for --journal")
	all := fs.Bool("all", false, "Describe every available fix")
	fs.Parse(args)

	printBanner()

	dir := resolveJournalDir(*journalDir, *outDir)
	engine := remediate.NewEngine(nil, dir, *dryRun)
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
		fmt.Println()
		fmt.Printf("Journal: %s\n", remediate.NewJournal(dir).Path())
		if !*all {
			fmt.Println()
			fmt.Println("Use: nvcheckup fix --id <action-id> to apply a fix")
			fmt.Println("     nvcheckup fix --id <action-id> --dry-run to preview")
		}
		return
	}

	var target *types.RemediationAction
	for i := range actions {
		if actions[i].ID == *id {
			target = &actions[i]
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

	// Check elevation BEFORE asking, so the user is not prompted for a fix
	// that is guaranteed to fail.
	if target.NeedsAdmin && !remediate.IsElevated() {
		printElevationHint(fmt.Sprintf("action %q requires elevated (Administrator/root) privileges", target.ID))
		os.Exit(types.ExitError)
	}

	fmt.Print("Apply this fix? (yes/no): ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "yes" && answer != "y" {
		fmt.Println("Aborted.")
		return
	}

	ensureJournalDir(dir)
	result, err := engine.Apply(*target)
	if err != nil {
		if isElevationError(err.Error()) {
			printElevationHint(err.Error())
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(types.ExitError)
	}

	if result.Success {
		fmt.Printf("Applied: %s\n", result.Output)
		fmt.Printf("Journaled to: %s\n", remediate.NewJournal(dir).Path())
		if target.NeedsReboot {
			fmt.Println("  A reboot is required for this change to take effect.")
		}
		fmt.Printf("  Undo with: nvcheckup undo --id %s\n", target.ID)
		return
	}

	failure := result.Output
	if result.Error != "" {
		failure = result.Error
	}
	if isElevationError(failure) {
		printElevationHint(failure)
	} else {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", failure)
	}
	os.Exit(types.ExitError)
}

func undoCmd(args []string) {
	fs := flag.NewFlagSet("undo", flag.ExitOnError)
	id := fs.String("id", "", "Action ID to undo (omit to list journal entries)")
	journalDir := fs.String("journal", "", "Directory containing the change journal (default: <UserConfigDir>/nvcheckup)")
	outDir := fs.String("out", "", "Deprecated alias for --journal")
	fs.Parse(args)

	printBanner()

	dir := resolveJournalDir(*journalDir, *outDir)
	engine := remediate.NewEngine(nil, dir, false)
	journal := remediate.NewJournal(dir)
	entries, err := journal.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading change journal: %v\n", err)
		os.Exit(types.ExitError)
	}

	// List mode
	if *id == "" {
		fmt.Printf("Journal: %s\n", journal.Path())
		fmt.Println()
		if len(entries) == 0 {
			fmt.Println("No changes recorded in the journal.")
			return
		}
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
					status = "undo FAILED (retryable)"
				}
			}
			fmt.Printf("  %d. [%s] %-25s %s (%s)\n", i+1, status, e.ActionID, e.Title, e.AppliedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		fmt.Println("Use: nvcheckup undo --id <action-id> to reverse a change")
		return
	}

	target := newestUndoable(entries, *id)
	if target == nil {
		fmt.Fprintf(os.Stderr, "No undoable entry found for action: %s\n", *id)
		fmt.Fprintf(os.Stderr, "Journal: %s\n", journal.Path())
		os.Exit(types.ExitError)
	}

	fmt.Printf("Undoing: %s (applied %s)\n", target.Title, target.AppliedAt.Format("2006-01-02 15:04:05"))

	// Check elevation BEFORE asking, exactly like fixCmd, so the user is not
	// prompted for an undo that is guaranteed to fail.
	if def, ok := remediate.ActionByID(target.ActionID); ok && def.NeedsAdmin && !remediate.IsElevated() {
		printElevationHintFor(fmt.Sprintf("undoing %q requires elevated (Administrator/root) privileges", target.ActionID), "undo --id "+target.ActionID)
		os.Exit(types.ExitError)
	}

	fmt.Print("Proceed? (yes/no): ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "yes" && answer != "y" {
		fmt.Println("Aborted.")
		return
	}

	ensureJournalDir(dir)
	if err := engine.Undo(*target); err != nil {
		if isElevationError(err.Error()) {
			printElevationHintFor(err.Error(), "undo --id "+target.ActionID)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(types.ExitError)
	}

	fmt.Println("Successfully undone.")
	fmt.Printf("Journal: %s\n", journal.Path())
}

// newestUndoable returns the most recent successful journal entry for id
// that has not been successfully undone. Engine.Undo stamps UndoneAt on every
// attempt, so an entry whose undo FAILED still qualifies and can be retried
// (all undo operations are idempotent). Iterating from the end matters when
// the same fix was applied, undone and applied again: the old (already
// undone) entry must not win.
func newestUndoable(entries []types.ChangeJournalEntry, id string) *types.ChangeJournalEntry {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.ActionID == id && e.Success && (e.UndoneAt.IsZero() || !e.UndoSuccess) {
			return &entries[i]
		}
	}
	return nil
}

func networkTestCmd(args []string) {
	fs := flag.NewFlagSet("network-test", flag.ExitOnError)
	timeout := fs.Int("timeout", 30, "Command timeout in seconds")
	fs.Parse(args)

	printBanner()
	fmt.Println("Running network diagnostics (ping/traceroute to 1.1.1.1, DNS lookup of google.com)...")
	fmt.Println("This takes 30-60 s.")
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

	for _, l := range networkVerdictLines(&netInfo) {
		fmt.Println("  " + l)
	}
}

// networkPingSampled reports whether ping produced any samples. Hops alone
// (traceroute worked, ping did not) are not evidence about latency or loss.
func networkPingSampled(n *types.NetworkInfo) bool {
	return n.LatencyMs > 0 || n.PacketLossPct > 0
}

// networkVerdictLines turns the probe results into the one-line verdicts the
// network-test command prints. "Network appears healthy" requires real ping
// samples; when only traceroute produced data the command says so instead of
// declaring 0.0 ms latency healthy.
func networkVerdictLines(n *types.NetworkInfo) []string {
	var lines []string
	pinged := networkPingSampled(n)
	if pinged {
		if n.PacketLossPct > 5 {
			lines = append(lines, "CRITICAL: High packet loss detected.")
		} else if n.PacketLossPct > 1 {
			lines = append(lines, "WARNING: Packet loss detected.")
		}
		if n.JitterMs > 15 {
			lines = append(lines, "WARNING: High jitter may cause lag in games/streaming.")
		}
		if n.LatencyMs > 100 {
			lines = append(lines, "WARNING: High latency detected.")
		}
	} else if len(n.Hops) > 0 {
		lines = append(lines, "INFO: Ping produced no samples; latency, jitter and loss could not be measured.")
	}
	if n.DNSTimeMs > 100 {
		lines = append(lines, "INFO: DNS resolution is slow. Consider using 1.1.1.1 or 8.8.8.8.")
	}
	if pinged && n.PacketLossPct == 0 && n.JitterMs < 15 && n.LatencyMs < 100 {
		lines = append(lines, "Network appears healthy.")
	}
	return lines
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
  run           Run read-only diagnostics and generate a report
  snapshot      Create a timestamped JSON snapshot (redacted by default)
  compare       Compare two snapshots
  doctor        Interactive guided diagnostic mode
  fix           List and apply safe, reversible fixes (asks for confirmation)
  undo          Reverse a previously applied fix using the change journal
  network-test  Run standalone network probes (ping/traceroute to 1.1.1.1, DNS)
  self-test     Verify environment, dependencies, and permissions
  version       Show version information
  help          Show this help

run [flags]
  --mode MODE      gaming, ai, creator, streaming, full (default: full)
  --out DIR        Output directory (default: current directory)
  --json           Also write report.json (includes schema_version)
  --md             Also write report.md (GitHub/Reddit-ready)
  --zip            Bundle the report files into a zip
  --network        Run network probes (off by default; takes 30-60 s)
  --verbose        Print per-phase timings and collector notes
  --timeout N      Per-command timeout in seconds (default: 30)
  --redact         Enable PII redaction (default: true)
  --no-redact      Disable PII redaction (not recommended for sharing)
  --include-logs   Linux only: include journalctl/dmesg snippets in the report

snapshot [flags]
  --out DIR        Output directory (default: current directory)
  --timeout N      Per-command timeout in seconds (default: 30)
  --no-redact      Disable PII redaction

compare [--out DIR] [--md] <a.json> <b.json>
  Flags must come before the two snapshot paths. The comparison is always
  printed; a file is written when --md or --out is given.
  --out DIR        Write comparison.txt / comparison.md to DIR (default: current directory)
  --md             Output as markdown and write comparison.md

fix [flags]
  (no flags)       List available fixes
  --id ID          Apply the fix with this ID (asks yes/no first)
  --dry-run        Preview what --id would change without applying it
  --all            Describe every available fix
  --journal DIR    Change journal directory (default: <UserConfigDir>/nvcheckup)
  --out DIR        Deprecated alias for --journal

undo [flags]
  (no flags)       List change journal entries
  --id ID          Undo the newest successful entry for ID (a failed undo can be retried)
  --journal DIR    Change journal directory (default: <UserConfigDir>/nvcheckup)
  --out DIR        Deprecated alias for --journal

network-test [--timeout N]

Exit codes: 0 no issues, 1 warnings, 2 critical findings, 3 tool error.

Examples:
  nvcheckup run --mode gaming --zip
  nvcheckup run --mode ai --json --md
  nvcheckup run --mode full --network --json --out ./reports
  nvcheckup snapshot --out ./snapshots
  nvcheckup compare --md before.json after.json
  nvcheckup fix
  nvcheckup fix --id set-high-performance --dry-run
  nvcheckup undo --id set-high-performance
  nvcheckup doctor
  nvcheckup self-test
`, types.Version, types.Disclaimer)
}
