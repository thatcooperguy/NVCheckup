package llmplan

import (
	"fmt"
	"strings"
)

// Runtime is an inference server the wizard has a flag template for (spec 7.6).
type Runtime string

// Runtime names as accepted by --runtime (spec 7.1).
const (
	RuntimeAuto     Runtime = "auto"
	RuntimeVLLM     Runtime = "vllm"
	RuntimeTRTLLM   Runtime = "trtllm"
	RuntimeSGLang   Runtime = "sglang"
	RuntimeLlamaCpp Runtime = "llamacpp"
	RuntimeOllama   Runtime = "ollama"
)

// AllRuntimes in the order they are offered.
var AllRuntimes = []Runtime{RuntimeVLLM, RuntimeTRTLLM, RuntimeSGLang, RuntimeLlamaCpp, RuntimeOllama}

// ParseRuntime resolves a --runtime value.
func ParseRuntime(s string) (Runtime, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return RuntimeAuto, true
	case "vllm":
		return RuntimeVLLM, true
	case "trtllm", "trt-llm", "tensorrt-llm", "tensorrt_llm", "trt":
		return RuntimeTRTLLM, true
	case "sglang":
		return RuntimeSGLang, true
	case "llamacpp", "llama.cpp", "llama-cpp", "llama_cpp":
		return RuntimeLlamaCpp, true
	case "ollama":
		return RuntimeOllama, true
	}
	return "", false
}

// ReserveGiB is spec 7.4 R: llama.cpp/Ollama 3 GiB; vLLM 12 GiB (~3 GB
// runtime + ~13 GB torch.compile/CUDA graphs, S85); SGLang 10 and TRT-LLM 10
// (no primary source; sized like vLLM minus the compile cache).
func (r Runtime) ReserveGiB() float64 {
	switch r {
	case RuntimeVLLM:
		return 12
	case RuntimeSGLang, RuntimeTRTLLM:
		return 10
	case RuntimeLlamaCpp, RuntimeOllama:
		return 3
	}
	return 0
}

// IsContainer reports whether the spec 7.6 template runs in docker.
func (r Runtime) IsContainer() bool {
	return r == RuntimeVLLM || r == RuntimeTRTLLM || r == RuntimeSGLang
}

// Port is the default listening port of each template (spec 7.6/7.7).
func (r Runtime) Port() int {
	switch r {
	case RuntimeVLLM:
		return 8000
	case RuntimeTRTLLM:
		return 8355
	case RuntimeSGLang, RuntimeLlamaCpp:
		return 30000
	case RuntimeOllama:
		return 11434
	}
	return 0
}

// Display is the human name.
func (r Runtime) Display() string {
	switch r {
	case RuntimeVLLM:
		return "vLLM"
	case RuntimeTRTLLM:
		return "TensorRT-LLM"
	case RuntimeSGLang:
		return "SGLang"
	case RuntimeLlamaCpp:
		return "llama.cpp"
	case RuntimeOllama:
		return "Ollama"
	}
	return string(r)
}

// ollamaFAArchitectures is the spec 7.6 list of architectures for which
// Ollama honours OLLAMA_KV_CACHE_TYPE=q8_0 (flash attention capable);
// otherwise it silently falls back to f16 (S97).
var ollamaFAArchitectures = []string{"gemma3", "gptoss", "mistral3", "qwen3", "qwen3moe", "qwen3vl"}

// OllamaSupportsQ8KV reports whether the model architecture is in the list.
func OllamaSupportsQ8KV(arch string) bool {
	a := strings.ToLower(strings.TrimSpace(arch))
	for _, x := range ollamaFAArchitectures {
		if a == x {
			return true
		}
	}
	return false
}

// DefaultKV is the KV dtype chosen for --kv-dtype auto. vLLM/TRT-LLM/SGLang
// stay at f16: spec 7.6 says --kv-cache-dtype fp8 is emitted only on an
// explicit --kv-dtype fp8 (FlashInfer hit an SM100 cuDNN path on sm_121,
// S92). llama.cpp: "KV q8_0 or higher" (spec 7.6). Ollama: q8_0 only for
// the FA-capable architectures, else f16.
func (r Runtime) DefaultKV(m ModelShape) KVDtype {
	switch r {
	case RuntimeLlamaCpp:
		return KVQ8_0
	case RuntimeOllama:
		if OllamaSupportsQ8KV(m.OllamaArch) {
			return KVQ8_0
		}
		return KVF16
	}
	return KVF16
}

