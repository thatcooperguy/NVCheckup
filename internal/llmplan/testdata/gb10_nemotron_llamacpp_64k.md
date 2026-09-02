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

**FITS: Nemotron-3-Super-120B-A12B NVFP4, 64K context x 2 streams, q8_0 KV, llama.cpp needs 83.1 GiB on this machine (pool 119.7 GiB, margin 36.6 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (NVFP4) | 62.6 | 120B x 0.56 B/param |
| KV cache (q8_0) | 0.5 | 4096 B/token x 65536 tokens x 2 streams |
| Mamba state | 9.0 | derived from the measured per-slot figure (S81) |
| Runtime R | 3.0 | llama.cpp |
| OS floor F | 8.0 | |
| **Total** | **83.1** | pool 119.7 GiB, margin 36.6 GiB, fits: yes |
| Now (W+KV+R) | 75.1 | MemAvailable 109.7 GiB, fits now: yes |

## Estimates

- Decode ceiling, one stream: 40.6 tok/s weights-only; 39.1 tok/s at 64K context; realism band 19.5-31.3 tok/s (50-80%).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).
- Measured by others: 23-38 tok/s measured (spec 7.4).

## Advice (spec 7.6)

- Keep NVFP4: NVFP4 is the least lossy format that fits with the 8.0 GiB headroom (margin 36.6 GiB).
- Headroom: more than 1M tokens per stream would still fit at NVFP4 with 2 streams; the KV cache is not the limit here.

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| bf16 | 223.5 | 244.1 | no | -124.4 |
| q8_0 | 118.5 | 139.0 | no | -19.3 |
| nvfp4 | 62.6 | 83.1 | yes | 36.6 |
| q4_k_m | 67.1 | 87.6 | yes | 32.1 |

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
| PASS | memavailable-fits | MemAvailable 109.7 GiB >= W + KV + R = 75.1 GiB |

## Commands (llama.cpp, spec 7.6)

Build:

```sh
cmake -B build -DGGML_NATIVE=ON -DGGML_CUDA=ON -DGGML_CURL=ON -DCMAKE_CUDA_ARCHITECTURES=121a-real
```

```sh
llama-server -hf {repo}:{quant} --host 0.0.0.0 --port 30000 -ngl 999 -fa on --no-mmap -c 65536 -np 2 --cache-type-k q8_0 --cache-type-v q8_0 -b 2048 -ub 2048 --jinja
```
- --no-mmap avoids the Spark mmap slow-load; keep the KV cache at q8_0 or higher (spec 7.6).
- Optional speculative decoding for models that ship MTP heads: --spec-type draft-mtp --spec-draft-n-max 3 (spec 7.6).
- -hf {repo}:{quant} names a GGUF repo on Hugging Face; llama-server fetches it on first start, llm-plan does not.

Unconfirmed / not covered by the spec:

- NVFP4 has no GGUF equivalent; pick a Q4_K_M or Q8_0 GGUF of this model for llama.cpp (the sizing above used the NVFP4 factor).

Exit code 1 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
