// Package doctor provides an interactive guided diagnostic mode.
package doctor

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/core"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// RunInteractive runs the interactive doctor mode.
func RunInteractive() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("NVCheckup Doctor — Interactive Diagnostic Guide")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("I'll ask a few questions to run the most relevant checks.")
	fmt.Println()

	// Question 1: What's your main use case?
	fmt.Println("1. What's your primary use case?")
	fmt.Println("   a) Gaming")
	fmt.Println("   b) AI / Machine Learning / CUDA development")
	fmt.Println("   c) Streaming / Content Creation")
	fmt.Println("   d) General / Not sure")
	fmt.Print("   > ")
	useCase := readInput(reader)

	mode := types.ModeFull
	switch strings.ToLower(useCase) {
	case "a", "gaming":
		mode = types.ModeGaming
	case "b", "ai", "ml", "cuda":
		mode = types.ModeAI
	case "c", "streaming", "creator":
		mode = types.ModeStreaming
	default:
		mode = types.ModeFull
	}

	// Question 2: What's your main issue?
	fmt.Println()
	fmt.Println("2. What issue are you experiencing?")
	fmt.Println("   a) Crashes / black screens / driver errors")
	fmt.Println("   b) Poor performance / stuttering")
	fmt.Println("   c) GPU not detected / CUDA not working")
	fmt.Println("   d) Encoding / streaming issues")
	fmt.Println("   e) Not sure / multiple issues")
	fmt.Print("   > ")
	issue := readInput(reader)
	_ = issue // Used for future targeted analysis

	// Question 3: Recent changes?
	fmt.Println()
	fmt.Println("3. Did this start after a recent change?")
	fmt.Println("   a) Windows/Linux update")
	fmt.Println("   b) Driver update")
	fmt.Println("   c) New hardware")
	fmt.Println("   d) Software install")
	fmt.Println("   e) No recent changes / not sure")
	fmt.Print("   > ")
	recentChange := readInput(reader)
	_ = recentChange

	// Question 4: Include logs?
	fmt.Println()
	fmt.Println("4. Include extended system logs? (more detail, but larger report)")
	fmt.Println("   a) Yes")
	fmt.Println("   b) No (default)")
	fmt.Print("   > ")
	logsAnswer := readInput(reader)
	includeLogs := strings.ToLower(logsAnswer) == "a" || strings.ToLower(logsAnswer) == "yes"

	// Question 5: Output format
	fmt.Println()
	fmt.Println("5. Output format?")
	fmt.Println("   a) Text report only (default)")
	fmt.Println("   b) Text + JSON + Markdown + Zip bundle")
	fmt.Print("   > ")
	formatAnswer := readInput(reader)
	fullOutput := strings.ToLower(formatAnswer) == "b"

	// Run diagnostics
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println("Running diagnostics...")
	fmt.Println()

	cfg := types.RunConfig{
		Mode:        mode,
		OutDir:      ".",
		Zip:         fullOutput,
		JSON:        fullOutput,
		Markdown:    fullOutput,
		Verbose:     false,
		NoAdmin:     false,
		Timeout:     30,
		Redact:      true,
		IncludeLogs: includeLogs,
	}

	report, err := core.Run(cfg, false, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(types.ExitError)
	}

	files, err := core.WriteReport(report, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
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
}

func readInput(reader *bufio.Reader) string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}