// SupportsKV reports whether the runtime can use the KV dtype at all.
func (r Runtime) SupportsKV(k KVDtype) bool {
	switch r {
	case RuntimeLlamaCpp:
		return k == KVF16 || k == KVQ8_0 || k == KVQ4_0
	case RuntimeOllama:
		return k == KVF16 || k == KVQ8_0
	default: // vLLM, TRT-LLM, SGLang
		return k == KVF16 || k == KVFP8
	}
}

// SupportsQuant reports whether a weight format makes sense for the runtime.
func (r Runtime) SupportsQuant(q Quant) bool {
	if r == RuntimeLlamaCpp || r == RuntimeOllama {
		return true // GGUF exists for every format (F16/BF16/Q8_0/Q4_K_M/MXFP4)
	}
	return !q.IsGGUF()
}

// Container images named in spec 7.6.
const (
	ImageVLLMNGC     = "nvcr.io/nvidia/vllm:26.05-py3"
	ImageVLLMNightly = "vllm/vllm-openai:cu130-nightly"
	ImageTRTLLM      = "nvcr.io/nvidia/tensorrt-llm/release:1.3.0rc13"
	ImageSGLang      = "lmsysorg/sglang:latest-cu130"
)

// ClusterFacts is what the two-node variant needs from the report (spec 9):
// the RDMA devices the collector saw ACTIVE, never a hard-coded port.
type ClusterFacts struct {
	ActiveRDMADevs []string
}

// Command is the rendered runtime block of a plan (spec 7.8 runtime{name,
// image,command,env}).
type Command struct {
	Runtime     Runtime  `json:"name"`
	Image       string   `json:"image"`                 // "" for llama.cpp / Ollama; the key is always present (spec 7.8)
	Build       string   `json:"build,omitempty"`       // llama.cpp cmake line
	Command     string   `json:"command"`               // the exact command to run (one line)
	Extra       []string `json:"extra,omitempty"`       // additional files/lines (cfg.yaml content, systemctl edit body)
	Env         []string `json:"env"`                   // KEY=value lines (always present in plan.json, spec 7.8)
	Notes       []string `json:"notes,omitempty"`       // facts from the spec that accompany the template
	Unconfirmed []string `json:"unconfirmed,omitempty"` // anything the spec marks unconfirmed or does not cover
}

// modelArg is the {model} placeholder: the base HF repo when known, else a
// placeholder the user fills in. When the requested quant differs from the
// checkpoint's native format the user must pick the quantized repo.
func modelArg(m ModelShape) string {
	if m.HFRepo != "" {
		return m.HFRepo
	}
	return "{model}"
}

// ggufQuantTag is the -hf {repo}:{quant} suffix (HF GGUF naming: Q4_K_M, Q8_0,
// F16, BF16, MXFP4). NVFP4 and FP8 have no GGUF type; the tag stays a
// placeholder and RenderCommand says so.
func ggufQuantTag(q Quant) string {
	switch q {
	case QuantFP16:
		return "F16"
	case QuantBF16:
		return "BF16"
	case QuantNVFP4, QuantFP8:
		return "{quant}"
	}
	return strings.ToUpper(string(q))
}

