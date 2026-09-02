# Changelog

All notable changes to NVCheckup are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [Unreleased]

### Removed
- The experimental Rust companion (`rust/`). It implemented 7 of the analyzer's rules, was never built in CI or shipped, and had already diverged from the Go analyzer. The knowledge pack under `knowledge/` stays as a reference for contributors and external tooling. The tree is in git history at tag `v0.2.1` if anyone wants to revive it.

### Added
- `linux-fieldtest.yml` workflow: runs the real binary end to end on Ubuntu 22.04/24.04 x86_64 and Ubuntu 24.04 ARM64 GitHub runners (no GPU) and asserts exit codes, report shape, expected findings, redaction and inert fix/undo behaviour.
- Release artifacts get a GitHub build-provenance attestation (verify with `gh attestation verify <file> --owner thatcooperguy`).

## [0.2.1] - 2026-09-01

A correctness and trust release. Every false positive reported against 0.2.0 was
traced to a collector that parsed the wrong thing, and every "read-only" claim in
the documentation was checked against what the binary actually does.

### Fixed

- Thermal collector queried an `nvidia-smi` field that does not exist, which made
  the whole thermal query fail and left thermal findings empty on every system.
- PCIe downshift false positive: NVIDIA GPUs drop the link to Gen1 when idle to
  save power. The analyzer now reports this as INFO "PCIe Link Power-Saving at
  Idle (expected)" and only warns about a real downshift when the GPU is busy.
- WHEA corrected hardware errors were reported as faults. Corrected errors are now
  INFO "Corrected Hardware Errors Logged (WHEA)"; only uncorrected errors warn.
- DNS timing measured the time to spawn an external resolver process. The lookup
  is now done in-process and reports resolution time only.
- Wi-Fi was misdetected on Windows because the adapter list was matched on the
  substring "disconnected". Detection now uses the interface type and state.
- `Get-WinEvent` returning zero matching events was treated as a collector failure.
  Zero events is now an empty result, not an error.
- HAGS and Game Mode registry values that are absent were shown as
  "Unknown (Unknown)". They now read "Default (not configured)", and the
  analyzer no longer treats an absent HAGS key as "enabled".
- Four-part version numbers were redacted as IP addresses: "NVIDIA App version
  11.0.7.247 is installed." came out as "NVIDIA App version <public-ip-redacted>
  is installed." Redaction now leaves a dotted quad alone when it follows
  "version", "ver", "release", "build", "driver" or a glued "v", or when it has a
  fifth component. Real addresses (`ping 8.8.8.8`, `gateway 192.168.1.1`,
  traceroute hops) are still replaced.
- The home-directory redaction matched the prefix of sibling profiles, turning
  `C:\Users\alice2\...` into `<home>2\...` for user `alice`. The match must now end
  at a path separator, a delimiter, or the end of the text.
- `self-test`'s Elevation row is informational. Running without admin/root is
  reported as INFO together with the checks that degrade, not as a warning.
- The DNS measurement reports the worst of three lookups instead of a single
  sample, so a cached hit cannot mask a slow resolver.
- `fix` and `undo` no longer create the journal directory for list and `--dry-run`
  invocations. It is created, with owner-only permissions, only when a change is
  actually applied or undone. List and dry-run output still print `Journal: <path>`.
- The elevation hint shown by `fix` also recognises the raw "Access is denied"
  text printed by `reg.exe` and `powercfg`, and `fix` uses the same elevation
  check as the remediation engine instead of a separate probe.
- The active power plan is read with `powercfg /getactivescheme`; the WMI
  `Win32_PowerPlan` class is unavailable on many systems.
- CUDA toolkit/driver mismatch direction: a toolkit *newer* than the driver
  supports is the problem; an older toolkit is fine and no longer warns.
- `snapshot` output is now redacted by default, like `run`. Use `--no-redact` to
  disable.
- The raw `nvidia-smi` table stored in `report.json` no longer includes the
  `Processes:` section.
- Source is gofmt-clean; the lint job now enforces it.
- Module path is `github.com/thatcooperguy/nvcheckup`.
- A failed `undo` no longer marks the journal entry as undone; the entry stays
  retryable.
- `undo` checks elevation before prompting, using the same preflight as `fix`.
- The "network healthy" finding requires actual ping samples; a probe that
  returned none no longer reports a healthy connection.
