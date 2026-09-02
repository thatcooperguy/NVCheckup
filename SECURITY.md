# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.2.x   | Yes                |
| 0.1.x   | No (upgrade)       |

## Reporting a Vulnerability

If you discover a security vulnerability in NVCheckup, please report it responsibly.

**Do not open a public issue for security vulnerabilities.**

Instead, please email the maintainers or use GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](../../security) of this repository.
2. Click "Report a vulnerability."
3. Provide a clear description of the issue, steps to reproduce, and potential impact.

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Scope

NVCheckup is a diagnostic tool that runs locally and never uploads data. Security concerns most relevant to this project include:

- **Information disclosure:** Ensuring redaction works correctly and PII is not leaked in reports or snapshots. Redaction is on by default for both `run` and `snapshot`; the `nvidia-smi` process list is stripped before storage.
- **Command injection:** Ensuring user-controlled or environment-controlled input (for example `CUDA_PATH`, command output, file names) cannot be injected into system commands. Values are passed as arguments, never interpolated into shell strings. Python probes run with `python -I`.
- **Path traversal:** Ensuring output files are written only to intended directories.
- **`nvcheckup fix` (privileged writes):** `fix` is the only command that writes to the system. It runs only after an interactive confirmation, checks elevation before prompting, and records every change in a journal. Bugs that let `fix` change something other than the previewed setting, skip confirmation, or run unelevated actions as elevated are in scope.
- **Journal trust:** The change journal (`nvcheckup-changes.json` in the user config directory) is user-writable and therefore untrusted. `undo` validates every entry (expected keys, value shape, allowed registry/file paths) before acting on it and never writes journal contents verbatim to a privileged path or command line. A journal entry that can cause `undo` to write outside the action's own setting is a vulnerability.
- **Dependency vulnerabilities:** The Go module has no third-party dependencies; CI runs `govulncheck` against the standard library.

## Design Principles

- NVCheckup is **read-only by default**. `run`, `snapshot`, `compare`, `doctor`, and `self-test` never modify system state. `fix` is opt-in, confirmed, journaled, and undoable.
- All external commands are executed with timeouts and error handling.
- No network calls are made at runtime unless you opt in with `run --network` or run `network-test`. In that case the only traffic is an ICMP ping and a traceroute to `1.1.1.1` and a DNS lookup of `google.com`, performed locally. Nothing is uploaded.
- No telemetry, analytics, or data collection of any kind.
- PII redaction is enabled by default.
- Release binaries are not code-signed. Verify the published SHA256 checksum before running a download.

## Acknowledgments

We appreciate responsible disclosure and will credit reporters (with permission) in release notes.
