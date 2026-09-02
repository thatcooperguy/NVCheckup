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
directories (`sys/fs/pstore`, `sys/firmware/efi`) cannot be committed and exist
only after the script has run, which the workflow does before every scenario.

Besides the release files, DMI, meminfo, cpuinfo, swaps, vmstat, PSI and
thermal fixtures, the trees carry `/proc/net/tcp` (dashboard port 11000
listening), `/proc/sys/kernel/osrelease`, the ConnectX-7
`/sys/class/infiniband` and `/sys/class/net` trees (state, rate, MTU, speed,
MAC placeholder, PCI_SLOT_NAME), `/etc/netplan/50-cx7.yaml` (cage-0 addresses,
`mtu: 9000`), `/etc/ufw/ufw.conf`, `/etc/nvidia/cx7-hotplug-enabled`,
`/etc/docker/daemon.json` with the nvidia runtime and `features.cdi`,
`/etc/cdi/nvidia.yaml`, and the container-toolkit apt source. The scenario's
`description` names which of these are placeholders pending a hardware capture.

`/proc/device-tree/model` is deliberately absent: whether DGX Spark exposes a
device tree is an open question (spec section 12), so the scenario leaves
`device_tree_model` empty and the generator creates no file.