- The summary line shows `PCIe: ... (DOWNSHIFTED ...)` only when a PCIe WARN
  finding actually fired; otherwise an idle link is labelled as idle.
- On Linux, `grep` exiting 1 (no match) inside a collector is an empty result,
  not a collector error.
- Windows remediation actions run `reg.exe` and `powercfg.exe` from
  `%SystemRoot%\System32` instead of whatever `PATH` resolves first.
- Every remediation command runs under the executor's timeout, so a hung
  command cannot stall `fix` or `undo` indefinitely.
- The nouveau checks fire only when a nouveau kernel module is actually built
  for the running kernel; an absent module is no longer reported as a conflict.
- `self-test`'s write-permission check creates and removes one uniquely named
  temporary file (`.nvcheckup-selftest-*`) in the current directory and changes
  no system settings; it no longer overwrites a pre-existing
  `.nvcheckup-selftest-write` file.
- On Linux, the CUDA toolkit version falls back to the version embedded in the
  directory `/usr/local/cuda` resolves to (including the Debian/Ubuntu
  `/etc/alternatives/cuda` indirection, e.g. `/usr/local/cuda-12.4` -> `12.4`)
  when `nvcc` is not on `PATH` or reports no release.
- WSL2 detection works on distributions booted with systemd.
- The thermal and PCIe collectors read only the first `nvidia-smi` row, so on a
  multi-GPU system GPUs 1..n were never collected or analyzed and a hot or
  downshifted second card went unreported. Every row is now parsed and attributed
  to its GPU by the `index` field.
- `nvidia-smi` failing with `No devices were found`, `Unable to determine the
  device handle` or `Failed to initialize NVML` was reported as a generic query
  failure. Each now produces a specific collector error that names the likely
  cause (no GPU bound to the driver, a GPU that dropped off the bus, or a
  driver/NVML library mismatch after an update).
- Jetson/Tegra boards, which have no `nvidia-smi` by design, were told they had
  no NVIDIA GPU and no driver. They are now detected and those findings are
  suppressed there.

### Changed

- Network probes are opt-in. `nvcheckup run --network` enables them in any mode;
  without the flag `run` opens no sockets. `network-test` still always probes.
  `metadata.network_probes` in `report.json` records whether they ran.
- `run --no-admin` was removed. It never changed behaviour.
- `compare` documents and parses flags before positionals:
  `nvcheckup compare [--out DIR] [--md] a.json b.json`. Extra positionals are an error.
- The change journal moved from the current directory to the user config
  directory (`%APPDATA%\nvcheckup\nvcheckup-changes.json` on Windows,
  `~/.config/nvcheckup/nvcheckup-changes.json` on Linux). On Linux,
  `sudo nvcheckup fix` journals under the invoking user's `~/.config/nvcheckup`
  when `SUDO_USER` is set, otherwise under `/root/.config/nvcheckup`. `fix` and
  `undo` accept `--journal DIR`; `--out` remains as a deprecated alias.
- Unused `RunConfig` fields that no code path read were removed from `pkg/types`.
- CI runs `self-test` on the built binary instead of through `go run`, which
  collapsed every non-zero exit code to 1; `govulncheck` runs with the current
  stable Go. The release workflow runs vet and tests before building and uses a
  read-only token except for the publish step.
- Documentation corrected against the binary's real output: `--include-logs` is
  Linux-only and adds `journalctl`/`dmesg` snippets to the report data (nothing
  extra goes into the zip); `doctor` asks six questions, including the network
  probe opt-in and a Creator use case; the `report.json` schema in `PRODUCT.md`
  is regenerated from real output (`thermal`, `pcie`, `displays` and `network`
  are top-level, not nested in `gpus[]`); `--dry-run` runs only read-only
  capture commands; network probes add 30-60 s; standalone usernames shorter
  than 3 characters are not redacted (paths containing them still are).
- `undo --id` reverses the newest successful, not-yet-undone entry for that id.
- Text and markdown report footers state that the report was generated locally,
  whether network probes ran at your request, and that `run` did not modify the
  system.
- Every finding carries a stable kebab-case `id` (for example `pcie-downshift`,
  `whea-errors`, `hags-enabled`).
- `report.json` gains `metadata.schema_version` (currently `"1"`).

### Added

- Elevation preflight for `fix`: if an action needs admin/root and the process is
  not elevated, `fix` exits before prompting.
