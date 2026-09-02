package llmplan

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Options are the parsed llm-plan flags (spec 7.1).
type Options struct {
	Model    string
	HFConfig string
	// Custom shape (--params, --active-params, --layers, --kv-heads, --head-dim, --hidden, --heads).
	ParamsB       float64
	ActiveParamsB float64
	Layers        int
	KVHeads       int
	HeadDim       int
	Hidden        int
	Heads         int

	Quant       string
	Context     int
	Concurrency int
	Profile     string
	Runtime     string
	KVDtype     string
	HeadroomGiB float64 // < 0 = spec default F
	MemoryGiB   float64 // > 0 overrides the pool total
	Nodes       int
	Timeout     int
	GOOS        string // defaults to runtime.GOOS
	Offline     bool   // --report: size the saved report, never query this host for memory
}

// Profiles and their design defaults (spec 7.1 profiles; agent contexts of
// 30K-120K are typical per S79 and the worked examples use 4 x 32K).
var profileDefaults = map[string]struct{ ctx, conc int }{
	"chat":  {8192, 1},
	"agent": {32768, 4},
	"batch": {4096, 8},
	"rag":   {32768, 1},
}

// ProfileNames for help text.
func ProfileNames() string { return "chat|agent|batch|rag" }

// Plan is the complete result (spec 7.8 plan.json layout plus advice).
type Plan struct {
	Verdict       string        `json:"verdict"`
	ExitCode      int           `json:"exit_code"`
	Platform      PlanPlatform  `json:"platform"`
	Memory        PlanMemory    `json:"memory"`
	Model         PlanModel     `json:"model"`
	Fit           PlanFit       `json:"fit"`
	Estimates     PlanEstimates `json:"estimates"`
	Runtime       Command       `json:"runtime"`
	Advice        PlanAdvice    `json:"advice"`
	Prerequisites []Prereq      `json:"prerequisites"`
	Warnings      []string      `json:"warnings"`
	Notes         []string      `json:"notes,omitempty"`
}

// PlanPlatform is plan.json "platform".
type PlanPlatform struct {
	Label         string  `json:"label"`
	Class         string  `json:"class,omitempty"`
	SoC           string  `json:"soc,omitempty"`
	OS            string  `json:"os"`
	GPU           string  `json:"gpu,omitempty"`
	UnifiedMemory bool    `json:"unified_memory"`
	BandwidthGBps float64 `json:"bandwidth_gbps,omitempty"`
	BandwidthNote string  `json:"bandwidth_note"`
}

// PlanMemory is plan.json "memory".
type PlanMemory struct {
	TotalGiB       float64 `json:"total_gib"`
	AvailableGiB   float64 `json:"available_gib"` // 0 = unknown
	HeadroomGiB    float64 `json:"headroom_gib"`  // F
	HeadroomReason string  `json:"headroom_reason"`
	AllocatableGiB float64 `json:"allocatable_gib,omitempty"`
	SwapUsedGiB    float64 `json:"swap_used_gib"`
	Source         string  `json:"source"`
	Nodes          int     `json:"nodes"`
}

// PlanModel is plan.json "model".
type PlanModel struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	HFRepo          string  `json:"hf_repo,omitempty"`
	ParamsB         float64 `json:"params_b"`
	ActiveParamsB   float64 `json:"active_params_b"`
	Layers          int     `json:"layers"`
	AttentionLayers int     `json:"attention_layers"`
	KVHeads         int     `json:"kv_heads"`
	HeadDim         int     `json:"head_dim"`
	Quant           string  `json:"quant"`
	KVDtype         string  `json:"kv_dtype"`
	KVBytesPerToken float64 `json:"kv_bytes_per_token"`
	Context         int     `json:"context"`
	Concurrency     int     `json:"concurrency"`
	Profile         string  `json:"profile"`
	License         string  `json:"license,omitempty"`
	Notes           string  `json:"notes,omitempty"`
}

// PlanFit is plan.json "fit".
type PlanFit struct {
	WeightsGiB       float64 `json:"weights_gib"`
	WeightsMeasured  bool    `json:"weights_measured"`
	KVGiB            float64 `json:"kv_gib"`
	StateGiB         float64 `json:"state_gib,omitempty"`
	RuntimeGiB       float64 `json:"runtime_gib"`
	FloorGiB         float64 `json:"floor_gib"`
	TotalGiB         float64 `json:"total_gib"`
	NowGiB           float64 `json:"now_gib"`
	PoolGiB          float64 `json:"pool_gib"`
	MarginGiB        float64 `json:"margin_gib"`
	FitsTotal        bool    `json:"fits_total"`
	FitsNow          *bool   `json:"fits_now"` // null when MemAvailable is unknown
	Utilization      float64 `json:"gpu_memory_utilization"`
	MaxContextTokens int     `json:"max_context_tokens"`
	PerNode          bool    `json:"per_node"`
}

