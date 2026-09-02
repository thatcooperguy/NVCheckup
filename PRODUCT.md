# NVCheckup — Complete Product Description

> **Version 0.2.1** | MIT License | Written in Go 1.22 (standard library only)
> Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.

---

## What NVCheckup Is

NVCheckup is a single-binary, cross-platform diagnostic CLI for NVIDIA GPU environments. It scans your system, identifies common failure patterns across gaming, AI/CUDA, and streaming workloads, and generates clean, privacy-safe reports with ranked findings and actionable next steps.

It is designed for four moments:
1. **"I'm stuck."** — You have a black screen, a CUDA error, or driver crashes and don't know where to start.
2. **"I need to file a bug report."** — You need a clean system summary to paste in a forum or GitHub issue.
3. **"What changed?"** — You updated a driver or kernel and need to compare before/after state.
4. **"Just do the safe thing."** — A finding has a known, reversible fix and you would rather apply it with a journal than edit the registry by hand.

Diagnostics (`run`, `snapshot`, `compare`, `doctor`, `self-test`) never change system settings; `self-test` creates and removes one temporary file in the current directory to verify write access. `fix` modifies the system only after an interactive confirmation, journals what it did, and `undo` reverses it. Nothing is sent anywhere. Nothing runs in the background.

---

## Platform Support

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Beta. Tested on Windows 11. |
| Linux (Ubuntu, Debian, Fedora, RHEL, Arch, and others) | x86_64 | Beta. Builds and unit-tests in CI; needs field reports. |
| Linux | ARM64 (aarch64) | Beta. Builds and unit-tests in CI; needs field reports. |
| WSL2 (inside Linux guest) | x86_64 | Limited (GPU passthrough diagnostics) |
| Jetson / Tegra (L4T, JetPack) | ARM64 | Limited. Detected; `nvidia-smi` does not exist on these boards, so the no-GPU / no-driver / `nvidia-smi` findings are suppressed and `tegrastats` is suggested. |

Build targets: `windows/amd64`, `linux/amd64`, `linux/arm64`. All produce static binaries of a few megabytes with zero runtime dependencies.

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
| Jetson / Tegra | Jetson Orin, Xavier, Nano (L4T / JetPack) | Detected, limited. These boards have no `nvidia-smi`. NVCheckup recognises the platform from `/etc/nv_tegra_release` (or an `NVIDIA Jetson` model string in `/proc/device-tree/model`), reports `system.is_jetson` and `system.jetson_release`, emits the INFO finding `jetson-detected`, suppresses the no-GPU / no-driver / `nvidia-smi` missing findings that would otherwise be wrong, and suggests `tegrastats` for thermal and load data. |
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

Answers determine the mode, log inclusion, whether network probes run, and the output format. The tool then runs the same pipeline as `nvcheckup run`.

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

NVCheckup collects roughly 60 data points through five collector packages (`common`, `windows`, `linux`, `wsl`, `ai`); a sixth package, `remediate`, is the only one that writes. Every collection is read-only — no writes, no registry edits, no kernel module changes, no package installs.

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
- **Next steps**: Safe, reversible actions (never destructive)
- **Remediation**: Optional pointer to a `fix` action
- **Category**: driver, gpu, cuda, ai, overlay, performance, hardware, secureboot, wsl, updates, network, display

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
| Hardware | Low VRAM |

The authoritative list is `internal/analyzer/analyzer.go`; `knowledge/rules.json` mirrors the ids so external tooling can look them up.

### Report Summaries

Every report automatically generates:
- **Top 5 Issues**: Highest-severity findings (CRIT and WARN only)
- **Top 5 Next Steps**: Deduplicated actionable steps from top findings
- **Summary Block**: 4-6 line pasteable summary with OS, GPU, driver, CUDA, and finding counts. On systems with two or more NVIDIA GPUs one extra line is added: `GPUs: N NVIDIA (worst temp XX°C on GPU i)`. The single-GPU format is unchanged.

---

## Output Formats

### report.txt (always generated)

Human-readable, forum-pasteable, 72-character-wide formatting. Sections:
1. Header (version, disclaimer, timestamp, mode, platform, runtime, redaction status)
2. Summary block (designed for copy-paste into support threads)
3. System info table
4. GPU inventory (per-GPU detail, thermal and PCIe state; with one NVIDIA GPU the `Thermal:` and `PCIe:` lines describe GPU 0, with two or more there is one `Thermal:` and one `PCIe:` line per GPU)
5. Platform-specific details (Windows/Linux/WSL/AI/Network — whichever applies)
6. Findings with full evidence, explanation, and next steps
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
    "tool_version": "0.2.1",
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
  "summary_block": "NVCheckup v0.2.1 | 2026-09-01 20:41:40\nOS: Microsoft Windows 11 Enterprise 10.0.26200 | Arch: amd64\n..."
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

### report.md (with `--md`)

GitHub/Reddit-optimized markdown:
- System and GPU info in tables
- Findings in a summary table + expandable `<details>` blocks per finding
- Code block for the summary (paste-ready)
- Same privacy footer as `report.txt`
- Suitable for issue templates and forum posts