- Undo for settings that did not exist before the fix: the value is removed again
  instead of being replaced with a placeholder.
- Undo information is validated before anything is written to a privileged path.
- `blacklist-nouveau` rebuilds the initramfs so the blacklist takes effect on the
  next boot.
- `self-test` runs the collector queries the tool depends on (for example the
  `nvidia-smi` field list) and reports which ones the installed driver rejects.
- `CHANGELOG.md`, `CONTRIBUTING.md`, and `.gitattributes` (LF line endings).
- Per-GPU thermal and PCIe collection. The `nvidia-smi` queries carry the `index`
  field; `report.json` gains `gpu_thermal` and `gpu_pcie` arrays with one object
  per NVIDIA GPU (`gpu_index`), while the top-level `thermal` and `pcie` objects
  remain GPU 0 for compatibility. When two or more NVIDIA GPUs are present the
  GPU INVENTORY section prints one `Thermal:` and one `PCIe:` line per GPU and the
  summary block gains `GPUs: N NVIDIA (worst temp XX°C on GPU i)`. Single-GPU
  output is unchanged.
- Jetson/Tegra detection: `system.is_jetson` and `system.jetson_release` in
  `report.json`, an INFO finding `jetson-detected` that points at `tegrastats`,
  and suppression of the no-GPU / no-driver / `nvidia-smi` missing findings on
  Tegra.
- Parser fixtures captured from GPU classes other than the development machine:
  RTX 5090 (Gen5 link), RTX 4060 Laptop (native x8 link), GTX 1060 on a pre-R535
  driver (legacy `clocks_throttle_reasons` field), A100-SXM4 (no fan), H100 in
  MIG mode (`[N/A]` utilization), Tesla T4, Quadro RTX 8000, and a 3-GPU rig.
- `PRODUCT.md` gains a "Supported GPUs" section, and `CONTRIBUTING.md` explains how
  to capture `nvidia-smi` rows from hardware you do not own and turn them into
  fixtures.

### Security

- Removed interpolation of the `CUDA_PATH` environment variable into a PowerShell
  command string. The value is now passed as an argument.
- Python probes run with `python -I` (isolated mode) so `PYTHONPATH`, user
  site-packages, and the current directory cannot inject code into the probe.

## [0.2.0] - 2026-02-15

### Added

- `nvcheckup fix` and `nvcheckup undo`: an opt-in remediation engine with five
  actions (`set-high-performance`, `disable-hags`, `disable-game-mode`,
  `blacklist-nouveau`, `update-ldconfig`), risk levels, dry-run preview, and a
  JSON change journal.
- `nvcheckup network-test` and network collectors: latency, jitter, packet loss,
  DNS time, traceroute, Wi-Fi band and signal.
- GPU thermal and PCIe link collectors with throttling, running-hot, fan, and
  link-speed rules.
- Windows display chain collection (monitors, refresh rates) with mixed-refresh
  and complexity rules.
- Linux Xid error collection with a reference table of Xid codes.
- `knowledge/` reference pack (rules, Xid codes, remediations).
- Experimental Rust companion under `rust/` (partial port, not built in CI; removed again in 0.2.2).
- GitHub Pages landing page (`docs/index.html`) and `PRODUCT.md`.

### Changed

- Rule count grew from 31 to more than 45.
- README refocused on general users.

## [0.1.0] - 2026-02-14

### Added

- Initial release: single-binary Go CLI for Windows and Linux (x86_64, ARM64).
- Collectors for system, GPU and driver (`nvidia-smi`, WMI, lspci), Windows event
  logs and settings, Linux kernel modules, DKMS, Secure Boot, PRIME, WSL2, and the
  AI/CUDA stack (CUDA toolkit, cuDNN, Python, PyTorch, TensorFlow).
- 31 analyzer rules with severity, evidence, and next steps.
- Text, JSON, and Markdown reports; zip bundles.
- `snapshot` and `compare`, interactive `doctor`, and `self-test`.
- PII redaction on by default.
- CI (test, build, lint) and release (cross-compile, SHA256) workflows.

[Unreleased]: https://github.com/thatcooperguy/NVCheckup/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/thatcooperguy/NVCheckup/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/thatcooperguy/NVCheckup/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/thatcooperguy/NVCheckup/releases/tag/v0.1.0
