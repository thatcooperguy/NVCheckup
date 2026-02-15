# NVCheckup — Complete Product Description

> **Version 0.1.0** | MIT License | Written in Go 1.22
> Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.

---

## What NVCheckup Is

NVCheckup is a single-binary, cross-platform diagnostic CLI for NVIDIA GPU environments. It scans your system in read-only mode, identifies common failure patterns across gaming, AI/CUDA, and streaming workloads, and generates clean, privacy-safe reports with ranked findings and actionable next steps.

It is designed for three moments:
1. **"I'm stuck."** — You have a black screen, a CUDA error, or driver crashes and don't know where to start.
2. **"I need to file a bug report."** — You need a clean system summary to paste in a forum or GitHub issue.
3. **"What changed?"** — You updated a driver or kernel and need to compare before/after state.

NVCheckup never modifies your system. It never sends data anywhere. It never runs in the background.

---

## Platform Support

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Fully supported |
| Ubuntu / Debian | x86_64 | Fully supported |
| Fedora / RHEL / CentOS | x86_64 | Fully supported |
| Arch / Manjaro | x86_64 | Fully supported |
| Linux (general) | ARM64 (aarch64) | Supported |
| WSL2 (inside Linux guest) | x86_64 | Supported (GPU passthrough diagnostics) |

Build targets: `windows/amd64`, `linux/amd64`, `linux/arm64`. All produce static binaries under 3 MB with zero runtime dependencies.

---

## CLI Commands

### `nvcheckup run`

The primary command. Runs collectors, analyzes results, and generates reports.

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
| `--verbose` | off | Print detailed progress to console |
| `--timeout` | `30` | Per-command timeout in seconds |
| `--redact` | **on** | Redact usernames, hostnames, IPs from all output |
| `--no-redact` | off | Disable PII redaction |
| `--include-logs` | off | Include extended system logs (journalctl, dmesg) in bundle |
| `--no-admin` | off | Skip checks that benefit from elevated permissions |

### `nvcheckup snapshot`

Creates a timestamped JSON snapshot of system state. Filename: `nvcheckup-snapshot-YYYYMMDD-HHMMSS.json`.

```
nvcheckup snapshot [--out DIR] [--timeout SEC]
```

Snapshots capture: system info, GPU inventory, driver version, CUDA environment, and AI framework state. They do not include findings (those are computed at analysis time).

### `nvcheckup compare`

Diffs two snapshots and reports what changed. Useful for before/after driver updates, kernel updates, or framework installs.

```
nvcheckup compare <snapshotA.json> <snapshotB.json> [--out DIR] [--md]
```

Fields compared: OS version, kernel, driver version, CUDA version, GPU count, GPU names, GPU VRAM, CUDA toolkit, cuDNN, PyTorch version, PyTorch CUDA version, PyTorch CUDA availability (marked critical if changed).

### `nvcheckup doctor`

Interactive guided mode. Asks 5 questions to determine the most relevant diagnostic scope, then runs a targeted check.

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

Verifies the environment has the tools NVCheckup needs. No system modifications.

```
nvcheckup self-test
```

**Checks performed:**

| Check | All Platforms | Windows Only | Linux Only |
|-------|:---:|:---:|:---:|
| OS detection | x | | |
| Architecture (amd64/arm64) | x | | |
| nvidia-smi available and functional | x | | |
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
| `gaming` | GPU, driver, Windows event logs, overlays, power plan, HAGS | Black screens, crashes, stutter, driver resets |
| `ai` | GPU, driver, Linux modules, Secure Boot, CUDA stack, PyTorch, TensorFlow | `torch.cuda.is_available() == False`, CUDA errors |
| `streaming` | GPU, driver, Windows gaming checks, overlay detection | Recording/streaming software conflicts |
| `creator` | GPU, driver, Windows gaming checks, CUDA environment | Creative application readiness |
| `full` | Everything: all platform checks, AI/CUDA, WSL, VRAM analysis | When you don't know what's wrong |

---

## Data Collected

NVCheckup collects approximately 50 data points across 6 collector modules. Every collection is read-only — no writes, no registry edits, no kernel module changes, no package installs.

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
| WDDM version | Windows registry `HKLM:\SOFTWARE\Microsoft\DirectX` |
| iGPU detection (Intel/AMD) | WMI `Win32_VideoController` / lspci vendor IDs |

### Windows-Specific Collection