### nvcheckup-bundle-<timestamp>.zip (with `--zip`)

Timestamped zip archive (`nvcheckup-bundle-YYYYMMDD-HHMMSS.zip`) containing the generated report files (`report.txt`, plus `report.json` and `report.md` when requested). Nothing else is added to the archive. `--include-logs` does not add files to the bundle; on Linux it adds `journalctl` and `dmesg` snippets to the report data itself, and on Windows it is ignored.

---

## Privacy & Redaction

### Guarantees

| Property | Status |
|----------|--------|
| Telemetry | None. Zero analytics, tracking, or phone-home. |
| Network calls | None unless you pass `--network` to `run`, answer yes to the network question in `doctor`, or use `network-test`. Then: ICMP ping and traceroute to `1.1.1.1` and a DNS lookup of `google.com`, performed locally. No data is uploaded. |
| System modification | `run`, `snapshot`, `compare`, and `doctor` never modify anything. `self-test` never changes system settings; it creates and removes one temporary file in the current directory to verify write access. `fix` is opt-in, confirmed interactively, journaled, and undoable with `undo`. |
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
│   │   ├── common/                system, GPU (nvidia-smi + WMI/lspci), thermal, PCIe, network
│   │   ├── windows/               Event logs, HAGS, power plan, overlays, updates, displays (build tag: windows)
│   │   ├── linux/                 Modules, DKMS, Secure Boot, PRIME, Xid (build tag: linux)
│   │   ├── wsl/                   WSL2 detection and /dev/dxg checks
│   │   └── ai/                    CUDA, Python, PyTorch, TensorFlow probes
│   ├── analyzer/analyzer.go       Findings engine with stable ids
│   ├── remediate/                 Engine, journal, per-OS actions (fix / undo)
│   ├── redact/redact.go           PII redaction engine
│   ├── report/                    text.go, json.go, markdown.go
│   ├── bundle/zip.go              Zip archive packaging
│   ├── snapshot/snapshot.go       Create + compare system snapshots
│   ├── doctor/doctor.go           Interactive guided diagnostic mode
│   └── selftest/selftest.go       Environment and collector-query verification
├── pkg/types/types.go             All shared data structures (append-only)
├── knowledge/                     Reference pack: rules.json, xid_codes.json, remediations.json
└── docs/index.html                Landing page (GitHub Pages)
```

**The seven phases of `run`:**
1. Collect system information
2. Detect GPUs and drivers
3. Collect GPU thermal and PCIe data
4. Run platform-specific checks (Windows or Linux, plus WSL)
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

---

## Testing

Run `go test ./...`. Packages with tests:

| Package | What is covered |
|---------|-----------------|
| `internal/util` | Command execution, timeouts, parsing helpers |
| `internal/redact` | IP classification, path redaction, disabled passthrough |
| `internal/analyzer` | Finding rules and ids, sorting, summary generation, idle-PCIe and corrected-WHEA non-firing cases |
| `internal/collector/common` | Pure parse functions against captured `nvidia-smi`, ping, traceroute, and network output. The `nvidia-smi` fixtures cover RTX 3090 and RTX 4090 (development hardware), RTX 5090 (Gen5 link), RTX 4060 Laptop (native x8), GTX 1060 on a pre-R535 driver (legacy `clocks_throttle_reasons`), A100-SXM4 (no fan), H100 in MIG mode (`[N/A]` utilization), Tesla T4, Quadro RTX 8000, and a 3-GPU rig. `CONTRIBUTING.md` explains how to capture rows from hardware not yet covered |
| `internal/remediate` | Engine preview/apply/undo with a fake executor, journal read/write, undo validation |
| `internal/report` | Text structure, JSON output, markdown structure, footers |
| `pkg/types` | Constants, defaults, exit codes |

`go test -race` requires a C toolchain; CI runs it on Ubuntu only. CI also cross-vets with `GOOS=linux` and `GOOS=windows` so build-tagged files stay in sync.

---

## Build & Distribution

```bash
# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Inject the version (releases do this from the git tag)
go build -ldflags="-s -w -X github.com/thatcooperguy/nvcheckup/pkg/types.Version=0.2.1" -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets (static, stripped)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup
```

**CI/CD:** GitHub Actions workflows for:
- `ci.yml` — Tests, build, and self-test of the built binary on push/PR (Windows + Ubuntu matrix, Go 1.22 + stable); gofmt, `go vet`, cross-GOOS vet, and a non-blocking `govulncheck` (run with the current stable Go) in the lint job
- `release.yml` — Runs `go vet` and the test suite, verifies `CHANGELOG.md` mentions the tag, cross-compiles with the version injected from the tag, publishes SHA256 checksums and a GitHub Release. Windows binaries are unsigned; the release notes explain how to verify the checksum and pass SmartScreen.

---

## What NVCheckup Does Not Do

- Does not modify drivers, kernel modules, or system configuration during `run`, `snapshot`, `doctor`, or `self-test`
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