// RenderCommand fills the spec 7.6 template of in.Runtime. profile is the
// spec 7.1 profile (chat|agent|batch|rag); agent adds the tool-call flags.
func RenderCommand(in Inputs, s Sizing, profile string, cluster ClusterFacts) Command {
	c := Command{Runtime: in.Runtime}
	u := fmt.Sprintf("%.2f", s.Utilization)
	ctx := fmt.Sprintf("%d", in.Context)
	n := fmt.Sprintf("%d", in.Concurrency)
	model := modelArg(in.Model)
	if in.Runtime.IsContainer() && in.Model.HFRepo != "" && in.Quant.Rank() != Quant(in.Model.DefaultQuant).Rank() {
		c.Notes = append(c.Notes, fmt.Sprintf("%s is the base checkpoint (%s); point the model argument at the %s export of this model instead (llm-plan does not choose or download repositories).", model, in.Model.DefaultQuant, strings.ToUpper(string(in.Quant))))
	}

	switch in.Runtime {
	case RuntimeVLLM:
		// spec 7.6 vLLM (S86 S87), verbatim template.
		c.Image = ImageVLLMNGC
		cmd := "docker run -d --name vllm --ipc=host --gpus all -p 8000:8000 -v ~/.cache/huggingface:/root/.cache/huggingface " +
			c.Image + " " + model +
			" --gpu-memory-utilization " + u + " --max-model-len " + ctx + " --max-num-seqs " + n
		if profile == "agent" {
			cmd += " --enable-auto-tool-choice --tool-call-parser {p}"
			c.Notes = append(c.Notes, "Replace {p} with your model's vLLM tool-call parser (and add --reasoning-parser {r} for reasoning models).")
		}
		if in.KV == KVFP8 {
			// Only on explicit --kv-dtype fp8 (spec 7.6, S92).
			cmd += " --kv-cache-dtype fp8"
			c.Unconfirmed = append(c.Unconfirmed, "--kv-cache-dtype fp8 was added because you asked for it: FlashInfer hit an SM100 cuDNN path on sm_121 (S92); verify on your vLLM build.")
		}
		c.Command = cmd
		c.Notes = append(c.Notes,
			"Alternative image: "+ImageVLLMNightly+" (spec 7.6).",
			"Leave --quantization unset for pre-quantized NVFP4 checkpoints (spec 7.6).",
			"vLLM's Spark guidance is u <= 0.85 and --max-num-seqs 4; the default 0.92 pre-allocates ~110 GiB (spec 7.4).",
			"First request JIT ~25 s (spec 7.6). Re-check MemAvailable after startup: one report saw transient kernel-init allocations up to ~50 GB on vllm:26.07-py3 (S111).",
		)
	case RuntimeTRTLLM:
		// spec 7.6 TensorRT-LLM (S91), verbatim template.
		c.Image = ImageTRTLLM
		c.Command = "docker run --rm -it --gpus all --ipc host --network host --ulimit memlock=-1 --ulimit stack=67108864 -v ~/.cache/huggingface:/root/.cache/huggingface " +
			c.Image + " trtllm-serve " + model + " --backend pytorch --port 8355 --max_batch_size " + n + " --extra_llm_api_options cfg.yaml"
		c.Extra = append(c.Extra, "cfg.yaml:", "kv_cache_config:", "  free_gpu_memory_fraction: "+u)
		c.Env = append(c.Env, "TRT_LLM_DISABLE_LOAD_WEIGHTS_IN_PARALLEL=1", "TRITON_PTXAS_PATH=/usr/local/cuda/bin/ptxas")
		c.Notes = append(c.Notes, "free_gpu_memory_fraction reuses u (spec 7.4).")
	case RuntimeSGLang:
		// spec 7.6 SGLang (S94), verbatim template.
		c.Image = ImageSGLang
		cmd := "docker run --gpus all --ipc=host --shm-size 32g -p 30000:30000 " + c.Image +
			" python3 -m sglang.launch_server --model-path " + model +
			" --host 0.0.0.0 --port 30000 --trust-remote-code --tp 1 --attention-backend flashinfer --mem-fraction-static " + u
		if in.Quant == QuantNVFP4 {
			cmd += " --quantization modelopt_fp4"
		}
		if profile == "agent" {
			cmd += " --reasoning-parser {r} --tool-call-parser {p}"
			c.Notes = append(c.Notes, "Replace {r}/{p} with your model's SGLang reasoning and tool-call parsers.")
		}
		c.Command = cmd
		c.Notes = append(c.Notes, "--mem-fraction-static reuses u (spec 7.4; the SGLang playbook default is 0.75).")
	case RuntimeLlamaCpp:
		// spec 7.6 llama.cpp (S95 S96), verbatim build and run lines.
		c.Build = "cmake -B build -DGGML_NATIVE=ON -DGGML_CUDA=ON -DGGML_CURL=ON -DCMAKE_CUDA_ARCHITECTURES=121a-real"
		repo := model
		if repo == "{model}" {
			repo = "{repo}"
		}
		kv := string(in.KV)
		c.Command = "llama-server -hf " + repo + ":" + ggufQuantTag(in.Quant) +
			" --host 0.0.0.0 --port 30000 -ngl 999 -fa on --no-mmap -c " + ctx + " -np " + n +
			" --cache-type-k " + kv + " --cache-type-v " + kv + " -b 2048 -ub 2048 --jinja"
		if ggufQuantTag(in.Quant) == "{quant}" {
			c.Unconfirmed = append(c.Unconfirmed, fmt.Sprintf("%s has no GGUF equivalent; pick a Q4_K_M or Q8_0 GGUF of this model for llama.cpp (the sizing above used the %s factor).", strings.ToUpper(string(in.Quant)), strings.ToUpper(string(in.Quant))))
		}
		c.Notes = append(c.Notes,
			"--no-mmap avoids the Spark mmap slow-load; keep the KV cache at q8_0 or higher (spec 7.6).",
			"Optional speculative decoding for models that ship MTP heads: --spec-type draft-mtp --spec-draft-n-max 3 (spec 7.6).",
			"-hf {repo}:{quant} names a GGUF repo on Hugging Face; llama-server fetches it on first start, llm-plan does not.",
		)
	case RuntimeOllama:
		// spec 7.6 Ollama (S97): the drop-in body the user pastes into systemctl edit.
		kv := string(in.KV)
		c.Command = "systemctl edit ollama.service"
		c.Extra = append(c.Extra,
			"[Service]",
			fmt.Sprintf(`Environment="OLLAMA_FLASH_ATTENTION=1" "OLLAMA_KV_CACHE_TYPE=%s" "OLLAMA_NUM_PARALLEL=%s" "OLLAMA_MAX_LOADED_MODELS=1" "OLLAMA_CONTEXT_LENGTH=%s"`, kv, n, ctx),
		)
		c.Env = append(c.Env, "OLLAMA_FLASH_ATTENTION=1", "OLLAMA_KV_CACHE_TYPE="+kv, "OLLAMA_NUM_PARALLEL="+n, "OLLAMA_MAX_LOADED_MODELS=1", "OLLAMA_CONTEXT_LENGTH="+ctx)
		c.Notes = append(c.Notes,
			"q8_0 KV only for FA-capable architectures (gemma3, gptoss, mistral3, qwen3/qwen3moe, qwen3vl); otherwise Ollama silently falls back to f16 (spec 7.6).",
			"Verify 'ollama ps' shows 100% GPU; the default context 4096 is too small for agents (spec 7.6).",
			"Ollama does not batch: aggregate throughput equals a single stream (spec 7.4).",
		)
		if in.KV == KVQ8_0 && !OllamaSupportsQ8KV(in.Model.OllamaArch) {
			c.Unconfirmed = append(c.Unconfirmed, fmt.Sprintf("architecture %q is not in the FA-capable list; Ollama will fall back to f16 KV, which doubles the KV figure above.", in.Model.OllamaArch))
		}
	}

	if in.Nodes >= 2 {
		c.Env = append(c.Env, clusterEnv(cluster)...)
		c.Unconfirmed = append(c.Unconfirmed,
			"Two-node target: the spec has no verified multi-node launch template; the command above is the single-node form. NVIDIA lists Qwen3-235B-A22B as multi-node only via the TRT-LLM playbook (S91) and documents the fabric in the connect-two-sparks playbook (S18). Add your runtime's tensor/pipeline-parallel flags after verifying them against those sources.",
			"Healthy fabric (spec 9): both twins of the cabled cage ACTIVE/LinkUp at 200000 Mb/s, distinct /24s, MTU 9000, NCCL_NET_PLUGIN=none, NCCL log shows NET/IB, ~22-24 GB/s busbw.",
		)
	}
	return c
}

