# Roadmap: DGX Spark, RTX Spark, NVIDIA Arm and unified memory

Status: research in progress (started 2026-09-02). This document is the durable record of what is known and what is planned, so work can resume from any machine or session. Tracking issue: see the issue titled "DGX Spark / RTX Spark support" in this repository.

## Goal

Make NVCheckup a solid diagnostic (and, for LLM workloads, an optimization guide) on NVIDIA's new Arm-based unified-memory platforms:

- **DGX Spark** (GB10 Grace Blackwell, Arm64, DGX OS) and OEM variants (for example ASUS Ascent GX10).
- **RTX Spark** (the N1X superchip in Windows on Arm laptops and compact desktops, shipping fall 2026).

Both share the properties that break today's GPU tooling: no discrete VRAM (CPU and GPU share one LPDDR5X pool), an Arm CPU, Blackwell `sm_121`, CUDA 13 only, and `nvidia-smi` that reports memory as not supported.

## What we know so far (with sources)

### DGX Spark hardware and software

- GB10 Grace Blackwell superchip: 20-core Arm CPU (10 Cortex-X925 + 10 Cortex-A725), Blackwell GPU with 6,144 CUDA cores (`sm_121`), 128 GB unified LPDDR5X at about 273 GB/s, ConnectX-7 networking for clustering two units. Sources: [LMSYS review](https://www.lmsys.org/blog/2025-10-13-nvidia-dgx-spark/), [Kubesimplify day 3](https://blog.kubesimplify.com/day-3-the-dgx-spark-unpacked-gb10-unified-memory-sm-121-and-the-one-reason-this-hardware-exists).
- DGX OS 7 (Ubuntu 24.04 based) with the NVIDIA-optimized Arm64 kernel; current at time of writing: DGX OS 7.4.0, driver 580.126.09, CUDA 13.0.2; driver packages are the `-open` kernel modules (`nvidia-driver-580-open`, with the branch dropped from package names on Spark). `/etc/dgx-release` carries the DGX name, software version and OTA information. Sources: [DGX OS 7 user guide](https://docs.nvidia.com/dgx/dgx-os-7-user-guide), [DGX Spark release notes](https://docs.nvidia.com/dgx/dgx-spark/release-notes.html).
- `nvidia-smi` reports **Memory-Usage: Not Supported** on DGX Spark; memory must be read from Linux (`free`, `/proc/meminfo`). Community workarounds exist (a drop-in NVML replacement, a memory dashboard). Sources: [DGX Spark docs](https://docs.nvidia.com/dgx/dgx-spark/index.html), [forum: nvidia-smi is broken on DGX Spark](https://forums.developer.nvidia.com/t/dear-nvidia-nvidia-smi-is-broken-on-the-dgx-spark/367765), [NVML community solution](https://forums.developer.nvidia.com/t/nvml-support-for-dgx-spark-grace-blackwell-unified-memory-community-solution/358869), [memory dashboard](https://forums.developer.nvidia.com/t/dgx-spark-memory-dashboard-live-gib-occupancy-map-can-i-load-next/381279).
- CUDA memory model note: for the integrated GPU, memory returned by `cudaMalloc` is not coherently accessible by the CPU or PCIe devices; managed/unified allocations behave differently from discrete GPUs. Source: [forum: GPUDirect RDMA on DGX Spark](https://forums.developer.nvidia.com/t/dgx-spark-gpudirect-rdma/348787/6).

### Known failure modes reported by users (candidate analyzer rules)

| Symptom | Cause / fix reported | Source |
|---|---|---|
| `cuBLAS INTERNAL_ERROR`, `CUDNN_FE failure 11` under batch load | Older 580.95 / 580.126 driver; fixed by 580.173 | [forum](https://forums.developer.nvidia.com/t/gb10-dgx-spark-cudnn-fe-failure-11-and-cublas-status-internal-error-under-batch-load-fixed-by-driver-580-173/380948) |
| "NVIDIA-GB10 not supported" after a restore | Driver 580-open reinstall path | [forum](https://forums.developer.nvidia.com/t/nvidia-gb10-not-supported-on-dgx-spark-2026-after-restore-driver-580-open-troubleshooting/367655) |
| `nvidia-smi: No devices found` after `apt upgrade` to 580.173.02 on OTA2607 | Driver / kernel mismatch after partial upgrade | [forum](https://forums.developer.nvidia.com/t/dgx-spark-apt-upgrade-to-driver-580-173-02-breaks-gpu-on-ota2607-nvidia-smi-no-devices-found/378200) |
| Random hard power-off with no log | Mitigated by a GPU clock cap made persistent with systemd | [tonyd2wild/dgx-spark-hard-poweroff-fix](https://github.com/tonyd2wild/dgx-spark-hard-poweroff-fix) |
| GX10 (ASUS variant) stuck throttled | Power-delivery throttle fix | [Sggin1/DGX-SPARK](https://github.com/Sggin1/DGX-SPARK/blob/main/GX10_PD_Throttle_Fix.md) |
| Stuck at low power | Stale 550.x driver with CUDA 12.4; fixed by 580.95 + CUDA 13 | forum threads via [natolambert/dgx-spark-setup](https://github.com/natolambert/dgx-spark-setup) |
| Only ~56 GB "VRAM" visible in AI Workbench | Unified memory reported through a tool that expects discrete VRAM | [forum](https://forums.developer.nvidia.com/t/dgx-spark-gb10-shows-only-56gb-vram-inside-ai-workbench-128gb-expected/351441) |
| `ImportError: libcudart.so.12` | Package built for CUDA 12 on a CUDA 13-only system | [martimramos/dgx-spark-ml-guide](https://github.com/martimramos/dgx-spark-ml-guide) |
| PyTorch cannot see the GPU / slow | CPU-only or cu12 wheel; use `--index-url https://download.pytorch.org/whl/cu130` (aarch64 wheels exist) | [natolambert/dgx-spark-setup](https://github.com/natolambert/dgx-spark-setup), [PyTorch forums](https://discuss.pytorch.org/t/dgx-spark-gb10-cuda-13-0-python-3-12-sm-121/223744) |
| vLLM / flash-attn / others lack `sm_121` wheels | Ecosystem gap; build from source or use NVIDIA containers | [forum roadmap thread](https://forums.developer.nvidia.com/t/dgx-spark-sm121-software-support-is-severely-lacking-official-roadmap-needed/357663), [vLLM issue](https://github.com/vllm-project/vllm/issues/31128) |
| HDMI display does not wake after deep sleep | Documented limitation | [DGX Spark docs](https://docs.nvidia.com/dgx/dgx-spark/index.html) |

### RTX Spark (N1X) for Windows on Arm

- Announced by NVIDIA and Microsoft on 2026-05-31: 20-core Grace CPU, Blackwell RTX GPU with up to 6,144 CUDA cores, up to 128 GB unified LPDDR5X, roughly RTX 5070 Laptop class, CUDA runs natively on Windows on Arm. Laptops (14 mm, 3 lb, 14 to 16 inch) and compact desktops from ASUS, Dell, HP, Lenovo, Microsoft Surface and MSI in fall 2026; Acer and GIGABYTE later. Sources: [NVIDIA newsroom](https://nvidianews.nvidia.com/news/nvidia-microsoft-windows-pcs-agents-rtx-spark), [NVIDIA RTX Spark page](https://www.nvidia.com/en-us/products/rtx-spark/), [Wikipedia](https://en.wikipedia.org/wiki/Nvidia_RTX_Spark), [PCWorld list of devices](https://www.pcworld.com/article/3154922/rtx-spark-all-the-laptops-and-mini-pcs-announced-so-far.html), [CNBC](https://www.cnbc.com/2026/05/31/nvidias-new-chip-to-power-fresh-line-of-windows-laptops-by-dell-hp.html).
- Implication for NVCheckup: a **windows/arm64** build target, Windows-on-Arm collectors (WMI works; `nvidia-smi.exe` behavior on unified memory to be confirmed), and the same unified-memory reporting rules as DGX Spark.

### Public code to learn from

- [NVIDIA/dgx-spark-playbooks](https://github.com/NVIDIA/dgx-spark-playbooks): 47+ playbooks (vLLM, TensorRT-LLM, SGLang, llama.cpp, Ollama, NeMo, Unsloth, JAX, Isaac, NCCL, connect-two-sparks with a `discover-sparks` script and a performance benchmarking guide). Each has troubleshooting sections that are a rich source of rules and recommended flags.
- [NVIDIA/k8s-device-plugin issue 1482](https://github.com/NVIDIA/k8s-device-plugin/issues/1482): GB10 support in the device plugin.
- Community: [natolambert/dgx-spark-setup](https://github.com/natolambert/dgx-spark-setup), [Sggin1/DGX-SPARK](https://github.com/Sggin1/DGX-SPARK), [ogulcanaydogan/dgx-spark-llm-stack](https://github.com/ogulcanaydogan/dgx-spark-llm-stack), [assix pytorch aarch64 cu130 wheels](https://github.com/assix/pytorch-aarch64-cuda130-python310-wheels).
- Note: the NVIDIA GitHub organization's private repositories are not reachable from the current token (SAML SSO not authorized). Everything above is public.

## Plan

1. **Detection.** Recognize the platform class: `dgx-spark` (GB10 via `nvidia-smi -L` name "NVIDIA GB10", `/etc/dgx-release`, `/proc/device-tree/model`, aarch64), `rtx-spark` (N1X on Windows on Arm via WMI processor/system product names and GPU name), and a generic `unified-memory` flag whenever `nvidia-smi` reports memory as not supported on an integrated GPU.
2. **Unified memory collector.** Read total/available/used from `/proc/meminfo` (Linux) or WMI (Windows), swap and zram state, hugepages, `vm.overcommit_memory`; record the largest single allocation the GPU could plausibly take; never call absent VRAM "low VRAM".
3. **Arm CUDA ecosystem probes.** Detect CPU-only or cu12 wheels on a CUDA 13-only system, `libcudart.so.12` mismatches, missing `sm_121` support in installed frameworks, driver-below-580.173 with known cuBLAS failures, `nvidia-smi` "No devices found" after a partial upgrade, and DGX OS OTA state.
4. **Clustering.** ConnectX-7 presence and link state, NCCL sanity, MTU, when two Sparks are connected.
5. **Rules and fixes.** New analyzer rules with stable ids for every row in the table above; no new `fix` actions that could brick a Spark (clock caps stay advisory).
6. **LLM optimization wizard.** `nvcheckup llm-plan` (name to be decided): interactive or flag-driven; takes the model (parameters, quantization) and target runtime (llama.cpp, Ollama, vLLM, SGLang, TensorRT-LLM), reads the unified-memory budget and bandwidth class, computes fit including KV cache for the requested context, and emits a plan: quantization to use (NVFP4 / Q4_K_M), context and batch limits, runtime flags, container invocation, and the checks that must pass first (cu130 wheel, sm_121 kernels, driver version).
7. **Windows on Arm.** Add `windows/arm64` to release and CI; verify collectors on the `windows-11-arm` runner.
8. **Simulated scenarios.** A GB10 scenario for the simulated-GPU field test (memory fields `[N/A]`, `sm_121`, aarch64) and, when available, a real DGX Spark run.

## Open questions for research

- Exact `nvidia-smi` field behavior on GB10 (which fields are `[N/A]`, what `pcie.link.*`, `pstate`, `power.draw`, `fan.speed` return).
- The exact `/proc/device-tree/model` and `/etc/dgx-release` contents on DGX Spark and the GX10.
- Whether `nvidia-smi.exe` exists on RTX Spark Windows on Arm and what it reports; WMI names for the N1X CPU and GPU.
- Recommended memory headroom and swap policy for LLM serving on unified memory; how the playbooks size KV cache.
- Which frameworks ship `sm_121` wheels today and the canonical install commands.
