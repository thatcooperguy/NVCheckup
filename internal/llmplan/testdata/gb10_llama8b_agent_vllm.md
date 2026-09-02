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

**FITS: Llama 3.1 8B Instruct BF16, 32K context x 4 streams, f16 KV, vLLM needs 51.0 GiB on this machine (pool 119.7 GiB, margin 68.7 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (BF16) | 15.0 | 8.03B x 2.00 B/param |
| KV cache (f16) | 16.0 | 131072 B/token x 32768 tokens x 4 streams |
| Runtime R | 12.0 | vLLM |
| OS floor F | 8.0 | |
| **Total** | **51.0** | pool 119.7 GiB, margin 68.7 GiB, fits: yes |
| Now (W+KV+R) | 43.0 | MemAvailable 109.7 GiB, fits now: yes |
| u | 0.40 | gpu-memory-utilization / free_gpu_memory_fraction / mem-fraction-static |

## Estimates

- Decode ceiling, one stream: 17.0 tok/s weights-only; 13.4 tok/s at 32K context; realism band 6.7-10.7 tok/s (50-80%).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).
- Measured by others: 8B FP8: 20.5 tok/s measured (S89), vs 34 weights-only / 22 at 32K.

## Advice (spec 7.6)

- Keep BF16: BF16 is the least lossy format that fits with the 8.0 GiB headroom (margin 68.7 GiB).
- Headroom: up to about 173552 tokens per stream would still fit at BF16 with 4 streams.
- u = ceil05((W + KV + R) / MemTotal) = 0.40, clamped to 0.30..0.85 (spec 7.4).

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| bf16 | 15.0 | 51.0 | yes | 68.7 |
| fp8 | 7.5 | 43.5 | yes | 76.2 |
| nvfp4 | 4.2 | 40.2 | yes | 79.5 |

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
| PASS | memavailable-fits | MemAvailable 109.7 GiB >= W + KV + R = 43.0 GiB |

## Commands (vLLM, spec 7.6)

Image: `nvcr.io/nvidia/vllm:26.05-py3`

```sh
docker run -d --name vllm --ipc=host --gpus all -p 8000:8000 -v ~/.cache/huggingface:/root/.cache/huggingface nvcr.io/nvidia/vllm:26.05-py3 meta-llama/Llama-3.1-8B-Instruct --gpu-memory-utilization 0.40 --max-model-len 32768 --max-num-seqs 4 --enable-auto-tool-choice --tool-call-parser {p}
```
- Replace {p} with your model's vLLM tool-call parser (and add --reasoning-parser {r} for reasoning models).
- Alternative image: vllm/vllm-openai:cu130-nightly (spec 7.6).
- Leave --quantization unset for pre-quantized NVFP4 checkpoints (spec 7.6).
- vLLM's Spark guidance is u <= 0.85 and --max-num-seqs 4; the default 0.92 pre-allocates ~110 GiB (spec 7.4).
- First request JIT ~25 s (spec 7.6). Re-check MemAvailable after startup: one report saw transient kernel-init allocations up to ~50 GB on vllm:26.07-py3 (S111).

Exit code 0 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
