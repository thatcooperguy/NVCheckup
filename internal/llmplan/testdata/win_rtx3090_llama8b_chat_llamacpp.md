# NVCheckup llm-plan

_Read-only; estimates, not measurements._

| | |
|---|---|
| Platform | NVIDIA GeForce RTX 3090 |
| GPU | NVIDIA GeForce RTX 3090 |
| Pool | 24.0 GiB (nvidia-smi memory.total of NVIDIA GeForce RTX 3090 (dedicated VRAM, discrete GPU)) |
| VRAM free | 22.5 GiB |
| Bandwidth | unknown for this GPU (no figure in the spec) |
| OS floor F | 0 GiB: dedicated VRAM of a discrete GPU (assumption; spec 7.4 F is a unified-memory reservation; set --headroom-gib to reserve VRAM) |

## Verdict

**FITS: Llama 3.1 8B Instruct BF16, 8K context x 1 stream, q8_0 KV, llama.cpp needs 18.5 GiB on this machine (pool 24.0 GiB, margin 5.5 GiB).**

## Sizing (spec 7.4)

| Term | GiB | Detail |
|---|---:|---|
| Weights W (BF16) | 15.0 | 8.03B x 2.00 B/param |
| KV cache (q8_0) | 0.5 | 65536 B/token x 8192 tokens x 1 streams |
| Runtime R | 3.0 | llama.cpp |
| OS floor F | 0.0 | |
| **Total** | **18.5** | pool 24.0 GiB, margin 5.5 GiB, fits: yes |
| Now (W+KV+R) | 18.5 | VRAM free 22.5 GiB, fits now: yes |

## Estimates

- Decode ceiling: not printed (memory bandwidth of this platform is not known to the wizard; no decode ceiling is printed).
- Prefill reference: 2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here).
- Measured by others: 8B FP8: 20.5 tok/s measured (S89), vs 34 weights-only / 22 at 32K.

## Advice (spec 7.6)

- Keep BF16: BF16 is the least lossy format that fits with the 0.0 GiB headroom (margin 5.5 GiB).
- Headroom: up to about 99007 tokens per stream would still fit at BF16 with 1 stream.

| Quant | Weights GiB | Total GiB | Fits | Margin GiB |
|---|---:|---:|---|---:|
| bf16 | 15.0 | 18.5 | yes | 5.5 |
| q8_0 | 7.9 | 11.4 | yes | 12.6 |
| q4_k_m | 4.5 | 8.0 | yes | 16.0 |

## Prerequisites (spec 7.7)

| Status | Check | Detail |
|---|---|---|
| PASS | driver-present | driver 591.86 |
| PASS | cuda-13 | CUDA 13.0 |
| SKIP | torch-cu130-sm120 | torch not found in the probed python (container runtimes bring their own) |
| SKIP | triton-ptxas-path | Triton not installed in the probed python |
| SKIP | swap-in-use | swap usage not available |
| SKIP | page-cache | page cache vs MemAvailable is a unified-memory check; the pool here is dedicated VRAM |
| SKIP | model-server-ports | listening ports not available |
| PASS | memavailable-fits | VRAM free 22.5 GiB >= W + KV + R = 18.5 GiB |

## Commands (llama.cpp, spec 7.6)

Build:

```sh
cmake -B build -DGGML_NATIVE=ON -DGGML_CUDA=ON -DGGML_CURL=ON -DCMAKE_CUDA_ARCHITECTURES=121a-real
```

```sh
llama-server -hf meta-llama/Llama-3.1-8B-Instruct:BF16 --host 0.0.0.0 --port 30000 -ngl 999 -fa on --no-mmap -c 8192 -np 1 --cache-type-k q8_0 --cache-type-v q8_0 -b 2048 -ub 2048 --jinja
```
- --no-mmap avoids the Spark mmap slow-load; keep the KV cache at q8_0 or higher (spec 7.6).
- Optional speculative decoding for models that ship MTP heads: --spec-type draft-mtp --spec-draft-n-max 3 (spec 7.6).
- -hf {repo}:{quant} names a GGUF repo on Hugging Face; llama-server fetches it on first start, llm-plan does not.

## Warnings

- Discrete GPU: the spec's formulas target unified-memory Spark systems; the pool here is dedicated VRAM, so the OS floor F defaults to 0 (an assumption, not a spec figure; --headroom-gib N reserves N GiB of VRAM for the desktop or other GPU work).

## Notes

- Discrete GPU: the pool is dedicated VRAM, not the shared pool spec 7.4 sizes; the OS floor F defaults to 0 here (F is a host-OS reservation out of unified memory) unless --headroom-gib is given, and the swap/page-cache checks are skipped.

Exit code 1 (0 fits, 1 fits with warnings, 2 does not fit, 3 error).

_llm-plan is read-only: nothing was downloaded, no process or container was started or stopped, and nothing was written outside the output directory._

_Ceilings and bands are formula bounds from the spec, not measurements of this machine; figures quoted as measured were measured by others._
