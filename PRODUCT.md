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

Diagnostics (`run`, `snapshot`, `compare`, `doctor`, `self-test`) never modify your system. `fix` modifies it only after an interactive confirmation, journals what it did, and `undo` reverses it. Nothing is sent anywhere. Nothing runs in the background.

---

## Platform Support

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Beta. Tested on Windows 11. |
| Linux (Ubuntu, Debian, Fedora, RHEL, Arch, and others) | x86_64 | Beta. Builds and unit-tests in CI; needs field reports. |
| Linux | ARM64 (aarch64) | Beta. Builds and unit-tests in CI; needs field reports. |
| WSL2 (inside Linux guest) | x86_64 | Limited (GPU passthrough diagnostics) |

Build targets: `windows/amd64`, `linux/amd64`, `linux/arm64`. All produce static binaries of a few megabytes with zero runtime dependencies.

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
| `--zip` | off | Package reports + logs into a timestamped zip bundle |
| `--json` | off | Generate `report.json` (structured machine-readable output) |
| `--md` | off | Generate `report.md` (GitHub/Reddit-ready markdown) |
| `--network` | off | Opt in to network probes: ICMP ping (10 echoes) and traceroute to `1.1.1.1`, in-process DNS lookup of `google.com`. Runs in any mode when set; never runs when unset. |
| `--verbose` | off | Print detailed progress to console |
| `--timeout` | `30` | Per-command timeout in seconds |
| `--redact` | **on** | Redact usernames, hostnames, home paths, IPs, and emails from all output |
| `--no-redact` | off | Disable PII redaction |
| `--include-logs` | off | Include extended system logs (journalctl, dmesg) in bundle |

### `nvcheckup fix`

Lists and applies remediation actions. The only command that changes system state.

```
nvcheckup fix                                # list actions for this platform
nvcheckup fix --all                          # preview every action
nvcheckup fix --id <id> --dry-run            # preview one action; nothing is executed
nvcheckup fix --id <id> [--journal DIR]      # apply after typing "yes"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--id` | | Action id to preview or apply |
| `--all` | off | Preview all available actions |
| `--dry-run` | off | Print what would happen; execute nothing |
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
- The previous value is captured before the change and stored, with the action id, timestamp, and result, in the journal. Default journal path: `<os.UserConfigDir()>/nvcheckup/nvcheckup-changes.json` (`%APPDATA%\nvcheckup\nvcheckup-changes.json` on Windows, `~/.config/nvcheckup/nvcheckup-changes.json` on Linux).

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

Fields compared: OS version, kernel, driver version, CUDA version, GPU count, GPU names, GPU VRAM, CUDA toolkit, cuDNN, PyTorch version, PyTorch CUDA version, PyTorch CUDA availability (marked critical if changed).

### `nvcheckup doctor`

Interactive guided mode. Asks 5 questions to determine the most relevant diagnostic scope, then runs a targeted, read-only check.

```
nvcheckup doctor
```

**Questions asked:**
1. Primary use case (Gaming / AI / Streaming / General)
2. Issue type (Crashes / Performance / GPU not detected / Other / Unsure)
3. Recent changes (OS update / Driver update / New hardware / Software install / None)
4. Include extended logs? (Yes / No)
5. Output format (Text only / Full bundle with JSON + Markdown + Zip)

Answers determine the mode, log inclusion, and output format. The tool then runs the same pipeline as `nvcheckup run`.

### `nvcheckup self-test`

Verifies the environment has the tools NVCheckup needs and that the collector queries it depends on succeed on this driver. No system modifications. Exit code 1 means warnings (for example no GPU present), which CI treats as acceptable.

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

Network probes are not part of any mode. They run only with `--network`.

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

### GPU & Driver Inventory

