package llmplan

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var updateModels = flag.Bool("update-models", false, "rewrite knowledge/models.json from the Go catalogue")

func modelsJSONPath() string { return filepath.Join("..", "..", "knowledge", "models.json") }

// TestModelsJSON_MatchesCatalogue keeps knowledge/models.json identical to the
// built-in catalogue (same pattern as rules.json vs the analyzer). Regenerate
// with: go test ./internal/llmplan -run TestModelsJSON -update-models
func TestModelsJSON_MatchesCatalogue(t *testing.T) {
	want := ModelsFile{
		Description: "Model shapes for nvcheckup llm-plan (docs/roadmap/spark-support.md section 7.3, HF config.json values S80/S81). The Go catalogue in internal/llmplan/models.go is the shipped copy; TestModelsJSON_MatchesCatalogue keeps this file identical to it.",
		Version:     modelsFileVersion,
		Models:      Catalogue(),
	}
	if *updateModels {
		data, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(modelsJSONPath(), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadModelsFile(modelsJSONPath())
	if err != nil {
		t.Fatalf("knowledge/models.json: %v", err)
	}
	// Round-trip the catalogue through JSON so nil/empty slices compare equal.
	var wantRT ModelsFile
	data, _ := json.Marshal(want)
	_ = json.Unmarshal(data, &wantRT)
	if !reflect.DeepEqual(got, wantRT) {
		t.Errorf("knowledge/models.json differs from the Go catalogue; run: go test ./internal/llmplan -run TestModelsJSON -update-models")
	}
}

// TestCatalogue_MatchesSpecTable asserts the spec 7.3 table column by column.
func TestCatalogue_MatchesSpecTable(t *testing.T) {
	rows := []struct {
		id                   string
		params, active       float64
		layers, attnLayers   int
		kvHeads, headDim, kv int
		quant                string
	}{
		{"llama-3.1-8b-instruct", 8.03, 8.03, 32, 32, 8, 128, 131072, "bf16"},
		{"llama-3.3-70b-instruct", 70.6, 70.6, 80, 80, 8, 128, 327680, "bf16"},
		{"qwen3-32b", 32.8, 32.8, 64, 64, 8, 128, 262144, "bf16"},
		{"qwen3-235b-a22b", 235, 22, 94, 94, 4, 128, 192512, "bf16"},
		{"gpt-oss-120b", 117, 5.1, 36, 36, 8, 64, 73728, "mxfp4"},
		{"gpt-oss-20b", 21, 3.6, 24, 24, 8, 64, 49152, "mxfp4"},
		{"nemotron-3-super-120b-a12b-nvfp4", 120, 12, 88, 8, 2, 128, 8192, "nvfp4"},
	}
	if len(Catalogue()) != len(rows) {
		t.Fatalf("catalogue has %d models, spec 7.3 lists %d", len(Catalogue()), len(rows))
	}
	for i, r := range rows {
		m := Catalogue()[i]
		if m.ID != r.id {
			t.Errorf("row %d id %q, want %q (spec order)", i, m.ID, r.id)
		}
		if m.ParamsB != r.params || m.ActiveParamsB != r.active {
			t.Errorf("%s params %v (%v), want %v (%v)", r.id, m.ParamsB, m.ActiveParamsB, r.params, r.active)
		}
		if m.Layers != r.layers || m.EffectiveAttentionLayers() != r.attnLayers {
			t.Errorf("%s layers %d/%d, want %d/%d", r.id, m.EffectiveAttentionLayers(), m.Layers, r.attnLayers, r.layers)
		}
		if m.KVHeads != r.kvHeads || m.HeadDim != r.headDim {
			t.Errorf("%s kv heads %d d_head %d, want %d %d", r.id, m.KVHeads, m.HeadDim, r.kvHeads, r.headDim)
		}
		if m.KVBytesPerTokenF16 != r.kv {
			t.Errorf("%s KV B/token %d, want %d", r.id, m.KVBytesPerTokenF16, r.kv)
		}
		if m.DefaultQuant != r.quant {
			t.Errorf("%s default quant %s, want %s", r.id, m.DefaultQuant, r.quant)
		}
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", r.id, err)
		}
	}
	// Measured checkpoint sizes of spec 7.4/7.5.
	if Catalogue()[4].MeasuredCheckpointGiB["mxfp4"] != 56.8 || Catalogue()[5].MeasuredCheckpointGiB["mxfp4"] != 12.1 {
		t.Error("gpt-oss measured checkpoint sizes must be 56.8 / 12.1 GiB")
	}
	if Catalogue()[6].MeasuredSlotGBAt262K != 7.0 {
		t.Error("Nemotron per-slot measurement must be 7 GB at 262K")
	}
}

func TestFindModel(t *testing.T) {
	for alias, id := range map[string]string{
		"llama-3.1-8b-instruct":        "llama-3.1-8b-instruct",
		"8b":                           "llama-3.1-8b-instruct",
		"Llama 3.1 8B Instruct":        "llama-3.1-8b-instruct",
		"R1-Distill-Llama-70B":         "llama-3.3-70b-instruct",
		"deepseek-r1-distill-qwen-32b": "qwen3-32b",
		"gptoss-120b":                  "gpt-oss-120b",
		"qwen3-235":                    "qwen3-235b-a22b",
		"nemotron-3-super":             "nemotron-3-super-120b-a12b-nvfp4",
	} {
		m, ok := FindModel(alias)
		if !ok || m.ID != id {
			t.Errorf("FindModel(%q) = %q,%v want %q", alias, m.ID, ok, id)
		}
	}
	if _, ok := FindModel("gpt-oss"); ok {
		t.Error("ambiguous prefix must not resolve")
	}
	if _, ok := FindModel("no-such-model"); ok {
		t.Error("unknown model must not resolve")
	}
}

func TestCustomShape(t *testing.T) {
	m, err := CustomShape(8.03, 0, 32, 8, 0, 4096, 32, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.HeadDim != 128 || m.KVBytesPerToken(2) != 131072 || m.DefaultQuant != "bf16" || m.ActiveParamsB != 8.03 {
		t.Errorf("custom shape = %+v", m)
	}
	if _, err := CustomShape(8, 0, 0, 8, 128, 0, 0, ""); err == nil {
		t.Error("missing layers must fail")
	}
}

func TestParseHFConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{"architectures":["LlamaForCausalLM"],"model_type":"llama","num_hidden_layers":32,"num_attention_heads":32,
	"num_key_value_heads":8,"hidden_size":4096,"vocab_size":128256,"torch_dtype":"bfloat16",
	"quantization_config":{"quant_method":"fp8","activation_scheme":"static"}}`
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseHFConfig(path, 8.03, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.HeadDim != 128 || m.KVHeads != 8 || m.Layers != 32 || m.KVBytesPerToken(2) != 131072 {
		t.Errorf("shape from config.json = %+v", m)
	}
	if strings.Contains(m.Name, filepath.Dir(path)) || !strings.Contains(m.Name, "config.json") {
		t.Errorf("model name must carry only the base name of --hf-config, got %q", m.Name)
	}
	if m.DefaultQuant != "fp8" || m.OllamaArch != "llama" || m.Vocab != 128256 {
		t.Errorf("quant/arch/vocab from config.json = %s %s %d", m.DefaultQuant, m.OllamaArch, m.Vocab)
	}
	if _, err := ParseHFConfig(path, 0, 0); err == nil {
		t.Error("--hf-config without --params must fail")
	}
}

func TestLoadModelsFile_Rejects(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "models.json")
	if err := os.WriteFile(bad, []byte(`{"version":"1","models":[{"id":"x","params_b":1,"active_params_b":1,"layers":2,"kv_heads":1,"head_dim":8,"default_quant":"bf16","kv_bytes_per_token_f16":999}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModelsFile(bad); err == nil {
		t.Error("kv_bytes_per_token_f16 inconsistent with the formula must be rejected")
	}
}
