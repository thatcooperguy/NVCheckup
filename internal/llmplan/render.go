package llmplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Footer statements of the plan; the wording mirrors spec 7.9 and is part of
// the output contract.
const (
	footerReadOnly  = "llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory."
	footerEstimates = "Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others."
)

// RenderText produces the plan in the spec 7.8 / brief order: header,
// verdict, sizing, estimates, advice, prerequisites, commands, env, warnings.
func RenderText(p *Plan) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&b, format, args...); b.WriteByte('\n') }

	w("NVCheckup llm-plan (read-only; estimates, not measurements)")
	w("%s", strings.Repeat("=", 60))
	w("Platform:      %s", p.Platform.Label)
	if p.Platform.GPU != "" {
		w("GPU:           %s", p.Platform.GPU)
	}
	pool := fmt.Sprintf("%.1f GiB", p.Memory.TotalGiB)
	if p.Memory.Nodes > 1 {
		pool += fmt.Sprintf(" per node x %d nodes", p.Memory.Nodes)
	}
	w("Pool:          %s (source: %s)", pool, p.Memory.Source)
	avail := memAvailLabel(p)
	switch {
	case p.Memory.Discrete && p.Memory.AvailableGiB > 0:
		w("VRAM free:     %.1f GiB   (nvidia-smi memory.free; no swap or page cache on a dedicated pool)", p.Memory.AvailableGiB)
	case p.Memory.AvailableGiB > 0:
		w("MemAvailable:  %.1f GiB   swap used %.1f GiB   allocatable (spec 3.3) %.1f GiB", p.Memory.AvailableGiB, p.Memory.SwapUsedGiB, p.Memory.AllocatableGiB)
	default:
		w("%-14s unknown", avail+":")
	}
	w("Bandwidth:     %s", p.Platform.BandwidthNote)
	w("OS floor F:    %s", p.Memory.HeadroomReason)
	w("")

	w("VERDICT")
	w("  %s", p.Verdict)
	w("")

	m, f := p.Model, p.Fit
	w("SIZING (spec 7.4)")
	shape := fmt.Sprintf("%.4gB params", m.ParamsB)
	if m.ActiveParamsB != m.ParamsB {
		shape += fmt.Sprintf(" (%.3gB active)", m.ActiveParamsB)
	}
	layers := fmt.Sprintf("%d layers", m.Layers)
	if m.AttentionLayers != m.Layers {
		layers = fmt.Sprintf("%d attention of %d layers", m.AttentionLayers, m.Layers)
	}
	w("  Model            %s: %s, %s, %d KV heads, d_head %d", m.Name, shape, layers, m.KVHeads, m.HeadDim)
	w("  Profile          %s: %s context x %s, %s", m.Profile, fmtTokens(m.Context), plural(m.Concurrency, "stream"), p.Runtime.Runtime.Display())
	weights := fmt.Sprintf("%.1f GiB", f.WeightsGiB)
	if f.WeightsMeasured {
		weights += "  (measured checkpoint size, spec 7.4)"
	} else {
		weights += fmt.Sprintf("  (%.4gB x %.2f B/param)", m.ParamsB, Quant(m.Quant).BytesPerParam())
	}
	w("  Weights W        %s", weights)
	w("  Quant            %s, KV cache %s", strings.ToUpper(m.Quant), m.KVDtype)
	w("  KV cache         %.1f GiB  (%.0f B/token x %d tokens x %s)", f.KVGiB, m.KVBytesPerToken, m.Context, plural(m.Concurrency, "stream"))
	if f.StateGiB > 0 {
		w("  Mamba state      %.1f GiB  (derived from the measured per-slot figure, single source S81)", f.StateGiB)
	}
	w("  Runtime R        %.1f GiB  (%s)", f.RuntimeGiB, p.Runtime.Runtime.Display())
	w("  OS floor F       %.1f GiB", f.FloorGiB)
	per := ""
	if f.PerNode {
		per = " per node (W, KV and state split evenly)"
	}
	w("  Total            %.1f GiB%s  vs pool %.1f GiB  ->  margin %.1f GiB  (fits: %s)", f.TotalGiB, per, f.PoolGiB, f.MarginGiB, yesNo(f.FitsTotal))
	if f.FitsNow != nil {
		w("  Now (W+KV+R)     %.1f GiB  vs %s %.1f GiB  (fits now: %s)", f.NowGiB, avail, p.Memory.AvailableGiB, yesNo(*f.FitsNow))
	} else {
		w("  Now (W+KV+R)     %.1f GiB  vs %s unknown", f.NowGiB, avail)
	}
	if p.Runtime.Runtime.IsContainer() {
		w("  Utilization u    %.2f  (vLLM --gpu-memory-utilization; TRT-LLM free_gpu_memory_fraction; SGLang --mem-fraction-static)", f.Utilization)
	}
	w("")

	e := p.Estimates
	w("ESTIMATES (formula ceilings; not measured on this machine)")
	if e.Note == "" {
		w("  Decode ceiling, one stream:  %s tok/s weights-only; %s tok/s at %s context", fmtTPS(e.DecodeCeilingWeightsOnlyTPS), fmtTPS(e.DecodeCeilingTPS), fmtTokens(m.Context))
		w("  Realism band (50-80%% of at-context):  %s - %s tok/s", fmtTPS(e.DecodeBandTPS[0]), fmtTPS(e.DecodeBandTPS[1]))
	} else {
		w("  Decode ceiling:  not printed (%s)", e.Note)
	}
	w("  Prefill reference:  %s", e.PrefillRefTPS)
	if e.MeasuredByOthers != "" {
		w("  Measured by others:  %s", e.MeasuredByOthers)
	}
	w("")

	w("ADVICE (spec 7.6)")
	for _, l := range p.Advice.Lines {
		w("  - %s", l)
	}
	if len(p.Advice.Quants) > 0 {
		w("  %-8s %-12s %-12s %-8s %s", "Quant", "Weights", "Total", "Fits", "Margin")
		for _, q := range p.Advice.Quants {
			fits := yesNo(q.FitsTotal)
			if q.FitsNow != nil && q.FitsTotal && !*q.FitsNow {
				fits = "design only"
			}
			w("  %-8s %-12s %-12s %-8s %.1f GiB", q.Quant, fmt.Sprintf("%.1f GiB", q.WeightsGiB), fmt.Sprintf("%.1f GiB", q.TotalGiB), fits, q.MarginGiB)
		}
	}
	w("")

	w("PREREQUISITES (spec 7.7, from the read-only report)")
	for _, pr := range p.Prerequisites {
		w("  %-4s %-22s %s", pr.Status, pr.ID, pr.Detail)
	}
	w("")

	c := p.Runtime
	w("COMMANDS (spec 7.6 template for %s)", c.Runtime.Display())
	if c.Image != "" {
		w("  Image:  %s", c.Image)
	}
	if c.Build != "" {
		w("  Build:  %s", c.Build)
	}
	w("  $ %s", c.Command)
	for _, x := range c.Extra {
		w("    %s", x)
	}
	if len(c.Env) > 0 {
		w("  Env:")
		for _, e := range c.Env {
			w("    %s", e)
		}
	}
	if len(c.Notes) > 0 {
		w("  Notes:")
		for _, n := range c.Notes {
			w("    - %s", n)
		}
	}
	if len(c.Unconfirmed) > 0 {
		w("  Unconfirmed / not covered by the spec:")
		for _, n := range c.Unconfirmed {
			w("    - %s", n)
		}
	}
	w("")

	if rest := warningsNotIn(p.Warnings, c.Unconfirmed); len(rest) > 0 {
		w("WARNINGS")
		for _, x := range rest {
			w("  - %s", x)
		}
		w("")
	} else if len(c.Unconfirmed) > 0 {
		w("WARNINGS: see the unconfirmed items above.")
		w("")
	}
	if len(p.Notes) > 0 {
		w("NOTES (pool source and caveats)")
		for _, n := range p.Notes {
			w("  - %s", n)
		}
		w("")
	}
	w("Exit code %d (0 fits, 1 fits with warnings, 2 does not fit, 3 error).", p.ExitCode)
	w("%s", footerReadOnly)
	w("%s", footerEstimates)
	return b.String()
}

