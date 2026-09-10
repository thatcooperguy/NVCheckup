package llmplan

import (
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// These assertions compare what a runtime will allocate/load with what was
// sized, rather than just maintaining literal command snapshots.
func TestRecipeMatchesPlannedWorkload(t *testing.T) {
	for _, rt := range AllRuntimes {
		in, _ := llama8BInputs(t, rt, rt.DefaultKV(mustModel(t, "8b")))
		in.Context, in.Concurrency = 8192, 3
		cmd := RenderCommand(in, Compute(in), "chat", ClusterFacts{})
		var contextFlag, concurrencyFlag string
		switch rt {
		case RuntimeVLLM:
			contextFlag, concurrencyFlag = "--max-model-len 8192", "--max-num-seqs 3"
		case RuntimeTRTLLM:
			contextFlag, concurrencyFlag = "--max_seq_len 8192", "--max_batch_size 3"
		case RuntimeSGLang:
			contextFlag, concurrencyFlag = "--context-length 8192", "--max-running-requests 3"
		case RuntimeLlamaCpp:
			contextFlag, concurrencyFlag = "-c 24576", "-np 3"
		case RuntimeOllama:
			contextFlag, concurrencyFlag = "OLLAMA_CONTEXT_LENGTH=8192", "OLLAMA_NUM_PARALLEL=3"
		}
		text := cmd.Command + " " + strings.Join(cmd.Env, " ")
		if !strings.Contains(text, contextFlag) || !strings.Contains(text, concurrencyFlag) {
			t.Errorf("%s does not enforce the sized context/concurrency: %s", rt, text)
		}
	}
}

func TestQuantizedRecipesNeverLoadBaseWeights(t *testing.T) {
	for _, rt := range []Runtime{RuntimeVLLM, RuntimeTRTLLM, RuntimeSGLang} {
		in, _ := llama8BInputs(t, rt, KVF16)
		in.Quant = QuantNVFP4
		cmd := RenderCommand(in, Compute(in), "chat", ClusterFacts{})
		if strings.Contains(cmd.Command, in.Model.HFRepo) || !strings.Contains(cmd.Command, "{quantized-model-repo}") || len(cmd.Unconfirmed) == 0 {
			t.Errorf("%s must not start BF16 weights after sizing NVFP4: %+v", rt, cmd)
		}
		// MXFP4 and NVFP4 have the same lossy rank, but cannot share a checkpoint.
		in.Model.DefaultQuant, in.Quant = string(QuantMXFP4), QuantNVFP4
		cmd = RenderCommand(in, Compute(in), "chat", ClusterFacts{})
		if strings.Contains(cmd.Command, in.Model.HFRepo) {
			t.Errorf("%s treated equal bit-width as equal checkpoint format", rt)
		}
	}
}

func TestTRTRecipePassesConfigAndEnvironmentIntoContainer(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeTRTLLM, KVF16)
	cmd := RenderCommand(in, s, "chat", ClusterFacts{})
	imageAt := strings.Index(cmd.Command, cmd.Image)
	for _, env := range cmd.Env {
		i := strings.Index(cmd.Command, "-e "+env)
		if i < 0 || i > imageAt {
			t.Errorf("environment is not a Docker argument: %s", env)
		}
	}
	if !strings.Contains(cmd.Command, "dst=/etc/nvcheckup/cfg.yaml,readonly") || !strings.Contains(cmd.Command, "--extra_llm_api_options /etc/nvcheckup/cfg.yaml") {
		t.Error("TRT configuration must be mounted at the path passed to the server")
	}
}

func TestWindowsRecipesUseTargetPlatform(t *testing.T) {
	p := buildOffline(t, rtx3090Report(), "windows", func(o *Options) {
		o.Model, o.Runtime = "8b", "ollama"
	})
	text := RenderText(p)
	if strings.Contains(text, "systemctl") || !strings.Contains(text, "$env:OLLAMA_CONTEXT_LENGTH") || p.Runtime.Command != "ollama serve" {
		t.Errorf("Windows Ollama recipe must use PowerShell environment, not systemd: %s", text)
	}
	p = buildOffline(t, rtx3090Report(), "windows", func(o *Options) {
		o.Model, o.Runtime = "8b", "vllm"
	})
	if hasWarning(p, "Windows on Arm") || !hasWarning(p, "WSL2/Linux") {
		t.Errorf("x64 Windows must not be described as Arm: %v", p.Warnings)
	}
}

func TestOfflinePlanIgnoresRenderingHostTritonEnvironment(t *testing.T) {
	r := gb10Report()
	r.Ecosystem = &types.EcosystemInfo{TritonPtxasVersion: "13.0", TritonPtxasPath: "/saved-report/ptxas"}
	o := DefaultOptions()
	o.GOOS, o.Model, o.Runtime, o.Offline = "linux", "8b", "vllm", true
	pool := poolFromUnifiedMemory(r.UnifiedMemory)
	t.Setenv("TRITON_PTXAS_PATH", "/rendering-host/must-not-appear")
	p, err := Build(r, pool, nil, true, o)
	if err != nil {
		t.Fatal(err)
	}
	if text := RenderText(p); strings.Contains(text, "rendering-host") || !strings.Contains(text, "/saved-report/ptxas") {
		t.Errorf("offline plan must use the saved report, not the current process: %s", text)
	}
}

func TestRecipeContextProductOverflowRejected(t *testing.T) {
	_, err := buildGB10(t, func(o *Options) {
		o.Model, o.Runtime = "8b", "llamacpp"
		o.Context, o.Concurrency = 1<<30, 4
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "context x concurrency") {
		t.Fatalf("overflowing total runtime context must fail, got %v", err)
	}
}

func TestImagePreflightChecksExactRequestedImage(t *testing.T) {
	r := gb10Report()
	r.Ecosystem = &types.EcosystemInfo{Images: []types.ContainerImage{
		{Ref: ImageVLLMNGC, Arch: "amd64"},
		{Ref: "nvcr.io/nvidia/vllm:other", Arch: "arm64"},
	}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusFail)
	r.Ecosystem.Images = []types.ContainerImage{{Ref: ImageVLLMNGC, Arch: ""}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusWarn)
	r.System.Architecture = "x86_64"
	r.Ecosystem.Images = []types.ContainerImage{{Ref: ImageVLLMNGC, Arch: "arm64"}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusFail)
}
