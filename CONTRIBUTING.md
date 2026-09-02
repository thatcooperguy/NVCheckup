# Contributing to NVCheckup

Thanks for helping. NVCheckup is a small, standard-library-only Go program, and
the bar for a change is simple: it must be correct on a machine you cannot see,
it must never surprise the user, and it must be testable without a GPU.

## Development setup

- Go 1.22 or newer. No other dependencies; there is no `go.sum` on purpose.
- Clone and build:

  ```bash
  git clone https://github.com/thatcooperguy/NVCheckup.git
  cd NVCheckup
  go build ./cmd/nvcheckup
  ```

- Before every push, run the same checks CI runs:

  ```bash
  gofmt -l .            # must print nothing
  go vet ./...
  go build ./...
  go test ./...
  GOOS=linux   go vet ./... && GOOS=linux   go build ./cmd/nvcheckup
  GOOS=windows go vet ./... && GOOS=windows go build ./cmd/nvcheckup
  GOOS=linux   GOARCH=arm64 go build ./...      # DGX Spark
  GOOS=windows GOARCH=arm64 go build ./...      # RTX Spark
  GOOS=darwin  GOARCH=arm64 go build ./...      # must keep building: use build tags or runtime.GOOS checks
  ```

- One test runs the real collectors end to end
  (`internal/snapshot.TestCreate_RedactedSnapshotHasNoIdentity`, about 30 s).
  It is skipped unless you set `NVCHECKUP_LIVE_TESTS=1`; run it on a machine
  with an NVIDIA GPU before a release. `go test -short ./...` skips it as well.
- `go test -race` needs a C toolchain. On Windows that usually means it does not
  work out of the box; run it on Linux or under WSL instead. CI runs the race
  detector on Ubuntu only.
- Platform-specific files carry build tags (`//go:build windows`,
  `//go:build linux`). Cross-vetting both GOOS values catches the classic mistake
  of referencing a symbol that only exists on one platform.

## Project layout

```
cmd/nvcheckup/        CLI entry point and flag parsing
internal/core/        7-phase pipeline: collect, analyze, redact, report
internal/collector/   common, windows, linux, wsl, ai
internal/analyzer/    rules that turn collected data into findings (analyzer.go, analyzer_spark.go, analyzer_cluster.go, analyzer_woa.go)
internal/remediate/   opt-in fixes: preview, elevation check, journal, undo
internal/redact/      PII redaction
internal/report/      text, JSON, markdown output
internal/llmplan/     nvcheckup llm-plan: sizing, model shapes, runtime templates, prerequisites, rendering
pkg/types/            shared structs; append-only, other packages depend on them
knowledge/            reference JSON (rules, Xid codes, remediations, LLM model shapes)
.github/fieldtest/    simulated-GPU scenarios and the shims that answer them
docs/roadmap/         the Spark specification, rule catalogue and work packages
```

## Adding a collector

Collectors run external commands and turn their output into `pkg/types` structs.
They must degrade gracefully: a missing tool or a rejected query is a
`CollectorError`, never a panic and never a failed run.

1. **Split "run" from "parse".** Put the `exec` call in a thin function and the
   parsing in a pure function that takes a string (or `[]byte`) and returns the
   struct. For example `CollectThermalInfo` calls `nvidia-smi` and hands the
   output to `parseThermalCSV`. Pure functions are what the tests exercise.
2. **Capture a fixture.** Save real output from the tool (redacted if needed) as a
   string constant or a file under `testdata/`. Include at least one odd case:
   `[N/A]`, `[Not Supported]`, an empty result, a localized header.
3. **Write the fixture test** against the pure parse function. Assert the fields
   you care about, and assert that malformed input yields an error or a zero
   value rather than garbage.
4. **Run the query in `self-test`** if the collector depends on a specific field
   list or command flag. The 0.2.0 thermal collector shipped with a field name
   `nvidia-smi` rejects; `self-test` now catches that class of bug on the user's
   machine.
5. **Respect the timeout.** Use `util.RunCommand(timeout, ...)`; never call
   `exec.Command` directly.
