# NVCheckup llm-plan

_Read-only; estimates, not measurements._

| | |
|---|---|
| Platform | DGX Spark (GB10) |
| GPU | NVIDIA GB10 |
| Pool | 119.7 GiB (report.unified_memory (/proc/meminfo MemTotal, measured)) |
| MemAvailable | 109.7 GiB (swap used 0.0 GiB) |
| Bandwidth | 273 GB/s (GB10 LPDDR5X, spec 2.1) |
| OS floor F | 8 GiB: headless Linux (spec 7.4) |

## Verdict

**FITS: gpt-oss-120b (MXFP4), 32K context x 4 streams, q8_0 KV, Ollama needs 41.7 GiB per node across 2 nodes (pool 119.7 GiB, margin 78.0 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (MXFP4) | 56.8 | measured checkpoint size |
| KV cache (q8_0) | 4.5 | 36864 B/token x 32768 tokens x 4 streams |
| Runtime R | 3.0 | Ollama |
| OS floor F | 8.0 | |
| **Total per node** | **41.7** | pool 119.7 GiB, margin 78.0 GiB, fits: yes |
| Now (W+KV+R) | 33.7 | MemAvailable 109.7 GiB, fits now: yes |

## Estimates

- Decode ceiling: not printed (no formula ceiling: the active-weight bytes of a mixed bf16/MXFP4 MoE checkpoint are not known to +/-10% (spec 7.5); 42-61 tok/s measured (S90)).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).
- Measured by others: 42-61 tok/s measured (S90).

## Advice (spec 7.6)

- Keep MXFP4: MXFP4 is the least lossy format that fits with the 8.0 GiB headroom (margin 78.0 GiB).
- Headroom: more than 1M tokens per stream would still fit at MXFP4 with 4 streams; the KV cache is not the limit here.
- Ollama does not batch: aggregate throughput equals one stream; vLLM aggregate reaches hundreds of tok/s at c=8..256 (spec 7.4).

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| mxfp4 | 56.8 | 41.7 | yes | 78.0 |

## Prerequisites (spec 7.7)

| Status | Check | Detail |
|---|---|---|
| PASS | driver-present | driver 580.95.05 |
| PASS | cuda-13 | CUDA 13.0 |
| PASS | ota-not-torn | OTA torn score 0 |
| PASS | torch-cu130-sm120 | torch 2.9.0+cu130, arch list includes sm_120/sm_121 |
| SKIP | triton-ptxas-path | Triton not installed in the probed python |
| PASS | swap-in-use | swap in use 0.0 GiB |
| PASS | page-cache | page cache 19.1 GiB is reclaimable and already counted in MemAvailable 109.7 GiB (MemFree 85.8 GiB) |
| PASS | model-server-ports | ports 8000/30000/11434/8355 are free |
| PASS | memavailable-fits | MemAvailable 109.7 GiB >= W + KV + R = 33.7 GiB |
| PASS | ollama-kv-q8-arch | architecture gptoss honours OLLAMA_KV_CACHE_TYPE=q8_0 |
| PASS | cx7-link | roceP2p1s0f0,rocep1s0f0 ACTIVE/LinkUp at 200000 Mb/s, MTU 9000 |

## Commands (Ollama, spec 7.6)

```sh
systemctl edit ollama.service
[Service]
Environment="OLLAMA_FLASH_ATTENTION=1" "OLLAMA_KV_CACHE_TYPE=q8_0" "OLLAMA_NUM_PARALLEL=4" "OLLAMA_MAX_LOADED_MODELS=1" "OLLAMA_CONTEXT_LENGTH=32768"
```

Environment:

```sh
OLLAMA_FLASH_ATTENTION=1
OLLAMA_KV_CACHE_TYPE=q8_0
OLLAMA_NUM_PARALLEL=4
OLLAMA_MAX_LOADED_MODELS=1
OLLAMA_CONTEXT_LENGTH=32768
```
- q8_0 KV only for FA-capable architectures (gemma3, gptoss, mistral3, qwen3/qwen3moe, qwen3vl); otherwise Ollama silently falls back to f16 (spec 7.6).
- Verify 'ollama ps' shows 100% GPU; the default context 4096 is too small for agents (spec 7.6).
- Ollama does not batch: aggregate throughput equals a single stream (spec 7.4).

Unconfirmed / not covered by the spec:

- Two-node target: the spec has no verified multi-node launch template; the command above is the single-node form. NVIDIA lists Qwen3-235B-A22B as multi-node only via the TRT-LLM playbook (S91) and documents the fabric in the connect-two-sparks playbook (S18). Add your runtime's tensor/pipeline-parallel flags after verifying them against those sources.
- Healthy fabric (spec 9): both twins of the cabled cage ACTIVE/LinkUp at 200000 Mb/s, distinct /24s, MTU 9000, NCCL_NET_PLUGIN=none, NCCL log shows NET/IB, ~22-24 GB/s busbw.
- Ollama has no two-node mode in the spec (spec 7.6 lists none for Ollama/llama.cpp); the NCCL settings of spec 9 do not apply to it. Use vLLM, TensorRT-LLM or SGLang for a cluster of two.

Exit code 1 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