// memAvailLabel names the "available now" figure of the plan.
func memAvailLabel(p *Plan) string {
	if p.Memory.Discrete {
		return "VRAM free"
	}
	return "MemAvailable"
}

// warningsNotIn drops the warnings already printed in the command block's
// unconfirmed list so the text does not repeat them.
func warningsNotIn(warnings, printed []string) []string {
	seen := map[string]bool{}
	for _, s := range printed {
		seen[s] = true
	}
	var out []string
	for _, s := range warnings {
		if !seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// RenderMarkdown is the GitHub/forum-ready variant of RenderText.
func RenderMarkdown(p *Plan) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&b, format, args...); b.WriteByte('\n') }
	m, f, e, c := p.Model, p.Fit, p.Estimates, p.Runtime

	w("# NVCheckup llm-plan")
	w("")
	w("_Read-only; estimates, not measurements._")
	w("")
	w("| | |")
	w("|---|---|")
	w("| Platform | %s |", p.Platform.Label)
	if p.Platform.GPU != "" {
		w("| GPU | %s |", p.Platform.GPU)
	}
	w("| Pool | %.1f GiB (%s) |", p.Memory.TotalGiB, p.Memory.Source)
	avail := memAvailLabel(p)
	switch {
	case p.Memory.Discrete && p.Memory.AvailableGiB > 0:
		w("| VRAM free | %.1f GiB |", p.Memory.AvailableGiB)
	case p.Memory.AvailableGiB > 0:
		w("| MemAvailable | %.1f GiB (swap used %.1f GiB) |", p.Memory.AvailableGiB, p.Memory.SwapUsedGiB)
	default:
		w("| %s | unknown |", avail)
	}
	w("| Bandwidth | %s |", p.Platform.BandwidthNote)
	w("| OS floor F | %s |", p.Memory.HeadroomReason)
	w("")
	w("## Verdict")
	w("")
	w("**%s**", p.Verdict)
	w("")
	w("## Sizing (spec 7.4)")
	w("")
	w("| Term | GiB | Detail |")
	w("|---|---:|---|")
	wd := fmt.Sprintf("%.4gB x %.2f B/param", m.ParamsB, Quant(m.Quant).BytesPerParam())
	if f.WeightsMeasured {
		wd = "measured checkpoint size"
	}
	w("| Weights W (%s) | %.1f | %s |", strings.ToUpper(m.Quant), f.WeightsGiB, wd)
	w("| KV cache (%s) | %.1f | %.0f B/token x %d tokens x %d streams |", m.KVDtype, f.KVGiB, m.KVBytesPerToken, m.Context, m.Concurrency)
	if f.StateGiB > 0 {
		w("| Mamba state | %.1f | derived from the measured per-slot figure (S81) |", f.StateGiB)
	}
	w("| Runtime R | %.1f | %s |", f.RuntimeGiB, c.Runtime.Display())
	w("| OS floor F | %.1f | |", f.FloorGiB)
	per := ""
	if f.PerNode {
		per = " per node"
	}
	w("| **Total%s** | **%.1f** | pool %.1f GiB, margin %.1f GiB, fits: %s |", per, f.TotalGiB, f.PoolGiB, f.MarginGiB, yesNo(f.FitsTotal))
	if f.FitsNow != nil {
		w("| Now (W+KV+R) | %.1f | %s %.1f GiB, fits now: %s |", f.NowGiB, avail, p.Memory.AvailableGiB, yesNo(*f.FitsNow))
	} else {
		w("| Now (W+KV+R) | %.1f | %s unknown |", f.NowGiB, avail)
	}
	if c.Runtime.IsContainer() {
		w("| u | %.2f | gpu-memory-utilization / free_gpu_memory_fraction / mem-fraction-static |", f.Utilization)
	}
	w("")
	w("## Estimates")
	w("")
	if e.Note == "" {
		w("- Decode ceiling, one stream: %s tok/s weights-only; %s tok/s at %s context; realism band %s-%s tok/s (50-80%%).", fmtTPS(e.DecodeCeilingWeightsOnlyTPS), fmtTPS(e.DecodeCeilingTPS), fmtTokens(m.Context), fmtTPS(e.DecodeBandTPS[0]), fmtTPS(e.DecodeBandTPS[1]))
	} else {
		w("- Decode ceiling: not printed (%s).", e.Note)
	}
	w("- Prefill reference: %s.", e.PrefillRefTPS)
	if e.MeasuredByOthers != "" {
		w("- Measured by others: %s.", e.MeasuredByOthers)
	}
	w("")
	w("## Advice (spec 7.6)")
	w("")
	for _, l := range p.Advice.Lines {
		w("- %s", l)
	}
	if len(p.Advice.Quants) > 0 {
		w("")
		w("| Quant | Weights GiB | Total GiB | Fits | Margin GiB |")
		w("|---|---:|---:|---|---:|")
		for _, q := range p.Advice.Quants {
			w("| %s | %.1f | %.1f | %s | %.1f |", q.Quant, q.WeightsGiB, q.TotalGiB, yesNo(q.FitsTotal), q.MarginGiB)
		}
	}
	w("")
	w("## Prerequisites (spec 7.7)")
	w("")
	w("| Status | Check | Detail |")
	w("|---|---|---|")
	for _, pr := range p.Prerequisites {
		w("| %s | %s | %s |", pr.Status, pr.ID, pr.Detail)
	}
	w("")
	w("## Commands (%s, spec 7.6)", c.Runtime.Display())
	w("")
	if c.Image != "" {
		w("Image: `%s`", c.Image)
		w("")
	}
	if c.Build != "" {
		w("Build:")
		w("")
		w("```sh")
		w("%s", c.Build)
		w("```")
		w("")
	}
	w("```sh")
	w("%s", c.Command)
	for _, x := range c.Extra {
		w("%s", x)
	}
	w("```")
	if len(c.Env) > 0 {
		w("")
		w("Environment:")
		w("")
		w("```sh")
		for _, e := range c.Env {
			w("%s", e)
		}
		w("```")
	}
	for _, n := range c.Notes {
		w("- %s", n)
	}
	if len(c.Unconfirmed) > 0 {
		w("")
		w("Unconfirmed / not covered by the spec:")
		w("")
		for _, n := range c.Unconfirmed {
			w("- %s", n)
		}
	}
	if rest := warningsNotIn(p.Warnings, c.Unconfirmed); len(rest) > 0 {
		w("")
		w("## Warnings")
		w("")
		for _, x := range rest {
			w("- %s", x)
		}
	}
	if len(p.Notes) > 0 {
		w("")
		w("## Notes")
		w("")
		for _, n := range p.Notes {
			w("- %s", n)
		}
	}
	w("")
	w("Exit code %d (0 fits, 1 fits with warnings, 2 does not fit, 3 error).", p.ExitCode)
	w("")
	w("_%s_", footerReadOnly)
	w("")
	w("_%s_", footerEstimates)
	return b.String()
}

// RenderJSON is plan.json (spec 7.8).
func RenderJSON(p *Plan) (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// WriteFiles writes plan.txt (always) and plan.json / plan.md (on request)
// into dir and nowhere else (spec 7.9: never write outside --out).
func WriteFiles(dir string, p *Plan, withJSON, withMD bool) ([]string, error) {
	if dir == "" || dir == "." {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}
	var files []string
	write := func(name, content string) error {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("cannot write %s: %w", name, err)
		}
		files = append(files, path)
		return nil
	}
	if err := write("plan.txt", RenderText(p)); err != nil {
		return files, err
	}
	if withJSON {
		js, err := RenderJSON(p)
		if err != nil {
			return files, err
		}
		if err := write("plan.json", js); err != nil {
			return files, err
		}
	}
	if withMD {
		if err := write("plan.md", RenderMarkdown(p)); err != nil {
			return files, err
		}
	}
	return files, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