// PlanEstimates is plan.json "estimates". All values are formula ceilings or
// quotes of measurements made by others, never measurements of this machine.
type PlanEstimates struct {
	DecodeCeilingTPS            float64    `json:"decode_ceiling_tps"` // at-context, one stream
	DecodeCeilingWeightsOnlyTPS float64    `json:"decode_ceiling_weights_only_tps"`
	DecodeBandTPS               [2]float64 `json:"decode_band_tps"`
	PrefillRefTPS               string     `json:"prefill_ref_tps"`
	Note                        string     `json:"note,omitempty"`
	MeasuredByOthers            string     `json:"measured_by_others,omitempty"`
}

// QuantOption is one row of the quantization advice table.
type QuantOption struct {
	Quant      string  `json:"quant"`
	WeightsGiB float64 `json:"weights_gib"`
	TotalGiB   float64 `json:"total_gib"`
	FitsTotal  bool    `json:"fits_total"`
	FitsNow    *bool   `json:"fits_now"`
	MarginGiB  float64 `json:"margin_gib"`
}

// ModelOption is a catalogue model that fits at the same context/concurrency.
type ModelOption struct {
	ID       string  `json:"id"`
	Quant    string  `json:"quant"`
	TotalGiB float64 `json:"total_gib"`
}

// PlanAdvice is the spec 7.6 quantization and runtime advice.
type PlanAdvice struct {
	RecommendedQuant string        `json:"recommended_quant"`
	Reason           string        `json:"reason"`
	Quants           []QuantOption `json:"quants"`
	Alternatives     []ModelOption `json:"alternative_models,omitempty"`
	Lines            []string      `json:"lines"`
}

// ResolveModel picks the model shape from --model, --hf-config or the custom
// shape flags.
func ResolveModel(o Options) (ModelShape, error) {
	switch {
	case o.Model != "":
		m, ok := FindModel(o.Model)
		if !ok {
			return m, fmt.Errorf("unknown model %q; run 'nvcheckup llm-plan --list-models' or pass a custom shape", o.Model)
		}
		return m, nil
	case o.HFConfig != "":
		return ParseHFConfig(o.HFConfig, o.ParamsB, o.ActiveParamsB)
	case o.ParamsB > 0:
		return CustomShape(o.ParamsB, o.ActiveParamsB, o.Layers, o.KVHeads, o.HeadDim, o.Hidden, o.Heads, o.Quant)
	}
	return ModelShape{}, fmt.Errorf("no model given: use --model NAME, --hf-config config.json --params B, or a custom shape (--params --layers --kv-heads --head-dim)")
}