| Data Point | Source |
|------------|--------|
| GPU list (name, vendor, index) | `nvidia-smi -L` + WMI/lspci |
| PCI vendor/device IDs | WMI PNPDeviceID / `lspci -nn` |
| PCI bus ID | `nvidia-smi --query-gpu` |
| Driver version | `nvidia-smi --query-gpu=driver_version` |
| VRAM total/used/free (MB) | `nvidia-smi --query-gpu=memory.*` |
| GPU temperature (°C) | `nvidia-smi --query-gpu=temperature.gpu` |
| Power draw | `nvidia-smi --query-gpu=power.draw` |
| CUDA version (from driver) | Parsed from `nvidia-smi` header |
| Raw `nvidia-smi` table | `nvidia-smi` with the `Processes:` section removed before storage |
| WDDM version | Windows registry `HKLM:\SOFTWARE\Microsoft\DirectX` |
| iGPU detection (Intel/AMD) | WMI `Win32_VideoController` / lspci vendor IDs |

### Thermal & PCIe (all platforms)

| Data Point | Source | Notes |
|------------|--------|-------|
| Current/max clocks, power draw/limit, fan speed, utilization | `nvidia-smi --query-gpu=...` | Fan reported as unsupported on passively cooled cards |
| Active throttle reasons | `nvidia-smi --query-gpu=clocks_event_reasons.*` | `gpu_idle` is not treated as a slowdown; only thermal, power, and HW slowdown reasons are |
| PCIe current/max generation and width, power state | `nvidia-smi --query-gpu=pcie.link.*,pstate` | A Gen1 link at low utilization is reported as expected idle power-saving, not a downshift |

### Windows-Specific Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| HAGS state | Registry `HwSchMode` | 2=Enabled, 1=Disabled, absent="Not set" |
| Game Mode state | Registry `AutoGameModeEnabled` | absent="Not set" |
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
| DNS resolution time | In-process `net.Resolver` lookup of `google.com` | Measures the lookup only, not process start-up |

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
| GPU detection and driver basics | No NVIDIA GPU, hybrid iGPU + dGPU, driver version missing, `nvidia-smi` not in PATH |
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

The authoritative list is `internal/analyzer/analyzer.go`; `knowledge/rules.json` mirrors the ids for the experimental Rust companion.

### Report Summaries

Every report automatically generates:
- **Top 5 Issues**: Highest-severity findings (CRIT and WARN only)
- **Top 5 Next Steps**: Deduplicated actionable steps from top findings
- **Summary Block**: 4-6 line pasteable summary with OS, GPU, driver, CUDA, and finding counts

---

## Output Formats

### report.txt (always generated)

Human-readable, forum-pasteable, 72-character-wide formatting. Sections:
1. Header (version, disclaimer, timestamp, mode, platform, runtime, redaction status)
2. Summary block (designed for copy-paste into support threads)
3. System info table
4. GPU inventory (per-GPU detail, thermal and PCIe state)
5. Platform-specific details (Windows/Linux/WSL/AI/Network — whichever applies)
6. Findings with full evidence, explanation, and next steps
7. Top Issues summary
8. Recommended Next Steps
9. Collector Notes (commands that failed or timed out)
10. Privacy & Data footer: "This report was generated locally. No diagnostic data was transmitted." followed, only when probes ran, by "Network probes were run at your request (ICMP ping and traceroute to 1.1.1.1, DNS lookup of google.com)." and then "The run command did not modify your system. Changes are made only by 'nvcheckup fix' after explicit confirmation."

### report.json (with `--json`)