5b. **Honour `NVC_SIM_ROOT`.** Every absolute file path a collector reads
   (`/etc/...`, `/proc/...`, `/sys/...`, `/var/...`, `/run/...`) goes through the
   one small path helper that prefixes it with `NVC_SIM_ROOT` when the variable
   is set. Commands are resolved through `PATH` as usual so the shims can answer.
   See "The NVC_SIM_ROOT contract" below.
5c. **Never guess hardware facts.** Everything about DGX Spark, RTX Spark, GB10,
   N1X and unified memory comes from `docs/roadmap/spark-support.md` and
   `docs/roadmap/spark-rules.json`. Put a comment naming the spec section next
   to every threshold, string or command you take from it, and put values the
   spec marks `unconfirmed` or `assumption` behind a clearly named constant with
   a comment saying so.
6. **Never interpolate untrusted values into a shell string.** Pass them as
   arguments. Environment variables such as `CUDA_PATH` are untrusted.
7. **Think about redaction.** If the output can contain a username, hostname, or
   IP, make sure the field is covered by `internal/redact`.
8. **Add the data to the report** (text, JSON, markdown) and to the JSON schema
   block in `PRODUCT.md`.

## Testing on real Linux hardware

If you have a Linux machine with an NVIDIA GPU (or a Jetson), `scripts/linux-fieldtest.sh` does the whole run for you: it downloads the release binary for your CPU, verifies the checksum, runs `self-test`, `run`, `snapshot` and the `fix` catalog in `--dry-run` only, and tars a redacted bundle you can attach to [issue #2](https://github.com/thatcooperguy/NVCheckup/issues/2). Nothing it does changes your system.

CI approximates this with `.github/workflows/linux-fieldtest-sim.yml`: shims in `.github/fieldtest/shims` answer the exact `nvidia-smi`, `lspci`, `lsmod`, `modinfo`, `dmesg`, `dpkg`, `dmidecode`, `lscpu`, `ibdev2netdev`, `fwupdmgr` and `nvidia-spark-ota-check` queries the collectors make from a scenario file such as `.github/fieldtest/scenarios/rig3.json` or `gb10.json`. Add a scenario there to reproduce a machine you have seen.

## Testing on DGX Spark, OEM GB10 or RTX Spark hardware

The whole Spark feature set was implemented from public documentation and community field reports and is exercised only against the simulated `gb10` scenario. Nothing has run on a GB10 or N1X yet, and the open questions in spec section 12 can only be answered by one. If you have the hardware, run `scripts/spark-capture.sh` (read-only, redacted) and attach the bundle to [issue #3](https://github.com/thatcooperguy/NVCheckup/issues/3). It captures exactly the fixture wish-list of spec section 12:

```bash
cat /etc/dgx-release /etc/fastos-release
sudo dmidecode -s system-manufacturer -s system-product-name -s system-version -s bios-version
lscpu
grep -E 'MemTotal|MemAvailable|Swap' /proc/meminfo
lspci -nn -d 10de: ; lspci -nn -d 15b3:
dpkg -l 'nvidia-dkms*' 'nvidia-driver*'
nvidia-smi -L
# every query-field list the collectors use (GPUQueryFields, ThermalQueryFields, ThermalEventQueryFields,
# PCIeQueryFields and GPUCapQueryFields = index,compute_cap), plus clocks.max.graphics
nvidia-smi -q -d MEMORY,PERFORMANCE,CLOCK,POWER,TEMPERATURE
sudo nvidia-spark-ota-check summary
fwupdmgr get-devices
ibdev2netdev ; ip -br addr
ls /proc/device-tree/model
# RTX Spark (PowerShell):
# Get-CimInstance Win32_VideoController | fl Name,PNPDeviceID,DriverVersion,InfFilename,AdapterRAM
# (Get-CimInstance Win32_Processor).Name ; nvidia-smi   # if present
```

The things we most want to see verbatim: the `nvidia-smi -L` line and the CSV value of every queried field on GB10 (`pstate` and clocks at idle, `pcie.link.gen.max` / `width.max`, `clocks_event_reasons.active` idle and under load, `compute_cap`, `power.draw` formatting); whether `/proc/device-tree/model` exists; the `fwupdmgr get-devices` names and version formats (to confirm `0x03000508` = 3.5.8); whether a stock DGX OS install carries `nvidia-dkms-580-open`; the default swap configuration; exact `ibstat` / `/sys/class/infiniband/*/ports/1/rate` strings; and on RTX Spark whether `nvidia-smi.exe` ships at all and what it prints for memory. The scenario file's `description` says which of its values are placeholders; a capture replaces them.

## Adding a simulated scenario and shim

Scenarios are JSON files under `.github/fieldtest/scenarios/`. A scenario describes one machine: the GPU rows (`index`, `name`, `uuid`, `driver_version`, `pci.bus_id`, the `memory.*`, `temperature.gpu`, `power.*`, `pstate`, `clocks.*`, `fan.speed`, `utilization.gpu`, `pcie.link.*`, `clocks_event_reasons.active`, `compute_cap` fields the shim answers per query field, plus `compute_apps` and `performance_counters_us`), line lists that the other shims replay verbatim (`lspci_lines`, `dpkg_lines`, `lsmod_lines`, `dmesg_lines`, `lscpu_lines`, `cpuinfo_lines`, `ldconfig_libs`), structured blocks for `dmi`, `meminfo`, `swaps`, `pressure_memory`, `thermal_zones`, `listening_tcp_ports`, `cx7_ports` and `other_netdevs` (one source for `ibdev2netdev`, `ibstat`, `ip` and the sysfs tree), `systemd_units`, `fwupd_devices` / `fwupd_updates`, `spark_ota_*`, `journal_boots` / `journal_units` / `hostname` (the `journalctl` shim), `netplan`, `cx7_hotplug_enabled`, `docker`, `apt_sources`, and the file contents written under `NVC_SIM_ROOT` (`dgx_release`, `fastos_release`, `os_release`, `kernel`, `device_tree_model`). Look at `gb10.json` for the full shape (its `description` names every placeholder) and at `rig3.json` for a plain multi-GPU rig.

1. **Start from a capture.** Copy real command output into the line lists; use `[N/A]`, `Not Supported` and odd formatting exactly as the tool printed them. Mark anything you had to invent in the scenario's `description` so a later capture can replace it.
2. **Shims answer only what the collectors ask.** Each shim in `.github/fieldtest/shims/` is a small script that reads the scenario named by `NVC_SIM_SCENARIO` and prints the answer for the exact arguments the collector passes (`nvidia-smi --query-gpu=<fields> --format=csv,noheader,nounits`, `lspci -nn`, `dpkg-query -W -f ...`, `systemctl is-active` / `show -p LoadState --value`, `journalctl --list-boots` / `-b -1 -o cat -n 30` / `-k -b -g ...`, `ldconfig -p`, `ip -4 -o addr show`, `fwupdmgr get-devices` / `get-upgrades`, ...). A shim never calls the tool it replaces and refuses every state-changing form (`systemctl start`, `ldconfig` without `-p`, `journalctl --vacuum-*`, `ip link set`, `fwupdmgr update`, ...). When you add a collector that runs a new command, add a shim for it and a scenario key it reads (`grep -rn "RunCommand(" internal/collector` lists every command the collectors run); when you add a query field, teach the `nvidia-smi` shim to answer it and print `[N/A]` verbatim where the hardware would.
3. **Files go under `NVC_SIM_ROOT`.** `scripts/make-simroot.sh <scenario.json> <outdir>` writes the file side of a scenario (`/etc/dgx-release`, `/etc/fastos-release`, `/etc/os-release`, `/etc/ufw/ufw.conf`, `/etc/netplan/*.yaml`, `/etc/nvidia/cx7-hotplug-enabled`, `/etc/docker/daemon.json`, `/etc/cdi/nvidia.yaml`, `/etc/apt/sources.list.d/*`, `/proc/{meminfo,cpuinfo,version,swaps,vmstat}`, `/proc/sys/kernel/osrelease`, `/proc/sys/vm/swappiness`, `/proc/pressure/memory`, `/proc/net/tcp`, `/sys/class/dmi/id/*`, `/sys/class/thermal/*`, the ConnectX-7 `/sys/class/infiniband` and `/sys/class/net` trees, an empty `/sys/fs/pstore` and `/sys/firmware/efi`, `/lib/modules/<kernel>/modules.dep`). The trees for `gb10*.json` are committed under `.github/fieldtest/simroot/`; run `scripts/make-simroot.sh --all` after changing a scenario or the generator and commit the result, because CI regenerates them and fails when the committed copy is stale. Anything a collector reads from the filesystem must be provided this way (`grep -rn '"/etc/\|"/proc/\|"/sys/' internal/collector` lists the paths); anything it runs is a shim on `PATH`.
4. **Add a job with assertions.** Each scenario gets a workflow step that runs `self-test` and `run --json`, then asserts the exit code, the expected finding ids present, the forbidden ids absent, the summary lines and the `report.json` fields that prove the platform logic worked. The `gb10` job asserts, among others, `platform.class == "dgx-spark"`, `pcie.on_package == true`, `gpus[0].memory_reporting == "not-supported"`, `unified_memory.mem_total_kb == 125513944`, `dgx_os.units_queried == true` with no `dgx-spark-dashboard-unhealthy`, four ConnectX-7 ports with the two cage-0 twins `ACTIVE`, a non-empty `impact` on every finding and the spec 7.5 numbers from `llm-plan --report gb10/report.json`; the `gb10-gsp-fail` variant asserts `dgx-spark-gsp-init-failure` and no `no-nvidia-gpu`.
5. **Run it locally** before pushing: build the binary, put the shims first on `PATH`, point `NVC_SIM_SCENARIO` at your scenario, generate the fixture root and run: `export PATH="$PWD/.github/fieldtest/shims:$PATH" NVC_SIM_SCENARIO="$PWD/.github/fieldtest/scenarios/gb10.json" NVC_SIM_ROOT="$PWD/.github/fieldtest/simroot/gb10"; ./nvcheckup run --mode full --json --out /tmp/out`. The bash shims need a Python 3 for JSON; on a Windows checkout whose `python3` is the Store stub set `NVC_SIM_PYTHON=python` and run the Python shims as `python .github/fieldtest/shims/nvidia-smi ...`.

### The NVC_SIM_ROOT contract

When the environment variable `NVC_SIM_ROOT` is set, every absolute file path a collector reads (`/etc/...`, `/proc/...`, `/sys/...`, `/var/...`, `/run/...`) is prefixed with it; commands (`nvidia-smi`, `lspci`, `dmidecode`, `lscpu`, `dpkg-query`, `fwupdmgr`, `journalctl`, `systemctl`, ...) are resolved via `PATH` as today so shims can answer. One small helper does the prefixing and every collector uses it; do not open an absolute path directly. The variable is a test hook: it never changes what a collector does, only where it reads from, and collectors stay read-only with or without it. A collector that forgets the helper works on a developer's machine and silently reads the runner's real `/proc/meminfo` in CI, which is exactly the kind of bug the scenario assertions exist to catch (the `gb10` job's `mem_total_kb == 125513944` check is there for that reason).

## Testing against GPUs you do not own

NVCheckup has no model-specific code, but its parsers only stay honest if they
are exercised against real `nvidia-smi` output from more than one card. The
development machine is a single RTX 3090; every other GPU class in the fixtures
came from a captured row. You can add one without writing any Go beyond a test
case.

### Capture the rows

On any machine with an NVIDIA driver, run the exact queries the collectors run.
The field lists are the exported constants `GPUQueryFields`,
`ThermalQueryFields`, `ThermalEventQueryFields` (or
`ThermalEventQueryFieldsLegacy` on drivers before R535) and `PCIeQueryFields` in
`internal/collector/common`; copy them from the source so the fixture matches
the query. As of 0.2.2 that is:

```bash
# GPU inventory (gpu.go)
nvidia-smi --query-gpu=index,driver_version,pci.bus_id,memory.total,memory.free,memory.used,temperature.gpu,power.draw --format=csv,noheader,nounits

# Thermal (thermal.go)
nvidia-smi --query-gpu=index,temperature.gpu,pstate,clocks.current.graphics,clocks.max.graphics,power.limit,power.draw,fan.speed,utilization.gpu --format=csv,noheader,nounits
nvidia-smi --query-gpu=index,clocks_event_reasons.active --format=csv,noheader
# pre-R535 drivers reject the line above; capture this one instead
nvidia-smi --query-gpu=index,clocks_throttle_reasons.active --format=csv,noheader

# PCIe (pcie.go)
nvidia-smi --query-gpu=index,pcie.link.gen.current,pcie.link.gen.max,pcie.link.width.current,pcie.link.width.max,pstate,utilization.gpu --format=csv,noheader,nounits

# The plain table, for gpu_test.go (delete the Processes: section before pasting)
nvidia-smi
```

If you can, capture once at idle and once under load (a game, a training step,
a render, anything). Note the GPU model, driver version, OS, and anything
unusual about the machine: laptop, MIG enabled, passive cooling, riser cable,
x8 slot. `nvcheckup self-test` prints which of these queries the installed
driver rejects, which is itself useful information.

Nothing in these rows identifies you. The PCI bus id and driver version are the
only machine-specific values; there are no hostnames, usernames or serials.

### Drop them into the fixtures

Fixtures are string constants next to the tests, named by package:

| Output | Test file | Parse function under test |
|--------|-----------|---------------------------|
| Thermal rows and event/throttle masks | `internal/collector/common/thermal_test.go` | `parseThermalCSV`, `parseThrottleMask` |
| PCIe rows | `internal/collector/common/pcie_test.go` | `parsePCIeCSV` |
| Inventory rows and the plain table | `internal/collector/common/gpu_test.go` | `stripProcessSection`, `parseGPUList`, `applyGPUQueryRows` |

If `applyGPUQueryRows` is not present on your checkout, the `--query-gpu` row
parsing is still inline in `collectFromNvidiaSmi`; extracting it into a pure
function that takes the CSV text is a good first PR, and the fixture goes in
with it.

Add a constant with a comment that names the GPU, driver and condition
(`// Tesla T4, driver 535.104, passive cooling, idle`), then a test that asserts
the fields that make the card interesting: `FanSupported == false` for a passive
card, `MaxWidth == "x8"` for a laptop, `UtilizationPct` unavailable for a MIG
slice, `MaxSpeed == "Gen5"` for an RTX 50 card. For a multi-GPU capture assert
that every row is parsed and that `GPUIndex` matches the `index` column, not
the line number.

### What already has a fixture

RTX 3090 and RTX 4090 (development hardware), RTX 5090 (Gen5 link), RTX 4060
Laptop (native x8 link), GTX 1060 on a pre-R535 driver (legacy
`clocks_throttle_reasons` field), A100-SXM4 (no fan), H100 in MIG mode
(`[N/A]` utilization), Tesla T4, Quadro RTX 8000, and a 3-GPU rig.

### What we would like

Rows from anything not in that list are welcome, in an issue or a pull
request. Especially useful right now:

- Jetson / Tegra boards: the output of `tegrastats` (a few seconds of it) and
  the contents of `/etc/nv_tegra_release`, so the Jetson path can grow beyond
  detection.
- RTX 50 series laptops (Gen5 plus a native x8 link in one row).
- vGPU / GRID profiles and cloud GPU instances, where several fields come back
  as `[N/A]`.
- Any driver older than R470.

## Adding an analyzer rule

Rules live in `internal/analyzer/` and produce `types.Finding`. General rules are in
`analyzer.go`; Spark, cluster and Windows-on-Arm rules live in `analyzer_spark.go`,
`analyzer_cluster.go` and `analyzer_woa.go`. The lockstep test scans every non-test
`.go` file in the package, so a rule may live in whichever file fits.

1. **Choose a stable id.** Kebab-case, descriptive, never reused for a different
   meaning (`pcie-downshift`, `whea-errors`). If `knowledge/rules.json` already
   has a matching rule, use its id. The id appears in `report.json` as
   `findings[].id` and users script against it.
2. **Pick the severity honestly.** `CRIT` blocks the user; `WARN` is a likely
   contributor; `INFO` is context. When in doubt, go lower. A false CRIT costs
   more trust than a missed WARN.
3. **Set `Confidence`** (0-100). Use it to express how sure the rule is that the
   condition is a problem, not how sure it is that the data is accurate.
4. **Evidence must quote the data.** "Link is Gen1 x16 at 0% utilization (max
   Gen4 x16)" is evidence. "PCIe issue detected" is not.
5. **Ask "when is this normal?"** before you finish. GPUs idle at Gen1. Corrected
   WHEA errors are logged by design. If the normal case exists, either gate the
   rule on it or emit an INFO finding that says so explicitly.
6. **Next steps are safe and reversible**, and ordered from least to most
   invasive. If a `fix` action exists, set `Remediation` so the report can point
   to it. A step that would change driver, firmware, kernel, swap, systemd,
   firewall, Secure Boot, snap or netplan state is not something NVCheckup does;
   if the user genuinely needs it, write it as an **Advisory step**: it starts
   with the word `Advisory` (the renderers and the knowledge test match the
   regex `^Advisory` followed by a word boundary, so `Advisory:` and
   `Advisory: (data loss)` both qualify), names the exact command, and carries
   the exact revert command or an explicit data-loss warning. Advisory steps
   come after every read-only step, never before. Reimaging is a last resort
   and must say that it erases the unit.
6b. **Set `Impact`** to the most invasive of the finding's next steps: `none`,
   `reversible`, `persistent`, `irreversible` or `data-loss`. A finding whose
   steps are all read-only is `none`; one with an Advisory step is at least
   `reversible`; a firmware flash or OTA is `irreversible`; anything that can
   delete data (`snap remove docker`, the System Recovery image) is `data-loss`.
   Every Spark finding must carry one; the renderers print it next to the
   severity and `report.json` emits it as `findings[].impact` (`omitempty`, so
   old reports are unchanged and `schema_version` stays `"1"`).
6c. **Gate on the platform.** Rules that assume discrete VRAM, a fan, a power
   limit or a PCIe link must not fire when `Platform.UnifiedMemory` or the GPU's
   `OnPackage` flag is set; rules written for a platform class check
   `Platform.Class`. Spark rule ids, triggers, evidence templates, impact values
   and next steps come from `docs/roadmap/spark-rules.json`; implement them as
   written and cite the spec section in a comment.
7. **Add a test** in `internal/analyzer` that builds a `Snapshot` fixture,
   triggers the rule, and asserts id, severity, and that the normal case does not
   fire.
8. **Update the docs**: the rule category summary in `PRODUCT.md`, and
   `knowledge/rules.json`; the drift test (`TestRulesJSON_MatchesAnalyzer`) fails until you do.
   A `rules.json` entry carries `id`, `title`, `category`, `severity`,
   `base_confidence`, `modes` (which of `gaming`, `ai`, `creator`, `streaming`,
   `full` run it), `description`, and for platform rules `platforms` (a subset of
   the closed set `dgx-spark`, `rtx-spark`, `jetson`, `grace-hopper`,
   `arm64-dgpu`, `all`, matching the detector's class names) and `impact` (one
   of the five values above). A knowledge test rejects any other `platforms`
   value or `impact` spelling, and checks that steps beginning with `Advisory`
   never precede a read-only step.

## Adding a model shape to knowledge/models.json

`nvcheckup llm-plan` sizes models from the catalogue in `internal/llmplan/models.go`;
`knowledge/models.json` is its reference copy, kept identical by
`TestModelsJSON_MatchesCatalogue` (the binary embeds no file at runtime). Each entry
is one model shape, taken from the model's Hugging Face `config.json` (spec section
7.3): the id and aliases the `--model` flag accepts (`--list-models` prints them),
total and active parameters, `num_hidden_layers`, `num_key_value_heads`, `head_dim`
(or `hidden_size / num_attention_heads`), `num_local_experts` /
`num_experts_per_tok` for MoE models, `sliding_window` where some layers use one, a
measured checkpoint size per quantization when one is published (the wizard prefers
it to the bytes-per-parameter formula), and the source URL of every number. Add the
shape to the Go catalogue, then regenerate the JSON and the golden plans:

```
go test ./internal/llmplan -run TestModelsJSON -update-models   # rewrites knowledge/models.json
go test ./internal/llmplan -update                              # rewrites the golden plan files
go test ./internal/llmplan ./cmd/nvcheckup
```

1. **Take the numbers from `config.json`**, not from a blog post. Record the
   URL. If a value has a single source (as the Nemotron-3-Super Mamba state term
   does), say so in the entry.
2. **Compute the KV bytes per token by hand** (`2 x layers x kv_heads x head_dim x 2`
   for f16) and check it against the table in spec section 7.3 before you trust
   the entry; a wrong `head_dim` silently doubles the KV estimate.
3. **Add a sizing test** in `internal/llmplan` that reproduces a worked example
   for the new shape to +/-0.1 GiB, the way the existing tests reproduce the
   three examples of spec section 7.5 and the 17 / 13.4 and 6.9 / 3.3 tok/s
   ceilings.
4. **Never encode a memory tier.** The pool is always the measured `MemTotal` /
   `TotalVisibleMemorySize`; a model entry must not carry "fits on 128 GB"
   style flags.
5. **`--hf-config` is the escape hatch.** If a model is too niche for the
   knowledge pack, users can size it from a local `config.json` offline; prefer
   that to adding every model under the sun.

## Adding a remediation action

Actions live in `internal/remediate/actions_<os>.go`. They are the only code in
the project that changes the user's system, so they get the most scrutiny.

1. **Justify it.** An action must fix a condition an analyzer rule detects, and
   the manual steps must be something we would otherwise put in "Next Steps".
2. **Declare risk and elevation.** `Risk` is `low` (a user setting with no
   side-effects), `medium` (system-wide or needs a reboot), or `high` (we have not
   shipped one; think hard). Set `NeedsAdmin` truthfully; the engine checks
   elevation before prompting.
3. **Capture the previous state before changing anything** and store it in the
   journal entry. If the setting did not exist, record that fact; undo must then
   remove the setting, not write a made-up default.
4. **Implement undo** from the journal entry only. Undo must validate the entry
   (expected keys, value shape, path within the allowed location) before writing.
   The journal is user-writable; never copy its contents verbatim into a
   privileged path or command.
5. **Make preview truthful.** `Preview` should print the exact command or
   registry write that `Apply` will perform. `--dry-run` may run only the
   action's read-only capture commands (for example `reg query`,
   `powercfg /getactivescheme`, `modinfo`, a package listing) and must change
   nothing.
