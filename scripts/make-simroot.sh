#!/usr/bin/env bash
# Build an NVC_SIM_ROOT file tree from a field-test scenario.
#
# Spec docs/roadmap/spark-support.md section 10 (simulation contract): when
# NVC_SIM_ROOT is set, every absolute file the collectors read (/etc/...,
# /proc/..., /sys/..., /var/..., /run/...) is prefixed with it, while commands
# keep resolving via PATH so the shims in .github/fieldtest/shims answer them.
# This script writes the file side of that contract from the same scenario
# JSON the shims read, so files and command output cannot drift apart.
#
# Usage:
#   scripts/make-simroot.sh <scenario.json> <outdir>
#   scripts/make-simroot.sh --all        # regenerate .github/fieldtest/simroot/<name>/ for every gb10*.json
#
# The generated trees are committed; CI regenerates them and fails when the
# committed copy is stale (`git status --porcelain .github/fieldtest/simroot`).
# Only regular files are written (no symlinks: the trees must survive a Windows
# checkout), directories that must merely exist (sys/fs/pstore) are created
# empty and therefore only exist after running this script.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
PY="${NVC_SIM_PYTHON:-}"
if [ -z "$PY" ]; then
  if command -v python3 >/dev/null 2>&1 && python3 -c pass >/dev/null 2>&1; then PY=python3; else PY=python; fi
fi

