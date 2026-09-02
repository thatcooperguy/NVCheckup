<div align="center">

**Cross-platform NVIDIA diagnostics. For gamers, AI developers, and creators.**

[![CI](https://github.com/thatcooperguy/NVCheckup/actions/workflows/ci.yml/badge.svg)](https://github.com/thatcooperguy/NVCheckup/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20ARM64-76b900.svg)](#supported-platforms)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

*Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.*

---

</div>

## The Problem

You are staring at a black screen. Or `torch.cuda.is_available()` returns `False` and you have burned two hours debugging. Or your driver keeps crashing and Event Viewer shows errors you don't understand. You scroll through cryptic logs, google error codes, and paste fragments into forums hoping someone recognizes the pattern.

**NVCheckup turns "I'm stuck" into "here's what's wrong and what to try next."**

It is a single-binary diagnostic tool that scans your NVIDIA GPU environment, identifies common failure patterns, and produces a clean, redacted report with actionable next steps. It runs on Windows and Linux, on x86_64 and ARM64. Diagnostics are read-only. The optional `fix` command can apply a small set of well-understood settings changes, but only after you confirm each one, and every change is journaled and undoable.

---

## What It Does

```
nvcheckup run --mode full --zip
```

Typically in 20-60 seconds (add 30-60 s with `--network`), NVCheckup:

- **Scans** your GPU, driver, CUDA toolkit, PCIe link, thermal state, and system configuration
- **Detects** driver crashes (Event ID 4101, nvlddmkm), module loading failures, version mismatches
- **Identifies** overlay conflicts, Secure Boot blocks, nouveau interference, DKMS failures, thermal throttling
- **Probes** PyTorch, TensorFlow, and CUDA framework configurations
- **Generates** a redacted, forum-ready report with ranked findings and safe next steps
- **Packages** everything into a zip bundle you can attach to a bug report

---

## Quick Start

### Option A: Download a release binary

Grab the binary for your platform from [GitHub Releases](https://github.com/thatcooperguy/NVCheckup/releases). Each file ships with a `.sha256` checksum.

```powershell
# Windows (the binary is unsigned; if SmartScreen appears, choose "More info" -> "Run anyway")
nvcheckup-windows-amd64.exe run --mode full --zip
```

```bash
# Linux
chmod +x nvcheckup-linux-amd64
./nvcheckup-linux-amd64 run --mode full --zip
```

### Option B: Install or build with Go

```bash
# Install the latest release straight into $GOPATH/bin
go install github.com/thatcooperguy/nvcheckup/cmd/nvcheckup@latest

# ...or build from a clone (see "Building from Source" below)
git clone https://github.com/thatcooperguy/NVCheckup.git
cd NVCheckup
go build -o nvcheckup ./cmd/nvcheckup
```

### What You Get

The top of `report.txt` is a summary block you can paste into a support thread as-is:

```
────────────────────────────────────────────────────────────────────────
  NVCheckup v0.2.1 — NVIDIA Diagnostic Report
  NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.
────────────────────────────────────────────────────────────────────────
  Generated: 2026-09-01 14:32:10 UTC
  Mode:      full
  Platform:  windows
  Runtime:   48.9s
  Redaction: ENABLED (PII removed)
────────────────────────────────────────────────────────────────────────

== SUMMARY (paste this in support threads) ==

NVCheckup v0.2.1 | 2026-09-01 14:32:10
OS: Microsoft Windows 11 Pro 10.0.26100 | Arch: amd64
GPU: NVIDIA GeForce RTX 4070 | Driver: 591.86 | VRAM: 12282 MB
CUDA (driver): 13.1 | CUDA Toolkit: 12.8
PyTorch: 2.5.1+cu118 (CUDA available)
Temp: 42°C | P-State: P8 | Util: 0%
PCIe: Gen1 x16 (idle, max Gen4)
Findings: 1 CRITICAL, 1 WARNING, 6 total | 2 auto-fixable
Top: Display Driver Resets Detected (Event ID 4101); nvlddmkm Driver ...
```

The rest of the report holds the system, GPU, platform and AI/CUDA sections, then every finding with its evidence, why it matters, next steps and (when one exists) the `nvcheckup fix --id ...` command that addresses it. Complete examples: [`examples/sample-report-gaming.txt`](examples/sample-report-gaming.txt) and [`examples/sample-report-ai-linux.txt`](examples/sample-report-ai-linux.txt).

---

## Who This Is For

### Gamers

Your display driver stopped responding. Black screen mid-game. Event Viewer shows `Event ID 4101` and you have no idea what that means.

NVCheckup scans your event logs, checks your driver version, identifies overlay conflicts, flags HAGS and power plan settings, checks whether the GPU is thermally throttling or stuck on a narrow PCIe link, and tells you what to try next — in plain language.

**Common issues detected:**
- Display driver stopped responding / recovered (nvlddmkm resets)
- Thermal throttling and GPUs running hot
- PCIe link running below its rated speed or width under load
- Overlay software conflicts (Xbox Game Bar, Discord, RTSS)
- HAGS and power plan misconfigurations
- Windows Update regression correlation

### AI / CUDA Developers

`torch.cuda.is_available()` returns `False`. nvidia-smi works but PyTorch cannot see your GPU. You updated your kernel and now the NVIDIA module will not load.

NVCheckup checks your entire CUDA stack — driver, toolkit, cuDNN, Python environment, framework builds — and tells you exactly where the chain is broken.

**Common issues detected:**
- CPU-only PyTorch wheel installed (no CUDA compiled in)
- CUDA toolkit newer than what the driver supports
- NVIDIA kernel module not loaded (Linux)
- Secure Boot blocking unsigned modules
- DKMS build failure after kernel update
- `LD_LIBRARY_PATH` / `PATH` missing CUDA libraries
- WSL2 `/dev/dxg` not present

### Streamers and Creators

Dropped frames during recording. Overlay conflicts causing stutter. Driver issues affecting your creative workflow.

NVCheckup checks your driver health, identifies overlay conflicts, and flags configuration problems that affect recording and creative applications. With `--network` it can also measure latency, jitter, and packet loss to a public resolver.

### IT / Power Users

You want scriptable, machine-readable output. You want to diff system state before and after a driver update. You want a support bundle that does not leak your username.

NVCheckup gives you `--json` output with stable finding ids, `snapshot` + `compare` commands, and automatic PII redaction.

---

## Diagnostic Modes

| Mode | Focus | Use When |
|------|-------|----------|
| `gaming` | Driver stability, overlays, event logs, power settings, thermal/PCIe | Black screens, crashes, stutter |
| `ai` | CUDA stack, PyTorch/TF probes, kernel modules | `torch.cuda.is_available() == False` |
| `streaming` | Driver health, overlay detection, capture checks | Recording/streaming issues, dropped frames |
| `creator` | Driver health, CUDA environment, creative readiness | Creative application issues |
| `full` | Everything | When you are not sure what is wrong |

```bash
nvcheckup run --mode gaming --zip            # Gamer troubleshooting
nvcheckup run --mode ai --json --md          # AI/CUDA deep check
nvcheckup run --mode full --zip --json       # The works
nvcheckup run --mode gaming --network        # ...plus opt-in ping/traceroute/DNS probes
```

---

## Example Findings

### CRIT — Repeated Display Driver Resets Detected (Event ID 4101)

```
  [CRIT] #1: Display Driver Resets Detected (Event ID 4101) (driver-resets-4101)
    Evidence:     7 driver reset event(s) in the last 30 days. Most recent: 2026-08-30 22:15.
    Why:          Event ID 4101 indicates the display driver stopped responding and was recovered by Windows. Frequent occurrences cause black screens, freezes, and application crashes.
    Next Steps:
      • Update to the latest NVIDIA driver (clean install recommended).
      • Check GPU temperatures — overheating can trigger driver resets.
      • If overclocked, revert GPU clocks to stock settings.
      • Test with Hardware-Accelerated GPU Scheduling (HAGS) toggled off.
      • If recent Windows Update coincides with issues, consider testing a rollback (understand security implications first).
```

### WARN — PyTorch Installed Without CUDA Support

```
  [WARN] #2: PyTorch Installed Without CUDA Support (pytorch-cpu-only)
    Evidence:     PyTorch 2.8.0+cpu is installed but torch.version.cuda is empty — this is a CPU-only build.
    Why:          A CPU-only PyTorch wheel was installed. torch.cuda.is_available() returns False because the CUDA runtime is not compiled in.
    Next Steps:
      • Uninstall the current PyTorch: pip uninstall torch torchvision torchaudio
      • Reinstall with CUDA support from https://pytorch.org/get-started/locally/
      • Make sure to select a CUDA version no newer than your driver's (13.1).
```

### INFO — PCIe Link Power-Saving at Idle (expected)

```
  [INFO] #3: PCIe Link Power-Saving at Idle (expected) (pcie-idle-power-saving)
    Evidence:     Current: Gen1 x16. Maximum: Gen4 x16. P-state: P8. GPU utilization: 0%.
    Why:          Modern GPUs drop the PCIe link to Gen1 when idle to save power and raise it again under load. This reading was taken at idle, so it does not indicate a problem.
    Next Steps:
      • Re-run under GPU load to verify the link reaches Gen4.
```

### INFO with a fix attached — Power Plan Not Set to High Performance

```
  [INFO] #4: Power Plan Not Set to High Performance (power-plan-suboptimal)
    Evidence:     Active power plan: Balanced.
    Why:          Balanced or Power Saver plans may throttle CPU/GPU performance. For gaming or CUDA workloads, High Performance is generally recommended.
    Next Steps:
      • Open Power Options and switch to 'High Performance' for testing.
      • This is a reversible change with no risk.
    Fix:          nvcheckup fix --id set-high-performance
```

Every finding header ends with its stable id in parentheses (the same value as `findings[].id` in `report.json`), so a forum helper can refer to `pcie-idle-power-saving` without quoting the whole block.

---

## Privacy and Safety

NVCheckup is built on a simple principle: **your data stays on your machine.**

| Guarantee | Detail |
|-----------|--------|
| No telemetry | Zero analytics, zero tracking, zero phone-home |
| Network | None, unless you pass `--network` to `run`, answer yes to the network question in `doctor`, or use `network-test`. Then NVCheckup runs an ICMP ping and a traceroute to `1.1.1.1` and a DNS lookup of `google.com`, locally. Nothing is uploaded anywhere. |
| Read-only | `run`, `snapshot`, `compare`, and `doctor` never modify anything. `self-test` never changes system settings; it creates and removes one temporary file in the current directory to verify write access. `fix` is opt-in: it asks for confirmation, journals every change, and can be reversed with `undo`. |
| No background services | No daemons, no scheduled tasks, no auto-updates |
| PII redaction ON by default | For both `run` and `snapshot`. Usernames, hostnames, home paths, IPs, and email addresses are scrubbed. |

### What Is Collected (Read-Only)

- OS version, kernel version, CPU model, RAM total, disk free space
- GPU model, driver version, VRAM, temperature, PCI bus ID
- GPU thermal and throttle state (clocks, power draw and limit, fan speed, active slowdown reasons)
- PCIe link state (current and maximum generation and width, power state, utilization)
- The `nvidia-smi` GPU table (the `Processes:` section is stripped before it is stored)
- NVIDIA kernel module status and `/dev/nvidia*` nodes (Linux)
- Secure Boot state and DKMS build status (Linux)
- Event logs for driver crashes — Event ID 4101 and nvlddmkm (Windows, last 30 days)
- WHEA hardware error summaries, including the reporting device name (Windows, last 30 days)
- Windows Update history (last 60 days)
- Installed overlay software (by name only — no process scanning)
- Python versions, PyTorch/TensorFlow/JAX versions and GPU visibility
- CUDA toolkit, cuDNN, and nvidia-container-toolkit versions
- With `--network` only: interface type, Wi-Fi band and signal, latency, jitter, packet loss, DNS time, traceroute hops

### What Is Never Collected

Passwords, tokens, API keys, browser data, SSH keys, clipboard contents, process lists or command lines, private documents, or anything outside the NVIDIA diagnostic scope.

### Redaction

Redaction is on by default for `run` and `snapshot`:
- Your home directory becomes `<home>` (`C:\Users\yourname\AppData\...` becomes `<home>\AppData\...`)
- Other profile paths become `C:\Users\<user>\...`; your username as a bare word becomes `<user>` (standalone usernames shorter than 3 characters are not replaced, to avoid mangling ordinary words; paths containing them still are)
- Machine hostname becomes `<host>`
- Public IPs become `<public-ip-redacted>`
- LAN IPs become `<lan-ip>`
- Email addresses become `<email-redacted>`
- Wi-Fi network names become `SSID: <redacted>`

Version numbers that look like IP addresses (`NVIDIA App version 11.0.7.247`) are left intact.

Use `--no-redact` only when you specifically need raw output and do not intend to share it.

---

## Command Reference

### `nvcheckup run`

Run diagnostics and generate a report. Read-only.

```
nvcheckup run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `full` | `gaming`, `ai`, `creator`, `streaming`, `full` |
| `--out` | `.` | Output directory |
| `--zip` | off | Bundle the generated report files into `nvcheckup-bundle-<timestamp>.zip` |
| `--json` | off | Generate structured `report.json` |
| `--md` | off | Generate GitHub/Reddit-ready `report.md` |
| `--network` | off | Opt in to network probes (ping and traceroute to 1.1.1.1, DNS lookup of google.com) |
| `--verbose` | off | Verbose console output |
| `--timeout` | `30` | Per-command timeout in seconds |
| `--redact` | **on** | Redact PII from all output |
| `--no-redact` | off | Disable PII redaction |
| `--include-logs` | off | Linux only: add `journalctl` and `dmesg` snippets to the report data. Ignored on Windows. Adds no extra files to the zip bundle |

### `nvcheckup fix`

List, preview, and apply a small set of well-understood settings changes. This is the **only** command that modifies your system.

```
nvcheckup fix                                  # list actions available on this platform
nvcheckup fix --all                            # preview every available action
nvcheckup fix --id <id> --dry-run              # preview one action, change nothing
nvcheckup fix --id <id>                        # apply after an interactive "yes"
nvcheckup fix --id <id> --journal DIR          # store the change journal somewhere else
```

Available actions:

| ID | Platform | Risk | Needs admin/root | What it does |
|----|----------|------|------------------|--------------|
| `set-high-performance` | Windows | low | yes | Switches the active power plan to High Performance |
| `disable-hags` | Windows | medium | yes | Disables Hardware-Accelerated GPU Scheduling (takes effect after a restart) |
| `disable-game-mode` | Windows | low | no | Turns off Windows Game Mode for the current user |
| `blacklist-nouveau` | Linux | medium | yes | Blacklists the nouveau driver and rebuilds the initramfs (takes effect after a reboot) |
| `update-ldconfig` | Linux | low | yes | Refreshes the shared library cache so `libcuda.so` can be found |

How it behaves:

- Elevation is checked **before** you are prompted. If an action needs admin/root and you are not elevated, `fix` tells you and exits without asking.
- You are shown a preview and must type `yes` to proceed. Anything else aborts.
- Every applied change is written to a journal at `<user config dir>/nvcheckup/nvcheckup-changes.json` (Windows: `%APPDATA%\nvcheckup\nvcheckup-changes.json`; Linux: `~/.config/nvcheckup/nvcheckup-changes.json`). On Linux, `sudo nvcheckup fix` journals under the invoking user's `~/.config/nvcheckup` when `SUDO_USER` is set, otherwise under `/root/.config/nvcheckup`. Use `--journal DIR` to override. `--out DIR` is accepted as a deprecated alias.
- `--dry-run` prints exactly what would be executed and changes nothing. It runs only the read-only capture commands the action uses to record the current state (for example `reg query`, `powercfg /getactivescheme`, `modinfo`, a package listing). Listing fixes, `--dry-run` and `undo` without `--id` do not even create the journal directory; it is created only when a change is applied or undone.

### `nvcheckup undo`

Reverse a change made by `fix`.

```
nvcheckup undo                    # list journal entries
nvcheckup undo --id <id>          # undo the newest successful, not-yet-undone entry for that id
nvcheckup undo --id <id> --journal DIR
```

`undo` uses the previous value recorded in the journal. If the setting did not exist before the fix, undo removes it again rather than writing a made-up value. Journal entries are validated before anything is written back to a privileged location.

### `nvcheckup network-test`

Standalone network diagnostics. This command always probes: 10 ICMP echoes to `1.1.1.1`, a traceroute/tracert to `1.1.1.1`, and an in-process DNS lookup of `google.com`.

```
nvcheckup network-test [--timeout SEC]
```

### `nvcheckup snapshot`

Create a timestamped JSON snapshot for later comparison. Redacted by default.

```
nvcheckup snapshot [--out DIR] [--timeout SEC] [--no-redact]
```

### `nvcheckup compare`

Diff two snapshots. Useful for before/after driver updates. Flags come before the two positional files; extra positionals are an error.

```
nvcheckup compare [--out DIR] [--md] before.json after.json
```

### `nvcheckup doctor`

Interactive guided mode. Asks six questions (primary use case including Creator, the issue, any recent change, extended logs, whether to run the opt-in network probes, and output format), then runs targeted checks. Read-only; network probes run only if you answer yes to that question.

```
nvcheckup doctor
```

### `nvcheckup self-test`

Verifies your environment has the tools NVCheckup needs and that the collector queries it relies on (for example the `nvidia-smi` fields) actually work on your driver. It never changes system settings; it creates and removes one temporary file in the current directory to verify write access.

```
nvcheckup self-test
```

### `nvcheckup version`

Prints the version and disclaimer. Also accepts `--version` and `-v`.

---

## Output Formats

| File | Format | When Generated |
|------|--------|----------------|
| `report.txt` | Human-readable, forum-pasteable | Always |
| `report.json` | Structured, machine-parseable (`metadata.schema_version`, `findings[].id`) | `--json` |
| `report.md` | GitHub/Reddit markdown with tables | `--md` |
| `nvcheckup-bundle-<timestamp>.zip` | Zip containing the generated report files | `--zip` |

Every text and markdown report ends with a privacy footer stating that the report was generated locally, whether network probes were run at your request, and that `run` did not modify your system.

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
| Windows 10 / 11 | x86_64 | Beta. Tested on Windows 11. |
| Linux (Ubuntu, Debian, Fedora, RHEL, Arch, and others) | x86_64 | Beta. Builds and unit-tests in CI; field reports wanted. |
| Linux | ARM64 (aarch64) | Beta. Builds and unit-tests in CI; field reports wanted. |
| WSL2 | x86_64 | Limited (GPU passthrough diagnostics) |

NVCheckup is designed for systems with NVIDIA GPUs. It will run on systems without NVIDIA hardware but most diagnostics will report "not detected."

---

## Architecture

```
nvcheckup
├── cmd/nvcheckup/          CLI entry point
├── internal/
│   ├── core/               7-phase pipeline (see below)
│   ├── collector/
│   │   ├── common/         Cross-platform (system, GPU, nvidia-smi, thermal, PCIe, network)
│   │   ├── windows/        WMI, event logs, overlays, updates, WHEA
│   │   ├── linux/          Kernel modules, DKMS, Secure Boot, PRIME, Xid
│   │   ├── wsl/            WSL2 detection and /dev/dxg checks
│   │   └── ai/             CUDA, PyTorch, TensorFlow, Python envs
│   ├── analyzer/           Findings engine (rules → evidence → next steps), stable finding ids
│   ├── remediate/          Opt-in fixes: preview, elevation check, journal, undo
│   ├── redact/             PII redaction engine
│   ├── report/             Output generators (txt, json, md)
│   ├── bundle/             Zip packaging
│   ├── snapshot/           Snapshot create/compare
│   ├── doctor/             Interactive guided mode
│   └── selftest/           Environment verification
├── pkg/types/              Shared data structures
├── knowledge/              Reference knowledge pack (rules, Xid codes, remediations);
│                           embedded by the experimental Rust companion
└── rust/                   Experimental partial Rust port (not built in CI, not shipped)
```

The `run` pipeline has seven phases: collect system information, detect GPUs and drivers, collect thermal and PCIe data, run platform-specific checks, check the AI/CUDA environment (mode-dependent), run network diagnostics (only with `--network`), and analyze results into findings.

Every collector returns structured data. Every analyzer rule produces a finding with a stable kebab-case id, severity, evidence, and safe next steps. There are more than 45 diagnostic rules; the authoritative list is `internal/analyzer/analyzer.go`. If a collector fails, NVCheckup logs the error and continues — it never crashes the whole run because one command is missing.

---

## Building from Source

```bash
git clone https://github.com/thatcooperguy/NVCheckup.git
cd NVCheckup

# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Build with a specific version string baked in
go build -ldflags="-s -w -X github.com/thatcooperguy/nvcheckup/pkg/types.Version=0.2.1" -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup

# Run tests (-race needs a C toolchain; on Windows run it under WSL or skip the flag)
go test ./...
```

NVCheckup is standard-library only. There is no `go.sum` because there are no dependencies.

---

## FAQ

**Is this official NVIDIA software?**
No. NVCheckup is an independent, community-maintained, open-source tool. It is not affiliated with, endorsed by, or supported by NVIDIA Corporation. It uses only public OS interfaces and publicly available command outputs.

**Does this replace DDU?**
No. NVCheckup diagnoses; it does not remove drivers. DDU (Display Driver Uninstaller) is a separate tool for that. If NVCheckup suggests a clean reinstall, DDU is one way to do it.

**Will NVCheckup fix anything automatically?**
Not unless you ask it to. `nvcheckup run` is read-only and only suggests steps. `nvcheckup fix --id <id>` applies one specific change after you type `yes`; it is journaled and `nvcheckup undo --id <id>` reverses it.

**Is it safe to share the report publicly?**
Yes, with default settings. PII redaction is on by default — usernames, hostnames, home paths, IP addresses, and email addresses are replaced with placeholders, and the `nvidia-smi` process list is never stored. Review the report before sharing if you have specific concerns.

**Does it work without admin/root?**
Mostly. Some checks (Windows event logs, Linux dmesg) benefit from elevated permissions. NVCheckup reports what it could not collect and why. `fix` actions that need elevation refuse to run until you are elevated.

**Why does `torch.cuda.is_available()` return False?**
The most common causes, in order:
1. CPU-only PyTorch wheel installed (check `torch.version.cuda` — if empty, this is it)
2. NVIDIA driver not installed or not loading
3. CUDA toolkit newer than the driver supports
4. Wrong Python environment (conda/venv confusion)
5. On Linux: Secure Boot blocking the NVIDIA kernel module

Run `nvcheckup run --mode ai` for automated diagnosis.

**How do I handle Secure Boot + NVIDIA on Linux?**
Two options:
1. **(Recommended)** Sign the NVIDIA module with a MOK key and enroll it. This preserves Secure Boot security while allowing the driver to load.
2. Disable Secure Boot in BIOS/UEFI. This works but reduces system security.

NVCheckup will detect this situation and provide specific guidance.

**Why is the Windows download flagged by SmartScreen?**
The release binaries are not code-signed. Verify the `.sha256` checksum, then choose "More info" -> "Run anyway". Or build from source.

---

## Contributing

Contributions are welcome. This project values clarity, safety, and cross-platform reliability. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, how to add collectors, analyzer rules, and remediation actions, and the pull request checklist.

- **Bug reports** — Open an issue. Attach your redacted NVCheckup report if relevant.
- **Feature requests** — Open an issue describing the use case and which persona it serves.
- **Pull requests** — Fork, branch, test, submit. Include unit tests for new collectors or analyzer rules.
- **Platform testing** — Testing on less common Linux distributions, ARM64 hardware, or edge cases is especially valuable.

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community guidelines and [CHANGELOG.md](CHANGELOG.md) for release history.

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
