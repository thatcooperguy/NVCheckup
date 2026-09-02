package llmplan

import (
	"strings"
	"testing"
)

func llama8BInputs(t *testing.T, rt Runtime, kv KVDtype) (Inputs, Sizing) {
	t.Helper()
	in := Inputs{Model: mustModel(t, "llama-3.1-8b-instruct"), Quant: QuantBF16, KV: kv, Context: 32768, Concurrency: 4, Runtime: rt, Nodes: 1,
		PoolBytes: poolGB10Bytes, FloorBytes: floorLinux, BandwidthBytesPerSec: GB10BandwidthBytesPerSec}
	return in, Compute(in)
}

// spec 7.6 vLLM template, filled with the row-1 worked example.
func TestRenderCommand_VLLM(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeVLLM, KVF16)
	c := RenderCommand(in, s, "chat", ClusterFacts{})
	want := "docker run -d --name vllm --ipc=host --gpus all -p 8000:8000 -v ~/.cache/huggingface:/root/.cache/huggingface nvcr.io/nvidia/vllm:26.05-py3 meta-llama/Llama-3.1-8B-Instruct --gpu-memory-utilization 0.40 --max-model-len 32768 --max-num-seqs 4"
	if c.Command != want {
		t.Errorf("vLLM command:\n got %s\nwant %s", c.Command, want)
	}
	if strings.Contains(c.Command, "--kv-cache-dtype") {
		t.Error("--kv-cache-dtype fp8 must never be emitted without an explicit --kv-dtype fp8 (spec 7.6, S92)")
	}
	if c.Image != ImageVLLMNGC {
		t.Errorf("image %s", c.Image)
	}
	agent := RenderCommand(in, s, "agent", ClusterFacts{})
	if !strings.Contains(agent.Command, "--enable-auto-tool-choice --tool-call-parser {p}") {
		t.Error("agent profile must add the tool-call flags of the template")
	}
	in.KV = KVFP8
	fp8 := RenderCommand(in, Compute(in), "chat", ClusterFacts{})
	if !strings.HasSuffix(fp8.Command, " --kv-cache-dtype fp8") || len(fp8.Unconfirmed) == 0 {
		t.Error("explicit fp8 KV must add --kv-cache-dtype fp8 with the S92 caveat")
	}
}

func TestRenderCommand_TRTLLM(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeTRTLLM, KVF16)
	c := RenderCommand(in, s, "chat", ClusterFacts{})
	for _, frag := range []string{
		"docker run --rm -it --gpus all --ipc host --network host --ulimit memlock=-1 --ulimit stack=67108864 -v ~/.cache/huggingface:/root/.cache/huggingface nvcr.io/nvidia/tensorrt-llm/release:1.3.0rc13 trtllm-serve meta-llama/Llama-3.1-8B-Instruct --backend pytorch --port 8355 --max_batch_size 4 --extra_llm_api_options cfg.yaml",
	} {
		if c.Command != frag {
			t.Errorf("TRT-LLM command:\n got %s\nwant %s", c.Command, frag)
		}
	}
	// R = 10 GiB for TRT-LLM: u = ceil05((15 + 16 + 10) / 119.7) = 0.35.
	if strings.Join(c.Extra, "\n") != "cfg.yaml:\nkv_cache_config:\n  free_gpu_memory_fraction: 0.35" {
		t.Errorf("cfg.yaml = %q", strings.Join(c.Extra, "\n"))
	}
	if strings.Join(c.Env, " ") != "TRT_LLM_DISABLE_LOAD_WEIGHTS_IN_PARALLEL=1 TRITON_PTXAS_PATH=/usr/local/cuda/bin/ptxas" {
		t.Errorf("env = %v", c.Env)
	}
}

func TestRenderCommand_SGLang(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeSGLang, KVF16)
	c := RenderCommand(in, s, "chat", ClusterFacts{})
	want := "docker run --gpus all --ipc=host --shm-size 32g -p 30000:30000 lmsysorg/sglang:latest-cu130 python3 -m sglang.launch_server --model-path meta-llama/Llama-3.1-8B-Instruct --host 0.0.0.0 --port 30000 --trust-remote-code --tp 1 --attention-backend flashinfer --mem-fraction-static 0.35"
	if c.Command != want {
		t.Errorf("SGLang command:\n got %s\nwant %s", c.Command, want)
	}
	in.Quant = QuantNVFP4
	if q := RenderCommand(in, Compute(in), "agent", ClusterFacts{}); !strings.Contains(q.Command, "--quantization modelopt_fp4") || !strings.Contains(q.Command, "--reasoning-parser {r} --tool-call-parser {p}") {
		t.Errorf("NVFP4 agent SGLang command: %s", q.Command)
	}
}

func TestRenderCommand_LlamaCpp(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeLlamaCpp, KVQ8_0)
	in.Quant = QuantQ4KM
	c := RenderCommand(in, s, "chat", ClusterFacts{})
	if c.Build != "cmake -B build -DGGML_NATIVE=ON -DGGML_CUDA=ON -DGGML_CURL=ON -DCMAKE_CUDA_ARCHITECTURES=121a-real" {
		t.Errorf("build line: %s", c.Build)
	}
	want := "llama-server -hf meta-llama/Llama-3.1-8B-Instruct:Q4_K_M --host 0.0.0.0 --port 30000 -ngl 999 -fa on --no-mmap -c 32768 -np 4 --cache-type-k q8_0 --cache-type-v q8_0 -b 2048 -ub 2048 --jinja"
	if c.Command != want {
		t.Errorf("llama.cpp command:\n got %s\nwant %s", c.Command, want)
	}
}

