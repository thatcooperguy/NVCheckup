<div align="center">

**Cross-platform NVIDIA diagnostics. For gamers, AI developers, and creators.**

[![CI](https://github.com/nicholasgasior/nvcheckup/actions/workflows/ci.yml/badge.svg)](https://github.com/nicholasgasior/nvcheckup/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20ARM64-76b900.svg)](#supported-platforms)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)](https://go.dev)

*Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.*

---

</div>

## The Problem

You are staring at a black screen. Or `torch.cuda.is_available()` returns `False` and you have burned two hours debugging. Or your stream is dropping frames and NVENC has vanished. You open Event Viewer, scroll through cryptic logs, google error codes, and paste fragments into forums hoping someone recognizes the pattern.

**NVCheckup turns "I'm stuck" into "here's what's wrong and what to try next."**

It is a single-binary diagnostic tool that scans your NVIDIA GPU environment, identifies common failure patterns, and produces a clean, redacted report with actionable next steps. It runs on Windows and Linux, on x86_64 and ARM64, and it never touches your drivers, registry, or system configuration.

---

## What It Does

```
nvcheckup run --mode full --zip
```

In under 30 seconds, NVCheckup:

- **Scans** your GPU, driver, CUDA toolkit, and system configuration
- **Detects** driver crashes (Event ID 4101, nvlddmkm), module loading failures, version mismatches
- **Identifies** overlay conflicts, Secure Boot blocks, nouveau interference, DKMS failures
- **Probes** PyTorch, TensorFlow, and CUDA framework configurations
- **Generates** a redacted, forum-ready report with ranked findings and safe next steps
- **Packages** everything into a zip bundle you can attach to a bug report

---

## Quick Start

### Windows

```powershell
# Download from GitHub Releases, then:
nvcheckup.exe run --mode full --zip
```

### Linux

```bash
# Download the binary for your architecture
chmod +x nvcheckup
./nvcheckup run --mode full --zip
```

### What You Get

```
────────────────────────────────────────────────────────────────────────
  NVCheckup v0.1.0 — NVIDIA Diagnostic Report
  Unofficial community tool, not affiliated with NVIDIA Corporation.
────────────────────────────────────────────────────────────────────────
  Generated: 2025-01-15 14:32:10 UTC
  Mode:      full
  Platform:  windows
────────────────────────────────────────────────────────────────────────

== SUMMARY (paste this in support threads) ==

NVCheckup v0.1.0 | 2025-01-15 14:32:10 UTC
OS: Windows 11 23H2 | Arch: amd64
GPU: NVIDIA GeForce RTX 4070 | Driver: 566.36 | VRAM: 12288 MB
CUDA (driver): 12.7
Findings: 1 CRITICAL, 2 WARNING, 3 total

Top Issues:
  1. [CRIT] Display Driver Resets Detected (Event ID 4101)
  2. [WARN] Hardware-Accelerated GPU Scheduling (HAGS) is Enabled
  3. [WARN] Overlay/Recording Software Detected

Recommended Next Steps:
  1. Update to the latest NVIDIA driver (clean install recommended).
  2. Check GPU temperatures — overheating can trigger driver resets.
  3. Try disabling HAGS in Settings > Display > Graphics.
```

---

## Who This Is For

### Gamers

Your display driver stopped responding. Black screen mid-game. Event Viewer shows `Event ID 4101` and you have no idea what that means.

NVCheckup scans your event logs, checks your driver version, identifies overlay conflicts, flags HAGS and power plan settings, and tells you what to try next — in plain language.

**Common issues detected:**
- Display driver stopped responding / recovered (nvlddmkm resets)
- Microstutter and frame pacing anomalies
- Overlay software conflicts (Xbox Game Bar, Discord, RTSS)
- HAGS and power plan misconfigurations
- Windows Update regression correlation

### AI / CUDA Developers

`torch.cuda.is_available()` returns `False`. nvidia-smi works but PyTorch cannot see your GPU. You updated your kernel and now the NVIDIA module will not load.

NVCheckup checks your entire CUDA stack — driver, toolkit, cuDNN, Python environment, framework builds — and tells you exactly where the chain is broken.

**Common issues detected:**
- CPU-only PyTorch wheel installed (no CUDA compiled in)
- CUDA driver/toolkit major version mismatch
- NVIDIA kernel module not loaded (Linux)
- Secure Boot blocking unsigned modules
- DKMS build failure after kernel update
- `LD_LIBRARY_PATH` / `PATH` missing CUDA libraries
- WSL2 `/dev/dxg` not present

### Streamers and Creators

NVENC is unavailable in OBS. Dropped frames during recording. Encode errors you cannot explain.

NVCheckup verifies your GPU supports hardware encoding, checks driver health, and identifies configuration problems.

### IT / Power Users

You want scriptable, machine-readable output. You want to diff system state before and after a driver update. You want a support bundle that does not leak your username.

NVCheckup gives you `--json` output, `snapshot` + `compare` commands, and automatic PII redaction.

---

## Diagnostic Modes

| Mode | Focus | Use When |
|------|-------|----------|
| `gaming` | Driver stability, overlays, event logs, power settings | Black screens, crashes, stutter |
| `ai` | CUDA stack, PyTorch/TF probes, kernel modules | `torch.cuda.is_available() == False` |
| `streaming` | NVENC availability, capture/encode checks | OBS issues, dropped frames |
| `creator` | DCC readiness, CUDA + driver health | Professional application issues |
| `full` | Everything | When you are not sure what is wrong |

```bash
nvcheckup run --mode gaming --zip      # Gamer troubleshooting
nvcheckup run --mode ai --json --md    # AI/CUDA deep check
nvcheckup run --mode full --zip --json # The works
```

---

## Example Findings

### CRIT — Repeated Display Driver Resets Detected (Event ID 4101)

```
  [CRIT] #1: Display Driver Resets Detected (Event ID 4101)
    Evidence:     7 driver reset event(s) in the last 30 days. Most recent: 2025-01-14 22:15.
    Why:          Event ID 4101 indicates the display driver stopped responding and was
                  recovered by Windows. Frequent occurrences cause black screens, freezes,
                  and application crashes.
    Next Steps:
      • Update to the latest NVIDIA driver (clean install recommended).
      • Check GPU temperatures — overheating can trigger driver resets.
      • If overclocked, revert GPU clocks to stock settings.
      • Test with Hardware-Accelerated GPU Scheduling (HAGS) toggled off.
```

### WARN — PyTorch Installed Without CUDA Support

```
  [WARN] #2: PyTorch Installed Without CUDA Support
    Evidence:     PyTorch 2.2.0 is installed but torch.version.cuda is empty — CPU-only build.
    Why:          A CPU-only PyTorch wheel was installed. torch.cuda.is_available() returns
                  False because the CUDA runtime is not compiled in.
    Next Steps:
      • Uninstall: pip uninstall torch torchvision torchaudio
      • Reinstall with CUDA: pip install torch --index-url https://download.pytorch.org/whl/cu121
      • Select the correct CUDA version matching your driver.
```

### WARN — Secure Boot Enabled, NVIDIA Module May Be Blocked

```
  [WARN] #3: Secure Boot Enabled — NVIDIA Module May Be Blocked
    Evidence:     Secure Boot is enabled and the NVIDIA kernel module is not loaded.
    Why:          Secure Boot requires kernel modules to be signed with an enrolled key.
                  Unsigned NVIDIA modules will be rejected by the kernel.
    Next Steps:
      • Option A (Recommended): Sign the NVIDIA module and enroll via MOK.
      • Option B: Disable Secure Boot in BIOS/UEFI (reduces system security).
```

---

## Privacy and Safety

NVCheckup is built on a simple principle: **your data stays on your machine.**

| Guarantee | Detail |
|-----------|--------|
| No telemetry | Zero analytics, zero tracking, zero phone-home |
| No network calls | The binary never opens a socket |
| Read-only | Never modifies drivers, registry, kernel modules, or configs |
| No background services | No daemons, no scheduled tasks, no auto-updates |
| PII redaction ON by default | Usernames, hostnames, IPs automatically scrubbed |

### What Is Collected (Read-Only)

- OS version, kernel version, CPU model, RAM total, disk free space
- GPU model, driver version, VRAM, temperature, PCI bus ID
- NVIDIA kernel module status and `/dev/nvidia*` nodes (Linux)
- Secure Boot state and DKMS build status (Linux)
- Event logs for driver crashes — Event ID 4101 and nvlddmkm (Windows, last 30 days)
- Windows Update history (last 60 days)
- Installed overlay software (by name only — no process scanning)
- Python versions, PyTorch/TensorFlow/JAX versions and GPU visibility
- CUDA toolkit, cuDNN, and nvidia-container-toolkit versions

### What Is Never Collected

Passwords, tokens, API keys, browser data, SSH keys, clipboard contents, full process lists with command lines, private documents, email addresses, or anything outside the NVIDIA diagnostic scope.

### Redaction

With `--redact` (default ON):
- `C:\Users\yourname\...` becomes `C:\Users\<user>\...`
- Machine hostname becomes `<host>`
- Public IPs become `<public-ip-redacted>`
- LAN IPs become `<lan-ip>`
- Email addresses become `<email-redacted>`

Use `--no-redact` only when you specifically need raw output.

---

## Command Reference

### `nvcheckup run`

Run diagnostics and generate a report.

```
nvcheckup run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `full` | `gaming`, `ai`, `creator`, `streaming`, `full` |
| `--out` | `.` | Output directory |
| `--zip` | off | Create a zip bundle |
| `--json` | off | Generate structured `report.json` |
| `--md` | off | Generate GitHub/Reddit-ready `report.md` |
| `--verbose` | off | Verbose console output |
| `--timeout` | `30` | Per-command timeout in seconds |
| `--redact` | **on** | Redact PII from all output |
| `--no-redact` | off | Disable PII redaction |
| `--include-logs` | off | Include extended system logs in bundle |
| `--no-admin` | off | Skip checks requiring elevated permissions |

### `nvcheckup snapshot`

Create a timestamped JSON snapshot for later comparison.

```
nvcheckup snapshot [--out DIR]
```

### `nvcheckup compare`

Diff two snapshots. Useful for before/after driver updates.

```
nvcheckup compare <before.json> <after.json> [--out DIR] [--md]
```

### `nvcheckup doctor`

Interactive guided mode. Asks 5 questions, then runs targeted checks.

```
nvcheckup doctor
```

### `nvcheckup self-test`

Verifies your environment has the tools NVCheckup needs. No modifications.

```
nvcheckup self-test
```

---

## Output Formats

| File | Format | When Generated |
|------|--------|----------------|
| `report.txt` | Human-readable, forum-pasteable | Always |
| `report.json` | Structured, machine-parseable | `--json` |
| `report.md` | GitHub/Reddit markdown with tables | `--md` |
| `bundle.zip` | Report + logs archive | `--zip` |

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | No significant issues detected |
| `1` | Warnings detected (non-critical) |
| `2` | Critical issues detected |
| `3` | Internal error |

---

## Supported Platforms

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Fully supported |
| Ubuntu / Debian | x86_64 | Fully supported |
| Fedora / RHEL / CentOS | x86_64 | Fully supported |
| Arch / Manjaro | x86_64 | Fully supported |
| Linux (general) | ARM64 (aarch64) | Supported |
| WSL2 | x86_64 | Limited (GPU passthrough diagnostics) |

NVCheckup is designed for systems with NVIDIA GPUs. It will run on systems without NVIDIA hardware but most diagnostics will report "not detected."

---

## Architecture

```
nvcheckup
├── cmd/nvcheckup/          CLI entry point
├── internal/
│   ├── core/               Orchestration pipeline
│   ├── collector/
│   │   ├── common/         Cross-platform (system, GPU, nvidia-smi)
│   │   ├── windows/        WMI, event logs, overlays, updates
│   │   ├── linux/          Kernel modules, DKMS, Secure Boot, PRIME
│   │   ├── wsl/            WSL2 detection and /dev/dxg checks
│   │   └── ai/             CUDA, PyTorch, TensorFlow, Python envs
│   ├── analyzer/           Findings engine (rules → evidence → next steps)
│   ├── redact/             PII redaction engine
│   ├── report/             Output generators (txt, json, md)
│   ├── bundle/             Zip packaging
│   ├── snapshot/           Snapshot create/compare
│   ├── doctor/             Interactive guided mode
│   └── selftest/           Environment verification
└── pkg/types/              Shared data structures
```

Every collector returns structured data. Every analyzer produces findings with severity, evidence, and safe next steps. If a collector fails, NVCheckup logs the error and continues — it never crashes the whole run because one command is missing.

---

## Building from Source

```bash
git clone https://github.com/nicholasgasior/nvcheckup.git
cd nvcheckup

# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup

# Run tests
go test ./... -v -race
```

---

## FAQ

**Is this official NVIDIA software?**
No. NVCheckup is an independent, community-maintained, open-source tool. It is not affiliated with, endorsed by, or supported by NVIDIA Corporation. It uses only public OS interfaces and publicly available command outputs.

**Does this replace DDU?**
No. NVCheckup is diagnostic-only — it tells you what is wrong. DDU (Display Driver Uninstaller) is a separate tool that removes drivers. If NVCheckup suggests a clean reinstall, DDU is one way to do that.

**Will NVCheckup fix anything automatically?**
No. NVCheckup never modifies your system. It identifies issues and suggests safe, reversible steps you can take manually. The report includes "Suggested Manual Steps" where appropriate.

**Is it safe to share the report publicly?**
Yes, with default settings. PII redaction is on by default — usernames, hostnames, and IP addresses are automatically replaced with placeholders. Review the report before sharing if you have specific concerns.

**Does it work without admin/root?**
Mostly. Some checks (Windows event logs, Linux dmesg) benefit from elevated permissions. Use `--no-admin` to skip those. NVCheckup reports what it could not collect and why.

**Why does `torch.cuda.is_available()` return False?**
The most common causes, in order:
1. CPU-only PyTorch wheel installed (check `torch.version.cuda` — if empty, this is it)
2. NVIDIA driver not installed or not loading
3. CUDA version mismatch between PyTorch build and driver
4. Wrong Python environment (conda/venv confusion)
5. On Linux: Secure Boot blocking the NVIDIA kernel module

Run `nvcheckup run --mode ai` for automated diagnosis.

**How do I handle Secure Boot + NVIDIA on Linux?**
Two options:
1. **(Recommended)** Sign the NVIDIA module with a MOK key and enroll it. This preserves Secure Boot security while allowing the driver to load.
2. Disable Secure Boot in BIOS/UEFI. This works but reduces system security.

NVCheckup will detect this situation and provide specific guidance.

---

## Contributing

Contributions are welcome. This project values clarity, safety, and cross-platform reliability.

- **Bug reports** — Open an issue. Attach your redacted NVCheckup report if relevant.
- **Feature requests** — Open an issue describing the use case and which persona it serves.
- **Pull requests** — Fork, branch, test, submit. Include unit tests for new collectors or analyzer rules.
- **Platform testing** — Testing on less common Linux distributions, ARM64 hardware, or edge cases is especially valuable.

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community guidelines.

---

## License

[MIT License](LICENSE). Community maintained.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting guidelines.

---

<div align="center">

**NVCheckup exists to make diagnosing NVIDIA ecosystem issues faster, safer, and less frustrating.**

*Built by the community. For the community.*

</div>
