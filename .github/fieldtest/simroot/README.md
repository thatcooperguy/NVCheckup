# Simulated file trees for `NVC_SIM_ROOT`

Each directory here is a root that NVCheckup's collectors read instead of `/`
when the environment variable `NVC_SIM_ROOT` points at it (spec
`docs/roadmap/spark-support.md` section 10). Commands (`nvidia-smi`, `lspci`,
`dmidecode`, ...) are not affected by `NVC_SIM_ROOT`; they are answered by the
shims in `../shims` from the same scenario JSON.

| Tree | Scenario | What it simulates |
|---|---|---|
| `gb10/` | `../scenarios/gb10.json` | Healthy DGX Spark Founders Edition on DGX OS 7.5.0 / OTA2607 |
| `gb10-gsp-fail/` | `../scenarios/gb10-gsp-fail.json` | Same machine after a GSP init failure (files are identical; the difference is in `nvidia-smi` and `dmesg`) |

Do not edit these files by hand. They are generated from the scenario by

```bash
scripts/make-simroot.sh --all
```

and CI regenerates them and fails when the committed copy is stale. Empty
directories (`sys/fs/pstore`) cannot be committed and exist only after the
script has run, which the workflow does before every scenario.

`/proc/device-tree/model` is deliberately absent: whether DGX Spark exposes a
device tree is an open question (spec section 12), so the scenario leaves
`device_tree_model` empty and the generator creates no file.
