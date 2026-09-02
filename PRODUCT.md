# NVCheckup — Complete Product Description

> **Version 0.2.3** | MIT License | Written in Go 1.22 (standard library only)
> Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.

---

## What NVCheckup Is

NVCheckup is a single-binary, cross-platform diagnostic CLI for NVIDIA GPU environments. It scans your system, identifies common failure patterns across gaming, AI/CUDA, and streaming workloads, and generates clean, privacy-safe reports with ranked findings and actionable next steps.

It is designed for four moments:
1. **"I'm stuck."** — You have a black screen, a CUDA error, or driver crashes and don't know where to start.
2. **"I need to file a bug report."** — You need a clean system summary to paste in a forum or GitHub issue.
3. **"What changed?"** — You updated a driver or kernel and need to compare before/after state.
4. **"Just do the safe thing."** — A finding has a known, reversible fix and you would rather apply it with a journal than edit the registry by hand.
5. **"Will this model fit?"** — You have a DGX Spark or RTX Spark with one shared memory pool and want a deployment plan computed from the memory you actually have, not from the number on the box (`llm-plan`).

Diagnostics (`run`, `snapshot`, `compare`, `doctor`, `self-test`, `llm-plan`) never change system settings; `self-test` creates and removes one temporary file in the current directory to verify write access. `fix` modifies the system only after an interactive confirmation, journals what it did, and `undo` reverses it. Nothing is sent anywhere. Nothing runs in the background.

---

