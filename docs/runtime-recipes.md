# Runtime recipes and demo verification

`llm-plan` evaluates a memory estimate and reports prerequisites. It does not start a server or perform inference, and a local image inventory only establishes which image and architecture were reported. Spark support remains exercised with simulated fixtures until a real capture is recorded. A container version tag does not prove that every model runs on every GPU.

## Corrections to the original Spark templates

Ollama's current FAQ documents parallel request processing. The plan no longer repeats the original specification's claim that its aggregate throughput equals one stream; throughput comparisons require a benchmark on the selected model and hardware.

- **Model format:** the catalogue stores base checkpoints, not an inventory of GGUF and quantized exports. A plan for a different quantization prints `{quantized-model-repo}`; llama.cpp prints `{gguf-repo}`. Replace the placeholder with an export matching the model shape and weight format before launching it. Equal bit widths do not make MXFP4 and NVFP4 interchangeable.
- **Context and concurrency:** `--context` is per stream. llama.cpp's `-c` is the total context across `-np` slots, so 8,192 tokens with three streams becomes `-c 24576 -np 3`. TensorRT-LLM receives `--max_seq_len` and `--max_batch_size`; SGLang receives `--context-length` and `--max-running-requests`. Confirm the model's supported context and actual per-slot limits after startup.
- **TensorRT-LLM:** save the printed YAML body as `cfg.yaml` in your current directory first. The generated Docker command mounts it read-only at `/etc/nvcheckup/cfg.yaml`, uses that absolute path and passes the specified environment through `-e` options. Docker does not automatically inherit host files or environment.
- **Target platform:** llama.cpp's CMake configuration detects the build machine's CUDA architecture; it no longer forces `sm_121` for ordinary RTX desktops. Windows Ollama instructions use PowerShell environment variables. Windows container recipes target a separately configured WSL2/Linux environment, which the Windows host report cannot validate. Windows on Arm remains explicitly unverified.
- **Offline reports:** `--report` uses the saved host's Triton information and listening ports, never the environment or network state of the workstation rendering the report. Missing recorded ports remain unknown.
- **Image identity:** preflight checks the exact image/tag and reported architecture. A different tag cannot make an incompatible image pass. The existing SGLang `latest-cu130` recipe is labelled as floating: pin a verified digest for a repeatable demonstration rather than inventing one.

## Repeatable demo check

1. Capture the real demo machine with `nvcheckup run --mode ai --json --out preflight`, then generate its plan with `nvcheckup llm-plan --report preflight/report.json --model <catalogue-id> --runtime <runtime> --json --out plan`.
2. Resolve every model, parser and image placeholder. Record the runtime version or image digest, model revision, quantization, GPU, context and concurrency with the results. The planner does not download or validate those assets.
3. Follow the chosen runtime's setup instructions. Send a real short request, then exercise the intended context and number of simultaneous requests. Check per-slot context, latency and memory from that run; an HTTP health check alone does not validate generation.
4. Retain the preflight report and measured results separately from simulated fixtures. No hardware result is implied by a passing Go test or cross-build.

## Primary references

- [llama.cpp server arguments and slots](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)
- [llama.cpp CUDA build instructions](https://github.com/ggml-org/llama.cpp/blob/master/docs/build.md)
- [TensorRT-LLM serving arguments](https://nvidia.github.io/TensorRT-LLM/commands/trtllm-serve.html)
- [SGLang context and scheduling arguments](https://docs.sglang.io/docs/advanced_features/server_arguments)
- [Docker runtime environment and bind mounts](https://docs.docker.com/engine/containers/run/)
- [Ollama server environment on Windows and Linux](https://docs.ollama.com/faq#how-do-i-configure-ollama-server)

These changes correct recipe wiring; they do not replace model-specific compatibility checks or the outstanding real Spark validation.
