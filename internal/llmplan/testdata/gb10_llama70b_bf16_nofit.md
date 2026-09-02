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

**DOES NOT FIT: Llama 3.3 70B Instruct / DeepSeek-R1-Distill-Llama-70B BF16, 128K context x 1 stream, f16 KV, vLLM needs 191.5 GiB on this machine, pool is 119.7 GiB (short by 71.8 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (BF16) | 131.5 | 70.6B x 2.00 B/param |
| KV cache (f16) | 40.0 | 327680 B/token x 131072 tokens x 1 streams |
| Runtime R | 12.0 | vLLM |
| OS floor F | 8.0 | |
| **Total** | **191.5** | pool 119.7 GiB, margin -71.8 GiB, fits: no |
| Now (W+KV+R) | 183.5 | MemAvailable 109.7 GiB, fits now: no |
| u | 0.85 | gpu-memory-utilization / free_gpu_memory_fraction / mem-fraction-static |

## Estimates

- Decode ceiling, one stream: 1.9 tok/s weights-only; 1.5 tok/s at 128K context; realism band 0.7-1.2 tok/s (50-80%).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).
- Measured by others: 70B FP8: 2.7 tok/s measured (S89), vs 3.9 weights-only.

## Advice (spec 7.6)

- Consider NVFP4: BF16 does not fit; NVFP4 is the smallest quantization step that fits (total 96.8 GiB, margin 22.9 GiB).
- u = ceil05((W + KV + R) / MemTotal) = 0.85, clamped to 0.30..0.85 (spec 7.4).
- Catalogue models that fit at the same context and concurrency: qwen3-32b (bf16, 113.1 GiB); nemotron-3-super-120b-a12b-nvfp4 (nvfp4, 88.1 GiB); gpt-oss-120b (mxfp4, 85.8 GiB); llama-3.1-8b-instruct (bf16, 51.0 GiB); gpt-oss-20b (mxfp4, 38.1 GiB).

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| bf16 | 131.5 | 191.5 | no | -71.8 |
| fp8 | 65.8 | 125.8 | no | -6.1 |
| nvfp4 | 36.8 | 96.8 | yes | 22.9 |

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
| SKIP | container-image | local image inventory not collected; the template uses nvcr.io/nvidia/vllm:26.05-py3 |
| PASS | docker-gpu-runtime | docker with NVIDIA Container Toolkit 1.17.8 |
| PASS | ipc-shm | template uses --ipc=host (spec 7.6) |
| FAIL | memavailable-fits | MemAvailable 109.7 GiB < W + KV + R = 183.5 GiB right now; free memory (other servers, caches) before starting |

## Commands (vLLM, spec 7.6)

Image: `nvcr.io/nvidia/vllm:26.05-py3`

```sh
docker run -d --name vllm --ipc=host --gpus all -p 8000:8000 -v ~/.cache/huggingface:/root/.cache/huggingface nvcr.io/nvidia/vllm:26.05-py3 meta-llama/Llama-3.3-70B-Instruct --gpu-memory-utilization 0.85 --max-model-len 131072 --max-num-seqs 1
```
- Alternative image: vllm/vllm-openai:cu130-nightly (spec 7.6).
- Leave --quantization unset for pre-quantized NVFP4 checkpoints (spec 7.6).
- vLLM's Spark guidance is u <= 0.85 and --max-num-seqs 4; the default 0.92 pre-allocates ~110 GiB (spec 7.4).
- First request JIT ~25 s (spec 7.6). Re-check MemAvailable after startup: one report saw transient kernel-init allocations up to ~50 GB on vllm:26.07-py3 (S111).

## Warnings

- FAIL memavailable-fits: MemAvailable 109.7 GiB < W + KV + R = 183.5 GiB right now; free memory (other servers, caches) before starting

Exit code 2 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
