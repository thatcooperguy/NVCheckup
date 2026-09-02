package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/llmplan"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const llmPlanUsage = "Usage: nvcheckup llm-plan [--model NAME | --hf-config config.json --params B | --params B --layers N --kv-heads N --head-dim N] [--quant Q] [--context N] [--concurrency N] [--profile P] [--runtime R] [--kv-dtype K] [--nodes 1|2] [--headroom-gib N] [--memory-gib N] [--json] [--md] [--out DIR] [--report FILE] [--list-models]"

// llmPlanFlags are the command-level flags that are not sizing inputs.
type llmPlanFlags struct {
	listModels bool
	json       bool
	md         bool
	out        string
	outSet     bool
	report     string
}

// parseLLMPlanFlags follows the flag style of the other commands. Aliases:
// --params-b for --params and --ctx for --context (brief), --active-params.
func parseLLMPlanFlags(args []string, stderr io.Writer) (llmplan.Options, llmPlanFlags, error) {
	o := llmplan.DefaultOptions()
	var f llmPlanFlags
	fs := flag.NewFlagSet("llm-plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, llmPlanUsage)
		fs.PrintDefaults()
	}
	fs.StringVar(&o.Model, "model", "", "Model from knowledge/models.json (id or alias; see --list-models)")
	fs.StringVar(&o.HFConfig, "hf-config", "", "Local Hugging Face config.json to size a model not in the catalogue (offline; needs --params)")
	fs.Float64Var(&o.ParamsB, "params", 0, "Custom shape: total parameters in billions")
	fs.Float64Var(&o.ParamsB, "params-b", 0, "Alias for --params")
	fs.Float64Var(&o.ActiveParamsB, "active-params", 0, "Custom shape: active parameters in billions (MoE; default = --params)")
	fs.IntVar(&o.Layers, "layers", 0, "Custom shape: num_hidden_layers")
	fs.IntVar(&o.KVHeads, "kv-heads", 0, "Custom shape: num_key_value_heads")
	fs.IntVar(&o.HeadDim, "head-dim", 0, "Custom shape: head_dim (or give --hidden and --heads)")
	fs.IntVar(&o.Hidden, "hidden", 0, "Custom shape: hidden_size (head_dim = hidden/heads)")
	fs.IntVar(&o.Heads, "heads", 0, "Custom shape: num_attention_heads")
	fs.StringVar(&o.Quant, "quant", "", "Weight format: "+llmplan.QuantNames()+" (default: the checkpoint's native format)")
	var context string
	fs.StringVar(&context, "context", "", "Context length per stream in tokens, e.g. 32768 or 32K (default from --profile)")
	fs.StringVar(&context, "ctx", "", "Alias for --context")
	fs.IntVar(&o.Concurrency, "concurrency", 0, "Concurrent streams; agent = 1 + subagents (default from --profile)")
	fs.StringVar(&o.Profile, "profile", "chat", "Workload profile: "+llmplan.ProfileNames())
	fs.StringVar(&o.Runtime, "runtime", "auto", "Runtime: vllm|trtllm|sglang|llamacpp|ollama|auto")
	fs.StringVar(&o.KVDtype, "kv-dtype", "auto", "KV cache dtype: auto|f16|fp8|q8_0 (fp8 for vLLM only when given explicitly)")
	fs.IntVar(&o.Nodes, "nodes", 1, "Target: 1 = this node, 2 = a cluster of two Sparks")
	fs.Float64Var(&o.HeadroomGiB, "headroom-gib", -1, "OS floor F in GiB (default: 8 headless Linux, 10 desktop/Windows; 0 disables)")
	fs.Float64Var(&o.MemoryGiB, "memory-gib", 0, "Override the memory pool total in GiB (MemAvailable then unknown)")
	fs.IntVar(&o.Timeout, "timeout", 30, "Per-command timeout in seconds for the collectors")
	fs.BoolVar(&f.listModels, "list-models", false, "List the shipped model shapes and exit")
	fs.BoolVar(&f.json, "json", false, "Also write plan.json into --out")
	fs.BoolVar(&f.md, "md", false, "Also write plan.md into --out")
	fs.StringVar(&f.out, "out", ".", "Output directory for plan.txt / plan.json / plan.md (files are written when --out, --json or --md is given)")
	fs.StringVar(&f.report, "report", "", "Use an existing report.json instead of collecting (offline planning)")
	if err := fs.Parse(args); err != nil {
		return o, f, err
	}
	if fs.NArg() > 0 {
		return o, f, fmt.Errorf("unexpected argument(s): %s", strings.Join(fs.Args(), " "))
	}
	if context != "" {
		n, ok := llmplan.ParseTokens(context)
		if !ok {
			return o, f, fmt.Errorf("invalid --context %q (use tokens, e.g. 32768 or 32K)", context)
		}
		o.Context = n
	}
	var memorySet, headroomSet bool
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "out":
			f.outSet = true
		case "memory-gib":
			memorySet = true
		case "headroom-gib":
			headroomSet = true
		}
	})
	// Out-of-range numbers are errors (exit 3), never silently ignored.
	if memorySet && o.MemoryGiB <= 0 {
		return o, f, fmt.Errorf("--memory-gib must be > 0 (got %g)", o.MemoryGiB)
	}
	if headroomSet && o.HeadroomGiB < 0 {
		return o, f, fmt.Errorf("--headroom-gib must be >= 0 (got %g)", o.HeadroomGiB)
	}
	if o.Concurrency < 0 {
		return o, f, fmt.Errorf("--concurrency must be > 0 (got %d)", o.Concurrency)
	}
	if o.Nodes < 1 || o.Nodes > 2 {
		return o, f, fmt.Errorf("--nodes must be 1 or 2 (got %d)", o.Nodes)
	}
	if o.Timeout <= 0 {
		return o, f, fmt.Errorf("--timeout must be > 0 (got %d)", o.Timeout)
	}
	return o, f, nil
}

