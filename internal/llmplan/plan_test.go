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