## Platform Support

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Beta. Tested on Windows 11. |
| Linux (Ubuntu, Debian, Fedora, RHEL, Arch, and others) | x86_64 | Beta. Builds and unit-tests in CI; needs field reports. |
| Linux | ARM64 (aarch64) | Beta. Builds and unit-tests in CI; needs field reports. |
| Windows 11 on Arm (RTX Spark / N1X devices) | ARM64 | Beta. `nvcheckup-windows-arm64.exe` builds, unit-tests and self-tests on a `windows-11-arm` runner (no GPU). Platform class `rtx-spark`; unverified on hardware. |
| DGX Spark Founders Edition and OEM GB10 (ASUS Ascent GX10, HP ZGX Nano G1n, Lenovo ThinkStation PGX, Dell Pro Max GB10, MSI EdgeXpert, Acer, Gigabyte) on DGX OS 7 | ARM64 | Beta. Platform class `dgx-spark`, unified-memory handling, DGX OS, firmware, ConnectX-7 and ecosystem collectors. Simulated in CI (`gb10` scenario); unverified on hardware, capture wanted via `scripts/spark-capture.sh` (issue #3). |
| WSL2 (inside Linux guest) | x86_64; Arm64 where the device ships GPU passthrough (Surface RTX Spark Dev Box) | Limited (GPU passthrough diagnostics) |
| Jetson / Tegra (L4T, JetPack) | ARM64 | Limited. Detected from `/etc/nv_tegra_release` or the device-tree model; Orin-class boards have no `nvidia-smi`, so the no-GPU / no-driver / `nvidia-smi` findings are suppressed and `tegrastats` is suggested. Jetson Thor (compute capability 11.0) ships `nvidia-smi`, so its absence is not the test. |
| Grace Hopper / Grace Blackwell (GH200, GB200, GB300) | ARM64 | Platform class `grace-hopper`: coherent memory but discrete HBM, so the unified-memory suppressions are explicitly switched off. |

Build targets: `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`. All produce static binaries of a few megabytes with zero runtime dependencies.

---

## Supported GPUs

NVCheckup contains no GPU-model-specific logic. Every reading comes from the installed NVIDIA driver through `nvidia-smi --query-gpu`, and every rule reasons about the values that come back (temperature, P-state, clock event bits, link generation and width, memory), never about the product name. Any GPU the installed driver exposes through `nvidia-smi` is therefore supported. The development machine happened to be a single RTX 3090 on driver 591.86; nothing in the tool depends on that, and the parser fixtures cover the classes below.

| GPU class | Examples | Status and notes |
|-----------|----------|------------------|
| GeForce desktop | GTX 900 series and newer; RTX 20 / 30 / 40 / 50 series | Supported. RTX 50 series cards report a Gen5 link; the PCIe rules compare the current link against the maximum the GPU reports, so Gen5 needs no special case. |
| GeForce Laptop (Optimus / hybrid) | RTX 4060 Laptop GPU, RTX 3070 Ti Laptop GPU | Supported. Many laptops wire the dGPU with a native x8 link. The tool compares current width against the reported maximum, so x8 of x8 is not flagged. The hybrid iGPU + dGPU finding is informational. |
| Workstation RTX / Quadro | RTX A4000, RTX 6000 Ada, Quadro RTX 8000, Quadro P4000 | Supported. |
| Datacenter Tesla | Tesla T4, V100, P100 | Supported. Passively cooled cards report `fan.speed` as `[N/A]` or `[Not Supported]`; the tool records `fan_supported: false` and the fan rules do not fire. |
| Datacenter A-series | A100 (PCIe and SXM4), A30, A10, A2 | Supported. Same passive-cooling behaviour; SXM modules still report a PCIe link. |
| Datacenter H-series | H100 (PCIe and SXM5), H200 | Supported. In MIG mode `utilization.gpu` is `[N/A]`, so idle/load inference falls back to the P-state. |
| Multi-GPU systems | Two or more of any of the above | Supported. Every GPU is collected and analyzed. The report prints one `Thermal:` and one `PCIe:` line per GPU, `report.json` carries `gpu_thermal[]` and `gpu_pcie[]` (one object per GPU, keyed by `gpu_index`), and the summary block adds `GPUs: N NVIDIA (worst temp XX°C on GPU i)`. |
| Older drivers | Any driver before R535 | Supported. When the driver rejects `clocks_event_reasons.active`, the collector re-queries the legacy `clocks_throttle_reasons.active` field, which carries the same bits. |
| Jetson / Tegra | Jetson Orin, Xavier, Nano, Thor (L4T / JetPack) | Detected, limited. NVCheckup recognises the platform from `/etc/nv_tegra_release` (or an `NVIDIA Jetson` model string in `/proc/device-tree/model`), reports `system.is_jetson` and `system.jetson_release`, emits the INFO finding `jetson-detected`, suppresses the no-GPU / no-driver / `nvidia-smi` missing findings on boards that have no `nvidia-smi`, and suggests `tegrastats` for thermal and load data. Thor ships `nvidia-smi` and is handled as unified memory (class `jetson`). |
| DGX Spark / OEM GB10 and RTX Spark (N1X) | `NVIDIA GB10` (PCI `10de:2e12`, compute capability 12.1); `NVIDIA RTX Spark N1X` (PCI `10de:2e03` 6,144-core / `10de:2e06` 5,120-core, compute capability inferred 12.1) | Detected and specially handled. Unified memory: there is no VRAM figure, `nvidia-smi` reports `memory.total` as `[N/A]` (`Memory-Usage: Not Supported`), the fan and power limit as `N/A` and the PCIe link as `GEN 1@ 1x`, all by design. `gpus[].memory_reporting` is `not-supported`, `gpus[].on_package` and `pcie.on_package` are `true`, the pool is read from `/proc/meminfo` (Windows: `Win32_OperatingSystem.TotalVisibleMemorySize`), the summary prints `Unified memory: X GiB total, Y GiB available` and `PCIe: n/a (on-package, NVLink-C2C)`, and the VRAM, fan, power-limit and PCIe rules are suppressed. Implemented from public documentation and simulated in CI; unverified on hardware. |
| vGPU and cloud instances | GRID / vGPU profiles, cloud GPU VMs | Expected to work wherever `nvidia-smi` works. Untested; fixture contributions are welcome (see `CONTRIBUTING.md`). |

If a GPU you own is not in this table, it still works as long as `nvidia-smi` does. Rows captured from hardware not listed here are the most useful contribution you can make; `CONTRIBUTING.md` explains how to capture them.

---

## CLI Commands

### `nvcheckup run`

The primary command. Runs collectors, analyzes results, and generates reports. Read-only.

```
nvcheckup run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `full` | Diagnostic focus: `gaming`, `ai`, `creator`, `streaming`, `full` |
| `--out` | `.` | Output directory for all generated files |
| `--zip` | off | Bundle the generated report files into `nvcheckup-bundle-YYYYMMDD-HHMMSS.zip` |
| `--json` | off | Generate `report.json` (structured machine-readable output) |
| `--md` | off | Generate `report.md` (GitHub/Reddit-ready markdown) |
| `--network` | off | Opt in to network probes: ICMP ping (10 echoes) and traceroute to `1.1.1.1`, in-process DNS lookup of `google.com`. Runs in any mode when set; never runs when unset. |
| `--verbose` | off | Print detailed progress to console |
| `--timeout` | `30` | Per-command timeout in seconds |
| `--redact` | **on** | Redact usernames, hostnames, home paths, IPs, and emails from all output |
| `--no-redact` | off | Disable PII redaction |
| `--include-logs` | off | Linux only: add `journalctl` and `dmesg` snippets to the report data. Ignored on Windows. Adds no extra files to the zip bundle |

### `nvcheckup fix`

Lists and applies remediation actions. The only command that changes system state.

```
nvcheckup fix                                # list actions for this platform
nvcheckup fix --all                          # preview every action
nvcheckup fix --id <id> --dry-run            # preview one action; changes nothing
nvcheckup fix --id <id> [--journal DIR]      # apply after typing "yes"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--id` | | Action id to preview or apply |
| `--all` | off | Preview all available actions |
| `--dry-run` | off | Print what would happen. Runs only the action's read-only capture commands (for example `reg query`, `powercfg /getactivescheme`, `modinfo`, a package listing) and changes nothing |
| `--journal` | user config dir | Directory holding `nvcheckup-changes.json` |
| `--out` | | Deprecated alias for `--journal` |

| Action id | Platform | Risk | Needs admin/root | Effect |
|-----------|----------|------|------------------|--------|
| `set-high-performance` | Windows | low | yes | Set the active power plan to High Performance |
| `disable-hags` | Windows | medium | yes | Set `HwSchMode` to disabled; restart required |
| `disable-game-mode` | Windows | low | no | Set `AutoGameModeEnabled` to 0 for the current user |
| `blacklist-nouveau` | Linux | medium | yes | Write a modprobe blacklist and rebuild the initramfs; reboot required |
| `update-ldconfig` | Linux | low | yes | Run `ldconfig` to refresh the shared library cache |

Behaviour:
- If the action has `NeedsAdmin` and the process is not elevated, `fix` reports that and exits **before** prompting.
- The preview shows the exact command or registry write. The user must type `yes`.
- The previous value is captured before the change and stored, with the action id, timestamp, and result, in the journal. Default journal path: `<os.UserConfigDir()>/nvcheckup/nvcheckup-changes.json` (`%APPDATA%\nvcheckup\nvcheckup-changes.json` on Windows, `~/.config/nvcheckup/nvcheckup-changes.json` on Linux). On Linux, `sudo nvcheckup fix` journals under the invoking user's `~/.config/nvcheckup` when `SUDO_USER` is set, otherwise under `/root/.config/nvcheckup`. `--journal DIR` overrides the location.

### `nvcheckup undo`

```
nvcheckup undo [--journal DIR]              # list journal entries
nvcheckup undo --id <id> [--journal DIR]    # reverse the newest successful, not-yet-undone entry for that id
```

Undo restores the recorded previous value. If the value was absent before the fix, undo removes it rather than inventing a default. Journal entries are validated (expected keys, value shape, allowed paths) before anything is written to a privileged location; the journal is never copied verbatim into a command or registry path.

### `nvcheckup network-test`

Standalone network diagnostics. Always probes (ping and traceroute to `1.1.1.1`, DNS lookup of `google.com`) and prints latency, jitter, packet loss, DNS time, interface type, Wi-Fi band/signal, and hops.

```
nvcheckup network-test [--timeout SEC]
```

### `nvcheckup snapshot`

Creates a timestamped JSON snapshot of system state. Filename: `nvcheckup-snapshot-YYYYMMDD-HHMMSS.json`. Redacted by default.

```
nvcheckup snapshot [--out DIR] [--timeout SEC] [--no-redact]
```

Snapshots capture: system info, GPU inventory, driver version, CUDA environment, and AI framework state. They do not include findings (those are computed at analysis time).

### `nvcheckup compare`

Diffs two snapshots and reports what changed. Flags come before the positionals; more than two positionals is an error.

```
nvcheckup compare [--out DIR] [--md] <before.json> <after.json>
```

Fields compared include OS version, kernel, driver version, CUDA version, GPU count, GPU names, GPU VRAM, CUDA toolkit, cuDNN, PyTorch version, PyTorch CUDA version, PyTorch CUDA availability (marked critical if changed).

### `nvcheckup doctor`

Interactive guided mode. Asks six questions to determine the most relevant diagnostic scope, then runs a targeted, read-only check.

```
nvcheckup doctor
```

**Questions asked:**
1. Primary use case (Gaming / AI / Creator / Streaming / General)
2. Issue type (Crashes / Performance / GPU not detected / Encoding or streaming / Unsure)
3. Recent changes (OS update / Driver update / New hardware / Software install / None)
4. Include extended logs? (Yes / No)
5. Run network probes (ping/traceroute to `1.1.1.1` and a DNS lookup)? (Yes / No; they take 30-60 s and contact external hosts, so the default is No)
6. Output format (Text only / Full bundle with JSON + Markdown + Zip)

Answers determine the mode, log inclusion, whether network probes run, and the output format. The tool then runs the same pipeline as `nvcheckup run`. On a GB10 or N1X host a seventh question, "Plan an LLM deployment for this machine?", hands off to `llm-plan`.

### `nvcheckup llm-plan`

LLM deployment sizing for unified-memory platforms (DGX Spark, OEM GB10, RTX Spark). Read-only: it reads the platform detection, `/proc/meminfo` (Windows: `TotalVisibleMemorySize` / `FreePhysicalMemory`), the listening inference ports and the ecosystem probes, then prints a plan. It never downloads a model or an image, never starts, stops or kills a process or container, never edits systemd units, sysctl, swap, `/etc/fstab`, env files or GNOME settings, never locks clocks, writes only inside `--out`, never assumes "128 GB", never reads `nvidia-smi` memory on a unified platform and never presents an estimate as a measurement.

```
nvcheckup llm-plan [--model NAME | --params B --active-params B --layers N --kv-heads N --head-dim N | --hf-config config.json]
                   [--quant bf16|fp8|q8_0|nvfp4|mxfp4|q4_k_m] [--context TOKENS] [--concurrency N]
                   [--profile chat|agent|batch|rag] [--runtime vllm|trtllm|sglang|llamacpp|ollama|auto]
                   [--kv-dtype auto|f16|fp8|q8_0] [--headroom-gib N] [--memory-gib N] [--json] [--out DIR]
```

Without `--model` it asks doctor-style questions. Model shapes (layers, KV heads, head dimension, total and active parameters, sliding-window layers) ship in `knowledge/models.json` with their sources; `--hf-config` sizes any model from a local Hugging Face `config.json` offline.

**Arithmetic (spec section 7.4).** Weights `W = P_total x b` with `b` = 2.00 (bf16/fp16), 1.00 (fp8), 1.06 (q8_0), 0.56 (nvfp4), 0.53 for the expert weights of mxfp4, 0.60 (q4_k_m); the measured checkpoint size is preferred when known. KV per token `k = 2 x L x H_kv x d_head x bytes_kv`; `KV = k x context x concurrency`, and in the agent profile concurrency is 1 + subagents. Runtime reserve `R`: 3 GiB for llama.cpp and Ollama, 12 GiB for vLLM, 10 GiB for SGLang and TensorRT-LLM. OS floor `F`: 8 GiB headless DGX OS, 10 GiB with GNOME or on Windows. A plan fits when `W + KV + R + F <= MemTotal` and `W + KV + R <= MemAvailable` now; the pool is always the measured `MemTotal` (119.7 GiB on a 128 GB unit), never a tier table. vLLM's `--gpu-memory-utilization` is `ceil05((W + KV + R) / MemTotal)` clamped to 0.30..0.85, and the same fraction feeds TensorRT-LLM's `free_gpu_memory_fraction` and SGLang's `--mem-fraction-static`. Two decode ceilings are printed per plan, weights-only (`273e9 / bytes_active_weights`) and at-context (`273e9 / (bytes_active_weights + k x context)`), with a 50-80% realism band on the second; prefill is quoted as the measured 2,000-8,000 tok/s range. `unified-memory-pressure`'s thresholds (WARN below 8 GiB, CRIT below 4 GiB) are aligned with `F` so a plan the wizard accepts does not WARN in `run`.

**Prerequisites** are evaluated PASS / WARN / FAIL from the existing read-only collectors: driver >= 580 and CUDA 13; OTA not torn; torch `+cu130` with `sm_120` in the arch list; `TRITON_PTXAS_PATH` when Triton is present; swap used < 1 GiB; page cache vs `MemAvailable`; no other resident model server on ports 8000 / 30000 / 11434 / 8355; a linux/arm64 container image with a cu130 tag; a working Docker GPU runtime; `--ipc=host` or `--shm-size`; `MemAvailable >= W + KV + R`.

**Output.** Text: header (platform, pool, `MemAvailable`, bandwidth), fit verdict with the arithmetic, runtime command block (vLLM, TensorRT-LLM, SGLang, llama.cpp or Ollama templates from the DGX Spark playbooks; vLLM's `--kv-cache-dtype fp8` only on an explicit `--kv-dtype fp8`), environment block, prerequisite table, estimates and warnings. `--json` writes `plan.json` (see Output Formats). Exit codes: 0 fits, 1 fits with warnings, 2 does not fit, 3 error. An illustration is in `examples/sample-llm-plan.txt`.

### `nvcheckup self-test`

Verifies the environment has the tools NVCheckup needs and that the collector queries it depends on succeed on this driver. It never changes system settings; it creates and removes one temporary file in the current directory to verify write access. Exit code 1 means warnings (for example no GPU present), which CI treats as acceptable.

```
nvcheckup self-test
```

**Checks performed:**

| Check | All Platforms | Windows Only | Linux Only |
|-------|:---:|:---:|:---:|
| OS detection | x | | |
| Architecture (amd64/arm64) | x | | |
| nvidia-smi available and functional | x | | |
| nvidia-smi accepts the thermal/PCIe query fields the collectors use | x | | |
| nvidia-smi accepts the tolerant `index,compute_cap` query (older drivers reject it; on GB10 memory `[N/A]` is reported as expected) | x | | |
| Write permissions to current directory | x | | |
| Python available (python3/python/py) | x | | |
| Elevation (informational: lists the checks that degrade without admin/root) | x | | |
| PowerShell available | | x | |
| lspci available (for GPU enumeration) | | | x |
| modinfo available (for module checks) | | | x |

### `nvcheckup version`

Prints version string and disclaimer. Also accepts `--version` and `-v`.

---

## Diagnostic Modes

| Mode | What Runs | Best For |
|------|-----------|----------|
| `gaming` | GPU, driver, thermal, PCIe, Windows event logs, overlays, power plan, HAGS, displays | Black screens, crashes, stutter, driver resets |
| `ai` | GPU, driver, thermal, PCIe, Linux modules, Secure Boot, CUDA stack, PyTorch, TensorFlow | `torch.cuda.is_available() == False`, CUDA errors |
| `streaming` | GPU, driver, thermal, PCIe, Windows gaming checks, overlay detection | Recording/streaming software conflicts |
| `creator` | GPU, driver, thermal, PCIe, Windows gaming checks, CUDA environment | Creative application readiness |
| `full` | Everything: all platform checks, AI/CUDA, WSL, VRAM analysis | When you don't know what's wrong |

Network probes are not part of any mode. They run only with `--network`, or when you answer yes to the network question in `doctor`.

---

## Data Collected

NVCheckup collects roughly 60 data points through five collector packages (`common`, `windows`, `linux`, `wsl`, `ai`), plus the platform, unified-memory, DGX OS, ConnectX-7, ecosystem and Windows-on-Arm collectors described below that run only when the platform calls for them; a sixth package, `remediate`, is the only one that writes. Every collection is read-only — no writes, no registry edits, no kernel module changes, no package installs. When the environment variable `NVC_SIM_ROOT` is set, every absolute file path a collector reads (`/etc/...`, `/proc/...`, `/sys/...`, `/var/...`, `/run/...`) is prefixed with it and commands are resolved through `PATH`, which is how CI injects a simulated GB10.

### Universal System Snapshot

| Data Point | Source (Windows) | Source (Linux) |
|------------|-----------------|----------------|
| OS name and version | WMI `Win32_OperatingSystem` | `/etc/os-release` |
| OS build number | WMI | N/A |
| Kernel version | N/A | `uname -r` |
| Architecture | `runtime.GOARCH` | `runtime.GOARCH` |
| Boot mode (UEFI/BIOS) | `Confirm-SecureBootUEFI` | `/sys/firmware/efi` existence |
| Secure Boot state | `Confirm-SecureBootUEFI` | `mokutil --sb-state` |
| CPU model | WMI `Win32_Processor` | `/proc/cpuinfo` |
| RAM total (MB) | WMI `Win32_ComputerSystem` | `/proc/meminfo` |
| Storage free (MB) | `Get-PSDrive C` | `df -m /` |
| System uptime | WMI boot time calculation | `uptime -p` |
| Timezone | Go `time.Now().Location()` | Go `time.Now().Location()` |
| Hostname | `os.Hostname()` | `os.Hostname()` |
| Jetson / Tegra detection (`is_jetson`, `jetson_release`) | N/A | `/etc/nv_tegra_release` (its first line becomes `jetson_release`) or `/proc/device-tree/model` containing `NVIDIA Jetson` |

### GPU & Driver Inventory

| Data Point | Source |
|------------|--------|
| GPU list (name, vendor, index) | `nvidia-smi -L` + WMI/lspci |
| PCI vendor/device IDs | WMI PNPDeviceID / `lspci -nn` |
| PCI bus ID | `nvidia-smi --query-gpu` |
| GPU index (row attribution on multi-GPU systems) | `nvidia-smi --query-gpu=index` (first field of the inventory, thermal and PCIe queries) |
| Driver version | `nvidia-smi --query-gpu=driver_version` |
| VRAM total/used/free (MB) | `nvidia-smi --query-gpu=memory.*` |
| GPU temperature (°C) | `nvidia-smi --query-gpu=temperature.gpu` |
| Power draw | `nvidia-smi --query-gpu=power.draw` |
| CUDA version (from driver) | Parsed from `nvidia-smi` header |
| Raw `nvidia-smi` table | `nvidia-smi` with the `Processes:` section removed before storage |
| WDDM version | Windows registry `HKLM:\SOFTWARE\Microsoft\DirectX` |
| iGPU detection (Intel/AMD) | WMI `Win32_VideoController` / lspci vendor IDs |

### Thermal & PCIe (all platforms, every NVIDIA GPU)

| Data Point | Source | Notes |
|------------|--------|-------|
| GPU index | `nvidia-smi --query-gpu=index,...` | Every thermal and PCIe query carries `index`, so each CSV row is attributed to the right GPU. All rows are parsed, not just the first. `report.json` keeps `thermal` and `pcie` as GPU 0 for compatibility and adds `gpu_thermal[]` / `gpu_pcie[]` with one object per GPU (`gpu_index`) |
| Current/max clocks, power draw/limit, fan speed, utilization | `nvidia-smi --query-gpu=...` | Fan reported as unsupported on passively cooled cards (`[N/A]`, `[Not Supported]`). Utilization is `[N/A]` on H100 in MIG mode; idle/load inference then uses the P-state |
| Active throttle reasons | `nvidia-smi --query-gpu=clocks_event_reasons.active` | `gpu_idle` is not treated as a slowdown; only thermal, power, and HW slowdown reasons are. Drivers before R535 reject this field; the collector falls back to the legacy `clocks_throttle_reasons.active` spelling |
| PCIe current/max generation and width, power state | `nvidia-smi --query-gpu=pcie.link.*,pstate` | A Gen1 link at low utilization is reported as expected idle power-saving, not a downshift. Width is compared against the GPU's own maximum, so a native x8 laptop link is not flagged. Gen5 (RTX 50 series) needs no special case |
| `nvidia-smi` failure text | `nvidia-smi` stderr/stdout | `No devices were found`, `Unable to determine the device handle` and `Failed to initialize NVML` each produce a specific collector error naming the likely cause instead of a generic query failure |

### Windows-Specific Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| HAGS state | Registry `HwSchMode` | 2=Enabled, 1=Disabled, absent="Default (not configured)" |
| Game Mode state | Registry `AutoGameModeEnabled` | absent="Default (not configured)" |
| Active power plan | `powercfg /getactivescheme` | WMI `Win32_PowerPlan` is unavailable on many systems |
| Monitor resolution/refresh | WMI `Win32_VideoController` | Per-adapter |
| Event ID 4101 (driver resets) | `Get-WinEvent` System log | Last 30 days, up to 50 events; zero matches is an empty result, not an error |
| nvlddmkm errors | `Get-WinEvent` by provider | Last 30 days, up to 50 events |
| WHEA hardware errors | `Get-WinEvent` WHEA-Logger | Last 30 days, up to 20 events; corrected and uncorrected are distinguished; the reporting device name is kept |
| Recent Windows Updates | `Get-HotFix` | Last 60 days, KB ID + date |
| NVIDIA App version | Registry `NVIDIA Corporation\NVIDIA App` | |
| GeForce Experience version | Registry `NVIDIA Corporation\Global\GFExperience` | |
| Installed overlay software | Registry uninstall keys | Name matching only, not process scanning |

**Overlay software detected by name:**
Xbox Game Bar, Discord, MSI Afterburner, RivaTuner Statistics Server (RTSS), OBS Studio, NVIDIA ShadowPlay, Overwolf, Medal.tv, Action! Screen Recorder

### Linux-Specific Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| Distro name and version | `/etc/os-release` | |
| Package manager | Checks `apt`, `dnf`, `yum`, `pacman`, `zypper` | |
| NVIDIA packages installed | `dpkg -l` / `rpm -qa` / `pacman -Q` | Distro-specific |
| Loaded kernel modules | `lsmod \| grep nvidia\|nouveau` | nvidia, nvidia_drm, nvidia_modeset, nvidia_uvm, nouveau |
| Module existence (not loaded) | `modinfo <module>` | Distinguishes "exists but not loaded" from "doesn't exist" |
| `/dev/nvidia*` device nodes | `filepath.Glob("/dev/nvidia*")` | |
| `libcuda.so` path | `ldconfig -p` + common path checks | 6 fallback locations |
| DKMS status | `dkms status` | |
| DKMS build errors | DKMS output parsing | |
| Secure Boot state | `mokutil --sb-state` | |
| MOK enrollment status | `mokutil --list-enrolled` | Checks for NVIDIA-specific keys |
| Session type | `XDG_SESSION_TYPE` / `loginctl` | x11 or wayland |
| PRIME offload status | `prime-select query` / env var | |
| Xid errors | kernel log | Matched against the Xid reference table |
| Container runtime | Checks `docker`, `podman` | |
| nvidia-container-toolkit | `nvidia-container-cli --version` / package query | |
| Journal log snippets | `journalctl -k -b -g nvidia\|NVRM\|gpu` | Only with `--include-logs` |
| dmesg snippets | `dmesg \| grep nvidia\|NVRM\|gpu\|nouveau` | Only with `--include-logs` |

### WSL2 Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| Is WSL environment | `/proc/version` contains "Microsoft" or "wsl" | |
| WSL version | `/proc/sys/fs/binfmt_misc/WSLInterop` existence | WSL2 if present |
| WSL distro name | `/etc/os-release` | |
| `/dev/dxg` exists | `os.Stat("/dev/dxg")` | GPU paravirtualization device |
| nvidia-smi works in WSL | `nvidia-smi -L` | |

### Platform Detection (all platforms)

| Data Point | Source | Notes |
|------------|--------|-------|
| `platform.class` | Phase 1: `/etc/dgx-release` (`DGX_NAME="DGX Spark"`), `/etc/fastos-release`, `/etc/nv_tegra_release`, `/proc/device-tree/model`, `lspci -nn` (`10de:2e12` GB10; `10de:2e03` / `10de:2e06` N1X), `/sys/class/dmi/id/{sys_vendor,product_name,product_version,bios_version,bios_date}`, `uname -r`; Windows: `IsWow64Process2`, `PROCESSOR_ARCHITECTURE` / `ARCHITEW6432`, `Win32_Processor.Architecture` (12 = ARM64), `Win32_ComputerSystemProduct`, PNP `DEV_2E03` / `DEV_2E06`, driver INF | One of `dgx-spark`, `rtx-spark`, `jetson`, `grace-hopper`, `arm64-dgpu` or empty. First match wins; PCI ids are evaluated before any compute-capability heuristic |
| GPU-derived rows and flag rules | After phase 3: `nvidia-smi -L` name (`NVIDIA GB10`, `GH200` / `GB200` / `GB300`), numeric vs `[N/A]` `memory.total`, `compute_cap` | `common.ApplyPlatformFlags` sets `unified_memory`, `gpus[].on_package`, `gpus[].memory_reporting` and `pcie.on_package` for every GPU. Rule A: class `dgx-spark` / `rtx-spark` (and `jetson` for unified memory). Rule B: any NVIDIA GPU with `[N/A]` memory, even with no class. Rule C: `grace-hopper` forces both flags off |
| Vendor, model, product version, BIOS | DMI / WMI | Founders Edition = `NVIDIA` / `NVIDIA_DGX_Spark` / `A.7`; other vendors with a GB10 are OEM units of the same class |
| CPU model on arm64 | `lscpu` `Model name:` lines, MIDR fallback (`CPU part` `0xd85` = Cortex-X925, `0xd87` = Cortex-A725, `0xd4f` = Neoverse V2) | `/proc/cpuinfo` has no `model name` on arm64 |
| `nvidia_kernel_flavour` | `uname -r` matching `^\d+\.\d+\.\d+-\d+-nvidia(-64k\|-lowlatency)?$` | Canonical `linux-nvidia`; informational, consumed only by `dgx-spark-non-nvidia-kernel` |
| `compute_cap` per GPU | `nvidia-smi --query-gpu=index,compute_cap` (`GPUCapQueryFields`) | A separate tolerant query because older drivers reject the field; also run by `self-test` |

### Unified Memory (only when `platform.unified_memory`)

| Data Point | Source | Notes |
|------------|--------|-------|
| `mem_total_kb`, `mem_free_kb`, `mem_available_kb`, buffers, cached | `/proc/meminfo` | The pool. On a 128 GB GB10 `MemTotal` is 125,513,944 kB = 119.7 GiB (2025 units) |
| Swap total / free, devices incl. zram, `vm.swappiness` | `/proc/meminfo`, `/proc/swaps`, `/proc/sys/vm/swappiness` | |
| Huge pages | `/proc/meminfo` `HugePages_Total`, `HugePages_Free`, `Hugepagesize` | |
| `allocatable_kb` | Computed: `MemAvailable + SwapFree`; when `HugePages_Total != 0`, `HugePages_Free x Hugepagesize` and swap counts 0 | NVIDIA's guidance (spec section 3.3); never `nvidia-smi`, `cudaMemGetInfo` or `MemFree` |
| Memory pressure | `/proc/pressure/memory` `some` / `full` avg10 | The thrash signal while a model server has pre-allocated its fraction |
| Swap-in delta | `/proc/vmstat` `pswpin`, sampled twice | Feeds `unified-memory-swap-in-use` |
| GPU process, OOM-kill and `NV_ERR_NO_MEMORY` counts | `nvidia-smi` process table (count only), kernel log | Counts only, never process names |
| Clock event counters | `nvidia-smi -q -d PERFORMANCE` "Clocks Event Reasons Counters" | `thermal.event_counters_us`; `thermal.power_limit_supported` is `false` when `power.limit` is `[N/A]` |
| Windows pool | `Win32_OperatingSystem.TotalVisibleMemorySize` / `FreePhysicalMemory`; `dxdiag /t` dedicated vs shared memory | RTX Spark |

### DGX OS (only on `dgx-spark`)

| Data Point | Source | Notes |
|------------|--------|-------|
| Release, OTA version and date, build, platform quirk, serial | `/etc/dgx-release` (`DGX_SWBUILD_VERSION`, `DGX_OTA_VERSION`, `DGX_OTA_DATE`, `DGX_SERIAL_NUMBER`, `DGX_PLATFORM`), `/etc/fastos-release` | Serial redacted to `<serial>` |
| OTA health | `nvidia-spark-ota-check summary` / `torn-score` (10 s timeout) | `ota_name` (`OTA2607`), `ota_torn`, `ota_failed` |
| Driver / firmware / module pairing | `dpkg-query` for `nvidia-driver-580-open`, `nvidia-firmware-580-*`, `linux-modules-nvidia-580-open-$(uname -r)` | A torn pair is `dgx-spark-ota-torn`; a foreign package set is `dgx-spark-foreign-driver-packages` (`nvidia-dkms-NNN-open` is excluded on purpose, pending field confirmation) |
| Service state | `systemctl is-active` for `dgx-dashboard`, `dgx-dashboard-admin`, `fwupd`, `nvidia-persistenced`, `nvidia-suspend`; `systemctl show -p LoadState` for the optional `gb10-clock-cap.service` | `units_queried` is true only when `systemctl` answered; then the `*_active` booleans are measurements. When it is false (no systemd, a container, a timeout) they are unknown, and the analyzer never raises `dgx-spark-dashboard-unhealthy` from them |
| Dashboard port | `/proc/net/tcp` and `/proc/net/tcp6` `LISTEN` rows on port 11000 | Read from procfs; no connection is opened |
| Firmware | `fwupdmgr get-devices` | `platform.firmware[]` with name, GUID, version (dotted or hex, e.g. `0x03000508` = 3.5.8, decode marked as an assumption) and pending version |
| Container toolkit source | First line of `/etc/apt/sources.list.d/nvidia-container-toolkit.list` | |
| Previous boot | `journalctl --list-boots`, tail of `journalctl -b -1` classified by clean-shutdown markers (`Journal stopped`, `systemd-shutdown`, `Shutting down.`, `Reached target Power-Off/Reboot/Halt`); unclean boots counted over N days | `prev_boot_clean`, `prev_boot_last_line` |
| pstore, ACPI thermal zones, GDM sleep policy, suspend markers | `/sys/fs/pstore` (empty or not), `/sys/class/thermal` `acpitz` zones, GDM greeter `sleep-inactive-ac-type`, journal suspend entries | `pstore_empty`, `acpi_thermal_mc`, `gdm_sleep_policy`, `suspend_attempts`, `suspend_failed` |

### Cluster Fabric: ConnectX-7 (only on `dgx-spark`, `ai` and `full` modes)

| Data Point | Source | Notes |
|------------|--------|-------|
| 15b3 functions | `lspci -nn` (`15b3:1021` at `0000:01:00.0/.1`, `0002:01:00.0/.1`) | |
| RDMA device to netdev mapping, port state, physical state, rate | `/sys/class/infiniband/<dev>/ports/1/{state,phys_state,rate}`, `device/net/*` | `rocep1s0f0` -> `enp1s0f0np0`, `roceP2p1s0f0` -> `enP2p1s0f0np0` (port 0 twins) |
| Netdev operstate, carrier, speed, MTU, IPv4 | `/sys/class/net/<if>/{operstate,carrier,speed,mtu}`, `ip -br addr` | Addresses are redacted as `<lan-ip>` |
| Cage grouping | Function index across PCI domains `0000` and `0002` | Never by stripping characters from the name |
| Bonds, hot-plug file, netplan | `/proc/net/bonding`, `/etc/nvidia/cx7-hotplug-enabled`, netplan address / MTU keys | |
| NCCL / UCX environment of the NVCheckup process, `libnccl.so.2` version, net plugin | Environment, `ldconfig -p` | Only this process's environment; no other process is inspected |
| avahi state and conflicts, ufw, RDMA tools | `systemctl is-active avahi-daemon`, `journalctl -u avahi-daemon`, `/etc/ufw/ufw.conf`, `command -v` | No packets are sent; an opt-in `cluster-test` is deferred |

### AI Ecosystem on Spark (only on `dgx-spark` / `rtx-spark`, `ai`, `creator` and `full` modes)

| Data Point | Source | Notes |
|------------|--------|-------|
| PyTorch probe stderr and `torch.cuda.get_arch_list()` | The existing `python -I` probe | `ai.pytorch_info.warnings`, `ai.pytorch_info.arch_list`; the "(8.0) - (12.0)" capability warning is benign |
| Triton `ptxas` version and `TRITON_PTXAS_PATH` | Triton's bundled `ptxas --version`, environment | |
| `libcudart.so.12` / `.13` presence | `ldconfig -p`, common paths | |
| `flash_attn`, `onnxruntime` versions and ORT providers | Python `importlib.metadata`, `onnxruntime.get_available_providers()` | |
| Docker image architectures and tags, runtimes, CDI | `docker image inspect`, `/etc/docker/daemon.json`, `/etc/cdi/nvidia.yaml`, `snap list docker` | |
| Listening inference ports | `/proc/net/tcp` for 8000, 30000, 11434, 8355, 11000, 7474 | Port numbers only, never process names |

### Windows on Arm (only on `rtx-spark`)

| Data Point | Source | Notes |
|------------|--------|-------|
| Native and process machine | `IsWow64Process2` via `kernel32.dll`, `PROCESSOR_ARCHITECTURE` / `ARCHITEW6432` | `is_windows_on_arm`, `process_emulated`, `native_machine` |
| Processor architecture, product name | `Win32_Processor.Architecture` (12), `Win32_ComputerSystemProduct.Name` | |
| GPU identity | PNP `PCI\VEN_10DE&DEV_2E03` / `DEV_2E06`, adapter name, INF (`nv_surface_woa.inf`), WDDM `DriverVersion` | The 616.00 Developer Preview is expected to end in `16.1600` |
| Memory | `dxdiag /t` dedicated vs shared, `TotalVisibleMemorySize` | |
| CUDA toolkit nativeness | `nvcc.exe` PE machine type | AMD64 under Prism is `woa-cuda-toolkit-not-native` |
| Windows build | `Win32_OperatingSystem.BuildNumber` | Below 26100 is `woa-windows-build-too-old` |
| `nvidia-smi.exe` | Presence | Missing is INFO on `rtx-spark`, not a missing driver |

### AI/CUDA Framework Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| CUDA Toolkit version | `nvcc --version` / `/usr/local/cuda` symlink / `CUDA_PATH` env | `CUDA_PATH` is passed as an argument, never interpolated into a shell string |
| nvcc path | `which nvcc` / common locations | |
| cuDNN version | Parsed from `cudnn_version.h` (`CUDNN_MAJOR.MINOR.PATCHLEVEL`) | Checks 4 header locations |
| Python versions + paths | `python3 --version`, `python --version`, `py --version` | |
| Conda present | `CommandExists("conda")` | |
| **PyTorch probe** | Inline Python script run with `python -I` | Isolated mode ignores `PYTHONPATH` and user site |
| — torch version | `torch.__version__` | |
| — torch CUDA version | `torch.version.cuda` | Empty = CPU-only build |
| — CUDA available | `torch.cuda.is_available()` | |
| — GPU device name | `torch.cuda.get_device_name(0)` | |
| **TensorFlow probe** | Inline Python script run with `python -I` | Extra 10s timeout (slow import) |
| — tf version | `tf.__version__` | |
| — Physical GPUs | `tf.config.list_physical_devices('GPU')` | |
| Key pip packages | Inline Python `__import__` | torch, tensorflow, jax, onnxruntime, transformers, numpy, scipy |

### Network Collection (only with `--network` or `network-test`)

| Data Point | Source | Notes |
|------------|--------|-------|
| Interface name and type, Wi-Fi band and signal | `netsh wlan show interfaces` / `iw` / `nmcli` | Type derived from the interface, not from the word "disconnected" |
| Latency, jitter, packet loss | `ping 1.1.1.1` (10 echoes) | |
| Traceroute hops | `tracert` / `traceroute 1.1.1.1` | |
| DNS resolution time | In-process `net.Resolver` lookup of `google.com`, three attempts | Measures the lookup only, not process start-up; the slowest of the three is reported |

---

## Findings Engine

The analyzer processes collected data through more than 45 diagnostic rules and produces ranked findings. Each finding includes:

- **ID**: Stable kebab-case identifier (for example `pcie-downshift`, `whea-errors`, `hags-enabled`, `nouveau-active`). Appears in `report.json` as `findings[].id`; script against it, not the title.
- **Severity**: `CRIT` (blocker), `WARN` (likely contributor), `INFO` (context)
- **Confidence**: 0-100, how sure the rule is that the condition is a problem
- **Title**: Clear, specific description
- **Evidence**: What was observed (with data)
- **Why it matters**: Plain-language impact explanation
- **Next steps**: Safe, reversible actions (never destructive), ordered from least to most invasive. A step that would change driver, firmware, kernel, swap, systemd, firewall, Secure Boot, snap or netplan state starts with the word `Advisory` (renderers match the regex `^Advisory` followed by a word boundary, so `Advisory:` and `Advisory: (data loss)` both qualify) and carries the exact revert command or an explicit data-loss warning. The text and markdown renderers print Advisory steps with their own marker, never before a read-only step. They are advice; NVCheckup does not run them
- **Impact**: `none`, `reversible`, `persistent`, `irreversible` or `data-loss`, the most invasive of the finding's next steps. Printed next to the severity in text and markdown, emitted as `findings[].impact` in `report.json` (`omitempty`, so reports from rules without an impact are unchanged; `schema_version` stays `"1"`)
- **Remediation**: Optional pointer to a `fix` action. No Spark rule has one
- **Category**: driver, gpu, cuda, ai, overlay, performance, hardware, secureboot, wsl, updates, network, display
- **Platforms** (knowledge pack only): the closed set `dgx-spark`, `rtx-spark`, `jetson`, `grace-hopper`, `arm64-dgpu`, `all`, matching the detector's class names; a knowledge test rejects anything else

### Rule Categories

| Category | Examples |
|----------|----------|
| GPU detection and driver basics | No NVIDIA GPU, hybrid iGPU + dGPU, driver version missing, `nvidia-smi` not in PATH, Jetson/Tegra board detected (`jetson-detected`, INFO; on Tegra the no-GPU, no-driver and `nvidia-smi` missing findings are suppressed because the board has no `nvidia-smi` by design) |
| Windows stability | Event ID 4101 resets, nvlddmkm errors, WHEA uncorrected errors (WARN) and corrected errors (INFO), recent Windows Updates correlated with resets |
| Windows settings | HAGS enabled, power plan not High Performance, Game Mode |
| Overlays | NVIDIA App / GeForce Experience, third-party overlay and recording software |
| Thermal and power | Thermal throttling active, GPU running hot, fan not spinning, power state stuck |
| PCIe | Link downshifted under load (WARN), link power-saving at idle (INFO, expected), legacy link speed |
| Displays | Mixed refresh rates, complex display chains |
| Linux kernel and driver | nouveau active, NVIDIA module not loaded, no `/dev/nvidia*`, `libcuda.so` not found, DKMS build failure, Xid errors, llvmpipe fallback, Wayland issues |
| Secure Boot | Module blocked (CRIT) or loading fine (INFO) |
| CUDA and frameworks | Toolkit newer than driver supports, PyTorch import error / CPU-only / no GPU / working, TensorFlow import error / no GPU / working |
| WSL2 | `/dev/dxg` missing, `nvidia-smi` failing inside WSL |
| Network (opt-in) | High jitter, packet loss, Wi-Fi congestion, slow DNS, healthy |
| Hardware | Low VRAM (never emitted on unified-memory platforms) |
| Platform (Spark) | `dgx-spark-detected`, `rtx-spark-detected`, `grace-hopper-detected`, `rtx-spark-linux-unsupported` |
| Unified memory | `unified-memory-nvsmi-expected` (INFO: `[N/A]` is expected), `unified-memory-pressure` (WARN below 8 GiB `MemAvailable` with a GPU process or PSI full > 0.1, CRIT below 4 GiB or PSI full > 1.0), `unified-memory-swap-in-use` (INFO, WARN with low `MemAvailable`; never advises `swapoff` under load), `unified-memory-page-cache-hold`, `unified-memory-oom-events` |
| DGX OS pairing and updates | `dgx-spark-gsp-init-failure` (CRIT: GB10 in `lspci`, `No devices were found`, GSP / SEC2 dmesg; replaces `no-nvidia-gpu`), `dgx-spark-ota-torn`, `dgx-spark-driver-too-old`, `dgx-spark-driver-branch-unsupported`, `dgx-spark-foreign-driver-packages`, `dgx-spark-cublas-batch-bug`, `dgx-spark-non-nvidia-kernel`, `dgx-spark-ota-outdated`, `dgx-spark-dashboard-unhealthy`, `dgx-spark-firmware-behind` |
| GB10 power and thermal | `gb10-pd-power-wedge` (CRIT when every thermal sample shows >= 90% utilization, SM clock < 1400 MHz, < 40 W and no active event reason; WARN on one sample), `gb10-logless-hard-poweroff` (>= 2 unclean boots, empty pstore), `gb10-acpi-thermal-zone-hot`, `gb10-clock-cap-active`, `dgx-spark-suspend-failure`, `dgx-spark-cx7-slot-power-benign` |
| sm_121 and Arm64 software | `arm64-cuda12-wheel-on-cuda13`, `sm121-torch-capability-warning-benign`, `sm121-kernel-missing`, `sm121-triton-ptxas-stale`, `arm64-flash-attn-no-wheel`, `arm64-container-amd64-image`, `sm121-ngc-image-too-old`, `docker-snap-gpu-blocked`, `docker-cdi-spec-missing`, `onnxruntime-cuda-provider-missing`, `gb10-k8s-device-plugin-old` |
| ConnectX-7 clustering | `cx7-not-enumerated` (CRIT; WARN on the 6.17.0-1021/1029 hot-plug regression), `cx7-twin-link-mismatch`, `cx7-link-speed-degraded`, `cx7-up-no-ip`, `cx7-twins-same-subnet`, `cx7-mtu-mismatch`, `nccl-env-misconfigured`, `nccl-gdr-assumed`, `cx7-mdns-hostname-conflict`, `cx7-firewall-blocks-cluster` |
| Windows on Arm | `rtx-spark-driver-developer-preview`, `woa-cuda-toolkit-not-native`, `woa-nvcheckup-emulated`, `woa-windows-build-too-old`, `wsl-linux-driver-installed` |

The authoritative list is the set of non-test `.go` files in `internal/analyzer` (`analyzer.go` plus `analyzer_spark.go`, `analyzer_cluster.go` and `analyzer_woa.go`); `knowledge/rules.json` mirrors the ids, modes, platforms and impact values so external tooling can look them up, and `TestRulesJSON_MatchesAnalyzer` fails when they drift. The 51 Spark rules, with triggers, evidence templates, sources and next steps, are catalogued in `docs/roadmap/spark-rules.json`.

**Suppressions on unified memory.** When `platform.unified_memory` is set: `low-vram` is never emitted; `pcie-downshift`, `pcie-width-reduced` and `pcie-idle-power-saving` are skipped for GPUs with `on_package` (which covers stock DGX OS whatever detection row matched, so the misreported `GEN 1@ 1x` link cannot fire them); `fan-not-spinning` stays gated on `fan_supported`; `gpu-power-cap`, `gpu-clock-slowdown` and `gpu-power-state-stuck` are kept because clocks, P-state and utilization are real, with the evidence printing `limit N/A (unified memory)`, and `gb10-pd-power-wedge` takes precedence; `no-nvidia-gpu` and `driver-not-detected` are not emitted when `dgx-spark-gsp-init-failure` fires; `nvidia-smi-missing` is INFO on `rtx-spark`; `jetson-detected` no longer claims `nvidia-smi` is absent (Thor has it); `nvidia-app-detected` is not expected on Windows ARM64; Xid 120 ("GSP task exception") is added and the Xid 119 evidence mentions driver / firmware pairing on GB10.

### Report Summaries

Every report automatically generates:
- **Top 5 Issues**: Highest-severity findings (CRIT and WARN only)
- **Top 5 Next Steps**: Deduplicated actionable steps from top findings
- **Summary Block**: 4-6 line pasteable summary with OS, GPU, driver, CUDA, and finding counts. On systems with two or more NVIDIA GPUs one extra line is added: `GPUs: N NVIDIA (worst temp XX°C on GPU i)`. The single-GPU format is unchanged. On unified-memory platforms `VRAM: N MB` becomes `Unified memory: 119.7 GiB total, 115.9 GiB available`, a `Platform:` line names the class, vendor, model and DGX OS / OTA version, and the PCIe line reads `PCIe: n/a (on-package, NVLink-C2C)`.

---

## Output Formats

### report.txt (always generated)

Human-readable, forum-pasteable, 72-character-wide formatting. Sections:
1. Header (version, disclaimer, timestamp, mode, platform, runtime, redaction status)
2. Summary block (designed for copy-paste into support threads)
3. System info table
4. GPU inventory (per-GPU detail, thermal and PCIe state; with one NVIDIA GPU the `Thermal:` and `PCIe:` lines describe GPU 0, with two or more there is one `Thermal:` and one `PCIe:` line per GPU)
5. Platform-specific details (Windows/Linux/WSL/AI/Network — whichever applies), plus on Spark systems a `PLATFORM` block and `UNIFIED MEMORY`, `DGX OS`, `FIRMWARE` and `CLUSTER FABRIC` sections
6. Findings with full evidence, explanation, and next steps; the impact is printed next to the severity and `Advisory:` steps carry their own marker
7. Top Issues summary
8. Recommended Next Steps
9. Collector Notes (commands that failed or timed out)
10. Privacy & Data footer: "This report was generated locally. No diagnostic data was transmitted." followed, only when probes ran, by "Network probes were run at your request (ICMP ping and traceroute to 1.1.1.1, DNS lookup of google.com)." and then "The run command did not modify your system. Changes are made only by 'nvcheckup fix' after explicit confirmation."

### report.json (with `--json`)

Complete structured output. The top-level keys are `metadata`, `system`, `gpus`, `driver`,
`windows` (Windows only), `linux` and `wsl` (Linux only), `ai`, `thermal`, `pcie`, `gpu_thermal`,
`gpu_pcie`, `displays`, `network`, `findings`, `collector_errors`, `top_issues`, `next_steps`, and
`summary_block`. `thermal`, `pcie`, `gpu_thermal`, `gpu_pcie`, `displays` and `network` are siblings
of `gpus`, not nested inside it. `thermal` and `pcie` describe GPU 0 (unchanged from 0.2.0);
`gpu_thermal` and `gpu_pcie` hold one object per NVIDIA GPU, each carrying `gpu_index`.
Keys marked `omitempty` in `pkg/types` are absent when there is nothing to report; in
particular `network` appears only when probes ran and `collector_errors` only when a
collector failed. The example below is abridged from a real Windows `--mode full` run
(schema version `"1"`, one representative element per array):

```json
{
  "metadata": {
    "tool_version": "0.2.3",
    "timestamp": "2026-09-01T20:41:40.512458-05:00",
    "mode": "full",
    "runtime_seconds": 59.1,
    "redaction_enabled": true,
    "platform": "windows",
    "schema_version": "1",
    "network_probes": false
  },
  "system": {
    "os_name": "Microsoft Windows 11 Enterprise",
    "os_version": "10.0.26200",
    "os_build": "26200",
    "architecture": "amd64",
    "boot_mode": "UEFI",
    "secure_boot": "Disabled",
    "cpu_model": "AMD Ryzen Threadripper PRO 3975WX 32-Cores",
    "ram_total_mb": 65382,
    "storage_free_mb": 30104,
    "uptime": "1d 9h 15m",
    "timezone": "Local (CDT, UTC-05:00)",
    "hostname": "<host>"
  },
  "gpus": [
    {
      "index": 0,
      "name": "NVIDIA GeForce RTX 3090",
      "vendor": "NVIDIA",
      "pci_bus_id": "00000000:41:00.0",
      "driver_version": "591.86",
      "wddm_version": "4.09.00.0904",
      "vram_total_mb": 24576,
      "vram_free_mb": 21590,
      "vram_used_mb": 2737,
      "temperature_c": 42,
      "power_draw": "31.55",
      "is_nvidia": true
    }
  ],
  "driver": {
    "version": "591.86",
    "cuda_version": "13.1",
    "nvidia_smi_path": "nvidia-smi",
    "nvidia_smi_output": "Tue Sep  1 20:41:59 2026 ..."
  },
  "windows": {
    "hags_enabled": "Default (not configured)",
    "game_mode": "Default (not configured)",
    "power_plan": "Balanced",
    "monitors": [
      { "name": "DEL DELL U2720Q", "resolution": "3840x2160", "refresh_rate": "60Hz", "primary": false }
    ],
    "whea_errors": [
      { "event_id": 17, "source": "System", "level": "Warning", "message": "A corrected hardware error has occurred. ...", "time": "2026-09-01T18:33:15.9783373Z" }
    ],
    "recent_kbs": [
      { "kb_id": "KB5120708", "title": "Update", "installed_on": "2026-08-31T00:00:00Z" }
    ],
    "nvidia_app_version": "11.0.7.247",
    "overlay_software": [ "Discord (may have overlay)" ]
  },
  "ai": {
    "cuda_toolkit_version": "13.1",
    "nvcc_path": "nvcc",
    "python_versions": [
      { "path": "<home>\\AppData\\Local\\Programs\\Python\\Python311\\python.exe", "version": "3.11.3" }
    ],
    "conda_present": false,
    "pytorch_info": {
      "version": "2.5.1+cu118",
      "cuda_version": "11.8",
      "cuda_available": true,
      "device_name": "NVIDIA GeForce RTX 3090"
    },
    "key_packages": [
      { "name": "torch", "version": "2.5.1+cu118" }
    ]
  },
  "thermal": {
    "temperature_c": 41,
    "thermal_throttle": false,
    "power_state": "P8",
    "current_clock_mhz": 210,
    "max_clock_mhz": 2100,
    "power_limit_w": "350.00",
    "power_draw_w": "31.11",
    "fan_speed_pct": 0,
    "fan_supported": true,
    "slowdown_active": false,
    "slowdown_reason": "0x0000000000000001",
    "throttle_reasons": [ "gpu_idle" ],
    "utilization_pct": 14
  },
  "pcie": {
    "current_speed": "Gen1",
    "max_speed": "Gen4",
    "current_width": "x16",
    "max_width": "x16",
    "downshifted": false,
    "power_state": "P8",
    "utilization_pct": 31,
    "idle_likely": true
  },
  "gpu_thermal": [
    {
      "gpu_index": 0,
      "temperature_c": 41,
      "thermal_throttle": false,
      "power_state": "P8",
      "current_clock_mhz": 210,
      "max_clock_mhz": 2100,
      "power_limit_w": "350.00",
      "power_draw_w": "31.11",
      "fan_speed_pct": 0,
      "fan_supported": true,
      "slowdown_active": false,
      "slowdown_reason": "0x0000000000000001",
      "throttle_reasons": [ "gpu_idle" ],
      "utilization_pct": 14
    }
  ],
  "gpu_pcie": [
    {
      "gpu_index": 0,
      "current_speed": "Gen1",
      "max_speed": "Gen4",
      "current_width": "x16",
      "max_width": "x16",
      "downshifted": false,
      "power_state": "P8",
      "utilization_pct": 31,
      "idle_likely": true
    }
  ],
  "displays": [
    {
      "name": "DEL DELL U2720Q",
      "resolution": "3840x2160",
      "refresh_hz": 60,
      "hdr_enabled": false,
      "hdr_capable": false,
      "vrr_enabled": false,
      "color_depth": "",
      "output_type": "DP",
      "gpu_index": 0,
      "primary": false,
      "scaling_pct": 0
    }
  ],
  "network": {
    "interface_name": "Ethernet 7",
    "interface_type": "ethernet",
    "latency_ms": 11.6,
    "jitter_ms": 0.5,
    "packet_loss_pct": 0,
    "dns_time_ms": 57.14,
    "hops": [
      { "number": 1, "address": "<lan-ip>", "latency_ms": 9, "loss": false }
    ]
  },
  "findings": [
    {
      "id": "power-plan-suboptimal",
      "severity": "INFO",
      "title": "Power Plan Not Set to High Performance",
      "evidence": "Active power plan: Balanced.",
      "why_it_matters": "Balanced or Power Saver plans may throttle CPU/GPU performance. ...",
      "next_steps": [
        "Open Power Options and switch to 'High Performance' for testing.",
        "This is a reversible change with no risk."
      ],
      "category": "performance",
      "confidence": 40,
      "remediation": {
        "id": "set-high-performance",
        "title": "Switch to High Performance power plan",
        "risk": "low",
        "description": "Sets the active Windows power plan to 'High performance' with powercfg. ...",
        "dry_run_desc": "Would run: powercfg /setactive 8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c (after capturing the current plan with powercfg /getactivescheme)",
        "undo_desc": "Restore the previously active plan: powercfg /setactive <captured GUID>",
        "platform": "windows",
        "needs_reboot": false,
        "needs_admin": true,
        "category": "power",
        "related_find": "Power plan is not set to High performance"
      }
    }
  ],
  "top_issues": [ "No significant issues detected." ],
  "next_steps": [ "Identify the device named in the event and update its driver and firmware." ],
  "summary_block": "NVCheckup v0.2.3 | 2026-09-01 20:41:40\nOS: Microsoft Windows 11 Enterprise 10.0.26200 | Arch: amd64\n..."
}
```

Objects not shown above because they are absent on Windows:

```
"linux": { distro, distro_version, package_manager, nvidia_packages, loaded_modules, dkms_status,
           dkms_errors, secure_boot_state, mok_status, session_type, prime_status, dev_nvidia_nodes,
           libcuda_path, container_runtime, nv_container_toolkit, journal_snippets, dmesg_snippets,
           xid_errors, llvmpipe_fallback, gl_renderer },
"wsl":   { is_wsl, wsl_version, distro, kernel_version, dev_dxg_exists, nvidia_smi_ok },
"collector_errors": [{ collector, error, fatal }]
```

The `network` object was taken from a separate `--network` run (its `metadata.network_probes` is
`true`); `wifi_band` and `wifi_signal_dbm` appear only on Wi-Fi. `system.kernel_version` appears on
Linux. On Jetson/Tegra boards `system.is_jetson` is `true` and `system.jetson_release` carries the
L4T release string; elsewhere both `is_jetson` and `jetson_release` are absent. `gpu_thermal[]`
and `gpu_pcie[]` have one element per NVIDIA GPU in `index` order (the example above shows a
single-GPU machine, so each array has one element identical to `thermal` / `pcie` apart from
`gpu_index`). `gpus[]` may also carry `pci_vendor_id`, `pci_device_id`, `pcie_link_speed` and
`pcie_link_width` when the collector could read them; `ai` may also carry `cuda_driver_version`,
`cudnn_version` and `tensorflow_info`. `findings[].references` is present only when a rule supplies
links, and `findings[].remediation` only when a `fix` action exists for the finding.
`nvidia_smi_output` contains the GPU table only; the `Processes:` section is removed before
storage. `displays` is populated on Windows.

**Spark and unified-memory additions (schema version stays `"1"`; everything below is additive and
`omitempty`, so reports from ordinary machines are unchanged).** `platform` is always present:

```
"platform": { class, vendor, model, product_version, bios_version, bios_date, gpu_soc, compute_cap,
              unified_memory, is_windows_on_arm, process_emulated, native_machine, nvidia_kernel_flavour,
              acpi_thermal_mc, prev_boot_clean, prev_boot_last_line, pstore_empty, clock_cap_unit,
              gdm_sleep_policy, suspend_attempts, suspend_failed, firmware[{ name, guid, version, pending }] },
"unified_memory": { mem_total_kb, mem_free_kb, mem_available_kb, buffers_kb, cached_kb, swap_total_kb,
                    swap_free_kb, huge_pages_total, huge_pages_free, hugepagesize_kb, allocatable_kb,
                    swap_devices, swappiness, psi_some_avg10, psi_full_avg10, gpu_processes, oom_kills,
                    nvrm_no_memory },
"dgx_os":    { name, pretty_name, sw_build_version, sw_build_date, ota_version, ota_date, platform, commit_id,
               serial_number ("<serial>"), fast_os_version, ota_name, ota_torn, ota_failed, driver_pkg_version,
               firmware_pkg_version, modules_for_kernel, dashboard_active, dashboard_admin_active, fwupd_active,
               persistenced_active, dashboard_port_open, fwupd_error, apt_source_corrupt, units_queried },
"cluster":   { ports[{ rdma_dev, netdev, pci_addr, cage, state, phys_state, speed_mbps, mtu, ipv4, bond, persistent }],
               hotplug_file_enabled, netplan_mtu, nccl_env, nccl_plugin_lib, nccl_version, peermem_attempted,
               avahi_active, avahi_conflicts, ufw_enabled, rdma_tools },
"ecosystem": { torch_arch_list, torch_warnings, triton_ptxas_version, triton_ptxas_path, libcudart_versions,
               flash_attn_version, ort_version, ort_providers, ort_gpu_shadowed, images[{ ref, arch }],
               docker_runtimes, docker_cdi, cdi_spec_present, snap_docker, listening_ports }
```

`unified_memory` appears only when `platform.unified_memory` is `true`; `dgx_os` only on `dgx-spark`; `cluster`
only when ConnectX-7 functions are enumerated; `ecosystem` only on `dgx-spark` / `rtx-spark`. Per-GPU additions:
`gpus[].compute_cap`, `gpus[].on_package`, `gpus[].memory_reporting` (`dedicated` | `not-supported`);
`thermal.power_limit_supported` and `thermal.event_counters_us` (also in `gpu_thermal[]`); `pcie.on_package` (also in
`gpu_pcie[]`); `ai.pytorch_info.warnings` and `ai.pytorch_info.arch_list`. Every finding may carry
`findings[].impact` (`none` | `reversible` | `persistent` | `irreversible` | `data-loss`); on Spark platforms every
emitted finding does. The simulated `gb10` CI job asserts `platform.class == "dgx-spark"`,
`platform.unified_memory == true`, `pcie.on_package == true`, `gpus[0].on_package == true`,
`gpus[0].memory_reporting == "not-supported"` and `unified_memory.mem_total_kb == 125513944`.

### report.md (with `--md`)

GitHub/Reddit-optimized markdown:
- System and GPU info in tables
- Findings in a summary table + expandable `<details>` blocks per finding
- Code block for the summary (paste-ready)
- Same privacy footer as `report.txt`
- Suitable for issue templates and forum posts

### plan.json (with `llm-plan --json`)

```
{ platform, memory{ total_gib, available_gib, headroom_gib }, model{ ... },
  fit{ weights_gib, kv_gib, runtime_gib, floor_gib, total_gib, fits_total, fits_now },
  estimates{ decode_ceiling_tps, decode_band_tps, prefill_ref_tps },
  runtime{ name, image, command, env }, prerequisites[{ id, status, detail }], warnings[] }
```

Written only inside `--out`. Estimates are bandwidth-derived ceilings and are labelled as such.

### nvcheckup-bundle-<timestamp>.zip (with `--zip`)

Timestamped zip archive (`nvcheckup-bundle-YYYYMMDD-HHMMSS.zip`) containing the generated report files (`report.txt`, plus `report.json` and `report.md` when requested). Nothing else is added to the archive. `--include-logs` does not add files to the bundle; on Linux it adds `journalctl` and `dmesg` snippets to the report data itself, and on Windows it is ignored.

---

## Privacy & Redaction

### Guarantees

| Property | Status |
|----------|--------|
| Telemetry | None. Zero analytics, tracking, or phone-home. |
| Network calls | None unless you pass `--network` to `run`, answer yes to the network question in `doctor`, or use `network-test`. Then: ICMP ping and traceroute to `1.1.1.1` and a DNS lookup of `google.com`, performed locally. No data is uploaded. |
| System modification | `run`, `snapshot`, `compare`, `doctor` and `llm-plan` never modify anything. Spark findings that need a state change print it as an `Advisory:` step with a revert command and carry an `impact` value; NVCheckup does not run those steps and ships no `fix` action for them. `self-test` never changes system settings; it creates and removes one temporary file in the current directory to verify write access. `fix` is opt-in, confirmed interactively, journaled, and undoable with `undo`. |
| Background services | None. No daemons, scheduled tasks, or auto-updates. |
| PII redaction | ON by default for `run` and `snapshot`. Disable with `--no-redact`. |

### What Is Never Collected

Passwords, tokens, API keys, browser data, SSH keys, clipboard contents, process lists or command lines, private documents, or anything outside the NVIDIA diagnostic scope. The `nvidia-smi` process list is stripped before the table is stored.

### Redaction Patterns (default ON)

| Pattern | Replacement |
|---------|-------------|
| Your home directory (`C:\Users\you\...`, `/home/you/...`) | `<home>\...` |
| Username in other profile paths (`C:\Users\name\...`) | `C:\Users\<user>\...` |
| Username standalone references | `<user>` (usernames shorter than 3 characters are not replaced as bare words; paths containing them still are) |
| Machine hostname | `<host>` |
| Public IPv4 addresses | `<public-ip-redacted>` |
| Private/LAN IPv4 addresses | `<lan-ip>` |
| Email addresses | `<email-redacted>` |
| Wi-Fi SSIDs | `SSID: <redacted>` |
| DGX Spark serial number (`DGX_SERIAL_NUMBER`; DMI serial files are not read) | `<serial>` |
| ConnectX-7 fabric addresses, `spark-xxxx` default hostnames | `<lan-ip>`, `<host>` (the existing rules cover them) |

Four-part version numbers such as `11.0.7.247` (NVIDIA App) or `32.0.101.6078` (a driver) are recognised by the word that introduces them and are not treated as IP addresses. The home-directory match ends at a path separator, so a sibling profile such as `C:\Users\alice2` is never mistaken for `C:\Users\alice`.

Redaction is applied to: hostname, summary block, GPU bus IDs, nvidia-smi output, nvidia-smi path, all finding evidence strings, all collector error messages, Linux libcuda path, Linux journal/dmesg snippets, AI nvcc path, all Python environment paths, and network hop addresses.

---

## Exit Codes

| Code | Meaning | When |
|------|---------|------|
| 0 | OK | No CRIT or WARN findings |
| 1 | Warnings | At least one WARN finding, no CRIT |
| 2 | Critical | At least one CRIT finding |
| 3 | Internal Error | Tool bug; debug info in collector notes |

Exit codes are set by the highest-severity finding in the report. Useful for CI/CD pipelines and scripting.

---

## Architecture

```
nvcheckup (static binary, a few MB)
├── cmd/nvcheckup/main.go          CLI entry point, subcommand dispatch
├── internal/
│   ├── core/                      Orchestration pipeline
│   │   ├── runner.go              7-phase pipeline (below)
│   │   ├── runner_windows.go      Platform dispatch (build tag: windows)
│   │   ├── runner_linux.go        Platform dispatch (build tag: linux)
│   │   └── runner_other.go        Fallback for unsupported platforms
│   ├── collector/
│   │   ├── common/                system, GPU (nvidia-smi + WMI/lspci), thermal, PCIe, network, platform.go, unified_memory.go
│   │   ├── windows/               Event logs, HAGS, power plan, overlays, updates, displays, woa.go (build tag: windows)
│   │   ├── linux/                 Modules, DKMS, Secure Boot, PRIME, Xid, dgxos.go, cx7.go, ecosystem.go (build tag: linux)
│   │   ├── wsl/                   WSL2 detection and /dev/dxg checks
│   │   └── ai/                    CUDA, Python, PyTorch, TensorFlow probes
│   ├── analyzer/                  Findings engine with stable ids: analyzer.go, analyzer_spark.go, analyzer_cluster.go, analyzer_woa.go
│   ├── remediate/                 Engine, journal, per-OS actions (fix / undo)
│   ├── redact/redact.go           PII redaction engine
│   ├── report/                    text.go, json.go, markdown.go
│   ├── bundle/zip.go              Zip archive packaging
│   ├── snapshot/snapshot.go       Create + compare system snapshots
│   ├── doctor/doctor.go           Interactive guided diagnostic mode
│   ├── llmplan/                   llm-plan: sizing.go, models.go, runtimes.go, prereqs.go, render.go
│   └── selftest/selftest.go       Environment and collector-query verification
├── pkg/types/types.go             All shared data structures (append-only)
├── knowledge/                     Reference pack: rules.json, xid_codes.json, remediations.json, models.json
└── docs/index.html                Landing page (GitHub Pages)
```

**The seven phases of `run`:**
1. Collect system information, then classify the platform from files, `lspci`, DMI and the kernel (`common.DetectPlatform`)
2. Detect GPUs and drivers
3. Collect GPU thermal and PCIe data, then apply the GPU-derived platform rows and the unified-memory / on-package flag rules (`common.ApplyPlatformFlags`)
4. Run platform-specific checks (Windows or Linux, plus WSL; on `dgx-spark` / `rtx-spark` also unified memory, DGX OS, ConnectX-7 and ecosystem)
5. Check the AI/CUDA environment (skipped in modes that do not need it)
6. Run network diagnostics (only with `--network`; otherwise skipped)
7. Analyze results into findings, then redact, report, and package

**Design principles:**
- Every external command runs with a configurable timeout (default 30s)
- Every collector catches its own errors and continues — one failed command never crashes the whole run
- Collectors separate "run the command" from "parse the output" so parsers are unit-tested against captured fixtures
- Platform-specific code uses Go build tags (`//go:build windows` / `//go:build linux`)
- No external Go dependencies — standard library only
- Cross-compilation produces static binaries for all three targets
- Untrusted strings (environment variables, command output, journal contents) are passed as arguments, never interpolated into shell commands

### Design decisions for Spark and unified-memory platforms

**Platform detection runs in two steps.** Phase 1 (`common.DetectPlatform`, right after `CollectSystemInfo`) classifies from things that exist whether or not the driver works: `/etc/dgx-release`, `/etc/fastos-release`, `lspci` PCI ids, DMI strings, the kernel version, and on Windows `IsWow64Process2` and PNP ids. That is what lets a GB10 whose GSP firmware failed to initialise (`nvidia-smi`: `No devices were found`) be diagnosed as `dgx-spark-gsp-init-failure` instead of `no-nvidia-gpu`. Step two (`common.ApplyPlatformFlags`, after phase 3 and before the platform-specific collectors) evaluates the rows that need GPU data (`nvidia-smi` names, numeric vs `[N/A]` memory, compute capability) and then applies flag rules A-C to every `GPUInfo` and `PCIeInfo`, so `unified_memory` and `on_package` never depend on which row happened to match first. The v1 design set those flags only in the hardware row and left stock DGX OS, which matches the `/etc/dgx-release` row, with `pcie-width-reduced` and a `VRAM:` line. PCI ids are evaluated before any compute-capability heuristic, so an N1X running Linux is never classed `dgx-spark`, and the capability-12.1-with-`[N/A]` heuristic no longer asserts a class on its own (it sets `gpu_soc` to `unknown-cc12.1` and lets rule B handle the memory).

**On-package GPUs suppress the PCIe rules entirely.** GB10 and N1X sit on the SoC package and talk to the CPU over NVLink-C2C; the PCIe link `nvidia-smi` reports (`GEN 1@ 1x`) is not a slot a user can reseat, so comparing it against a maximum produces a false `pcie-width-reduced` on every healthy unit. Rather than special-casing the values, the collectors mark the GPU and its PCIe sample `on_package` and the analyzer skips `pcieFindings` for it; the report prints `PCIe: n/a (on-package, NVLink-C2C)`. The same flag rule B applies to any NVIDIA GPU with `[N/A]` memory, so a future integrated part gets the same treatment without a code change. Grace Hopper is the deliberate exception (rule C): coherent memory, but a discrete HBM GPU with a real link.

**The wizard never runs installs, and no Spark finding has a `fix` action.** Most of the remedies users need on a Spark change driver, firmware, kernel, swap, systemd, netplan or firewall state, several of them cannot be undone by software (firmware flashes, OTA updates) and one erases the unit (the System Recovery image). Journaling such changes would not make them reversible, so NVCheckup does not perform them. Instead the rule catalogue carries an `impact` per rule and marks state-changing next steps `Advisory:` with the exact revert command or a data-loss warning; the renderers print them distinctly and after every read-only step. `llm-plan` follows the same rule (spec section 7.9): it prints the `docker run` line, the environment and the prerequisites, and it never downloads, starts, stops, edits or locks anything.

**Simulation contract.** When `NVC_SIM_ROOT` is set, one small helper prefixes every absolute path a collector reads; commands are still resolved through `PATH` so shims can answer. CI's `gb10` scenario relies on this: `scripts/make-simroot.sh` generates `/etc/dgx-release`, `/etc/fastos-release`, DMI, `/proc/meminfo`, `/proc/cpuinfo`, `/proc/net/tcp`, thermal, pstore, the ConnectX-7 sysfs trees, netplan, ufw, Docker and apt fixtures from the scenario, and shims answer `nvidia-smi`, `lspci`, `dpkg-query`, `systemctl`, `journalctl`, `ldconfig`, `ip`, `fwupdmgr`, `nvidia-spark-ota-check` and the rest. Values the spec marks unconfirmed (hex firmware decoding, the `32.0.16.1600` WDDM prefix, default swap size, `nvidia-dkms-580-open` on a stock image) sit behind named constants with a comment saying so.

**Unknown is not unhealthy (`dgx_os.units_queried`).** `DGXOSInfo` records the DGX Dashboard, fwupd and persistenced states as booleans, and a boolean cannot say "I could not ask". On a host without a usable `systemctl` (a container, a timeout, a non-systemd image) every `*_active` field would read `false` and `dgx-spark-dashboard-unhealthy` would fire on a perfectly healthy unit. The integration therefore added a third state: `units_queried` is set to true only when `systemctl` actually answered, the analyzer evaluates the dashboard rule only in that case, and the simulated `gb10` job asserts both that the flag is true and that the rule stays silent. The alternative, three-valued `*bool` fields, would have changed the JSON shape of fields that already existed.

**Field-capture plan.** Everything above is implemented from public documentation and community reports; none of it has run on a GB10 or N1X. `scripts/spark-capture.sh` captures the read-only, redacted fixture set of spec section 12 (`/etc/dgx-release`, DMI, `lscpu`, `/proc/meminfo`, `lspci -nn -d 10de:` / `-d 15b3:`, `dpkg -l 'nvidia-dkms*' 'nvidia-driver*'`, `nvidia-smi -L`, every query-field list including `compute_cap`, `nvidia-smi -q -d MEMORY,PERFORMANCE,CLOCK,POWER,TEMPERATURE`, `nvidia-spark-ota-check summary`, `fwupdmgr get-devices`, `ibdev2netdev`, `ip -br addr`, `/proc/device-tree/model`; on RTX Spark `Win32_VideoController`, `Win32_Processor.Name` and `nvidia-smi` if present). Captures go to issue #3 and become parser fixtures; the open questions they answer are listed in spec section 12 and `docs/roadmap/spark-work-packages.md`.

---

## Testing

Run `go test ./...`. Packages with tests:

| Package | What is covered |
|---------|-----------------|
| `internal/util` | Command execution, timeouts, parsing helpers |
| `internal/redact` | IP classification, path redaction, disabled passthrough |
| `internal/analyzer` | Finding rules and ids, sorting, summary generation, idle-PCIe and corrected-WHEA non-firing cases; one rule-corpus report per Spark rule; the healthy GB10 report yields exactly the expected INFO set and none of the forbidden ids; `unified-memory-swap-in-use` INFO / WARN split; `gb10-pd-power-wedge` WARN on one sample, CRIT only on all; every emitted finding carries an impact; knowledge tests for the closed `platforms` set and the five impact values |
| `internal/llmplan` | The three worked examples of spec 7.5 to +/-0.1 GiB and the ceilings 17 / 13.4 tok/s (8B BF16, 32K) and 6.9 / 3.3 tok/s (70B NVFP4, 128K) to +/-0.1; the 64 GB tier is never read from a table; vLLM `--kv-cache-dtype fp8` never emitted on sm_121 without `--kv-dtype fp8` |
| `internal/collector/common` | Pure parse functions against captured `nvidia-smi`, ping, traceroute, and network output, plus fixture tests for every Spark parser (`/etc/dgx-release`, DMI, `lscpu` and MIDR, `/proc/meminfo`, `[N/A]` and `Not Supported` memory rows, `compute_cap`, `-q -d PERFORMANCE` counters) using the verbatim strings of spec section 3. The `nvidia-smi` fixtures cover RTX 3090 and RTX 4090 (development hardware), RTX 5090 (Gen5 link), RTX 4060 Laptop (native x8), GTX 1060 on a pre-R535 driver (legacy `clocks_throttle_reasons`), A100-SXM4 (no fan), H100 in MIG mode (`[N/A]` utilization), Tesla T4, Quadro RTX 8000, and a 3-GPU rig. `CONTRIBUTING.md` explains how to capture rows from hardware not yet covered |
| `internal/remediate` | Engine preview/apply/undo with a fake executor, journal read/write, undo validation |
| `internal/report` | Text structure, JSON output, markdown structure, footers |
| `pkg/types` | Constants, defaults, exit codes |

`go test -race` requires a C toolchain; CI runs it on Ubuntu only. CI also cross-vets with `GOOS=linux` and `GOOS=windows` (both `amd64` and `arm64`) so build-tagged files stay in sync, and runs the test suite and `self-test` on a `windows-11-arm` runner.

---

## Build & Distribution

```bash
# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Inject the version (releases do this from the git tag)
go build -ldflags="-s -w -X github.com/thatcooperguy/nvcheckup/pkg/types.Version=0.2.3" -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets (static, stripped)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-windows-arm64.exe ./cmd/nvcheckup
```

**CI/CD:** GitHub Actions workflows for:
- `ci.yml` — Tests, build, and self-test of the built binary on push/PR (Windows, Windows 11 Arm and Ubuntu matrix, Go 1.22 + stable); gofmt, `go vet`, cross-GOOS vet including `GOOS=windows GOARCH=arm64`, and a non-blocking `govulncheck` (run with the current stable Go) in the lint job
- `linux-fieldtest-sim.yml` — Simulated GPUs on real Ubuntu runners: the `rig3` three-GPU rig, a Jetson, and on `ubuntu-24.04-arm` the `gb10` DGX Spark scenario (shims for `nvidia-smi`, `lspci`, `dpkg`, `lsmod`, `dmesg`, `dmidecode`, `lscpu`, `ibdev2netdev`, `fwupdmgr`, `nvidia-spark-ota-check`; fixtures under `NVC_SIM_ROOT`) with the assertions listed under report.json above, plus the `gb10-gsp-fail` variant that must yield `dgx-spark-gsp-init-failure` and not `no-nvidia-gpu`
- `release.yml` — Runs `go vet` and the test suite, verifies `CHANGELOG.md` mentions the tag, cross-compiles the four targets (including `nvcheckup-windows-arm64.exe`) with the version injected from the tag, publishes SHA256 checksums, build-provenance attestations and a GitHub Release. Windows binaries are unsigned; the release notes explain how to verify the checksum and pass SmartScreen.

---

## What NVCheckup Does Not Do

- Does not modify drivers, kernel modules, or system configuration during `run`, `snapshot`, `doctor`, `self-test` or `llm-plan`
- Does not change GPU clocks (`nvidia-smi -lgc` / `-rgc`), swap, `vm.swappiness`, systemd units, netplan, firewall rules, Secure Boot, snap packages, driver or firmware packages on a Spark; those appear only as `Advisory:` next steps with a revert command, and the finding's `impact` says how invasive they are
- Does not download models or container images, start, stop or kill processes or containers, or assume a "128 GB" pool in `llm-plan`; estimates are labelled as ceilings, never as measurements
- Does not send packets over the ConnectX-7 fabric; the cluster checks read sysfs, netplan and this process's environment only
- Does not read `nvidia-smi` memory as headroom on unified-memory platforms (`MemAvailable` is used)
- Does not apply any `fix` without an interactive confirmation, and never without journaling it
- Does not install packages or dependencies
- Does not delete files or clean caches
- Does not send data anywhere (no telemetry; network probes are local, opt-in, and upload nothing)
- Does not run in the background or create scheduled tasks
- Does not require admin/root for diagnostics (some checks benefit from it; `fix` actions that need it refuse to run unelevated)
- Does not scan browser data, clipboard, SSH keys, or documents
- Does not provide investment, legal, or warranty advice
- Is not affiliated with NVIDIA Corporation

---

*NVCheckup exists to make diagnosing NVIDIA ecosystem issues faster, safer, and less frustrating.*
