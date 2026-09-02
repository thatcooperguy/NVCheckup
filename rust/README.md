# Rust companion (experimental)

This directory is an experimental, partial port of NVCheckup to Rust. It loads
the reference rule pack from `../knowledge/rules.json`, but only about seven of
those rules are actually evaluated (GPU presence, hybrid GPU, driver detection,
`nvidia-smi` availability, low VRAM, running hot, thermal throttling). It produces
text output only, performs no PII redaction, and has none of the Windows event
log, Linux module, AI/CUDA, network, snapshot, or remediation functionality of
the Go tool. Treat any report it prints as unsafe to share.

The Rust tree is not built in CI and is not shipped in releases; the binaries on
the Releases page are the Go implementation. Contributions, bug reports, and new
diagnostic rules should target the Go code under `../cmd` and `../internal`.
Changes here are welcome only if they keep the port in step with the Go behaviour
and do not add maintenance burden to the primary implementation.
