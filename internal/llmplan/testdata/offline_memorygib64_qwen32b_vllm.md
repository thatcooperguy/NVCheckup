# NVCheckup llm-plan

_Read-only; estimates, not measurements._

| | |
|---|---|
| Platform | unknown |
| Pool | 64.0 GiB (--memory-gib 64.0 (user override)) |
| MemAvailable | unknown |
| Bandwidth | unknown for this GPU (no figure in the spec) |
| OS floor F | 8 GiB: headless Linux (spec 7.4) |

## Verdict

**DOES NOT FIT: Qwen3-32B / DeepSeek-R1-Distill-Qwen-32B BF16, 8K context x 1 stream, f16 KV, vLLM needs 83.1 GiB on this machine, pool is 64.0 GiB (short by 19.1 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (BF16) | 61.1 | 32.8B x 2.00 B/param |
| KV cache (f16) | 2.0 | 262144 B/token x 8192 tokens x 1 streams |
| Runtime R | 12.0 | vLLM |
| OS floor F | 8.0 | |
| **Total** | **83.1** | pool 64.0 GiB, margin -19.1 GiB, fits: no |
| Now (W+KV+R) | 75.1 | MemAvailable unknown |
| u | 0.85 | gpu-memory-utilization / free_gpu_memory_fraction / mem-fraction-static |

## Estimates

- Decode ceiling: not printed (memory bandwidth of this platform is not known to the wizard; no decode ceiling is printed).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).

## Advice (spec 7.6)

- Consider FP8: BF16 does not fit; FP8 is the smallest quantization step that fits (total 52.5 GiB, margin 11.5 GiB).
- u = ceil05((W + KV + R) / MemTotal) = 0.85, clamped to 0.30..0.85 (spec 7.4).
- Catalogue models that fit at the same context and concurrency: llama-3.1-8b-instruct (bf16, 36.0 GiB); gpt-oss-20b (mxfp4, 32.5 GiB).

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| bf16 | 61.1 | 83.1 | no | -19.1 |
| fp8 | 30.5 | 52.5 | yes | 11.5 |
| nvfp4 | 17.1 | 39.1 | yes | 24.9 |

## Prerequisites (spec 7.7)

| Status | Check | Detail |
|---|---|---|
| FAIL | driver-present | no NVIDIA driver version detected (nvidia-smi absent or failed) |
| SKIP | cuda-13 | CUDA version not reported |
| SKIP | torch-cu130-sm120 | torch not found in the probed python (container runtimes bring their own) |
| SKIP | triton-ptxas-path | Triton not installed in the probed python |
| SKIP | swap-in-use | swap usage not available |
| SKIP | page-cache | MemAvailable not available |
| SKIP | model-server-ports | listening ports not available |
| SKIP | container-image | local image inventory not collected; the template uses nvcr.io/nvidia/vllm:26.05-py3 |
| SKIP | docker-gpu-runtime | container runtime not probed |
| PASS | ipc-shm | template uses --ipc=host (spec 7.6) |
| SKIP | memavailable-fits | MemAvailable unknown; only the design fit against the pool total was evaluated |

## Commands (vLLM, spec 7.6)

Image: `nvcr.io/nvidia/vllm:26.05-py3`

```sh
docker run -d --name vllm --ipc=host --gpus all -p 8000:8000 -v ~/.cache/huggingface:/root/.cache/huggingface nvcr.io/nvidia/vllm:26.05-py3 Qwen/Qwen3-32B --gpu-memory-utilization 0.85 --max-model-len 8192 --max-num-seqs 1
```
- Alternative image: vllm/vllm-openai:cu130-nightly (spec 7.6).
- Leave --quantization unset for pre-quantized NVFP4 checkpoints (spec 7.6).
- vLLM's Spark guidance is u <= 0.85 and --max-num-seqs 4; the default 0.92 pre-allocates ~110 GiB (spec 7.4).
- First request JIT ~25 s (spec 7.6). Re-check MemAvailable after startup: one report saw transient kernel-init allocations up to ~50 GB on vllm:26.07-py3 (S111).

## Warnings

- FAIL driver-present: no NVIDIA driver version detected (nvidia-smi absent or failed)

## Notes

- MemAvailable unknown: only the design fit (W + KV + R + F <= pool) is evaluated.

Exit code 2 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
