# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | Yes                |

## Reporting a Vulnerability

If you discover a security vulnerability in NVCheckup, please report it responsibly.

**Do not open a public issue for security vulnerabilities.**

Instead, please email the maintainers or use GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](../../security) of this repository.
2. Click "Report a vulnerability."
3. Provide a clear description of the issue, steps to reproduce, and potential impact.

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Scope

NVCheckup is a diagnostic tool that runs locally and never transmits data. Security concerns most relevant to this project include:

- **Information disclosure:** Ensuring redaction works correctly and PII is not leaked in reports.
- **Command injection:** Ensuring user-controlled input cannot be injected into system commands.
- **Path traversal:** Ensuring output files are written only to intended directories.
- **Dependency vulnerabilities:** Keeping Go module dependencies (if any) up to date.

## Design Principles

- NVCheckup is **read-only by default** and never modifies system state.
- All external commands are executed with timeouts and error handling.
- No network calls are made at runtime.
- No telemetry, analytics, or data collection of any kind.
- PII redaction is enabled by default.

## Acknowledgments

We appreciate responsible disclosure and will credit reporters (with permission) in release notes.
