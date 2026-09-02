# Changelog

All notable changes to NVCheckup are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet.

## [0.2.3] - 2026-09-02

DGX Spark, RTX Spark and unified-memory support, specified in `docs/roadmap/spark-support.md` (v2.1) and
implemented from NVIDIA's public documentation and community field reports. Everything in this block is
exercised against a simulated GB10 in CI and is **unverified on hardware**; a real capture is tracked in
issue #3 (`scripts/spark-capture.sh`).

### Added
- Platform detection (`common.DetectPlatform` in phase 1, `common.ApplyPlatformFlags` after phase 3) and the `PlatformInfo` type, emitted as `platform` in `report.json`: classes `dgx-spark` (Founders Edition and OEM GB10), `rtx-spark`, `jetson`, `grace-hopper`, `arm64-dgpu`. Classification from `/etc/dgx-release`, `/etc/fastos-release`, `lspci` PCI ids (`10de:2e12`, `10de:2e03`, `10de:2e06`), DMI, the kernel flavour and, on Windows, `IsWow64Process2` and PNP ids; PCI ids are evaluated before any compute-capability heuristic. Flag rules set `unified_memory`, `gpus[].on_package`, `gpus[].memory_reporting` and `pcie.on_package` for every GPU whatever detection row matched, and any NVIDIA GPU whose `memory.total` is `[N/A]` is treated as unified memory even without a class. arm64 CPU models come from `lscpu` (with a MIDR fallback) because `/proc/cpuinfo` has no `model name` there.
- Unified memory collector (`unified_memory` in `report.json`, only when `platform.unified_memory`): `/proc/meminfo`, `/proc/swaps` incl. zram, `vm.swappiness`, `/proc/pressure/memory`, `/proc/vmstat` swap-in deltas, GPU-process / OOM / `NV_ERR_NO_MEMORY` counts, and `allocatable_kb = MemAvailable + SwapFree` (huge pages override) per NVIDIA's guidance. On GB10 the thermal collector also parses `nvidia-smi -q -d PERFORMANCE` "Clocks Event Reasons Counters" into `thermal.event_counters_us`, and `thermal.power_limit_supported` records a `[N/A]` power limit.
- DGX OS collector (`dgx_os`, `dgx-spark` only): release and OTA fields, `nvidia-spark-ota-check summary` / `torn-score`, driver / firmware / kernel-module package pairing (`dpkg-query -W`, `dpkg -l` fallback), DGX Dashboard, fwupd, persistenced and clock-cap unit states (`systemctl is-active`, `show -p LoadState`), the dashboard port judged from `/proc/net/tcp{,6}` `LISTEN` rows on 11000 (no connect), `fwupdmgr get-devices` / `get-upgrades` (dotted and hex versions), the container-toolkit apt source, previous-boot classification from `journalctl --list-boots` and message-only (`-o cat`) tails, `/sys/fs/pstore`, ACPI thermal zones, the GDM sleep policy and suspend markers.
- ConnectX-7 collector (`cluster`, `dgx-spark` only, `ai` and `full` modes): 15b3 functions, `/sys/class/infiniband` ports and their netdevs, operstate / speed / MTU / IPv4, cage grouping by function index across PCI domains `0000` and `0002`, bonds, `/etc/nvidia/cx7-hotplug-enabled`, netplan keys, the `NCCL_*` / `UCX_NET_DEVICES` environment of the NVCheckup process, `libnccl.so.2`, avahi state and conflicts, `/etc/ufw/ufw.conf`, RDMA tools. No packets are sent.
- Ecosystem collector (`ecosystem`, Spark platforms, `ai` / `creator` / `full`): PyTorch probe stderr and `torch.cuda.get_arch_list()` (`ai.pytorch_info.warnings`, `arch_list`), Triton `ptxas` version and `TRITON_PTXAS_PATH`, `libcudart.so.12` / `.13`, `flash_attn` and `onnxruntime` with ORT providers, Docker image architectures and tags, `daemon.json` runtimes and CDI, `/etc/cdi/nvidia.yaml`, snap Docker, and listening inference ports (8000, 30000, 11434, 8355, 11000, 7474) without process names.
- Windows on Arm collector (`rtx-spark`): `IsWow64Process2` native and process machine, `PROCESSOR_ARCHITECTURE` / `ARCHITEW6432`, `Win32_Processor.Architecture`, `Win32_ComputerSystemProduct`, PNP `DEV_2E03` / `DEV_2E06`, driver INF, `dxdiag` dedicated vs shared memory, `nvcc.exe` PE machine type, Windows build number. A missing `nvidia-smi.exe` on RTX Spark is INFO.
- 51 analyzer rules for Spark and unified-memory platforms (`analyzer_spark.go`, `analyzer_cluster.go`, `analyzer_woa.go`), catalogued in `docs/roadmap/spark-rules.json` and mirrored in `knowledge/rules.json` with `modes`, `platforms` and `impact`: platform detection (`dgx-spark-detected`, `rtx-spark-detected`, `grace-hopper-detected`), unified memory (`unified-memory-nvsmi-expected`, `-pressure`, `-swap-in-use`, `-page-cache-hold`, `-oom-events`), DGX OS pairing and updates (`dgx-spark-gsp-init-failure`, `-ota-torn`, `-driver-too-old`, `-driver-branch-unsupported`, `-foreign-driver-packages`, `-cublas-batch-bug`, `-non-nvidia-kernel`, `-ota-outdated`, `-dashboard-unhealthy`, `-firmware-behind`), GB10 power and thermal (`gb10-pd-power-wedge`, `gb10-logless-hard-poweroff`, `gb10-acpi-thermal-zone-hot`, `gb10-clock-cap-active`, `dgx-spark-suspend-failure`, `dgx-spark-cx7-slot-power-benign`), sm_121 and Arm64 software (`arm64-cuda12-wheel-on-cuda13`, `sm121-*`, `arm64-flash-attn-no-wheel`, `arm64-container-amd64-image`, `docker-snap-gpu-blocked`, `docker-cdi-spec-missing`, `onnxruntime-cuda-provider-missing`, `gb10-k8s-device-plugin-old`), ConnectX-7 clustering (`cx7-*`, `nccl-env-misconfigured`, `nccl-gdr-assumed`) and Windows on Arm (`rtx-spark-driver-developer-preview`, `woa-*`, `wsl-linux-driver-installed`, `rtx-spark-linux-unsupported`).
- `Finding.Impact` (`findings[].impact`, `omitempty`; `schema_version` stays `"1"`): `none`, `reversible`, `persistent`, `irreversible` or `data-loss`, the most invasive next step of the rule. Every Spark finding carries one; a knowledge test enforces the five values and the closed `platforms` set (`dgx-spark`, `rtx-spark`, `jetson`, `grace-hopper`, `arm64-dgpu`, `all`).
- Advisory rendering: next steps that would change driver, firmware, kernel, swap, systemd, firewall, Secure Boot, snap or netplan state start with `Advisory:` and carry the exact revert command or a data-loss warning. The text and markdown renderers print them with their own marker, after every read-only step, and show the impact next to the severity. They are advice; NVCheckup does not run them and ships no new `fix` action.
- `nvcheckup llm-plan`: LLM deployment sizing for unified-memory hosts from the measured pool (`MemTotal` / `TotalVisibleMemorySize`, never "128 GB"). Model shapes in `knowledge/models.json` (ids printed by `--list-models`, e.g. `llama-3.1-8b-instruct`, `llama-3.3-70b-instruct`; the seven spec 7.3 shapes: Llama 3.1 8B, Llama 3.3 70B, Qwen3-32B, Qwen3-235B-A22B, gpt-oss-120b / 20b, Nemotron-3-Super-120B-A12B), a hand-described shape or a local `--hf-config config.json`; `--profile chat|agent|batch|rag` defaults for context and concurrency; weights, KV cache, runtime reserve and OS floor arithmetic; both decode ceilings (weights-only and at-context) with a realism band; vLLM / TensorRT-LLM / SGLang / llama.cpp / Ollama command templates; a PASS / WARN / FAIL prerequisite table from the read-only collectors or from a saved `--report report.json`; `plan.txt`, `plan.json` (`--json`) and `plan.md` (`--md`) under `--out`; exit codes 0 fits, 1 fits with warnings, 2 does not fit, 3 error. It never downloads, starts, stops, edits or locks anything. `doctor` asks a hand-off question on GB10 / N1X hosts.
- `windows/arm64` build target (`nvcheckup-windows-arm64.exe`) in the release matrix, attestation subjects and download table; CI runs tests and `self-test` on `windows-11-arm` and cross-vets `GOOS=windows GOARCH=arm64`.
- Simulated GB10 CI job (`linux-fieldtest-sim.yml`, `ubuntu-24.04-arm` and `ubuntu-24.04`): the `gb10` scenario extends the shims (`nvidia-smi` with `compute_cap`, `[N/A]` memory and `Not Supported` in the table, `--query-compute-apps`, `-q -d PERFORMANCE`; `lspci`, `dpkg`, `lsmod`, `dmesg` from scenario lines; new `dmidecode`, `lscpu`, `uname`, `dpkg-query`, `systemctl`, `ip`, `ibstat`, `ibdev2netdev`, `fwupdmgr`, `nvidia-spark-ota-check`) and `scripts/make-simroot.sh` generates the committed `.github/fieldtest/simroot/{gb10,gb10-gsp-fail}` trees (`/etc/dgx-release`, `/etc/fastos-release`, `/etc/os-release`, DMI, meminfo, cpuinfo, swaps, vmstat, PSI, `/proc/net/tcp`, thermal, pstore, ConnectX-7 sysfs, ufw) from the same scenario; CI regenerates them and fails when they are stale. Asserts the expected and forbidden finding sets, `Platform: DGX Spark` and `Unified memory` in the summary, `platform.class == "dgx-spark"`, `pcie.on_package == true`, `gpus[0].memory_reporting == "not-supported"`, `unified_memory.mem_total_kb == 125513944` and an impact on every finding. The `gb10-gsp-fail` variant must yield `dgx-spark-gsp-init-failure` and not `no-nvidia-gpu`.
- Simulation contract: with `NVC_SIM_ROOT` set, every absolute path a collector reads is prefixed through one helper; commands still resolve via `PATH` so shims can answer.
- `scripts/spark-capture.sh`: read-only, redacted field kit capturing the fixture set of spec section 12 from a DGX Spark, OEM GB10 or RTX Spark for issue #3.
- Xid 120 ("GSP task exception") in `knowledge/xid_codes.json`; Xid 119 gains the GB10 driver / firmware pairing context.
- Redaction of `DGX_SERIAL_NUMBER` (`<serial>`), fabric LAN addresses and `spark-xxxx` default hostnames; `snapshot`/`compare` diff `platform.class`, `ota_version`, `mem_total_kb` and firmware versions; `self-test` runs the tolerant `index,compute_cap` query and, on GB10, reports memory `[N/A]` as expected.
- Documentation: README section "DGX Spark, RTX Spark and unified memory" with `docs/assets/unified-memory.svg`, the `llm-plan` command reference and two FAQ entries; PRODUCT.md platform, data-collected, rule, `report.json` and design-decision updates; CONTRIBUTING sections on scenarios and shims, the `NVC_SIM_ROOT` contract, rules with `impact` and `Advisory:` steps, and `knowledge/models.json`; `examples/sample-report-dgx-spark.txt` (from the simulated scenario) and `examples/sample-llm-plan.txt` (the spec's 8B worked example); a Spark card on the landing page.
- Integration of the Spark work packages (2026-09-02): `dgx_os.units_queried` records whether `systemctl` answered for the DGX OS units; when it is `false` the `*_active` booleans are unknown and `dgx-spark-dashboard-unhealthy` is not raised (the analyzer never reports units it could not query). Still pending on the collector integration branch (see `docs/roadmap/spark-work-packages.md`): wiring `linux.CollectDGXOS` / `CollectDGXHostState`, `CollectCluster`, `CollectNVRMMessages`, `ai.CollectEcosystem` and `windows.CollectWoA` into the phase-4 hook `internal/core/runner_platform.go`, which is still the stub; until then `dgx_os` carries only the release-file fields and `cluster` / `ecosystem` are absent. `nvcheckup llm-plan` gained `--list-models`, `--profile chat|agent|batch|rag` (context and concurrency defaults) and `--report FILE` (plan offline from a saved `report.json`); model ids are the `--list-models` spellings (`llama-3.1-8b-instruct`, ...).
- Simulated GB10 CI, integration pass: new read-only `journalctl` (boot table, per-boot tails, `-k`, `-u`, `-g`) and `ldconfig -p` shims, `systemctl show -p LoadState [--value]`, the DGX OS default hostname `spark-0f01` in journal lines to exercise its redaction; `scripts/make-simroot.sh` also writes `/etc/netplan/*.yaml` (cage-0 addresses, `mtu: 9000`), `/etc/nvidia/cx7-hotplug-enabled`, `/etc/docker/daemon.json` + `/etc/cdi/nvidia.yaml`, the container-toolkit apt source, `/sys/class/net/<if>/address` and an empty `/sys/firmware/efi`. The `gb10` job now asserts `dgx_os.units_queried == true` with no `dgx-spark-dashboard-unhealthy`, the OTA / package pairing fields, a clean previous boot and empty pstore, the three firmware components, four ConnectX-7 ports with exactly the two cage-0 twins `ACTIVE` at 200000 Mb/s / MTU 9000 and none of the `cx7-*` / `nccl-*` findings, an `ecosystem` section that may be absent on a runner without torch, no collector error mentioning `panic`, and redaction of the serial, both hostnames and the fabric addresses; the llm-plan step runs the real CLI offline (`--report gb10/report.json --model llama-3.1-8b-instruct --profile agent --runtime vllm --json`) and checks `fit.total_gib == 51.0`, `fit.gpu_memory_utilization == 0.40` and the 119.7 GiB pool, skipping with a notice only when the binary has no `llm-plan` subcommand. `gb10-gsp-fail` additionally asserts `ota_failed` contains `driver` and that the SEC2 / GSP lines reach `linux.gsp_failure_lines`. `scripts/spark-capture.sh` captures the remaining unconfirmed forms (`fwupdmgr get-upgrades` text and JSON with the collector's offline flags, `nvidia-spark-ota-check` raw output and exit codes with and without sudo, a standalone `nvidia-smi -q -d PERFORMANCE`, `--query-compute-apps=pid`, `journalctl --list-boots` and the previous boot's `-o cat -n 30` tail, `/proc/sys/kernel/osrelease`, `ibstat -l`, `ip -4 -o addr show`, `systemctl show -p LoadState`, `daemon.json`, the apt source, the `ldconfig -p` header).
- `linux-fieldtest-sim.yml` workflow: simulated `nvidia-smi`, `lspci`, `lsmod`, `modinfo` and `dmesg` shims (`.github/fieldtest/shims`) give the Ubuntu x86_64 and ARM64 runners a three-GPU rig and a Jetson, so per-GPU collection, multi-GPU attribution, Xid parsing, the `blacklist-nouveau` driver gate and Jetson detection run end to end on real Linux without hardware.
- `scripts/linux-fieldtest.sh`: one-command field kit for a real Linux box or Jetson. Downloads and checksum-verifies the release binary for the CPU, runs everything read-only (fixes in `--dry-run` only) and tars a redacted bundle to attach to issue #2.

### Changed
- Summary block on unified-memory platforms: `VRAM: N MB` becomes `Unified memory: 119.7 GiB total, 115.9 GiB available`, a `Platform:` line names the class, vendor, model and DGX OS / OTA version, and the PCIe line reads `PCIe: n/a (on-package, NVLink-C2C)`. The text report gains `PLATFORM`, `UNIFIED MEMORY`, `DGX OS`, `FIRMWARE` and `CLUSTER FABRIC` sections; `report.json` gains `platform`, `unified_memory`, `dgx_os`, `cluster` and `ecosystem`.
- On unified memory the discrete-GPU rules step aside: `low-vram` is never emitted; `pcie-downshift`, `pcie-width-reduced` and `pcie-idle-power-saving` skip on-package GPUs; `gpu-power-cap`, `gpu-clock-slowdown` and `gpu-power-state-stuck` print `limit N/A (unified memory)` and yield to `gb10-pd-power-wedge`; `no-nvidia-gpu` and `driver-not-detected` are not emitted when `dgx-spark-gsp-init-failure` explains the absence; `nvidia-smi-missing` and `driver-not-detected` are INFO on `rtx-spark` without `nvidia-smi.exe`; `nvidia-app-detected` is not expected on Windows ARM64.
- `jetson-detected` wording covers Jetson Thor, which ships `nvidia-smi`; Jetson is recognised from `/etc/nv_tegra_release` and the device-tree model, not from a missing `nvidia-smi`.
- `nvidia-smi` parsing: `isNotAvailable` accepts a bare `Not Supported`; the full BDF (domain `000f`) is kept in `pci_bus_id`; `No devices were found` with `lspci` `10de:2e12` present is explained as a GB10 GSP initialisation failure.
- `TestRulesJSON_MatchesAnalyzer` scans every non-test `.go` file in `internal/analyzer`, so rules may live in `analyzer_*.go`.
- `doctor`: a seventh question, "Plan an LLM deployment for this machine?", on GB10 / N1X hosts.
- Thermal: when the only active slowdown reason is `sw_power_cap` (the GPU sitting at its configured power limit under load, observed live on an RTX 3090 at 99% utilisation), the report shows the INFO finding `gpu-power-cap` instead of the WARN `gpu-clock-slowdown`. Hardware slowdown and power-brake reasons remain a WARN.

### Fixed
- A GB10 (DGX Spark) would have produced `low-vram`, `pcie-width-reduced` (the link is reported as `GEN 1@ 1x`), a wrong `VRAM:` summary line and, with a dead GSP, `no-nvidia-gpu` instead of the pairing diagnosis. Found by the spec review of `nvidia-smi` output in public field reports, not on hardware; the simulated `gb10` job now asserts none of them fire.
- arm64 systems reported an empty CPU model because `/proc/cpuinfo` carries no `model name` there; `lscpu` is used with a MIDR fallback.

## [0.2.2] - 2026-09-02

A hardening release driven by the first automated Linux runs and a redesigned README.

### Removed
- The experimental Rust companion (`rust/`). It implemented 7 of the analyzer's rules, was never built in CI or shipped, and had already diverged from the Go analyzer. The knowledge pack under `knowledge/` stays as a reference for contributors and external tooling. The tree is in git history at tag `v0.2.1` if anyone wants to revive it.

### Fixed
- Network: all ping probes lost while DNS resolution works is now reported as INFO `icmp-filtered` (a firewall, VPN or cloud network dropping ICMP) instead of a packet-loss WARN. Found by the first Linux field-test run: GitHub's runners block outbound ICMP.

### Changed
- README redesigned: banner, terminal rendering of the summary block and a pipeline diagram (`docs/assets/*.svg`), badge row with release and Linux field-test status, collapsible long lists. Every technical claim was re-checked against the code.

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
