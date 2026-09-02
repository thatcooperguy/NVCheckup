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
internal/analyzer/    rules that turn collected data into findings
internal/remediate/   opt-in fixes: preview, elevation check, journal, undo
internal/redact/      PII redaction
internal/report/      text, JSON, markdown output
pkg/types/            shared structs; append-only, other packages depend on them
knowledge/            reference JSON (rules, Xid codes, remediations)
rust/                 experimental partial port, not built in CI
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
6. **Never interpolate untrusted values into a shell string.** Pass them as
   arguments. Environment variables such as `CUDA_PATH` are untrusted.
7. **Think about redaction.** If the output can contain a username, hostname, or
   IP, make sure the field is covered by `internal/redact`.
8. **Add the data to the report** (text, JSON, markdown) and to the JSON schema
   block in `PRODUCT.md`.

## Adding an analyzer rule

Rules live in `internal/analyzer/analyzer.go` and produce `types.Finding`.

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
   to it.
7. **Add a test** in `internal/analyzer` that builds a `Snapshot` fixture,
   triggers the rule, and asserts id, severity, and that the normal case does not
   fire.
8. **Update the docs**: the rule category summary in `PRODUCT.md`, and
   `knowledge/rules.json` if you want the Rust companion to know about it.

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
   registry write that `Apply` will perform. `--dry-run` must execute nothing.
6. **Use the `Executor` interface** for every command so tests can substitute a
   fake and assert the argv without touching the machine.
7. **Test** apply, undo, undo-when-absent, and a tampered journal entry with the
   fake executor. Mark tests that need a real elevated shell with a skip.
8. **Document it** in the `fix` table in `README.md` and in
   `knowledge/remediations.json`.

## Pull request checklist

- [ ] `gofmt -l .` prints nothing; `go vet ./...` is clean for both GOOS values.
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] New parse logic is a pure function with a fixture test.
- [ ] New findings have a stable id, a severity you can defend, and a test.
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