func TestRenderCommand_Ollama(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeOllama, KVF16)
	c := RenderCommand(in, s, "chat", ClusterFacts{})
	if c.Command != "systemctl edit ollama.service" {
		t.Errorf("command: %s", c.Command)
	}
	want := `Environment="OLLAMA_FLASH_ATTENTION=1" "OLLAMA_KV_CACHE_TYPE=f16" "OLLAMA_NUM_PARALLEL=4" "OLLAMA_MAX_LOADED_MODELS=1" "OLLAMA_CONTEXT_LENGTH=32768"`
	if len(c.Extra) != 2 || c.Extra[1] != want {
		t.Errorf("drop-in = %q", c.Extra)
	}
	// llama is not FA-capable for q8_0 KV in Ollama: auto picks f16, explicit q8_0 warns.
	if RuntimeOllama.DefaultKV(in.Model) != KVF16 {
		t.Error("Ollama auto KV for llama must be f16")
	}
	if RuntimeOllama.DefaultKV(mustModel(t, "qwen3-32b")) != KVQ8_0 {
		t.Error("Ollama auto KV for qwen3 must be q8_0")
	}
	in.KV = KVQ8_0
	if q := RenderCommand(in, Compute(in), "chat", ClusterFacts{}); len(q.Unconfirmed) == 0 {
		t.Error("q8_0 KV on a non-FA architecture must carry the fallback warning")
	}
}

func TestRenderCommand_Cluster(t *testing.T) {
	in, s := llama8BInputs(t, RuntimeVLLM, KVF16)
	in.Nodes = 2
	c := RenderCommand(in, s, "chat", ClusterFacts{ActiveRDMADevs: []string{"roceP2p1s0f0", "rocep1s0f0"}})
	if strings.Join(c.Env, " ") != "NCCL_IB_HCA=roceP2p1s0f0,rocep1s0f0 NCCL_NET_PLUGIN=none" {
		t.Errorf("cluster env = %v", c.Env)
	}
	if len(c.Unconfirmed) == 0 || !strings.Contains(c.Unconfirmed[0], "no verified multi-node launch template") {
		t.Errorf("two-node variant must be labelled unconfirmed: %v", c.Unconfirmed)
	}
	none := RenderCommand(in, s, "chat", ClusterFacts{})
	if !strings.Contains(none.Env[0], "{rdma-devs-of-the-cabled-cage") {
		t.Errorf("without ACTIVE twins NCCL_IB_HCA must stay a placeholder, got %s", none.Env[0])
	}
}

func TestChooseRuntime(t *testing.T) {
	in, _ := llama8BInputs(t, RuntimeAuto, KVF16)
	if got := ChooseRuntime(in, "linux"); got != RuntimeVLLM {
		t.Errorf("8B BF16 on Linux -> %s, want vllm", got)
	}
	if got := ChooseRuntime(in, "windows"); got != RuntimeLlamaCpp {
		t.Errorf("Windows -> %s, want llamacpp (spec 7.6)", got)
	}
	in.Quant = QuantQ4KM
	if got := ChooseRuntime(in, "linux"); got != RuntimeLlamaCpp {
		t.Errorf("GGUF quant -> %s, want llamacpp", got)
	}
	// 70B BF16 at 128K fits no runtime: fall back to llama.cpp.
	big := Inputs{Model: mustModel(t, "llama-3.3-70b-instruct"), Quant: QuantBF16, KV: KVF16, Context: 131072, Concurrency: 1, PoolBytes: poolGB10Bytes, FloorBytes: floorLinux}
	if got := ChooseRuntime(big, "linux"); got != RuntimeLlamaCpp {
		t.Errorf("nothing fits -> %s, want llamacpp", got)
	}
	// 70B FP8 at 32K: vLLM (65.7 + 10 + 12 + 8 = 95.7) fits.
	big.Quant, big.Context = QuantFP8, 32768
	if got := ChooseRuntime(big, "linux"); got != RuntimeVLLM {
		t.Errorf("70B FP8 32K -> %s, want vllm", got)
	}
}

func TestParseRuntimeAndKV(t *testing.T) {
	for s, want := range map[string]Runtime{"vLLM": RuntimeVLLM, "trt-llm": RuntimeTRTLLM, "tensorrt-llm": RuntimeTRTLLM, "llama.cpp": RuntimeLlamaCpp, "": RuntimeAuto, "ollama": RuntimeOllama, "sglang": RuntimeSGLang} {
		if got, ok := ParseRuntime(s); !ok || got != want {
			t.Errorf("ParseRuntime(%q) = %s,%v", s, got, ok)
		}
	}
	if _, ok := ParseRuntime("tgi"); ok {
		t.Error("unknown runtime accepted")
	}
	if !RuntimeVLLM.SupportsKV(KVFP8) || RuntimeVLLM.SupportsKV(KVQ8_0) || !RuntimeLlamaCpp.SupportsKV(KVQ4_0) || RuntimeOllama.SupportsKV(KVFP8) {
		t.Error("SupportsKV matrix wrong")
	}
	if RuntimeVLLM.SupportsQuant(QuantQ4KM) || !RuntimeOllama.SupportsQuant(QuantQ4KM) || !RuntimeVLLM.SupportsQuant(QuantNVFP4) {
		t.Error("SupportsQuant matrix wrong")
	}
}