| Data Point | Source | Notes |
|------------|--------|-------|
| HAGS state | Registry `HwSchMode` | 2=Enabled, 1=Disabled |
| Game Mode state | Registry `AutoGameModeEnabled` | |
| Active power plan | WMI `Win32_PowerPlan` | |
| Monitor resolution/refresh | WMI `Win32_VideoController` | Per-adapter |
| Event ID 4101 (driver resets) | `Get-WinEvent` System log | Last 30 days, up to 50 events |
| nvlddmkm errors | `Get-WinEvent` by provider | Last 30 days, up to 50 events |
| WHEA hardware errors | `Get-WinEvent` WHEA-Logger | Last 30 days, up to 20 events |
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
| CUDA Toolkit version | `nvcc --version` / `/usr/local/cuda` symlink / `CUDA_PATH` env | |
| nvcc path | `which nvcc` / common locations | |
| cuDNN version | Parsed from `cudnn_version.h` (`CUDNN_MAJOR.MINOR.PATCHLEVEL`) | Checks 4 header locations |
| Python versions + paths | `python3 --version`, `python --version`, `py --version` | |
| Conda present | `CommandExists("conda")` | |
| **PyTorch probe** | Runs inline Python script | |
| — torch version | `torch.__version__` | |
| — torch CUDA version | `torch.version.cuda` | Empty = CPU-only build |
| — CUDA available | `torch.cuda.is_available()` | |
| — GPU device name | `torch.cuda.get_device_name(0)` | |
| **TensorFlow probe** | Runs inline Python script | Extra 10s timeout (slow import) |
| — tf version | `tf.__version__` | |
| — Physical GPUs | `tf.config.list_physical_devices('GPU')` | |
| Key pip packages | Inline Python `__import__` | torch, tensorflow, jax, onnxruntime, transformers, numpy, scipy |

---

## Findings Engine

The analyzer processes collected data through 31 diagnostic rules and produces ranked findings. Each finding includes:

- **Severity**: `CRIT` (blocker), `WARN` (likely contributor), `INFO` (context)
- **Title**: Clear, specific description
- **Evidence**: What was observed (with data)
- **Why it matters**: Plain-language impact explanation
- **Next steps**: Safe, reversible actions (never destructive)
- **Category**: driver, gpu, cuda, ai, overlay, performance, hardware, secureboot, wsl, updates

### All 31 Diagnostic Rules

