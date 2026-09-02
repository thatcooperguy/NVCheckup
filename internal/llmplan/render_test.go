package llmplan

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

type goldenCase struct {
	name string
	opts func(o *Options)
	rep  func() *types.Report
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{"gb10_llama8b_agent_vllm", func(o *Options) {
			o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "agent", "vllm"
		}, gb10Report},
		{"gb10_llama70b_bf16_nofit", func(o *Options) {
			o.Model, o.Quant, o.Context, o.Runtime = "llama-3.3-70b-instruct", "bf16", 131072, "vllm"
		}, gb10Report},
		{"gb10_gptoss120b_ollama_cluster", func(o *Options) {
			o.Model, o.Profile, o.Runtime, o.Nodes = "gpt-oss-120b", "agent", "ollama", 2
		}, func() *types.Report {
			r := gb10Report()
			r.Cluster = &types.ClusterInfo{Ports: []types.FabricPort{
				{RDMADev: "rocep1s0f0", State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000},
				{RDMADev: "roceP2p1s0f0", State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000},
			}}
			return r
		}},
		{"gb10_nemotron_llamacpp_64k", func(o *Options) {
			o.Model, o.Context, o.Concurrency, o.Runtime = "nemotron-3-super", 65536, 2, "llamacpp"
		}, gb10Report},
	}
}

func buildGolden(t *testing.T, gc goldenCase) *Plan {
	t.Helper()
	t.Setenv("TRITON_PTXAS_PATH", "")
	t.Setenv("NVC_SIM_ROOT", "")
	o := DefaultOptions()
	o.GOOS = "linux"
	gc.opts(&o)
	r := gc.rep()
	pool := poolFromUnifiedMemory(r.UnifiedMemory)
	p, err := Build(r, pool, nil, true, o)
	if err != nil {
		t.Fatalf("%s: %v", gc.name, err)
	}
	return p
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run: go test ./internal/llmplan -update)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden (run: go test ./internal/llmplan -update)\n--- got ---\n%s", path, got)
	}
}

func TestRender_Golden(t *testing.T) {
	for _, gc := range goldenCases() {
		t.Run(gc.name, func(t *testing.T) {
			p := buildGolden(t, gc)
			checkGolden(t, gc.name+".txt", RenderText(p))
			checkGolden(t, gc.name+".md", RenderMarkdown(p))
			js, err := RenderJSON(p)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, gc.name+".json", js)
		})
	}
}

// TestRender_Order: verdict first, then sizing, advice, prerequisites, commands.
func TestRender_Order(t *testing.T) {
	p := buildGolden(t, goldenCases()[0])
	txt := RenderText(p)
	order := []string{"VERDICT", "SIZING", "ESTIMATES", "ADVICE", "PREREQUISITES", "COMMANDS"}
	last := -1
	for _, sec := range order {
		i := strings.Index(txt, "\n"+sec)
		if i < 0 {
			t.Fatalf("section %s missing", sec)
		}
		if i < last {
			t.Errorf("section %s out of order", sec)
		}
		last = i
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.SplitN(txt, "VERDICT\n", 2)[1]), "FITS:") {
		t.Error("the verdict must come first and say FITS")
	}
	if !strings.Contains(txt, "source: report.unified_memory") {
		t.Error("the plan must say where the pool came from")
	}
}

func TestRenderJSON_Schema(t *testing.T) {
	p := buildGolden(t, goldenCases()[0])
	js, err := RenderJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"platform"`, `"memory"`, `"total_gib"`, `"available_gib"`, `"headroom_gib"`, `"model"`, `"fit"`, `"weights_gib"`, `"kv_gib"`, `"runtime_gib"`, `"floor_gib"`, `"fits_total"`, `"fits_now"`, `"estimates"`, `"decode_ceiling_tps"`, `"decode_band_tps"`, `"prefill_ref_tps"`, `"runtime"`, `"image"`, `"command"`, `"env"`, `"prerequisites"`, `"warnings"`} {
		if !strings.Contains(js, key) {
			t.Errorf("plan.json lacks %s (spec 7.8)", key)
		}
	}
}
