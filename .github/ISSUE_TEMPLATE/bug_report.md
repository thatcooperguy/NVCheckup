---
name: Bug Report
about: Report a bug in NVCheckup
title: "[Bug] "
labels: bug
assignees: ''
---

## Description

A clear description of the bug.

## Steps to Reproduce

1. Run `nvcheckup ...`
2. ...

## Expected Behavior

What you expected to happen.

## Actual Behavior

What actually happened.

## Environment

- OS: (e.g., Windows 11 24H2, Ubuntu 24.04)
- Architecture: (x86_64, ARM64)
- NVCheckup version: (output of `nvcheckup version`, e.g., v0.2.2)
- GPU: (e.g., RTX 4070)
- Driver version: (e.g., 591.86)

## Self-Test Output

Paste the output of `nvcheckup self-test`. It shows which tools were found and which
collector queries your driver accepts, and modifies nothing.

```
<paste self-test output here>
```

## Report

If possible, attach `report.json` from:

```
nvcheckup run --mode full --json
```

The report is redacted by default (usernames, hostnames, home paths, IPs, and emails are
replaced with placeholders, and the nvidia-smi process list is not stored). Please skim it
before attaching anyway. If the bug is about a specific finding, include its `id` from
`findings[].id`.

```
<paste report summary or relevant finding here>
```

## Additional Context

Any other relevant information.