**GPU Detection (2 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 1 | No NVIDIA GPU Detected | CRIT | Zero GPUs with IsNVIDIA=true |
| 2 | Hybrid GPU Configuration Detected | INFO | NVIDIA + Intel/AMD GPUs present |

**Driver Basics (2 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 3 | NVIDIA Driver Version Not Detected | CRIT | Driver version string empty |
| 4 | nvidia-smi Not Found in PATH | WARN | nvidia-smi binary not in PATH |

**Windows Gaming (6 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 5 | Display Driver Resets Detected (Event ID 4101) | WARN/CRIT | 1+ events (CRIT at 3+) in 30 days |
| 6 | nvlddmkm Driver Errors Detected | WARN/CRIT | 1+ events (CRIT at 5+) in 30 days |
| 7 | Hardware Errors (WHEA) Detected | WARN | Any WHEA events in 30 days |
| 8 | Power Plan Not Set to High Performance | INFO | Active plan lacks "high performance" |
| 9 | Hardware-Accelerated GPU Scheduling (HAGS) is Enabled | INFO | HAGS registry value = 2 |
| 10 | Recent Windows Updates Installed | INFO | KBs present AND driver resets present |

**Overlay Detection (2 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 11 | NVIDIA App / GeForce Experience Detected | INFO | App version found in registry |
| 12 | Overlay/Recording Software Detected | INFO | 1+ overlay apps found by name |

**Streaming (1 rule):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 13 | No NVIDIA GPU Available for Hardware Encoding | CRIT | No NVIDIA GPU in inventory |

**Linux Kernel Modules (5 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 14 | Nouveau Driver is Active (Instead of NVIDIA) | CRIT | nouveau module loaded |
| 15 | NVIDIA Kernel Module Not Loaded | CRIT | nvidia module not loaded AND no driver version |
| 16 | No /dev/nvidia* Device Nodes Found | WARN | nvidia loaded but zero dev nodes |
| 17 | libcuda.so Not Found | WARN | Library not found via ldconfig or common paths |
| 18 | DKMS Build Failure Detected | CRIT | DKMS status contains error keywords |

**Secure Boot (2 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 19 | Secure Boot Enabled — NVIDIA Module May Be Blocked | CRIT | Secure Boot on AND nvidia not loaded |
| 20 | Secure Boot Enabled — NVIDIA Module is Loading Successfully | INFO | Secure Boot on AND nvidia loaded |

**CUDA Stack (1 rule):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 21 | CUDA Toolkit / Driver Version Mismatch | WARN | Major version differs between toolkit and driver |

**PyTorch (4 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 22 | PyTorch Import Error | WARN | Import exception occurred |
| 23 | PyTorch Installed Without CUDA Support | WARN | CUDA unavailable AND torch.version.cuda empty |
| 24 | PyTorch CUDA Available but GPU Not Accessible | WARN | CUDA unavailable AND torch.version.cuda present |
| 25 | PyTorch CUDA is Working | INFO | torch.cuda.is_available() = true |

**TensorFlow (3 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 26 | TensorFlow Import Error | WARN | Import exception occurred |
| 27 | TensorFlow Cannot See GPU | WARN | Zero physical GPU devices |
| 28 | TensorFlow GPU is Working | INFO | 1+ physical GPU devices |

**WSL2 (2 rules):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 29 | WSL2 GPU Device (/dev/dxg) Not Found | CRIT | Inside WSL AND /dev/dxg missing |
| 30 | WSL2: /dev/dxg Exists but nvidia-smi Fails | WARN | /dev/dxg exists AND nvidia-smi fails |

**Hardware (1 rule):**
| # | Title | Severity | Trigger |
|---|-------|----------|---------|
| 31 | Low VRAM Detected | INFO | NVIDIA GPU with < 4096 MB VRAM |

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
4. GPU inventory (per-GPU detail)
5. Platform-specific details (Windows/Linux/WSL/AI — whichever applies)
6. Findings with full evidence, explanation, and next steps
7. Top Issues summary
8. Recommended Next Steps
9. Collector Notes (commands that failed or timed out)
10. Privacy & Data statement

### report.json (with `--json`)

Complete structured output. Schema:
```
{
  "metadata": { tool_version, timestamp, mode, runtime_seconds, redaction_enabled, platform },
  "system": { os_name, os_version, os_build, kernel_version, architecture, boot_mode,
               secure_boot, cpu_model, ram_total_mb, storage_free_mb, uptime, timezone },
  "gpus": [{ index, name, vendor, pci_vendor_id, pci_device_id, pci_bus_id,
              driver_version, wddm_version, vram_total_mb, vram_free_mb, vram_used_mb,
              temperature_c, power_draw, is_nvidia }],
  "driver": { version, cuda_version, nvidia_smi_path, nvidia_smi_output, source },
  "windows": { hags_enabled, game_mode, power_plan, monitors, driver_reset_events,
                nvlddmkm_errors, whea_errors, recent_kbs, nvidia_app_version,
                gfe_version, overlay_software, dxdiag_summary },
  "linux": { distro, distro_version, package_manager, nvidia_packages, loaded_modules,
              dkms_status, dkms_errors, secure_boot_state, mok_status, session_type,
              prime_status, dev_nvidia_nodes, libcuda_path, container_runtime,
              nv_container_toolkit, journal_snippets, dmesg_snippets },
  "wsl": { is_wsl, wsl_version, distro, kernel_version, dev_dxg_exists, nvidia_smi_ok },
  "ai": { cuda_driver_version, cuda_toolkit_version, nvcc_path, cudnn_version,
           python_versions, conda_present, pytorch_info, tensorflow_info, key_packages },
  "findings": [{ severity, title, evidence, why_it_matters, next_steps, references, category }],
  "collector_errors": [{ collector, error, fatal }],
  "top_issues": [],
  "next_steps": [],
  "summary_block": ""
}
```

### report.md (with `--md`)

GitHub/Reddit-optimized markdown:
- System and GPU info in tables
- Findings in a summary table + expandable `<details>` blocks per finding
- Code block for the summary (paste-ready)
- Suitable for issue templates and forum posts

### bundle.zip (with `--zip`)

Timestamped zip archive (`nvcheckup-bundle-YYYYMMDD-HHMMSS.zip`) containing all generated report files. If `--include-logs` is set, extended log snippets are included in the report files inside the bundle.

---

## Privacy & Redaction

### Guarantees

| Property | Status |
|----------|--------|
| Telemetry | None. Zero analytics, tracking, or phone-home. |
| Network calls | None. The binary never opens a socket. |
| System modification | None. Read-only at all times. |
| Background services | None. No daemons, scheduled tasks, or auto-updates. |
| PII redaction | ON by default. Disable with `--no-redact`. |

### What Is Never Collected

Passwords, tokens, API keys, browser data, SSH keys, clipboard contents, full process lists with command lines, private documents, email addresses, or anything outside the NVIDIA diagnostic scope.

### Redaction Patterns (with `--redact`, default ON)

| Pattern | Replacement |
|---------|-------------|
| Username in file paths (`C:\Users\name\...`) | `C:\Users\<user>\...` |
| Username standalone references | `<user>` |
| Machine hostname | `<host>` |
| Home directory full path | `<home>` |
| Public IPv4 addresses | `<public-ip-redacted>` |
| Private/LAN IPv4 addresses | `<lan-ip>` |
| Email addresses | `<email-redacted>` |
| WiFi SSID names | `SSID: <redacted>` |

Redaction is applied to: hostname, summary block, GPU bus IDs, nvidia-smi output, nvidia-smi path, all finding evidence strings, all collector error messages, Linux libcuda path, Linux journal/dmesg snippets, AI nvcc path, and all Python environment paths.

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
nvcheckup (2.7-2.8 MB static binary)
├── cmd/nvcheckup/main.go          CLI entry point, subcommand dispatch
├── internal/
│   ├── core/                      Orchestration pipeline
│   │   ├── runner.go              5-phase pipeline: collect → analyze → redact → report → package
│   │   ├── runner_windows.go      Platform dispatch (build tag: windows)
│   │   ├── runner_linux.go        Platform dispatch (build tag: linux)
│   │   └── runner_other.go        Fallback for unsupported platforms
│   ├── collector/
│   │   ├── common/system.go       Cross-platform system info
│   │   ├── common/gpu.go          nvidia-smi + WMI/lspci GPU enumeration
│   │   ├── windows/windows.go     Event logs, HAGS, overlays, updates (build tag: windows)
│   │   ├── linux/linux.go         Modules, DKMS, Secure Boot, PRIME (build tag: linux)
│   │   ├── wsl/wsl.go             WSL2 detection and /dev/dxg checks
│   │   └── ai/ai.go               CUDA, Python, PyTorch, TensorFlow probes
│   ├── analyzer/analyzer.go       31-rule findings engine
│   ├── redact/redact.go           7-pattern PII redaction engine
│   ├── report/
│   │   ├── text.go                Human-readable report generator
│   │   ├── json.go                Structured JSON output
│   │   └── markdown.go            GitHub/Reddit markdown output
│   ├── bundle/zip.go              Zip archive packaging
│   ├── snapshot/snapshot.go       Create + compare system snapshots
│   ├── doctor/doctor.go           Interactive guided diagnostic mode
│   └── selftest/selftest.go       Environment verification
├── pkg/types/types.go             All shared data structures
└── docs/index.html                Landing page (GitHub Pages)
```

**Design principles:**
- Every external command runs with a configurable timeout (default 30s)
- Every collector catches its own errors and continues — one failed command never crashes the whole run
- Platform-specific code uses Go build tags (`//go:build windows` / `//go:build linux`)
- No external Go dependencies — standard library only
- Cross-compilation produces static binaries for all three targets

---

## Testing

**35 unit tests** across 5 test files:

| Package | Tests | Coverage |
|---------|-------|----------|
| `internal/util` | 9 | Command execution, timeouts, parsing, helpers |
| `internal/redact` | 5 | IP classification, path redaction, disabled passthrough |
| `internal/analyzer` | 16 | All major finding rules, sorting, summary generation |
| `internal/report` | 7 | Text structure, JSON output, markdown structure, helpers |
| `pkg/types` | 4 | Constants, defaults, exit codes |

Run with: `go test ./... -v -race`

---

## Build & Distribution

```bash
# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets (static, stripped)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup
```

**Binary sizes:** ~2.7-2.8 MB per platform (stripped, static, zero dependencies).

**CI/CD:** GitHub Actions workflows for:
- `ci.yml` — Tests + lint on push/PR (Windows + Ubuntu matrix, Go 1.21 + 1.22)
- `release.yml` — Cross-compile + SHA256 checksums + GitHub Release on tag push

---

## What NVCheckup Does Not Do

- Does not modify drivers, registry, kernel modules, or system configuration
- Does not install packages or dependencies
- Does not delete files or clean caches
- Does not send data anywhere (no telemetry, no network calls)
- Does not run in the background or create scheduled tasks
- Does not require admin/root (some checks benefit from it, but the tool runs without it)
- Does not scan browser data, clipboard, SSH keys, or documents
- Does not provide investment, legal, or warranty advice
- Is not affiliated with NVIDIA Corporation

---

*NVCheckup exists to make diagnosing NVIDIA ecosystem issues faster, safer, and less frustrating.*