// clusterEnv is the NCCL environment of spec 9: NCCL_IB_HCA names both twins
// the collector saw ACTIVE (never a hard-coded port) and NCCL_NET_PLUGIN=none.
func clusterEnv(cf ClusterFacts) []string {
	hca := "{rdma-devs-of-the-cabled-cage, e.g. rocep1s0f0,roceP2p1s0f0}"
	if len(cf.ActiveRDMADevs) > 0 {
		hca = strings.Join(cf.ActiveRDMADevs, ",")
	}
	return []string{"NCCL_IB_HCA=" + hca, "NCCL_NET_PLUGIN=none"}
}

// ChooseRuntime implements --runtime auto: GGUF quants go to llama.cpp;
// Windows on Arm only has llama.cpp (spec 7.6: no win_arm64 torch wheels as
// of 2026-09-02, S93); otherwise vLLM when its 12 GiB reserve fits, then
// SGLang (10 GiB), then llama.cpp (3 GiB).
func ChooseRuntime(in Inputs, goos string) Runtime {
	if in.Quant.IsGGUF() || goos == "windows" {
		return RuntimeLlamaCpp
	}
	for _, r := range []Runtime{RuntimeVLLM, RuntimeSGLang, RuntimeLlamaCpp} {
		try := in
		try.Runtime = r
		try.KV = r.DefaultKV(in.Model)
		if Compute(try).FitsTotal {
			return r
		}
	}
	return RuntimeLlamaCpp
}
