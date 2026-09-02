package llmplan

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func buildGB10(t *testing.T, mutate func(o *Options), rep func(r *types.Report)) (*Plan, error) {
	t.Helper()
	t.Setenv("TRITON_PTXAS_PATH", "")
	r := gb10Report()
	if rep != nil {
		rep(r)
	}
	o := DefaultOptions()
	o.GOOS = "linux"
	mutate(&o)
	return Build(r, poolFromUnifiedMemory(r.UnifiedMemory), nil, true, o)
}

func TestBuild_ExitCodes(t *testing.T) {
	p, err := buildGB10(t, func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "agent", "vllm" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExitCode != types.ExitOK || !strings.HasPrefix(p.Verdict, "FITS:") {
		t.Errorf("clean fit: exit %d verdict %q warnings %v", p.ExitCode, p.Verdict, p.Warnings)
	}
	if p.Fit.Utilization != 0.40 || p.Fit.KVGiB != 16.0 || p.Fit.RuntimeGiB != 12 || p.Fit.FloorGiB != 8 {
		t.Errorf("fit = %+v", p.Fit)
	}
	if p.Advice.RecommendedQuant != "bf16" {
		t.Errorf("recommended quant = %q, want bf16 (least lossy that fits)", p.Advice.RecommendedQuant)
	}

	p, err = buildGB10(t, func(o *Options) {
		o.Model, o.Quant, o.Context, o.Runtime = "llama-3.3-70b-instruct", "bf16", 131072, "vllm"
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExitCode != types.ExitCritical || !strings.HasPrefix(p.Verdict, "DOES NOT FIT") {
		t.Errorf("70B BF16: exit %d verdict %q", p.ExitCode, p.Verdict)
	}
	if p.Advice.RecommendedQuant != "nvfp4" {
		t.Errorf("70B BF16 128K: recommended %q, want nvfp4 (the only container quant that fits)", p.Advice.RecommendedQuant)
	}
	if len(p.Advice.Alternatives) == 0 {
		t.Error("a non-fitting plan lists catalogue models that fit")
	}

	// Fits by design but not right now -> 1.
	p, err = buildGB10(t, func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "agent", "vllm" }, func(r *types.Report) {
		r.UnifiedMemory.MemAvailableKB = 30 * 1024 * 1024
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ExitCode != types.ExitWarnings || !strings.HasPrefix(p.Verdict, "FITS BY DESIGN, NOT RIGHT NOW") || p.Fit.FitsNow == nil || *p.Fit.FitsNow {
		t.Errorf("low MemAvailable: exit %d verdict %q", p.ExitCode, p.Verdict)
	}

	// A prerequisite WARN turns a fit into exit 1.
	p, err = buildGB10(t, func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "agent", "vllm" }, func(r *types.Report) {
		r.Driver.Version, r.GPUs[0].DriverVersion = "570.1", "570.1"
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ExitCode != types.ExitWarnings {
		t.Errorf("driver WARN: exit %d, want 1", p.ExitCode)
	}
}

func TestBuild_Errors(t *testing.T) {
	cases := []func(o *Options){
		func(o *Options) { o.Model = "no-such-model" },
		func(o *Options) { o.Model, o.Quant = "llama-3.1-8b-instruct", "nf4" },
		func(o *Options) { o.Model, o.Quant, o.Runtime = "llama-3.1-8b-instruct", "q4_k_m", "vllm" },
		func(o *Options) { o.Model, o.KVDtype, o.Runtime = "llama-3.1-8b-instruct", "q8_0", "vllm" },
		func(o *Options) { o.Model, o.Nodes = "llama-3.1-8b-instruct", 3 },
		func(o *Options) { o.Model, o.Profile = "llama-3.1-8b-instruct", "training" },
		func(o *Options) {},
	}
	for i, c := range cases {
		if _, err := buildGB10(t, c, nil); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
}

func TestBuild_ProfileDefaultsAndAuto(t *testing.T) {
	p, err := buildGB10(t, func(o *Options) { o.Model, o.Profile = "qwen3-32b", "agent" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model.Context != 32768 || p.Model.Concurrency != 4 {
		t.Errorf("agent defaults = %d x %d", p.Model.Context, p.Model.Concurrency)
	}
	if p.Runtime.Runtime != RuntimeVLLM || p.Model.KVDtype != "f16" {
		t.Errorf("auto runtime/kv = %s/%s, want vllm/f16", p.Runtime.Runtime, p.Model.KVDtype)
	}
	if strings.Contains(p.Runtime.Command, "--kv-cache-dtype") {
		t.Error("auto must never emit --kv-cache-dtype fp8")
	}
	p, err = buildGB10(t, func(o *Options) { o.Model, o.Quant = "llama-3.1-8b-instruct", "q4_k_m" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime.Runtime != RuntimeLlamaCpp || p.Model.KVDtype != "q8_0" {
		t.Errorf("GGUF auto = %s/%s, want llamacpp/q8_0", p.Runtime.Runtime, p.Model.KVDtype)
	}
}

func TestBuild_TwoNodes(t *testing.T) {
	p, err := buildGB10(t, func(o *Options) { o.Model, o.Quant, o.Runtime, o.Nodes = "qwen3-235b-a22b", "nvfp4", "trtllm", 2 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Fit.FitsTotal || !p.Fit.PerNode {
		t.Errorf("Qwen3-235B NVFP4 across two nodes: %+v", p.Fit)
	}
	if _, ok := statusOf(p.Prerequisites, "cx7-link"); !ok {
		t.Error("two-node plans evaluate the ConnectX-7 link")
	}
	if p.ExitCode != types.ExitWarnings {
		t.Errorf("no fabric enumerated must give exit 1 (fits with warnings), got %d", p.ExitCode)
	}
	if !strings.Contains(strings.Join(p.Runtime.Env, " "), "NCCL_NET_PLUGIN=none") {
		t.Errorf("cluster env missing: %v", p.Runtime.Env)
	}
}

func TestBuild_CustomAndMemoryOverride(t *testing.T) {
	t.Setenv("TRITON_PTXAS_PATH", "")
	o := DefaultOptions()
	o.GOOS = "linux"
	o.ParamsB, o.Layers, o.KVHeads, o.HeadDim, o.Quant, o.Context, o.Runtime = 8.03, 32, 8, 128, "bf16", 32768, "vllm"
	o.Concurrency = 4
	pool := MemoryPool{TotalBytes: 119.7 * GiB, Source: "--memory-gib 119.7 (user override)"}
	p, err := Build(&types.Report{}, pool, nil, false, o)
	if err != nil {
		t.Fatal(err)
	}
	if p.Fit.FitsNow != nil || p.Fit.WeightsGiB != 15.0 || p.Model.ID != "custom" {
		t.Errorf("custom plan = %+v", p.Fit)
	}
	if p.Estimates.DecodeCeilingTPS != 0 || p.Estimates.Note == "" {
		t.Error("no bandwidth for an unknown platform: no ceiling, with a note")
	}
}

// buildOffline builds a plan for a saved report: the pool comes from the
// report only (never this host's /proc/meminfo or CIM).
func buildOffline(t *testing.T, r *types.Report, goos string, mutate func(o *Options)) *Plan {
	t.Helper()
	t.Setenv("TRITON_PTXAS_PATH", "")
	t.Setenv("NVC_SIM_ROOT", "")
	o := DefaultOptions()
	o.GOOS = goos
	mutate(&o)
	pool, _ := DerivePool(r, goos, o.Timeout, o.MemoryGiB, true)
	p, err := Build(r, pool, nil, r.UnifiedMemory != nil, o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func hasWarning(p *Plan, substr string) bool {
	for _, w := range p.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// Spec 7.5/7.6: llama.cpp (clang-cl) and Ollama on Windows on Arm are
// unconfirmed, so an N1X plan for them is labelled and exits 1; an x86-64
// Windows desktop and a container runtime on WoA do not get that line.
func TestBuild_WindowsOnArmUnconfirmed(t *testing.T) {
	for _, rt := range []string{"llamacpp", "ollama"} {
		p := buildOffline(t, n1xWindowsReport(), "windows", func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "chat", rt })
		if !p.Platform.UnifiedMemory || p.Platform.SoC != "N1X" {
			t.Errorf("%s: N1X plan platform = %+v", rt, p.Platform)
		}
		if !hasWarning(p, "clang-cl") || !hasWarning(p, "CUDA 13.4 DP) is itself unverified") || !hasWarning(p, "Unconfirmed") {
			t.Errorf("%s on WoA: unconfirmed label missing, warnings %v", rt, p.Warnings)
		}
		if p.ExitCode != types.ExitWarnings {
			t.Errorf("%s on WoA: exit %d, want 1 (fits with warnings)", rt, p.ExitCode)
		}
		if !strings.Contains(RenderText(p), "clang-cl") || !strings.Contains(RenderMarkdown(p), "clang-cl") {
			t.Errorf("%s on WoA: the label must be rendered", rt)
		}
	}
	// A container runtime on WoA gets the coverage caveat, not the llama.cpp one.
	p := buildOffline(t, n1xWindowsReport(), "windows", func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "chat", "vllm" })
	if hasWarning(p, "clang-cl (spec 7.6); the cmake") || !hasWarning(p, "only llama.cpp (clang-cl)") {
		t.Errorf("vLLM on WoA warnings = %v", p.Warnings)
	}
	// x86-64 Windows (the win_rtx3090 golden) is exempt: the note is WoA-specific.
	p = buildOffline(t, rtx3090Report(), "windows", func(o *Options) { o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "chat", "llamacpp" })
	if hasWarning(p, "clang-cl") {
		t.Errorf("x86-64 Windows must not carry the WoA note: %v", p.Warnings)
	}
	if windowsOnArm(rtx3090Report(), "windows") || windowsOnArm(n1xWindowsReport(), "linux") || windowsOnArm(nil, "windows") || !windowsOnArm(n1xWindowsReport(), "windows") {
		t.Error("windowsOnArm predicate")
	}
}

// Grace Hopper end to end: discrete HBM pool (spec 3.1 flag rule C), no Spark
// bandwidth, no sm_121 prerequisites, 70B BF16 does not fit 95.6 GiB.
func TestBuild_GraceHopperDiscrete(t *testing.T) {
	p := buildOffline(t, gh200Report(), "linux", func(o *Options) {
		o.Model, o.Quant, o.Context, o.Runtime = "llama-3.3-70b-instruct", "bf16", 32768, "vllm"
	})
	if p.Platform.UnifiedMemory || p.Platform.SoC != "" || !p.Memory.Discrete || p.Memory.TotalGiB != 95.6 {
		t.Errorf("GH200 platform/memory = %+v / %+v", p.Platform, p.Memory)
	}
	if p.Fit.FitsTotal || p.ExitCode != types.ExitCritical {
		t.Errorf("70B BF16 on GH200: fits %v exit %d, want does-not-fit / 2", p.Fit.FitsTotal, p.ExitCode)
	}
	if p.Estimates.DecodeCeilingTPS != 0 || p.Estimates.Note == "" {
		t.Error("GH200 has no spec bandwidth: no ceiling, with a note")
	}
	if hasWarning(p, "sm_121") || hasWarning(p, "580 branch") {
		t.Errorf("GH200 must not get Spark driver/CUDA warnings: %v", p.Warnings)
	}
	for _, pr := range p.Prerequisites {
		if pr.ID == "driver-present" && pr.Status != StatusPass {
			t.Errorf("driver-present on GH200 with 570 = %s (%s), want PASS", pr.Status, pr.Detail)
		}
	}
}

// A context so long that the at-context ceiling rounds below 0.05 tok/s must
// still print both ceilings (three significant digits) instead of "not
// printed ()" with an empty reason, and plan.json keeps the same value.
func TestBuild_TinyCeilingStillPrinted(t *testing.T) {
	p, err := buildGB10(t, func(o *Options) {
		o.Model, o.Quant, o.Context, o.Concurrency, o.Runtime = "llama-3.1-8b-instruct", "bf16", 99999999999, 1, "vllm"
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := p.Estimates
	if e.Note != "" || e.DecodeCeilingWeightsOnlyTPS != 17.0 || e.DecodeCeilingTPS <= 0 || e.DecodeCeilingTPS >= 0.05 {
		t.Fatalf("estimates = %+v", e)
	}
	txt := RenderText(p)
	if strings.Contains(txt, "not printed") || strings.Contains(txt, " 0.0 tok/s") {
		t.Errorf("text hides a valid ceiling:\n%s", txt)
	}
	want := fmtTPS(e.DecodeCeilingTPS)
	if !strings.Contains(txt, "17.0 tok/s weights-only; "+want+" tok/s at") {
		t.Errorf("text ceiling line missing %q:\n%s", want, txt)
	}
	if md := RenderMarkdown(p); !strings.Contains(md, "17.0 tok/s weights-only; "+want+" tok/s at") {
		t.Errorf("markdown ceiling line missing %q", want)
	}
	js, err := RenderJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"decode_ceiling_tps": `+want) {
		t.Errorf("plan.json must carry the same at-context value %s", want)
	}
	// Ordinary values keep the one-decimal rounding of every other figure.
	if roundTPS(13.44) != 13.4 || fmtTPS(13.4) != "13.4" || roundTPS(0) != 0 || fmtTPS(0.0027312) != "0.00273" || roundTPS(0.0027312) != 0.00273 {
		t.Error("roundTPS/fmtTPS precision")
	}
}

func TestPrompt_DefaultsAndAnswers(t *testing.T) {
	var out bytes.Buffer
	o := DefaultOptions()
	// model 2 (70B), nvfp4, agent, 128K, 1 stream, vllm, cluster
	in := bufio.NewReader(strings.NewReader("2\nnvfp4\nb\n128K\n1\nvllm\nb\n"))
	if err := Prompt(in, &out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Model != "llama-3.3-70b-instruct" || o.Quant != "nvfp4" || o.Profile != "agent" || o.Context != 131072 || o.Concurrency != 1 || o.Runtime != "vllm" || o.Nodes != 2 {
		t.Errorf("prompted options = %+v", o)
	}
	// Exhausted input keeps every default.
	o = DefaultOptions()
	if err := Prompt(bufio.NewReader(strings.NewReader("")), &out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Model != "llama-3.1-8b-instruct" || o.Profile != "chat" || o.Nodes != 1 || o.Quant != "" {
		t.Errorf("default options = %+v", o)
	}
	// Flags given on the command line survive blank answers (--profile agent --nodes 2).
	o = DefaultOptions()
	o.Profile, o.Nodes = "agent", 2
	if err := Prompt(bufio.NewReader(strings.NewReader("")), &out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Profile != "agent" || o.Nodes != 2 || o.Model != "llama-3.1-8b-instruct" {
		t.Errorf("blank answers must keep --profile/--nodes: %+v", o)
	}
	// An explicit answer still overrides them.
	o = DefaultOptions()
	o.Profile, o.Nodes = "agent", 2
	if err := Prompt(bufio.NewReader(strings.NewReader("\n\na\n\n\n\na\n")), &out, &o); err != nil {
		t.Fatal(err)
	}
	if o.Profile != "chat" || o.Nodes != 1 {
		t.Errorf("explicit answers must override --profile/--nodes: %+v", o)
	}
	o = DefaultOptions()
	if err := Prompt(bufio.NewReader(strings.NewReader("\n\nz\n")), &out, &o); err == nil {
		t.Error("unknown profile answer must fail")
	}
	o = DefaultOptions()
	if err := Prompt(bufio.NewReader(strings.NewReader("\n\n\n\n\n\n3\n")), &out, &o); err == nil {
		t.Error("unknown target answer must fail")
	}
	// Custom shape path.
	o = DefaultOptions()
	if err := Prompt(bufio.NewReader(strings.NewReader("c\n8.03\n\n32\n8\n128\n")), &out, &o); err != nil {
		t.Fatal(err)
	}
	if o.ParamsB != 8.03 || o.Layers != 32 || o.KVHeads != 8 || o.HeadDim != 128 {
		t.Errorf("custom options = %+v", o)
	}
	if _, ok := ParseTokens("32k"); !ok {
		t.Error("32k must parse")
	}
	if n, _ := ParseTokens("131072"); n != 131072 {
		t.Error("plain integers must parse")
	}
}

func TestRunWithReport(t *testing.T) {
	t.Setenv("TRITON_PTXAS_PATH", "")
	var out bytes.Buffer
	code := RunWithReport(bufio.NewReader(strings.NewReader("1\n\nb\n\n\nvllm\n\n")), &out, gb10Report(), "linux")
	if code != types.ExitOK {
		t.Errorf("exit %d, output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "VERDICT") {
		t.Error("plan text not printed")
	}
}