gen() {
  "$PY" - "$1" "$2" <<'PY'
import json, os, shutil, sys

scenario, out = sys.argv[1], sys.argv[2]
sc = json.load(open(scenario, encoding="utf-8"))


def write(rel, text, nl=True):
    path = os.path.join(out, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(text)
        if nl and text and not text.endswith("\n"):
            f.write("\n")


def mkdir(rel):
    os.makedirs(os.path.join(out, rel), exist_ok=True)


# Start clean so files dropped from the scenario disappear from the tree.
for top in ("etc", "proc", "sys", "lib", "run", "var", "dev", "opt"):
    shutil.rmtree(os.path.join(out, top), ignore_errors=True)
os.makedirs(out, exist_ok=True)

# --- /etc -------------------------------------------------------------------
# spec 3.1 row 4: /etc/dgx-release (DGX_NAME="DGX Spark", ...) and /etc/fastos-release NAME="DGX SPARK FASTOS".
if sc.get("dgx_release"):
    write("etc/dgx-release", sc["dgx_release"])
if sc.get("fastos_release"):
    write("etc/fastos-release", sc["fastos_release"])
if sc.get("os_release"):
    write("etc/os-release", sc["os_release"])
units = sc.get("systemd_units") or {}
# spec 9: /etc/ufw/ufw.conf (cx7-firewall-blocks-cluster looks at ENABLED=).
if "ufw.service" in units:
    write("etc/ufw/ufw.conf", "# /etc/ufw/ufw.conf\n#\n# Set to yes to start on boot.\nENABLED=%s\n\n# Please use the 'ufw' command to set the loglevel.\nLOGLEVEL=low\n" % ("yes" if units["ufw.service"] == "active" else "no"))

# --- /proc ------------------------------------------------------------------
# spec 2.1 / 3.3: MemTotal 125513944 kB, MemAvailable + SwapFree, HugePages_* override.
mem = sc.get("meminfo") or {}
if mem:
    lines = []
    for k, v in mem.items():
        if k.startswith("HugePages_"):
            lines.append("%-15s %8s" % (k + ":", v))
        else:
            lines.append("%-15s %8s kB" % (k + ":", v))
    write("proc/meminfo", "\n".join(lines) + "\n")

# spec 3.1: arm64 /proc/cpuinfo has no "model name"; MIDR CPU part 0xd85 = Cortex-X925, 0xd87 = Cortex-A725.
cpu_lines = sc.get("cpuinfo_lines") or []
if cpu_lines:
    n = int(sc.get("cpu_count") or 1)
    parts = sc.get("cpu_parts") or []
    blocks = []
    for i in range(n):
        blk = ["processor\t: %d" % i]
        for ln in cpu_lines:
            if i < len(parts) and ln.startswith("CPU part"):
                ln = "CPU part\t: %s" % parts[i]
            blk.append(ln)
        blocks.append("\n".join(blk) + "\n")
    write("proc/cpuinfo", "\n".join(blocks))

if sc.get("proc_version"):
    write("proc/version", sc["proc_version"])
if sc.get("kernel"):
    write("proc/sys/kernel/osrelease", sc["kernel"])

# spec 5 unified-memory-swap-in-use: /proc/swaps, swappiness, PSI.
swaps = sc.get("swaps")
if swaps is not None:
    rows = ["Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority"]
    for s in swaps:
        rows.append("%-39s %-15s %-15s %-15s %s" % (s["filename"], s.get("type", "file"), s.get("size_kb", 0), s.get("used_kb", 0), s.get("priority", -2)))
    write("proc/swaps", "\n".join(rows) + "\n")
if "swappiness" in sc:
    write("proc/sys/vm/swappiness", str(sc["swappiness"]))
if sc.get("pressure_memory"):
    write("proc/pressure/memory", sc["pressure_memory"])
# spec 5 unified-memory-oom-events / swap-in-use: counters read from /proc/vmstat (healthy: zero).
vm = sc.get("vmstat") or {"pswpin": 0, "pswpout": 0, "oom_kill": 0}
write("proc/vmstat", "".join("%s %s\n" % (k, v) for k, v in vm.items()))

# spec 2.1: DGX Dashboard listens on http://localhost:11000 (DGXOSInfo.DashboardPortOpen); llm-plan 7.7 checks 8000/30000/11434/8355.
ports = sc.get("listening_tcp_ports")
if ports is not None:
    rows = ["  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"]
    for i, p in enumerate(ports):
        rows.append("%4d: 0100007F:%04X 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 %d 1 0000000000000000 100 0 0 10 0" % (i, int(p), 20000 + i))
    write("proc/net/tcp", "\n".join(rows) + "\n")

# spec 12: whether DGX Spark exposes /proc/device-tree/model is unconfirmed; an empty value means "do not create".
if sc.get("device_tree_model"):
    write("proc/device-tree/model", sc["device_tree_model"] + "\0", nl=False)

# --- /sys -------------------------------------------------------------------
# spec 3.1 row 10: DMI sys_vendor / product_name / product_version / bios_version.
dmi = sc.get("dmi") or {}
for k, v in dmi.items():
    write("sys/class/dmi/id/%s" % k, str(v))

# spec 2.1: ACPI acpitz zones are the only extra sensors; spec 5 gb10-acpi-thermal-zone-hot >= 93000 mC.
for i, z in enumerate(sc.get("thermal_zones") or []):
    write("sys/class/thermal/thermal_zone%d/type" % i, z.get("type", "acpitz"))
    write("sys/class/thermal/thermal_zone%d/temp" % i, str(z.get("temp_mc", 0)))


def netdev_files(base, d, with_device=True):
    write(base + "/operstate", d.get("operstate", "down"))
    write(base + "/carrier", str(d.get("carrier", 0)))
    write(base + "/speed", str(d.get("speed", -1)))
    write(base + "/mtu", str(d.get("mtu", 1500)))
    if with_device and d.get("pci"):
        write(base + "/device/uevent", "DRIVER=%s\nPCI_SLOT_NAME=%s\n" % (d.get("driver", "mlx5_core"), d["pci"]))
        if d.get("pci_id"):
            ven, dev = d["pci_id"].split(":")
            write(base + "/device/vendor", "0x" + ven)
            write(base + "/device/device", "0x" + dev)


# spec 9: /sys/class/infiniband/<dev>/ports/1/{state,phys_state,rate}, device/net/*; netdev operstate/carrier/speed/mtu.
for p in sc.get("cx7_ports") or []:
    ib = "sys/class/infiniband/%s" % p["rdma"]
    write(ib + "/ports/1/state", p.get("state", "1: DOWN"))
    write(ib + "/ports/1/phys_state", p.get("phys_state", "3: Disabled"))
    write(ib + "/ports/1/rate", p.get("rate", ""))
    write(ib + "/ports/1/link_layer", "Ethernet")
    write(ib + "/node_type", "1: CA")
    write(ib + "/device/uevent", "DRIVER=mlx5_core\nPCI_SLOT_NAME=%s\n" % p["pci"])
    nd = dict(p)
    nd.setdefault("pci_id", "15b3:1021")  # spec 2.1 ConnectX-7 PCI id
    # device/net/<netdev> mirrors the netdev's own files (on real sysfs it is the same directory).
    netdev_files(ib + "/device/net/%s" % p["netdev"], nd, with_device=False)
    netdev_files("sys/class/net/%s" % p["netdev"], nd)
for d in sc.get("other_netdevs") or []:
    netdev_files("sys/class/net/%s" % d["netdev"], d)

# spec 5 gb10-logless-hard-poweroff: PstoreEmpty. The directory exists and is empty on a healthy unit.
mkdir("sys/fs/pstore")

# --- /lib -------------------------------------------------------------------
if sc.get("kernel"):
    write("lib/modules/%s/modules.dep" % sc["kernel"], "")

print("simroot written: %s" % out)
PY
}

if [ "${1:-}" = "--all" ]; then
  for scn in "$here"/.github/fieldtest/scenarios/gb10*.json; do
    name="$(basename "$scn" .json)"
    gen "$scn" "$here/.github/fieldtest/simroot/$name"
  done
  exit 0
fi
if [ $# -ne 2 ]; then
  sed -n '2,22p' "$0" >&2
  exit 2
fi
gen "$1" "$2"
