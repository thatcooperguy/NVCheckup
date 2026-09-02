// Package doctor provides an interactive guided diagnostic mode.
package doctor

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/core"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// RunInteractive asks a few questions, runs the matching diagnostics and
// returns the same exit code 'nvcheckup run' would: 0 clean, 1 warnings,
// 2 critical findings, 3 tool error.
func RunInteractive() int {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("NVCheckup Doctor — Interactive Diagnostic Guide")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("I'll ask a few questions to run the most relevant checks.")
	fmt.Println("Nothing is changed on your system; this only reads and reports.")
	fmt.Println()

	// Question 1: primary use case
	fmt.Println("1. What's your primary use case?")
	fmt.Println("   a) Gaming")
	fmt.Println("   b) AI / Machine Learning / CUDA development")
	fmt.Println("   c) Creator (video editing, 3D, rendering)")
	fmt.Println("   d) Streaming / encoding")
	fmt.Println("   e) General / Not sure")
	fmt.Print("   > ")
	useCase := readInput(reader)

	// Question 2: main issue
	fmt.Println()
	fmt.Println("2. What issue are you experiencing?")
	fmt.Println("   a) Crashes / black screens / driver errors")
	fmt.Println("   b) Poor performance / stuttering")
	fmt.Println("   c) GPU not detected / CUDA not working")
	fmt.Println("   d) Encoding / streaming issues")
	fmt.Println("   e) Not sure / multiple issues")
	fmt.Print("   > ")
	issue := readInput(reader)

	mode := chooseMode(useCase, issue, runtime.GOOS)

	// Question 3: recent changes (recorded for the user; not yet used to steer collectors)
	fmt.Println()
	fmt.Println("3. Did this start after a recent change?")
	fmt.Println("   a) Windows/Linux update")
	fmt.Println("   b) Driver update")
	fmt.Println("   c) New hardware")
	fmt.Println("   d) Software install")
	fmt.Println("   e) No recent changes / not sure")
	fmt.Print("   > ")
	_ = readInput(reader)

	// Question 4: extended logs
	fmt.Println()
	fmt.Println("4. Include extended system logs? (more detail, but larger report)")
	fmt.Println("   a) Yes")
	fmt.Println("   b) No (default)")
	fmt.Print("   > ")
	includeLogs := isYes(readInput(reader), "a")

	// Question 5: network probes (opt-in because they contact external hosts)
	fmt.Println()
	fmt.Println("5. Run network probes (ping/traceroute to 1.1.1.1 and a DNS lookup)?")
	fmt.Println("   They take 30-60 s and are only useful for lag or streaming problems.")
	fmt.Println("   a) Yes")
	fmt.Println("   b) No (default)")
	fmt.Print("   > ")
	networkTest := isYes(readInput(reader), "a")

	// Question 6: output format
	fmt.Println()
	fmt.Println("6. Output format?")
	fmt.Println("   a) Text report only (default)")
	fmt.Println("   b) Text + JSON + Markdown + Zip bundle")
	fmt.Print("   > ")
	fullOutput := strings.EqualFold(readInput(reader), "b")

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("Running diagnostics in %s mode...\n", mode)
	fmt.Println()

	cfg := types.RunConfig{
		Mode:        mode,
		OutDir:      ".",
		Zip:         fullOutput,
		JSON:        fullOutput,
		Markdown:    fullOutput,
		Verbose:     false,
		Timeout:     30,
		Redact:      true,
		IncludeLogs: includeLogs,
		NetworkTest: networkTest,
	}

	report, err := core.Run(cfg, false, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return types.ExitError
	}

	files, err := core.WriteReport(report, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		return types.ExitError
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println(report.SummaryBlock)

	if len(report.TopIssues) > 0 {
		fmt.Println("Top Issues Found:")
		for i, issue := range report.TopIssues {
			fmt.Printf("  %d. %s\n", i+1, issue)
		}
		fmt.Println()
	}

	if len(report.NextSteps) > 0 {
		fmt.Println("Recommended Next Steps:")
		for i, step := range report.NextSteps {
			fmt.Printf("  %d. %s\n", i+1, step)
		}
		fmt.Println()
	}

	fmt.Println("Reports generated:")
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	fmt.Println()
	fmt.Println("You can share the report.txt file in support forums or GitHub issues.")
	fmt.Println("PII has been automatically redacted.")

	return core.ExitCodeFor(report)
}

// chooseMode maps the two answers to a run mode. The issue answer can
// override the use case: crashes need the Windows event logs that only the
// gaming/full collectors gather, and "GPU not detected" needs the CUDA and
// kernel-module checks (ai on Linux) or the widest net (full on Windows).
func chooseMode(useCase, issue, goos string) types.RunMode {
	mode := types.ModeFull
	switch strings.ToLower(strings.TrimSpace(useCase)) {
	case "a", "gaming", "games":
		mode = types.ModeGaming
	case "b", "ai", "ml", "cuda":
		mode = types.ModeAI
	case "c", "creator", "creative":
		mode = types.ModeCreator
	case "d", "streaming", "stream", "encoding":
		mode = types.ModeStreaming
	}

	switch strings.ToLower(strings.TrimSpace(issue)) {
	case "a", "crash", "crashes", "driver":
		// Driver resets, nvlddmkm and WHEA events are only collected in
		// gaming-family modes; an AI user with crashes still needs them.
		if mode == types.ModeAI {
			mode = types.ModeGaming
		}
	case "c", "detect", "not detected":
		if goos == "linux" {
			mode = types.ModeAI
		} else {
			mode = types.ModeFull
		}
	}
	return mode
}

// isYes accepts the lettered option or any common affirmative.
func isYes(answer, letter string) bool {
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == letter || a == "y" || a == "yes"
}

func readInput(reader *bufio.Reader) string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}