// resolveInputs validates the options against the model and platform.
func resolveInputs(o Options, m ModelShape, pool MemoryPool, floor float64, bw float64) (Inputs, string, []string, error) {
	var warnings []string
	profile := strings.ToLower(strings.TrimSpace(o.Profile))
	if profile == "" {
		profile = "chat"
	}
	def, ok := profileDefaults[profile]
	if !ok {
		return Inputs{}, "", nil, fmt.Errorf("unknown profile %q; use %s", o.Profile, ProfileNames())
	}
	in := Inputs{Model: m, Context: o.Context, Concurrency: o.Concurrency, Nodes: o.Nodes,
		PoolBytes: pool.TotalBytes, AvailableBytes: pool.AvailableBytes, FloorBytes: floor, BandwidthBytesPerSec: bw}
	if in.Context <= 0 {
		in.Context = def.ctx
	}
	if in.Concurrency <= 0 {
		in.Concurrency = def.conc
	}
	if in.Nodes <= 0 {
		in.Nodes = 1
	}
	if in.Nodes > 2 {
		return in, "", nil, fmt.Errorf("--nodes must be 1 or 2 (spec 7.1: single node or a cluster of two)")
	}

	q := Quant(m.DefaultQuant)
	if o.Quant != "" {
		var ok bool
		if q, ok = ParseQuant(o.Quant); !ok {
			return in, "", nil, fmt.Errorf("unknown quant %q; use %s", o.Quant, QuantNames())
		}
	}
	in.Quant = q

	rt, ok := ParseRuntime(o.Runtime)
	if !ok {
		return in, "", nil, fmt.Errorf("unknown runtime %q; use vllm|trtllm|sglang|llamacpp|ollama|auto", o.Runtime)
	}
	kv, ok := ParseKVDtype(o.KVDtype)
	if !ok {
		return in, "", nil, fmt.Errorf("unknown kv dtype %q; use auto|f16|fp8|q8_0|q4_0", o.KVDtype)
	}
	if rt == RuntimeAuto {
		in.KV = KVF16
		if kv != KVAuto {
			in.KV = kv
		}
		rt = ChooseRuntime(in, o.GOOS)
	}
	in.Runtime = rt
	if !rt.SupportsQuant(q) {
		return in, "", nil, fmt.Errorf("%s is a GGUF format; use --runtime llamacpp or ollama, or pick bf16/fp8/nvfp4 for %s", q, rt.Display())
	}
	if kv == KVAuto {
		kv = rt.DefaultKV(m)
	} else if !rt.SupportsKV(kv) {
		return in, "", nil, fmt.Errorf("%s does not support a %s KV cache (vLLM/TRT-LLM/SGLang: f16|fp8; llama.cpp: f16|q8_0|q4_0; Ollama: f16|q8_0)", rt.Display(), kv)
	}
	in.KV = kv
	if q == QuantMXFP4 && m.DefaultQuant != string(QuantMXFP4) {
		warnings = append(warnings, "mxfp4 is priced for expert weights of native MXFP4 checkpoints (gpt-oss); this model does not ship in MXFP4.")
	}
	if q == QuantFP16 && m.DefaultQuant == string(QuantBF16) {
		// same storage; nothing to warn about
	}
	return in, profile, warnings, nil
}

// Build turns a report and pool into a Plan. It does no I/O apart from
// reading the TRITON_PTXAS_PATH environment variable.
func Build(report *types.Report, pool MemoryPool, ports []int, portsKnown bool, o Options) (*Plan, error) {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	m, err := ResolveModel(o)
	if err != nil {
		return nil, err
	}
	if pool.TotalBytes <= 0 {
		return nil, fmt.Errorf("no memory pool: the report has no memory figure and --memory-gib was not given")
	}
	floor, floorReason := OSFloorBytes(report, o.GOOS, o.HeadroomGiB)
	bw, bwNote := Bandwidth(report)

	in, profile, warnings, err := resolveInputs(o, m, pool, floor, bw)
	if err != nil {
		return nil, err
	}
	s := Compute(in)
	cmd := RenderCommand(in, s, profile, ClusterFacts{ActiveRDMADevs: ActiveRDMADevs(report)})
	if cmd.Env == nil {
		cmd.Env = []string{} // spec 7.8: runtime.env is always present
	}
	facts := Facts{Report: report, Pool: pool, Ports: ports, PortsKnown: portsKnown, TritonEnv: os.Getenv("TRITON_PTXAS_PATH"), GOOS: o.GOOS}
	prereqs := Evaluate(facts, in, s, cmd)

	p := &Plan{
		Platform: PlanPlatform{
			Label:         PlatformLabel(report),
			SoC:           SparkSoC(report),
			OS:            o.GOOS,
			UnifiedMemory: pool.Unified,
			BandwidthGBps: bw / 1e9,
			BandwidthNote: bwNote,
		},
		Memory: PlanMemory{
			TotalGiB:       round1(GiBf(pool.TotalBytes)),
			AvailableGiB:   round1(GiBf(pool.AvailableBytes)),
			HeadroomGiB:    round1(GiBf(floor)),
			HeadroomReason: floorReason,
			AllocatableGiB: round1(GiBf(pool.AllocatableBytes)),
			SwapUsedGiB:    round1(GiBf(pool.SwapUsedBytes())),
			Source:         pool.Source,
			Nodes:          in.Nodes,
		},
		Model: PlanModel{
			ID: m.ID, Name: m.Name, HFRepo: m.HFRepo,
			ParamsB: m.ParamsB, ActiveParamsB: m.ActiveParamsB,
			Layers: m.Layers, AttentionLayers: m.EffectiveAttentionLayers(),
			KVHeads: m.KVHeads, HeadDim: m.HeadDim,
			Quant: string(in.Quant), KVDtype: string(in.KV), KVBytesPerToken: s.KVPerTokenBytes,
			Context: in.Context, Concurrency: in.Concurrency, Profile: profile,
			License: m.License, Notes: m.Notes,
		},
		Fit: PlanFit{
			WeightsGiB: round1(GiBf(s.WeightsBytes)), WeightsMeasured: s.WeightsMeasured,
			KVGiB: round1(GiBf(s.KVBytes)), StateGiB: round1(GiBf(s.StateBytes)),
			RuntimeGiB: round1(GiBf(s.ReserveBytes)), FloorGiB: round1(GiBf(s.FloorBytes)),
			TotalGiB: round1(GiBf(s.TotalBytes)), NowGiB: round1(GiBf(s.NowBytes)),
			PoolGiB: round1(GiBf(s.PoolBytes)), MarginGiB: round1(GiBf(s.MarginBytes)),
			FitsTotal: s.FitsTotal, Utilization: s.Utilization, MaxContextTokens: s.MaxContextTokens,
			PerNode: in.Nodes > 1,
		},
		Estimates: PlanEstimates{
			DecodeCeilingTPS:            round1(s.CeilingAtContextTPS),
			DecodeCeilingWeightsOnlyTPS: round1(s.CeilingWeightsOnlyTPS),
			DecodeBandTPS:               [2]float64{round1(s.BandLowTPS), round1(s.BandHighTPS)},
			PrefillRefTPS:               PrefillReferenceTPS,
			Note:                        s.CeilingNote,
			MeasuredByOthers:            m.MeasuredDecodeTPS,
		},
		Runtime:       cmd,
		Prerequisites: prereqs,
		Warnings:      warnings,
	}
	if report != nil {
		p.Platform.Class = report.Platform.Class
		for _, g := range report.GPUs {
			if g.IsNVIDIA {
				p.Platform.GPU = g.Name
				break
			}
		}
	}
	if s.FitsNowKnown {
		v := s.FitsNow
		p.Fit.FitsNow = &v
	}
	p.Advice = advise(in, s, m, pool)
	p.Warnings = append(p.Warnings, planWarnings(in, s, pool, prereqs, cmd, o.GOOS)...)
	if p.Warnings == nil {
		p.Warnings = []string{} // spec 7.8: warnings[] is always present
	}
	p.Verdict = verdict(in, s)
	p.ExitCode = exitCode(s, prereqs, p.Warnings)
	return p, nil
}

