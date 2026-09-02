package llmplan

import (
	"encoding/json"
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
		// Discrete GPU (no UnifiedMemory): pool = dedicated VRAM, F = 0 by
		// assumption, the "VRAM free" label and the pool note are exercised.
		{"win_rtx3090_llama8b_chat_llamacpp", func(o *Options) {
			o.GOOS = "windows"
			o.Model, o.Profile, o.Runtime = "llama-3.1-8b-instruct", "chat", "llamacpp"
		}, rtx3090Report},
		// Saved report without any memory figure, sized with --memory-gib: the
		// "MemAvailable unknown" note is exercised and no ceiling is printed.
		{"offline_memorygib64_qwen32b_vllm", func(o *Options) {
			o.Model, o.Runtime, o.MemoryGiB = "qwen3-32b", "vllm", 64
		}, func() *types.Report { return &types.Report{Metadata: types.ReportMetadata{Platform: "linux"}} }},
	}
}

// jsonEscaped is s exactly as encoding/json emits it inside a string (quotes
// and backslashes escaped, <, > and & as unicode escapes).
func jsonEscaped(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// rtx3090Report is a Windows desktop with a discrete 24 GiB GPU (values as
// nvidia-smi reports them on such a machine; nothing Spark-specific).
func rtx3090Report() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{Platform: "windows"},
		System:   types.SystemInfo{OSName: "Windows 11 Pro", Architecture: "x86_64", RAMTotalMB: 65536},
		GPUs: []types.GPUInfo{{
			Index: 0, Name: "NVIDIA GeForce RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true,
			DriverVersion: "591.86", VRAMTotalMB: 24576, VRAMFreeMB: 23000, MemoryReporting: "dedicated",
		}},
		Driver: types.DriverInfo{Version: "591.86", CUDAVersion: "13.0"},
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
	// offline: the golden never reads this host's /proc/meminfo or CIM.
	pool, notes := DerivePool(r, o.GOOS, o.Timeout, o.MemoryGiB, true)
	p, err := Build(r, pool, nil, r.UnifiedMemory != nil, o)
	if err != nil {
		t.Fatalf("%s: %v", gc.name, err)
	}
	p.Notes = append(p.Notes, notes...)
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
	if strings.Contains(txt, "NOTES") {
		t.Error("a plan without pool caveats prints no NOTES block")
	}
}

// TestRender_Notes: the pool caveats (Plan.Notes) are part of every rendering,
// so stdout, plan.txt, plan.md and plan.json all carry them.
func TestRender_Notes(t *testing.T) {
	for _, idx := range []int{4, 5} {
		gc := goldenCases()[idx]
		p := buildGolden(t, gc)
		if len(p.Notes) == 0 {
			t.Fatalf("%s: expected pool notes", gc.name)
		}
		js, err := RenderJSON(p)
		if err != nil {
			t.Fatal(err)
		}
		for name, out := range map[string]string{"text": RenderText(p), "markdown": RenderMarkdown(p)} {
			for _, n := range p.Notes {
				if !strings.Contains(out, n) {
					t.Errorf("%s %s lacks note %q", gc.name, name, n)
				}
			}
		}
		for _, n := range p.Notes {
			if !strings.Contains(js, jsonEscaped(n)) {
				t.Errorf("%s json lacks note %q", gc.name, n)
			}
		}
		txt := RenderText(p)
		if !strings.Contains(txt, "\nNOTES") || strings.Index(txt, "\nNOTES") > strings.Index(txt, "\nExit code") {
			t.Errorf("%s: NOTES block must precede the exit-code line", gc.name)
		}
	}
	// The discrete case: F = 0, "VRAM free" label, and the 8B model fits a 24 GiB card.
	p := buildGolden(t, goldenCases()[4])
	txt := RenderText(p)
	if p.Fit.FloorGiB != 0 || !p.Memory.Discrete || !strings.Contains(txt, "VRAM free:") || strings.Contains(txt, "MemAvailable:") {
		t.Errorf("discrete plan: floor %.1f discrete %v\n%s", p.Fit.FloorGiB, p.Memory.Discrete, txt)
	}
	if !p.Fit.FitsTotal || !strings.HasPrefix(p.Verdict, "FITS:") {
		t.Errorf("8B Q8_0-KV chat on a 24 GiB card must fit: %s", p.Verdict)
	}
}

func TestRenderJSON_Schema(t *testing.T) {
	// Case 0 is vLLM (container image), case 3 llama.cpp (no image): every
	// spec 7.8 key must be present in both, image included.
	for _, idx := range []int{0, 3} {
		testRenderJSONSchema(t, buildGolden(t, goldenCases()[idx]))
	}
}

func testRenderJSONSchema(t *testing.T, p *Plan) {
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