6. **Use the `Executor` interface** for every command so tests can substitute a
   fake and assert the argv without touching the machine.
7. **Test** apply, undo, undo-when-absent, and a tampered journal entry with the
   fake executor. Mark tests that need a real elevated shell with a skip.
8. **Document it** in the `fix` table in `README.md` and in
   `knowledge/remediations.json`.

## Pull request checklist

- [ ] `gofmt -l .` prints nothing; `go vet ./...` is clean for both GOOS values.
- [ ] `go build ./...` and `go test ./...` pass, and the cross-builds for
      `linux/arm64`, `windows/arm64`, `linux/amd64` and `darwin/arm64` still build.
- [ ] New parse logic is a pure function with a fixture test.
- [ ] New `nvidia-smi` parsing handles every row, carries the `index`, and is
      tested against at least one GPU class other than the one you own.
- [ ] New findings have a stable id, a severity you can defend, an `impact`, and a test;
      state-changing next steps are `Advisory:` lines with a revert command, after the read-only steps.
- [ ] New collectors read files through the `NVC_SIM_ROOT` helper and, if they run a
      new command, come with a shim and a scenario key.
- [ ] Spark / unified-memory facts cite a spec section; unconfirmed values sit behind a named constant.
- [ ] Nothing new runs a network probe unless `--network` or `network-test` asked for it.
- [ ] Nothing new writes to the system outside `internal/remediate`.
- [ ] Untrusted strings (env vars, command output, journal contents) are never
      interpolated into a shell command.
- [ ] Redaction covers any new field that could hold PII.
- [ ] `pkg/types` changes are append-only.
- [ ] `README.md`, `PRODUCT.md`, and `CHANGELOG.md` updated where behaviour changed.
- [ ] Commit message explains *why*, not just what.

## Reporting bugs

Open an issue with the output of `nvcheckup self-test` and, if you can, the
`report.json` from `nvcheckup run --mode full --json`. It is redacted by default.
Please look it over before attaching.

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community expectations and
[SECURITY.md](SECURITY.md) for how to report vulnerabilities privately.