// llmPlanNeedsPrompt reports whether no model input was given on the command line.
func llmPlanNeedsPrompt(o llmplan.Options) bool {
	return o.Model == "" && o.HFConfig == "" && o.ParamsB == 0
}

func llmPlanCmd(args []string) {
	os.Exit(runLLMPlan(args, os.Stdin, os.Stdout, os.Stderr, llmplan.IsTerminal(os.Stdin), runtime.GOOS))
}

// runLLMPlan is llmPlanCmd without the process exit, for tests. goos is the
// OS the plan is for; a --report file overrides it with the OS the report was
// collected on.
func runLLMPlan(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool, goos string) int {
	o, f, err := parseLLMPlanFlags(args, stderr)
	if err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			fmt.Fprintln(stderr, llmPlanUsage)
		}
		return types.ExitError
	}
	if f.listModels {
		fmt.Fprint(stdout, llmplan.ListModelsText(llmplan.Catalogue()))
		return types.ExitOK
	}
	o.GOOS = goos

	printBannerTo(stdout)

	reader := bufio.NewReader(stdin)
	if llmPlanNeedsPrompt(o) {
		if !interactive {
			fmt.Fprintln(stderr, "Error: --model (or --hf-config/--params) is required when stdin is not a terminal.")
			fmt.Fprintln(stderr, llmPlanUsage)
			return types.ExitError
		}
		if err := llmplan.Prompt(reader, stdout, &o); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return types.ExitError
		}
	}

	var report *types.Report
	if f.report != "" {
		report, err = loadReportJSON(f.report)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return types.ExitError
		}
		o.Offline = true // size the saved report, never the machine running llm-plan
		switch report.Metadata.Platform {
		case "linux", "wsl":
			o.GOOS = "linux"
		case "windows":
			o.GOOS = "windows"
		}
	} else {
		fmt.Fprintln(stdout, "Collecting the read-only report (AI mode, no network probes)...")
		report, err = llmplan.CollectReport(o.Timeout, func(msg string) { fmt.Fprintln(stdout, msg) })
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return types.ExitError
		}
		fmt.Fprintln(stdout)
	}

	pool, notes := llmplan.DerivePool(report, o.GOOS, o.Timeout, o.MemoryGiB, o.Offline)
	ports, known := llmplan.ListeningPorts(report, o.GOOS)
	plan, err := llmplan.Build(report, pool, ports, known, o)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		fmt.Fprintln(stderr, llmPlanUsage)
		return types.ExitError
	}
	plan.Notes = append(plan.Notes, notes...) // rendered by RenderText/RenderMarkdown/RenderJSON, so stdout and plan.txt match

	fmt.Fprint(stdout, llmplan.RenderText(plan))

	if f.outSet || f.json || f.md {
		files, err := llmplan.WriteFiles(f.out, plan, f.json, f.md)
		if err != nil {
			fmt.Fprintf(stderr, "Error writing plan: %v\n", err)
			return types.ExitError
		}
		fmt.Fprintln(stdout)
		for _, p := range files {
			fmt.Fprintf(stdout, "  Written: %s\n", p)
		}
	}
	return plan.ExitCode
}

// loadReportJSON reads a report.json produced by 'nvcheckup run --json'.
func loadReportJSON(path string) (*types.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r types.Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// printBannerTo is printBanner for an arbitrary writer.
func printBannerTo(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  NVCheckup v%s\n", types.Version)
	fmt.Fprintf(w, "  %s\n", types.Disclaimer)
	fmt.Fprintln(w)
}