Complete structured output. Schema (version `"1"`):
```
{
  "metadata": { tool_version, timestamp, mode, runtime_seconds, redaction_enabled, platform,
                schema_version, network_probes },
  "system": { os_name, os_version, os_build, kernel_version, architecture, boot_mode,
               secure_boot, cpu_model, ram_total_mb, storage_free_mb, uptime, timezone },
  "gpus": [{ index, name, vendor, pci_vendor_id, pci_device_id, pci_bus_id,
              driver_version, wddm_version, vram_total_mb, vram_free_mb, vram_used_mb,
              temperature_c, power_draw, is_nvidia,
              thermal: { temperature_c, thermal_throttle, power_state, current_clock_mhz,
                         max_clock_mhz, power_limit_w, power_draw_w, fan_speed_pct,
                         fan_supported, slowdown_active, slowdown_reason, throttle_reasons,
                         utilization_pct },
              pcie: { current_speed, max_speed, current_width, max_width, downshifted,
                      power_state, utilization_pct, idle_likely } }],
  "driver": { version, cuda_version, nvidia_smi_path, nvidia_smi_output, source },
  "windows": { hags_enabled, game_mode, power_plan, monitors, driver_reset_events,
                nvlddmkm_errors, whea_errors, recent_kbs, nvidia_app_version,
                gfe_version, overlay_software, dxdiag_summary },
  "displays": [{ name, resolution, refresh_hz, connection, primary }],
  "linux": { distro, distro_version, package_manager, nvidia_packages, loaded_modules,
              dkms_status, dkms_errors, secure_boot_state, mok_status, session_type,
              prime_status, dev_nvidia_nodes, libcuda_path, container_runtime,
              nv_container_toolkit, xid_errors, journal_snippets, dmesg_snippets },
  "wsl": { is_wsl, wsl_version, distro, kernel_version, dev_dxg_exists, nvidia_smi_ok },
  "ai": { cuda_driver_version, cuda_toolkit_version, nvcc_path, cudnn_version,
           python_versions, conda_present, pytorch_info, tensorflow_info, key_packages },
  "network": { interface_name, interface_type, wifi_band, wifi_signal_dbm, latency_ms,
                jitter_ms, packet_loss_pct, dns_time_ms, hops },
  "findings": [{ id, severity, title, evidence, why_it_matters, next_steps, references,
                  category, confidence, remediation }],
  "collector_errors": [{ collector, error, fatal }],
  "top_issues": [],
  "next_steps": [],
  "summary_block": ""
}
```

`nvidia_smi_output` contains the GPU table only; the `Processes:` section is removed before storage. `network` is present only when probes ran. `displays` is populated on Windows.

### report.md (with `--md`)

GitHub/Reddit-optimized markdown:
- System and GPU info in tables
- Findings in a summary table + expandable `<details>` blocks per finding
- Code block for the summary (paste-ready)
- Same privacy footer as `report.txt`
- Suitable for issue templates and forum posts

### bundle.zip (with `--zip`)

Timestamped zip archive (`nvcheckup-bundle-YYYYMMDD-HHMMSS.zip`) containing all generated report files. If `--include-logs` is set, extended log snippets are included in the report files inside the bundle.

---

## Privacy & Redaction

### Guarantees

| Property | Status |
|----------|--------|
| Telemetry | None. Zero analytics, tracking, or phone-home. |
| Network calls | None unless you pass `--network` to `run` or use `network-test`. Then: ICMP ping and traceroute to `1.1.1.1` and a DNS lookup of `google.com`, performed locally. No data is uploaded. |
| System modification | `run`, `snapshot`, `doctor`, and `self-test` never modify anything. `fix` is opt-in, confirmed interactively, journaled, and undoable with `undo`. |
| Background services | None. No daemons, scheduled tasks, or auto-updates. |
| PII redaction | ON by default for `run` and `snapshot`. Disable with `--no-redact`. |

### What Is Never Collected

Passwords, tokens, API keys, browser data, SSH keys, clipboard contents, process lists or command lines, private documents, or anything outside the NVIDIA diagnostic scope. The `nvidia-smi` process list is stripped before the table is stored.

### Redaction Patterns (default ON)

| Pattern | Replacement |
|---------|-------------|
| Username in file paths (`C:\Users\name\...`) | `C:\Users\<user>\...` |
| Username standalone references | `<user>` |
| Machine hostname | `<host>` |
| Home directory full path | `<home>` |
| Public IPv4 addresses | `<public-ip-redacted>` |
| Private/LAN IPv4 addresses | `<lan-ip>` |
| Email addresses | `<email-redacted>` |

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
├── rust/                          Experimental partial port; not built in CI, not shipped
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
| `internal/collector/common` | Pure parse functions against captured `nvidia-smi`, ping, traceroute, and network output |
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
- `ci.yml` — Tests, build, and self-test on push/PR (Windows + Ubuntu matrix, Go 1.22 + stable); gofmt, `go vet`, cross-GOOS vet, and a non-blocking `govulncheck` in the lint job
- `release.yml` — Verifies `CHANGELOG.md` mentions the tag, cross-compiles with the version injected from the tag, publishes SHA256 checksums and a GitHub Release. Windows binaries are unsigned; the release notes explain how to verify the checksum and pass SmartScreen.

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
