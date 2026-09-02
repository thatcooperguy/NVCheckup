<div align="center">

<img src="docs/assets/banner.svg" alt="NVCheckup: cross-platform NVIDIA diagnostics for gamers, AI developers, and creators" width="100%">

<br>

[![CI](https://github.com/thatcooperguy/NVCheckup/actions/workflows/ci.yml/badge.svg)](https://github.com/thatcooperguy/NVCheckup/actions/workflows/ci.yml)
[![Linux field test](https://github.com/thatcooperguy/NVCheckup/actions/workflows/linux-fieldtest.yml/badge.svg)](https://github.com/thatcooperguy/NVCheckup/actions/workflows/linux-fieldtest.yml)
[![Linux field test (simulated GPU)](https://github.com/thatcooperguy/NVCheckup/actions/workflows/linux-fieldtest-sim.yml/badge.svg)](https://github.com/thatcooperguy/NVCheckup/actions/workflows/linux-fieldtest-sim.yml)
[![Release](https://img.shields.io/github/v/release/thatcooperguy/NVCheckup?color=76b900&label=release)](https://github.com/thatcooperguy/NVCheckup/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20ARM64-76b900.svg)](#supported-platforms)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

**[Quick Start](#quick-start) · [What You Get](#what-you-get) · [Supported GPUs](#supported-gpus) · [Commands](#command-reference) · [Privacy](#privacy-and-safety) · [FAQ](#faq) · [Landing page](https://thatcooperguy.github.io/NVCheckup/)**

*Unofficial community tool. Not affiliated with or endorsed by NVIDIA Corporation.*

---

</div>

## The Problem

It is 1:47 AM. Your screen went black mid-match, came back, and Event Viewer is now showing you something called `Event ID 4101` as if that explains anything. Or `nvidia-smi` sees your GPU perfectly well and PyTorch insists there is no such thing. Or you updated your kernel and the NVIDIA module has decided to take some time for itself.

So you open a forum thread. The first reply is "post your specs." The second is "did you try DDU." The third is from someone with the same problem in 2019, unresolved.

**NVCheckup writes the "post your specs" reply for you, then reads it, and tells you what to try next.**

It is a single, dependency-free binary that scans your NVIDIA GPU environment, matches what it finds against 45+ known failure patterns, and produces a clean report with ranked findings, plain-language explanations, and safe next steps. It is read-only by default, redacts your identity before writing anything, and runs on Windows and Linux, x86_64 and ARM64. The optional `fix` command can apply a handful of well-understood settings changes, but only after you type `yes`, and every change is journaled so `undo` can put it back.

---

## What It Does

```
nvcheckup run --mode full --zip
```

In 20 to 60 seconds (add 30 to 60 more if you opt into `--network`), NVCheckup:

- **Scans** your GPU, driver, CUDA toolkit, PCIe link, thermal and throttle state, and system configuration
- **Detects** driver crashes (Event ID 4101, nvlddmkm), kernel module failures, version mismatches, thermal throttling
- **Identifies** overlay pile-ups, Secure Boot blocks, nouveau interference, DKMS build failures
- **Probes** PyTorch, TensorFlow, and CUDA framework configurations, and says exactly where the chain broke
- **Generates** a redacted, forum-ready report with the summary block at the top where the forum wants it
- **Packages** everything into a zip you can attach to a bug report without first grepping it for your own name

Every GPU the driver exposes gets diagnosed. Four GPUs means four sets of thermal and PCIe lines and a correspondingly longer report.

---

## Quick Start

### Option A: Download a release binary

Grab the binary for your platform from [GitHub Releases](https://github.com/thatcooperguy/NVCheckup/releases). Each file ships with a `.sha256` checksum. Check it; the binary is unsigned (see the FAQ).

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
# Install straight into $GOPATH/bin
go install github.com/thatcooperguy/nvcheckup/cmd/nvcheckup@latest

# ...or build from a clone (see "Building from Source" below)
git clone https://github.com/thatcooperguy/NVCheckup.git
cd NVCheckup
go build -o nvcheckup ./cmd/nvcheckup
```

### What You Get

The top of `report.txt` is a summary block designed to be pasted into a support thread exactly as-is. It answers "post your specs" in nine lines, two of which pre-empt "is it thermal throttling" and "is your PCIe link fine":

<p align="center"><img src="docs/assets/summary.svg" alt="Terminal showing the NVCheckup summary block" width="920"></p>

<details>
<summary>The same block as plain text, with the report header</summary>

```
────────────────────────────────────────────────────────────────────────
  NVCheckup v0.2.2 — NVIDIA Diagnostic Report
  NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.
────────────────────────────────────────────────────────────────────────
  Generated: 2026-09-01 14:32:10 UTC
  Mode:      full
  Platform:  windows
  Runtime:   48.9s
  Redaction: ENABLED (PII removed)
────────────────────────────────────────────────────────────────────────

== SUMMARY (paste this in support threads) ==

NVCheckup v0.2.2 | 2026-09-01 14:32:10
OS: Microsoft Windows 11 Pro 10.0.26100 | Arch: amd64
GPU: NVIDIA GeForce RTX 4070 | Driver: 591.86 | VRAM: 12282 MB
CUDA (driver): 13.1 | CUDA Toolkit: 12.8
PyTorch: 2.5.1+cu118 (CUDA available)
Temp: 42°C | P-State: P8 | Util: 0%
PCIe: Gen1 x16 (idle, max Gen4)
Findings: 1 CRITICAL, 1 WARNING, 6 total | 2 auto-fixable
Top: Display Driver Resets Detected (Event ID 4101); nvlddmkm Driver ...
```

</details>

The rest of the report holds the system, GPU, platform and AI/CUDA sections, then every finding with its evidence, why it matters, next steps, and (when one exists) the `nvcheckup fix --id ...` command that addresses it. Complete examples: [`examples/sample-report-gaming.txt`](examples/sample-report-gaming.txt) and [`examples/sample-report-ai-linux.txt`](examples/sample-report-ai-linux.txt).

---

## Who This Is For

### Gamers

Your display driver "stopped responding and has recovered." It did not feel recovered. NVCheckup reads the event log so you don't have to learn what nvlddmkm is, checks whether Discord, OBS, ShadowPlay and Game Bar are all drawing on the same frame (which is not a conflict, it is a default Windows install), flags HAGS and power plan settings, lists the Windows updates that landed in the last 60 days next to the crash count so you can blame the right vendor, and checks whether the GPU is throttling or stuck on a narrow PCIe link under load.

**Common issues detected:**
- Display driver stopped responding / recovered (nvlddmkm resets)
- Thermal throttling and GPUs running hot
- PCIe link running below its rated speed or width while busy
- Overlay software conflicts (Xbox Game Bar, Discord, RTSS, and friends)
- HAGS and power plan misconfigurations
- Windows Update regression correlation

### AI / CUDA Developers

`torch.cuda.is_available()` says `False`. `nvidia-smi` says everything is fine. They are both telling the truth, which is the annoying part. NVCheckup walks the whole chain, driver to toolkit to cuDNN to Python environment to framework build, and points at the exact link that is broken. Usually it is a CPU-only wheel. When it is Secure Boot instead, it will say so.

One thing worth knowing: the CUDA version in the top-right corner of `nvidia-smi` is what the driver supports, not what you installed. The report prints `CUDA (driver)` and `CUDA Toolkit` separately so that argument can end.

**Common issues detected:**
- CPU-only PyTorch wheel installed (no CUDA compiled in)
- CUDA toolkit or PyTorch build newer than what the driver supports
- NVIDIA kernel module not loaded (Linux)
- Secure Boot blocking unsigned modules
- DKMS build failure after a kernel update
- Kernel Xid errors, grouped by code, so "GPU has fallen off the bus" arrives as a finding rather than a screenshot (Linux)
- `LD_LIBRARY_PATH` / `PATH` missing CUDA libraries
- WSL2 `/dev/dxg` not present

### Streamers and Creators

Dropped frames during recording. Stutter that only shows up on stream. NVCheckup checks driver health, finds the overlay conflicts, and with `--network` can measure latency, jitter, and packet loss so you can stop blaming the encoder for what the Wi-Fi did.

### IT / Power Users

You want machine-readable output with stable identifiers. You want to diff system state before and after a driver update, so that "it was fine before the update" becomes a diff rather than a feeling. You want a support bundle that does not leak the hostname of a machine that is not supposed to exist. `--json` with stable finding ids, `snapshot` + `compare`, redaction on by default, and exit codes stable enough to use as a gate in a script.

---

## Supported GPUs

Anything the installed NVIDIA driver exposes through `nvidia-smi`. Nothing in NVCheckup is tied to a GPU model. The development machine happened to have an RTX 3090 in it; the parser fixtures deliberately do not stop there, and cover an RTX 5090 on a Gen5 link, an RTX 4060 Laptop wired at x8, a GTX 1060 on a pre-R535 driver, an A100 with no fan, an H100 in MIG mode, a Tesla T4, a Quadro RTX 8000 and a three-GPU rig.

| Class | Notes |
|-------|-------|
| GeForce GTX 900 series and newer, RTX 20/30/40/50 | Supported, including Gen5 PCIe on RTX 50. The PCIe rules compare against the maximum the GPU reports, so new generations need no special case |
| GeForce Laptop GPUs (Optimus / hybrid) | Many laptops wire the dGPU at x8. x8 of x8 is normal and is not flagged. When a powered-down dGPU makes `nvidia-smi` fail, the report quotes nvidia-smi's own message and the likely cause instead of a wall of parse errors |
| Workstation RTX / Quadro | Supported |
| Datacenter Tesla, A-series, H-series | Passive cards report no fan; we believe them. Under MIG, utilization is unavailable and idle/load inference falls back to P-state |
| Multi-GPU systems | Every GPU is collected and analyzed; the report gets per-GPU thermal and PCIe lines and `report.json` gets `gpu_thermal` and `gpu_pcie` arrays |
| Older drivers | If the driver rejects `clocks_event_reasons`, the legacy `clocks_throttle_reasons` field is used instead |
| Jetson / Tegra | Detected. There is no `nvidia-smi` on Tegra, so GPU, thermal and PCIe checks are limited, and the report says so rather than reporting a missing driver |

---

## Diagnostic Modes

| Mode | Focus | Use When |
|------|-------|----------|
| `gaming` | Driver stability, overlays, event logs, power settings, thermal/PCIe | Black screens, crashes, stutter |
| `ai` | CUDA stack, PyTorch/TF probes, kernel modules | `torch.cuda.is_available() == False` |
| `streaming` | Driver health, overlay detection, capture checks | Recording/streaming issues, dropped frames |
| `creator` | Driver health, CUDA environment, creative readiness | Creative application issues |
| `full` | Everything | You are not sure what is wrong (start here) |

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

Your GPU drops its PCIe link to Gen1 when it has nothing to do. This is not a defect. It is a nap. An earlier version of this tool used to wake it up and shout `DOWNSHIFTED`; the current version checks whether the GPU is asleep first.

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

Every finding header ends with its stable id in parentheses, the same value as `findings[].id` in `report.json`, so a forum helper can say "ignore `pcie-idle-power-saving`, that one's fine" without quoting the whole block.

Corrected hardware errors (WHEA Event IDs 17, 19 and 47) get the same discipline: they are INFO, not WARN, and the evidence quotes the component and PCI address the event names, with the full event text in `report.json`. So when sixteen "hardware errors" turn out to be your network card politely correcting itself, you can prove it before anyone tells you to run memtest. For the record, the development machine is not clean either: its own report shows one real nvlddmkm event and sixteen of exactly those WHEA entries.

---

## Privacy and Safety

NVCheckup is built on a simple principle: **your data stays on your machine.** It is also built by people who have read too many READMEs that say that and then phone home, so here is the specific version.

| Guarantee | Detail |
|-----------|--------|
| No telemetry | Zero analytics, zero tracking, zero phone-home. There is no server to phone. |
| Network | None, unless you pass `--network` to `run`, answer yes to the network question in `doctor`, or use `network-test`. Then NVCheckup runs an ICMP ping and a traceroute to `1.1.1.1` and a DNS lookup of `google.com`, locally, and says so in the report footer. Nothing is uploaded anywhere. |
| Read-only | `run`, `snapshot`, `compare`, and `doctor` never modify anything. `self-test` never changes system settings; it creates and removes one temporary file in the current directory to verify write access. `fix` is opt-in: it asks for confirmation, journals every change, and can be reversed with `undo`. |
| No background services | No daemons, no scheduled tasks, no auto-updates, no tray icon, nothing in your startup apps. |
| PII redaction ON by default | For both `run` and `snapshot`. Usernames, hostnames, home paths, IPs, and email addresses are scrubbed before anything is written. |

### What Is Collected (Read-Only)

<details>
<summary>Full list</summary>

- OS version, kernel version, CPU model, RAM total, disk free space
- GPU model, driver version, VRAM, temperature, PCI bus ID, for every NVIDIA GPU
- GPU thermal and throttle state (clocks, power draw and limit, fan speed, active slowdown reasons), per GPU
- PCIe link state (current and maximum generation and width, power state, utilization), per GPU
- The `nvidia-smi` GPU table. The process list is stripped before it is stored; the stored table ends with the line `(Processes section omitted: process names are private)`, which is the binary saying it so we don't have to
- NVIDIA kernel module status and `/dev/nvidia*` nodes (Linux)
- Secure Boot state and DKMS build status (Linux)
- Jetson / Tegra release information when running on a Jetson (Linux)
- Event logs for driver crashes: Event ID 4101 and nvlddmkm (Windows, last 30 days)
- WHEA hardware error summaries, including the reporting device's PCI identifiers (Windows, last 30 days)
- Windows Update history (last 60 days)
- Installed overlay software (by name only; no process scanning)
- Python versions, PyTorch/TensorFlow/JAX versions and GPU visibility
- CUDA toolkit, cuDNN, and nvidia-container-toolkit versions
- With `--network` only: interface type, Wi-Fi band and signal, latency, jitter, packet loss, DNS time, traceroute hops

</details>

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

Version numbers that look like IP addresses (`NVIDIA App version 11.0.7.247`) are left intact, and so is the home directory of a user whose name merely starts with yours (`C:\Users\alice2` is not `<home>2`). Both of those were bugs once. GPU model names are not redacted; the whole point of the report is to say which card you have.

Use `--no-redact` only when you specifically need raw output and do not intend to share it. The forum does not need your hostname, and your hostname is probably your name.

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
| `--verbose` | off | Per-phase timings and collector notes as they happen |
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

- Elevation is checked **before** you are prompted, for both `fix` and `undo`. If an action needs admin/root and you are not elevated, NVCheckup tells you and exits without asking you to type `yes` into the void.
- You are shown a preview and must type `yes` to proceed. Anything else aborts.
- Every applied change is written to a journal at `<user config dir>/nvcheckup/nvcheckup-changes.json` (Windows: `%APPDATA%\nvcheckup\nvcheckup-changes.json`; Linux: `~/.config/nvcheckup/nvcheckup-changes.json`). On Linux, `sudo nvcheckup fix` journals under the invoking user's `~/.config/nvcheckup` when `SUDO_USER` is set, otherwise under `/root/.config/nvcheckup`. Use `--journal DIR` to override. `--out DIR` is accepted as a deprecated alias.
- `--dry-run` prints exactly what would be executed and changes nothing. It runs only the read-only capture commands the action uses to record the current state (for example `reg query`, `powercfg /getactivescheme`, `modinfo`, a package listing). Listing fixes, `--dry-run` and `undo` without `--id` do not even create the journal directory; it is created only when a change is applied or undone.
- `blacklist-nouveau` refuses to run unless an NVIDIA kernel module (proprietary or `nvidia-open`) is actually available for your running kernel, because blacklisting the only driver you have is not a fix, it is a black screen scheduled for the next boot.

### `nvcheckup undo`

Reverse a change made by `fix`.

```
nvcheckup undo                    # list journal entries
nvcheckup undo --id <id>          # undo the newest successful, not-yet-undone entry for that id
nvcheckup undo --id <id> --journal DIR
```

`undo` uses the previous value recorded in the journal. If the setting did not exist before the fix, undo removes it again rather than writing a made-up value; inventing a default and calling it a restore is how you end up on the forum yourself. Journal entries are validated before anything is written back to a privileged location, and a failed undo stays retryable.

### `nvcheckup network-test`

Standalone network diagnostics. This command always probes: 10 ICMP echoes to `1.1.1.1`, a traceroute/tracert to `1.1.1.1`, and an in-process DNS lookup of `google.com`. If ping produces no samples it says so rather than declaring your network healthy at 0.0 ms.

```
nvcheckup network-test [--timeout SEC]
```

### `nvcheckup snapshot`

Create a timestamped JSON snapshot for later comparison. Redacted by default.

```
nvcheckup snapshot [--out DIR] [--timeout SEC] [--no-redact]
```

### `nvcheckup compare`

Diff two snapshots. Useful for before/after driver updates. Flags come before the two positional files; extra positionals are an error. `--md` or an explicit `--out` writes `comparison.md` / `comparison.txt` into `--out` and prints the path.

```
nvcheckup compare [--out DIR] [--md] before.json after.json
```

### `nvcheckup doctor`

Interactive guided mode. Asks six questions (primary use case including Creator, the issue, any recent change, extended logs, whether to run the opt-in network probes, and output format), then runs targeted checks. None of the six is "did you try DDU." Read-only; network probes run only if you answer yes to that question.

```
nvcheckup doctor
```

### `nvcheckup self-test`

Verifies your environment has the tools NVCheckup needs, and that the exact `nvidia-smi` query fields the collectors rely on actually work on your driver. This check exists because v0.2.0 asked `nvidia-smi` for a field that does not exist and got the silent treatment for six months. It never changes system settings; it creates and removes one uniquely named temporary file in the current directory to verify write access.

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
| `report.json` | Structured, machine-parseable (`metadata.schema_version`, `findings[].id`, per-GPU `gpu_thermal` and `gpu_pcie` arrays) | `--json` |
| `report.md` | GitHub/Reddit markdown with tables | `--md` |
| `nvcheckup-bundle-<timestamp>.zip` | Zip containing the generated report files | `--zip` |

Every text and markdown report ends with a privacy footer stating that the report was generated locally, whether network probes were run at your request, and that `run` did not modify your system.

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | No significant issues detected |
| `1` | Warnings detected (non-critical) |
| `2` | Critical issues detected |
| `3` | Internal error (please open an issue) |

---

## Supported Platforms

| Platform | Architecture | Status |
|----------|-------------|--------|
| Windows 10 / 11 | x86_64 | Beta. Tested on Windows 11. |
| Linux (Ubuntu, Debian, Fedora, RHEL, Arch, and others) | x86_64 | Beta. Builds and unit-tests in CI; field reports wanted. |
| Linux | ARM64 (aarch64) | Beta. Builds and unit-tests in CI; field reports wanted. |
| WSL2 | x86_64 | Limited (GPU passthrough diagnostics) |
| Jetson / Tegra | ARM64 | Limited (no `nvidia-smi`; detected) |

NVCheckup is designed for systems with NVIDIA GPUs. It will run on systems without NVIDIA hardware, but most diagnostics will report "not detected," and it will be right.

---

## Architecture

```
nvcheckup
├── cmd/nvcheckup/          CLI entry point
├── internal/
│   ├── core/               7-phase pipeline (see below)
│   ├── collector/
│   │   ├── common/         Cross-platform (system, GPU, nvidia-smi, thermal, PCIe, network, Jetson detection)
│   │   ├── windows/        WMI, event logs, overlays, updates, WHEA
│   │   ├── linux/          Kernel modules, DKMS, Secure Boot, PRIME, Xid
│   │   ├── wsl/            WSL2 detection and /dev/dxg checks
│   │   └── ai/             CUDA, PyTorch, TensorFlow, Python envs
│   ├── analyzer/           Findings engine (rules → evidence → next steps), stable finding ids
│   ├── remediate/          Opt-in fixes: catalog, preview, elevation check, journal, undo
│   ├── redact/             PII redaction engine
│   ├── report/             Output generators (txt, json, md)
│   ├── bundle/             Zip packaging
│   ├── snapshot/           Snapshot create/compare
│   ├── doctor/             Interactive guided mode
│   └── selftest/           Environment verification
├── pkg/types/              Shared data structures
└── knowledge/              Reference knowledge pack (rules, Xid codes, remediations),
                            kept in lockstep with the analyzer by a test
```

<p align="center"><img src="docs/assets/pipeline.svg" alt="The seven phases of nvcheckup run" width="100%"></p>

The `run` pipeline has seven phases: collect system information, detect GPUs and drivers, collect thermal and PCIe data for every GPU, run platform-specific checks, check the AI/CUDA environment (mode-dependent), run network diagnostics (only with `--network`), and analyze results into findings.

Every collector is split into "run the command" and "parse the output," so the parsers are tested against captured `nvidia-smi`, `netsh`, PowerShell and `ping` output from machines the maintainers do not own. Every analyzer rule produces a finding with a stable kebab-case id, severity, evidence, and safe next steps. There are more than 45 rules; the authoritative list is `internal/analyzer/analyzer.go`, and a test refuses to pass if `knowledge/rules.json` drifts from it. If a collector fails, NVCheckup records the error in the report's Collector Notes and continues. One missing command never takes down the whole run; a Windows event log with zero matching events, which older versions misread as a permissions failure, is now just a zero.

---

## Building from Source

```bash
git clone https://github.com/thatcooperguy/NVCheckup.git
cd NVCheckup

# Build for current platform
go build -o nvcheckup ./cmd/nvcheckup

# Build with a specific version string baked in
go build -ldflags="-s -w -X github.com/thatcooperguy/nvcheckup/pkg/types.Version=0.2.2" -o nvcheckup ./cmd/nvcheckup

# Cross-compile all targets
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-windows-amd64.exe ./cmd/nvcheckup
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-amd64       ./cmd/nvcheckup
GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o dist/nvcheckup-linux-arm64        ./cmd/nvcheckup

# Run tests (-race needs a C toolchain; on Windows run it under WSL or skip the flag)
go test ./...
```

NVCheckup is standard-library only. There is no `go.sum` because there are no dependencies. The stripped release binary is under 4 MB, which is smaller than the screenshot you were going to post instead.

---

## FAQ

**Is this official NVIDIA software?**
No. NVCheckup is an independent, community-maintained, open-source tool. It is not affiliated with, endorsed by, or supported by NVIDIA Corporation. It uses only public OS interfaces and publicly available command outputs. It also cannot get you a Founders Edition at MSRP.

**Does this replace DDU?**
No. NVCheckup diagnoses and does not remove drivers. If it recommends a clean reinstall, DDU (Display Driver Uninstaller) is one way to do the removing, and the report's next steps will say so.

**Will NVCheckup fix anything automatically?**
Not unless you ask it to. `nvcheckup run` is read-only and only suggests steps. `nvcheckup fix --id <id>` applies one specific change after you type `yes`; it is journaled and `nvcheckup undo --id <id>` reverses it.

**Is it safe to share the report publicly?**
Yes, with default settings. PII redaction is on by default: usernames, hostnames, home paths, IP addresses, and email addresses are replaced with placeholders, and the `nvidia-smi` process list is never stored, so nobody learns that you had Discord, three browsers and a game launcher open while training a model. Review the report before sharing if you have specific concerns.

**Does it work without admin/root?**
Mostly. Some checks (Windows event logs, Linux dmesg) benefit from elevated permissions. NVCheckup reports what it could not collect and why. `fix` actions that need elevation refuse to run until you are elevated.

**Why does `torch.cuda.is_available()` return False?**
The most common causes, in order:
1. CPU-only PyTorch wheel installed (check `torch.version.cuda`; if it is empty, this is it)
2. NVIDIA driver not installed or not loading
3. CUDA toolkit or PyTorch build newer than the driver supports
4. Wrong Python environment (conda/venv confusion)
5. On Linux: Secure Boot blocking the NVIDIA kernel module

Run `nvcheckup run --mode ai` for automated diagnosis. It has seen all five.

**How do I handle Secure Boot + NVIDIA on Linux?**
Two options:
1. **(Recommended)** Sign the NVIDIA module with a MOK key and enroll it. This preserves Secure Boot security while allowing the driver to load.
2. Disable Secure Boot in BIOS/UEFI. This works but reduces system security.

NVCheckup detects this situation and gives specific guidance.

**My report says my PCIe link is Gen1. Is my GPU broken?**
Almost certainly not. Idle GPUs drop to Gen1 to save power. The finding is tagged `(expected)` and INFO for exactly this reason. Run something heavy and re-run NVCheckup; if it still says Gen1 under load, now it is a WARN and worth chasing.

**Why is the Windows download flagged by SmartScreen?**
The release binaries are not code-signed. Verify the `.sha256` checksum, then choose "More info" -> "Run anyway". Or build from source; it takes a few seconds and needs nothing but Go.

---

## Contributing

Contributions are welcome. This project values clarity, safety, and cross-platform reliability. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, how to add collectors, analyzer rules, and remediation actions, and the pull request checklist.

- **Bug reports**: open an issue and attach your redacted NVCheckup report; the tool exists so that this is a reasonable thing to ask.
- **Feature requests**: open an issue describing the use case and which persona it serves.
- **Pull requests**: fork, branch, test, submit. Include unit tests for new collectors or analyzer rules.
- **Linux hardware**: have a Linux box or Jetson with an NVIDIA GPU? Run `scripts/linux-fieldtest.sh` (read-only, redacted) and attach the bundle to [issue #2](https://github.com/thatcooperguy/NVCheckup/issues/2). CI can only simulate a GPU on Linux; you have the real thing.
- **GPU fixtures**: "works on my machine" is a fixture-collection problem, and the fixture we are missing is yours. `nvidia-smi --query-gpu=... --format=csv,noheader,nounits` output from hardware we do not have is the single most useful contribution. Jetson `tegrastats`, RTX 50 laptops, vGPU: see CONTRIBUTING.md for where it goes.

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community guidelines and [CHANGELOG.md](CHANGELOG.md) for release history.

---

## License

[MIT License](LICENSE). Community maintained.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting guidelines.

---

<div align="center">

**NVCheckup exists to make diagnosing NVIDIA ecosystem issues faster, safer, and less frustrating.**

*Built by the community. For the community. Tested at 1:47 AM.*

</div>
