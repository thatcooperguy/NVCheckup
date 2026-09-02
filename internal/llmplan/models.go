// Package llmplan implements the read-only LLM deployment wizard behind
// "nvcheckup llm-plan" (docs/roadmap/spark-support.md section 7).
//
// Nothing in this package changes system state: it reads the diagnostic
// report, /proc/meminfo (or the Windows equivalent) and local config files,
// does arithmetic, and prints a plan. It never downloads models or images,
// never runs docker/pip/ollama and never writes outside the --out directory
// (spec section 7.9; enforced by mustnot_test.go).
package llmplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModelShape is one row of knowledge/models.json: the transformer geometry
// the KV-cache formula needs (spec section 7.3, HF config.json values, S80)
// plus informational fields (hidden size, heads, vocab, license).
//
// The Go catalogue below is the copy the binary ships with; models.json is
// the reference file and TestModelsJSON_MatchesCatalogue keeps both identical
// (same pattern as knowledge/rules.json and the analyzer).
type ModelShape struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	HFRepo  string   `json:"hf_repo,omitempty"` // base checkpoint; quantized variants are separate repos the user picks

	ParamsB       float64 `json:"params_b"`        // total parameters, billions (spec 7.3 "Params")
	ActiveParamsB float64 `json:"active_params_b"` // active parameters per token (MoE); equals ParamsB for dense models

	Layers          int `json:"layers"`                     // num_hidden_layers (spec 7.3 "L")
	AttentionLayers int `json:"attention_layers,omitempty"` // layers that carry a KV cache; 0 means all Layers (hybrid Mamba models: spec 7.3 Nemotron row)
	HiddenSize      int `json:"hidden_size,omitempty"`      // informational, not in the spec table
	Heads           int `json:"heads,omitempty"`            // num_attention_heads, informational
	KVHeads         int `json:"kv_heads"`                   // num_key_value_heads (spec 7.3 "KV heads")
	HeadDim         int `json:"head_dim"`                   // spec 7.3 "d_head"
	Vocab           int `json:"vocab,omitempty"`            // informational

	// MoE / attention variants (spec 7.2 config.json keys).
	Experts       int `json:"experts,omitempty"`         // num_local_experts
	ExpertsPerTok int `json:"experts_per_tok,omitempty"` // num_experts_per_tok
	SlidingWindow int `json:"sliding_window,omitempty"`  // tokens; spec 7.3: gpt-oss uses 128 on half the layers, KV figure is an upper bound

	DefaultQuant string `json:"default_quant"` // dtype/quant the checkpoint ships in (bf16, mxfp4, nvfp4)

	// KVBytesPerTokenF16 is the spec 7.3 "KV B/token f16" column and is
	// asserted against the formula in models_test.go.
	KVBytesPerTokenF16 int `json:"kv_bytes_per_token_f16"`

	// MeasuredCheckpointGiB overrides the P x b weight formula per quant when
	// the spec quotes a measured checkpoint size (spec 7.4: "prefer the
	// measured checkpoint size when known", e.g. gpt-oss-120b ~61 GB).
	MeasuredCheckpointGiB map[string]float64 `json:"measured_checkpoint_gib,omitempty"`

	// MeasuredSlotGBAt262K is the Nemotron per-Ollama-slot measurement from
	// spec 7.3 (single source S81, decimal GB at a 262K context); the Mamba
	// state term is derived from it in sizing.go.
	MeasuredSlotGBAt262K float64 `json:"measured_slot_gb_at_262k,omitempty"`

	// MeasuredDecodeTPS is the measured single-stream decode range the spec
	// quotes for this model on GB10 (spec 7.4/7.5), printed as "measured by
	// others", never as a prediction for this machine.
	MeasuredDecodeTPS string `json:"measured_decode_tps,omitempty"`

	// NoFormulaCeiling marks rows for which the spec says no formula ceiling
	// may be printed (spec 7.5 gpt-oss-120b: active-weight bytes of a mixed
	// bf16/MXFP4 MoE checkpoint are not known to +/-10%).
	NoFormulaCeiling bool `json:"no_formula_ceiling,omitempty"`

	OllamaArch string `json:"ollama_arch,omitempty"` // llama.cpp architecture name, for the Ollama FA/q8_0 KV rule (spec 7.6)
	License    string `json:"license,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Source     string `json:"source"` // spec section and source ids the row was taken from
}

// modelsFileVersion is the "version" field expected in knowledge/models.json.
const modelsFileVersion = "1"

// ModelsFile is the on-disk layout of knowledge/models.json.
type ModelsFile struct {
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Models      []ModelShape `json:"models"`
}

// catalogue is the built-in copy of knowledge/models.json (spec section 7.3).
// Geometry columns (params, L, KV heads, d_head, KV B/token) are verbatim
// from the spec table; hidden/heads/vocab/license are informational values
// from the public HF config.json files (S80) and are not used in any formula.
var catalogue = []ModelShape{
	{
		ID: "llama-3.1-8b-instruct", Name: "Llama 3.1 8B Instruct",
		Aliases: []string{"llama3.1-8b", "llama-3.1-8b", "llama-8b", "8b"},
		HFRepo:  "meta-llama/Llama-3.1-8B-Instruct",
		ParamsB: 8.03, ActiveParamsB: 8.03,
		Layers: 32, HiddenSize: 4096, Heads: 32, KVHeads: 8, HeadDim: 128, Vocab: 128256,
		DefaultQuant: "bf16", KVBytesPerTokenF16: 131072,
		MeasuredDecodeTPS: "8B FP8: 20.5 tok/s measured (S89), vs 34 weights-only / 22 at 32K",
		OllamaArch:        "llama", License: "Llama 3.1 Community License",
		Source: "spec 7.3 row 1 (S80)",
	},
	{
		ID: "llama-3.3-70b-instruct", Name: "Llama 3.3 70B Instruct / DeepSeek-R1-Distill-Llama-70B",
		Aliases: []string{"llama3.3-70b", "llama-3.3-70b", "llama-70b", "70b", "r1-distill-llama-70b", "deepseek-r1-distill-llama-70b"},
		HFRepo:  "meta-llama/Llama-3.3-70B-Instruct",
		ParamsB: 70.6, ActiveParamsB: 70.6,
		Layers: 80, HiddenSize: 8192, Heads: 64, KVHeads: 8, HeadDim: 128, Vocab: 128256,
		DefaultQuant: "bf16", KVBytesPerTokenF16: 327680,
		MeasuredDecodeTPS: "70B FP8: 2.7 tok/s measured (S89), vs 3.9 weights-only",
		OllamaArch:        "llama", License: "Llama 3.3 Community License (R1-Distill: MIT)",
		Source: "spec 7.3 row 2 (S80)",
	},
	{
		ID: "qwen3-32b", Name: "Qwen3-32B / DeepSeek-R1-Distill-Qwen-32B",
		Aliases: []string{"qwen-32b", "32b", "r1-distill-qwen-32b", "deepseek-r1-distill-qwen-32b"},
		HFRepo:  "Qwen/Qwen3-32B",
		ParamsB: 32.8, ActiveParamsB: 32.8,
		Layers: 64, HiddenSize: 5120, Heads: 64, KVHeads: 8, HeadDim: 128, Vocab: 151936,
		DefaultQuant: "bf16", KVBytesPerTokenF16: 262144,
		OllamaArch: "qwen3", License: "Apache-2.0 (R1-Distill: MIT)",
		Source: "spec 7.3 row 3 (S80)",
	},
	{
		ID: "qwen3-235b-a22b", Name: "Qwen3-235B-A22B",
		Aliases: []string{"qwen3-235b", "qwen-235b", "235b"},
		HFRepo:  "Qwen/Qwen3-235B-A22B",
		ParamsB: 235, ActiveParamsB: 22,
		Layers: 94, HiddenSize: 4096, Heads: 64, KVHeads: 4, HeadDim: 128, Vocab: 151936,
		Experts: 128, ExpertsPerTok: 8,
		DefaultQuant: "bf16", KVBytesPerTokenF16: 192512,
		OllamaArch: "qwen3moe", License: "Apache-2.0",
		Notes:  "NVFP4 = 235e9 x 0.56 = 122.6 GiB > 119.7 GiB: does not fit one Spark; NVIDIA lists it multi-node only (spec 7.5, S91).",
		Source: "spec 7.3 row 4 (S80, S82)",
	},
	{
		ID: "gpt-oss-120b", Name: "gpt-oss-120b (MXFP4)",
		Aliases: []string{"gptoss-120b", "gpt-oss-120", "120b"},
		HFRepo:  "openai/gpt-oss-120b",
		ParamsB: 117, ActiveParamsB: 5.1,
		Layers: 36, HiddenSize: 2880, Heads: 64, KVHeads: 8, HeadDim: 64, Vocab: 201088,
		Experts: 128, ExpertsPerTok: 4, SlidingWindow: 128,
		DefaultQuant: "mxfp4", KVBytesPerTokenF16: 73728,
		MeasuredCheckpointGiB: map[string]float64{"mxfp4": 56.8},
		MeasuredDecodeTPS:     "42-61 tok/s measured (S90)",
		NoFormulaCeiling:      true,
		OllamaArch:            "gptoss", License: "Apache-2.0",
		Notes:  "KV B/token is an upper bound: half the layers use a 128-token sliding window (spec 7.3). Checkpoint ~61 GB = 56.8 GiB (spec 7.4/7.5).",
		Source: "spec 7.3 row 5 (S80)",
	},
	{
		ID: "gpt-oss-20b", Name: "gpt-oss-20b (MXFP4)",
		Aliases: []string{"gptoss-20b", "gpt-oss-20", "20b"},
		HFRepo:  "openai/gpt-oss-20b",
		ParamsB: 21, ActiveParamsB: 3.6,
		Layers: 24, HiddenSize: 2880, Heads: 64, KVHeads: 8, HeadDim: 64, Vocab: 201088,
		Experts: 32, ExpertsPerTok: 4, SlidingWindow: 128,
		DefaultQuant: "mxfp4", KVBytesPerTokenF16: 49152,
		MeasuredCheckpointGiB: map[string]float64{"mxfp4": 12.1},
		NoFormulaCeiling:      true,
		OllamaArch:            "gptoss", License: "Apache-2.0",
		Notes:  "KV B/token is an upper bound (sliding window on half the layers). Checkpoint 12.1 GiB (spec 7.5, 64 GB column).",
		Source: "spec 7.3 row 6 (S80)",
	},
	{
		ID: "nemotron-3-super-120b-a12b-nvfp4", Name: "Nemotron-3-Super-120B-A12B NVFP4",
		Aliases: []string{"nemotron-3-super", "nemotron-super-120b", "nemotron-120b"},
		ParamsB: 120, ActiveParamsB: 12,
		Layers: 88, AttentionLayers: 8, KVHeads: 2, HeadDim: 128,
		DefaultQuant: "nvfp4", KVBytesPerTokenF16: 8192,
		MeasuredSlotGBAt262K: 7.0,
		MeasuredDecodeTPS:    "23-38 tok/s measured (spec 7.4)",
		License:              "NVIDIA Open Model License",
		Notes:                "Hybrid Mamba: 88 layers of which 8 attention (single source S81, unconfirmed); ~8 KiB attention KV/token plus a Mamba state term derived from the measured ~7 GB per Ollama slot at 262K (S81). hidden/heads/vocab not published in the spec.",
		Source:               "spec 7.3 row 7 (S81, single source)",
	},
}

// Catalogue returns the built-in model shapes in spec order.
func Catalogue() []ModelShape {
	out := make([]ModelShape, len(catalogue))
	copy(out, catalogue)
	return out
}

// FindModel resolves a --model value against ids, aliases and names
// (case-insensitive; "/", "_" and spaces are treated like "-").
func FindModel(name string) (ModelShape, bool) {
	return findIn(catalogue, name)
}

func findIn(shapes []ModelShape, name string) (ModelShape, bool) {
	key := normalizeName(name)
	if key == "" {
		return ModelShape{}, false
	}
	for _, m := range shapes {
		if normalizeName(m.ID) == key || normalizeName(m.Name) == key {
			return m, true
		}
		for _, a := range m.Aliases {
			if normalizeName(a) == key {
				return m, true
			}
		}
	}
	// Last resort: a unique id/alias prefix match ("qwen3-235" -> qwen3-235b-a22b).
	var hits []ModelShape
	for _, m := range shapes {
		cands := append([]string{m.ID}, m.Aliases...)
		for _, c := range cands {
			if strings.HasPrefix(normalizeName(c), key) {
				hits = append(hits, m)
				break
			}
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return ModelShape{}, false
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(s)
	return s
}

// EffectiveAttentionLayers is the number of layers that hold a KV cache.
func (m ModelShape) EffectiveAttentionLayers() int {
	if m.AttentionLayers > 0 {
		return m.AttentionLayers
	}
	return m.Layers
}

// KVBytesPerToken is spec 7.4: k = 2 x L x H_kv x d_head x bytes_kv.
func (m ModelShape) KVBytesPerToken(bytesKV float64) float64 {
	return 2 * float64(m.EffectiveAttentionLayers()) * float64(m.KVHeads) * float64(m.HeadDim) * bytesKV
}

// IsMoE reports whether the shape has an expert layer.
func (m ModelShape) IsMoE() bool { return m.Experts > 0 || m.ActiveParamsB < m.ParamsB }

// Validate checks that a shape has everything the formulas need.
func (m ModelShape) Validate() error {
	switch {
	case m.ID == "":
		return fmt.Errorf("model shape without id")
	case m.ParamsB <= 0:
		return fmt.Errorf("%s: params_b must be > 0", m.ID)
	case m.ActiveParamsB <= 0 || m.ActiveParamsB > m.ParamsB:
		return fmt.Errorf("%s: active_params_b must be in (0, params_b]", m.ID)
	case m.Layers <= 0:
		return fmt.Errorf("%s: layers must be > 0", m.ID)
	case m.AttentionLayers < 0 || m.AttentionLayers > m.Layers:
		return fmt.Errorf("%s: attention_layers must be in [0, layers]", m.ID)
	case m.KVHeads <= 0:
		return fmt.Errorf("%s: kv_heads must be > 0", m.ID)
	case m.HeadDim <= 0:
		return fmt.Errorf("%s: head_dim must be > 0", m.ID)
	case m.DefaultQuant == "":
		return fmt.Errorf("%s: default_quant missing", m.ID)
	}
	if _, ok := ParseQuant(m.DefaultQuant); !ok {
		return fmt.Errorf("%s: unknown default_quant %q", m.ID, m.DefaultQuant)
	}
	if m.KVBytesPerTokenF16 != 0 && float64(m.KVBytesPerTokenF16) != m.KVBytesPerToken(2) {
		return fmt.Errorf("%s: kv_bytes_per_token_f16 %d does not match 2*L*H_kv*d_head*2 = %.0f", m.ID, m.KVBytesPerTokenF16, m.KVBytesPerToken(2))
	}
	return nil
}

// LoadModelsFile parses and validates a models.json file.
func LoadModelsFile(path string) (ModelsFile, error) {
	var f ModelsFile
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version != modelsFileVersion {
		return f, fmt.Errorf("%s: version %q, want %q", path, f.Version, modelsFileVersion)
	}
	seen := map[string]bool{}
	for _, m := range f.Models {
		if err := m.Validate(); err != nil {
			return f, fmt.Errorf("%s: %w", path, err)
		}
		if seen[m.ID] {
			return f, fmt.Errorf("%s: duplicate id %q", path, m.ID)
		}
		seen[m.ID] = true
	}
	return f, nil
}

// CustomShape builds a shape from the --params/--active-params/--layers/
// --kv-heads/--head-dim (or --hidden/--heads) flags (spec 7.1).
func CustomShape(paramsB, activeB float64, layers, kvHeads, headDim, hidden, heads int, quant string) (ModelShape, error) {
	if headDim == 0 && hidden > 0 && heads > 0 {
		headDim = hidden / heads // spec 7.2: head_dim or hidden_size/num_attention_heads
	}
	if activeB == 0 {
		activeB = paramsB
	}
	if quant == "" {
		quant = "bf16"
	}
	m := ModelShape{
		ID: "custom", Name: fmt.Sprintf("custom %.4gB model", paramsB),
		ParamsB: paramsB, ActiveParamsB: activeB,
		Layers: layers, HiddenSize: hidden, Heads: heads, KVHeads: kvHeads, HeadDim: headDim,
		DefaultQuant: quant, Source: "command-line flags",
	}
	if err := m.Validate(); err != nil {
		return m, fmt.Errorf("custom shape: %w (need --params, --layers, --kv-heads and --head-dim or --hidden/--heads)", err)
	}
	return m, nil
}

// hfConfig mirrors the config.json keys spec 7.2 names.
type hfConfig struct {
	Architectures      []string `json:"architectures"`
	ModelType          string   `json:"model_type"`
	NumHiddenLayers    int      `json:"num_hidden_layers"`
	NumKeyValueHeads   int      `json:"num_key_value_heads"`
	NumAttentionHeads  int      `json:"num_attention_heads"`
	HiddenSize         int      `json:"hidden_size"`
	HeadDim            int      `json:"head_dim"`
	VocabSize          int      `json:"vocab_size"`
	NumLocalExperts    int      `json:"num_local_experts"`
	NumExpertsPerTok   int      `json:"num_experts_per_tok"`
	SlidingWindow      int      `json:"sliding_window"`
	TorchDtype         string   `json:"torch_dtype"`
	QuantizationConfig struct {
		QuantMethod string `json:"quant_method"`
		QuantAlgo   string `json:"quant_algo"`
	} `json:"quantization_config"`
}

// ParseHFConfig reads a local Hugging Face config.json offline (spec 7.1
// --hf-config). The parameter count is not in config.json, so paramsB (and
// optionally activeB) must be supplied by the caller.
func ParseHFConfig(path string, paramsB, activeB float64) (ModelShape, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelShape{}, err
	}
	var c hfConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return ModelShape{}, fmt.Errorf("%s: %w", path, err)
	}
	if paramsB <= 0 {
		return ModelShape{}, fmt.Errorf("--hf-config needs --params B (config.json does not record the parameter count)")
	}
	if activeB == 0 {
		activeB = paramsB
	}
	headDim := c.HeadDim
	if headDim == 0 && c.NumAttentionHeads > 0 {
		headDim = c.HiddenSize / c.NumAttentionHeads
	}
	kvHeads := c.NumKeyValueHeads
	if kvHeads == 0 {
		kvHeads = c.NumAttentionHeads // MHA: no GQA key present
	}
	quant := hfQuant(c)
	name := c.ModelType
	if len(c.Architectures) > 0 {
		name = c.Architectures[0]
	}
	// Only the base name goes into the plan: the full path usually contains the
	// home directory / user name and plan.* is not passed through internal/redact.
	m := ModelShape{
		ID: "hf-config", Name: fmt.Sprintf("%s (%s)", name, filepath.Base(path)),
		ParamsB: paramsB, ActiveParamsB: activeB,
		Layers: c.NumHiddenLayers, HiddenSize: c.HiddenSize, Heads: c.NumAttentionHeads,
		KVHeads: kvHeads, HeadDim: headDim, Vocab: c.VocabSize,
		Experts: c.NumLocalExperts, ExpertsPerTok: c.NumExpertsPerTok, SlidingWindow: c.SlidingWindow,
		DefaultQuant: quant, OllamaArch: c.ModelType, Source: "local config.json (offline)",
	}
	if err := m.Validate(); err != nil {
		return m, fmt.Errorf("hf-config: %w", err)
	}
	return m, nil
}

// hfQuant maps quantization_config / torch_dtype to a wizard quant name.
func hfQuant(c hfConfig) string {
	q := strings.ToLower(c.QuantizationConfig.QuantMethod + " " + c.QuantizationConfig.QuantAlgo)
	switch {
	case strings.Contains(q, "mxfp4"):
		return "mxfp4"
	case strings.Contains(q, "nvfp4"), strings.Contains(q, "fp4"):
		return "nvfp4"
	case strings.Contains(q, "fp8"):
		return "fp8"
	}
	switch strings.ToLower(c.TorchDtype) {
	case "float16", "fp16":
		return "fp16"
	}
	return "bf16"
}

// ListModelsText renders the catalogue for --list-models.
func ListModelsText(shapes []ModelShape) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-36s %-12s %-6s %-3s %-6s %-12s %s\n", "ID", "Params(B)", "L", "KV", "d_head", "KV B/tok f16", "Default")
	for _, m := range shapes {
		params := fmt.Sprintf("%.4g", m.ParamsB)
		if m.ActiveParamsB != m.ParamsB {
			params = fmt.Sprintf("%.4g(%.3g)", m.ParamsB, m.ActiveParamsB)
		}
		layers := fmt.Sprintf("%d", m.Layers)
		if m.AttentionLayers > 0 && m.AttentionLayers != m.Layers {
			layers = fmt.Sprintf("%d/%d", m.AttentionLayers, m.Layers)
		}
		fmt.Fprintf(&b, "%-36s %-12s %-6s %-3d %-6d %-12d %s\n", m.ID, params, layers, m.KVHeads, m.HeadDim, m.KVBytesPerTokenF16, m.DefaultQuant)
		if len(m.Aliases) > 0 {
			aliases := append([]string(nil), m.Aliases...)
			sort.Strings(aliases)
			fmt.Fprintf(&b, "%-36s aliases: %s\n", "", strings.Join(aliases, ", "))
		}
	}
	b.WriteString("\nL = layers (attention/total for hybrid models). Custom shapes: --params B [--active-params B] --layers N --kv-heads N --head-dim N (or --hidden N --heads N), or --hf-config config.json --params B.\n")
	return b.String()
}
