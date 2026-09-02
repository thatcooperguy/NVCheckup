# Changelog

All notable changes to NVCheckup are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

### Changed

- Network probes are opt-in. `nvcheckup run --network` enables them in any mode;
  without the flag `run` opens no sockets. `network-test` still always probes.
  `metadata.network_probes` in `report.json` records whether they ran.
- `run --no-admin` was removed. It never changed behaviour.
- `compare` documents and parses flags before positionals:
  `nvcheckup compare [--out DIR] [--md] a.json b.json`. Extra positionals are an error.
- The change journal moved from the current directory to the user config
  directory (`%APPDATA%\nvcheckup\nvcheckup-changes.json` on Windows,
  `~/.config/nvcheckup/nvcheckup-changes.json` on Linux). `fix` and `undo` accept
  `--journal DIR`; `--out` remains as a deprecated alias.
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
- Experimental Rust companion under `rust/` (partial port, not built in CI).
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