// DefaultOptions returns Options with the "use the spec default" sentinels
// set (HeadroomGiB < 0 means F from spec 7.4; an explicit 0 disables the floor).
func DefaultOptions() Options {
	return Options{HeadroomGiB: -1, Nodes: 1, Timeout: 30}
}

// verdict is the first line of the plan.
func verdict(in Inputs, s Sizing) string {
	where := "on this machine"
	if in.Nodes > 1 {
		where = fmt.Sprintf("per node across %d nodes", in.Nodes)
	}
	name := in.Model.Name
	if !strings.Contains(strings.ToUpper(name), strings.ToUpper(string(in.Quant))) {
		name += " " + strings.ToUpper(string(in.Quant))
	}
	desc := fmt.Sprintf("%s, %s context x %s, %s KV, %s", name, fmtTokens(in.Context), plural(in.Concurrency, "stream"), in.KV, in.Runtime.Display())
	switch {
	case !s.FitsTotal:
		return fmt.Sprintf("DOES NOT FIT: %s needs %s %s, pool is %s (short by %s).", desc, fmtGiB(s.TotalBytes), where, fmtGiB(s.PoolBytes), fmtGiB(-s.MarginBytes))
	case s.FitsNowKnown && !s.FitsNow:
		return fmt.Sprintf("FITS BY DESIGN, NOT RIGHT NOW: %s needs %s %s (pool %s, margin %s) but MemAvailable is %s < %s.", desc, fmtGiB(s.TotalBytes), where, fmtGiB(s.PoolBytes), fmtGiB(s.MarginBytes), fmtGiB(s.AvailBytes), fmtGiB(s.NowBytes))
	}
	return fmt.Sprintf("FITS: %s needs %s %s (pool %s, margin %s).", desc, fmtGiB(s.TotalBytes), where, fmtGiB(s.PoolBytes), fmtGiB(s.MarginBytes))
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// exitCode: 0 fits, 1 fits with warnings, 2 does not fit (spec 7.1; 3 is
// reserved for tool errors and set by the caller).
func exitCode(s Sizing, prereqs []Prereq, warnings []string) int {
	if !s.FitsTotal {
		return types.ExitCritical
	}
	if (s.FitsNowKnown && !s.FitsNow) || len(warnings) > 0 || WorstStatus(prereqs) != StatusPass {
		return types.ExitWarnings
	}
	return types.ExitOK
}

// planWarnings collects the plan-level warnings (spec 7.8 warnings[]).
func planWarnings(in Inputs, s Sizing, pool MemoryPool, prereqs []Prereq, cmd Command, goos string) []string {
	var w []string
	for _, p := range prereqs {
		if p.Status == StatusFail || p.Status == StatusWarn {
			w = append(w, fmt.Sprintf("%s %s: %s", p.Status, p.ID, p.Detail))
		}
	}
	w = append(w, cmd.Unconfirmed...)
	if pool.Discrete {
		w = append(w, "Discrete GPU: the spec's formulas target unified-memory Spark systems; the pool here is dedicated VRAM and the OS floor F (--headroom-gib) may be lowered.")
	}
	if pool.Unified && pool.HugePagesTotal != 0 {
		w = append(w, fmt.Sprintf("HugePages are configured: allocatable = HugePages_Free x Hugepagesize = %s and swap counts 0 (spec 3.3).", fmtGiB(pool.Allocatable())))
	}
	if goos == "windows" && in.Runtime.IsContainer() {
		w = append(w, "Windows on Arm: only llama.cpp (clang-cl) and, when released, Arm64 Ollama/LM Studio are covered (spec 7.6).")
	}
	return w
}

// round1 rounds to one decimal (half away from zero) for every printed figure.
func round1(x float64) float64 {
	return math.Round(x*10) / 10
}

// quantCandidates lists the formats worth comparing for the runtime family,
// least lossy first (spec 7.6: "smallest quant that fits"), always including
// the requested format.
func quantCandidates(m ModelShape, rt Runtime, requested Quant) []Quant {
	var base []Quant
	switch {
	case m.DefaultQuant == string(QuantMXFP4):
		base = []Quant{QuantMXFP4} // native MXFP4 checkpoints have no other release
	case rt == RuntimeLlamaCpp || rt == RuntimeOllama:
		base = []Quant{QuantBF16, QuantQ8_0, QuantQ4KM}
	default:
		base = []Quant{QuantBF16, QuantFP8, QuantNVFP4}
	}
	found := false
	for _, q := range base {
		if q == requested {
			found = true
		}
	}
	if !found {
		base = append(base, requested)
	}
	sort.SliceStable(base, func(i, j int) bool {
		if base[i].Rank() != base[j].Rank() {
			return base[i].Rank() < base[j].Rank()
		}
		return base[i] == requested // the requested format leads its rank
	})
	return base
}

// maxContextDisplay caps the "headroom" figure: beyond 1M tokens the KV cache
// is not what limits the deployment.
const maxContextDisplay = 1 << 20

// advise builds the spec 7.6 quantization advice and the alternatives list.
func advise(in Inputs, s Sizing, m ModelShape, pool MemoryPool) PlanAdvice {
	var a PlanAdvice
	var recommended *QuantOption
	for _, q := range quantCandidates(m, in.Runtime, in.Quant) {
		try := in
		try.Quant = q
		ts := Compute(try)
		opt := QuantOption{Quant: string(q), WeightsGiB: round1(GiBf(ts.WeightsBytes)), TotalGiB: round1(GiBf(ts.TotalBytes)), FitsTotal: ts.FitsTotal, MarginGiB: round1(GiBf(ts.MarginBytes))}
		if ts.FitsNowKnown {
			v := ts.FitsNow
			opt.FitsNow = &v
		}
		a.Quants = append(a.Quants, opt)
		if recommended == nil && ts.FitsTotal && (!ts.FitsNowKnown || ts.FitsNow) {
			o := opt
			recommended = &o
		}
	}
	if recommended == nil {
		for i := range a.Quants {
			if a.Quants[i].FitsTotal {
				recommended = &a.Quants[i]
				break
			}
		}
	}

	req := strings.ToUpper(string(in.Quant))
	switch {
	case recommended == nil:
		a.Reason = fmt.Sprintf("no quantization of %s fits %s context x %s with %s at this pool", m.Name, fmtTokens(in.Context), plural(in.Concurrency, "stream"), in.Runtime.Display())
		a.Lines = append(a.Lines, a.Reason+".")
		if s.MaxContextTokens > 0 {
			a.Lines = append(a.Lines, fmt.Sprintf("At %s the largest context that fits is about %d tokens per stream (%s).", req, s.MaxContextTokens, plural(in.Concurrency, "stream")))
		} else {
			a.Lines = append(a.Lines, fmt.Sprintf("The %s weights alone (%s) plus R + F exceed the pool; choose a smaller model.", req, fmtGiB(s.WeightsBytes)))
		}
	case Quant(recommended.Quant) == in.Quant || (s.FitsTotal && Quant(recommended.Quant).Rank() >= in.Quant.Rank()):
		// The requested format fits and nothing less lossy does.
		a.RecommendedQuant = string(in.Quant)
		a.Reason = fmt.Sprintf("%s is the least lossy format that fits with the %s headroom (margin %s)", req, fmtGiB(s.FloorBytes), fmtGiB(s.MarginBytes))
		a.Lines = append(a.Lines, "Keep "+req+": "+a.Reason+".")
	case s.FitsTotal:
		a.RecommendedQuant = recommended.Quant
		a.Reason = fmt.Sprintf("%s also fits (total %s, margin %s) and is less lossy than %s", strings.ToUpper(recommended.Quant), fmtGiB(recommended.TotalGiB*GiB), fmtGiB(recommended.MarginGiB*GiB), req)
		a.Lines = append(a.Lines, "Consider "+strings.ToUpper(recommended.Quant)+": "+a.Reason+".")
	default:
		a.RecommendedQuant = recommended.Quant
		a.Reason = fmt.Sprintf("%s does not fit; %s is the smallest quantization step that fits (total %s, margin %s)", req, strings.ToUpper(recommended.Quant), fmtGiB(recommended.TotalGiB*GiB), fmtGiB(recommended.MarginGiB*GiB))
		a.Lines = append(a.Lines, "Consider "+strings.ToUpper(recommended.Quant)+": "+a.Reason+".")
	}
	if s.FitsTotal && s.MaxContextTokens > in.Context {
		if s.MaxContextTokens >= maxContextDisplay {
			a.Lines = append(a.Lines, fmt.Sprintf("Headroom: more than 1M tokens per stream would still fit at %s with %s; the KV cache is not the limit here.", req, plural(in.Concurrency, "stream")))
		} else {
			a.Lines = append(a.Lines, fmt.Sprintf("Headroom: up to about %d tokens per stream would still fit at %s with %s.", s.MaxContextTokens, req, plural(in.Concurrency, "stream")))
		}
	}
	if in.Runtime.IsContainer() {
		a.Lines = append(a.Lines, fmt.Sprintf("u = ceil05((W + KV + R) / MemTotal) = %.2f, clamped to 0.30..0.85 (spec 7.4).", s.Utilization))
	}
	if in.Runtime == RuntimeOllama {
		a.Lines = append(a.Lines, "Ollama does not batch: aggregate throughput equals one stream; vLLM aggregate reaches hundreds of tok/s at c=8..256 (spec 7.4).")
	}

	// Catalogue models that would fit at their default quant with the same context/concurrency.
	if !s.FitsTotal {
		for _, alt := range catalogue {
			if alt.ID == m.ID {
				continue
			}
			try := in
			try.Model = alt
			try.Quant = Quant(alt.DefaultQuant)
			if !in.Runtime.SupportsQuant(try.Quant) {
				continue
			}
			ts := Compute(try)
			if ts.FitsTotal {
				a.Alternatives = append(a.Alternatives, ModelOption{ID: alt.ID, Quant: alt.DefaultQuant, TotalGiB: round1(GiBf(ts.TotalBytes))})
			}
		}
		sort.Slice(a.Alternatives, func(i, j int) bool { return a.Alternatives[i].TotalGiB > a.Alternatives[j].TotalGiB })
		if len(a.Alternatives) > 0 {
			var ids []string
			for _, alt := range a.Alternatives {
				ids = append(ids, fmt.Sprintf("%s (%s, %s)", alt.ID, alt.Quant, fmtGiB(alt.TotalGiB*GiB)))
			}
			a.Lines = append(a.Lines, "Catalogue models that fit at the same context and concurrency: "+strings.Join(ids, "; ")+".")
		}
	}
	return a
}
