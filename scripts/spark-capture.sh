#!/usr/bin/env bash
# NVCheckup DGX Spark capture kit (read-only).
#
# Saves the exact command outputs that the open questions of
# docs/roadmap/spark-support.md section 12 need from a real DGX Spark / GB10
# (Founders Edition or OEM), redacts identifying data, and packs everything
# into one tarball with a CHECKLIST.md that says which question each file
# answers. Nothing here changes system state: every command is a query, sudo is
# used only when it works without a password and only for read-only tools
# (dmidecode, dmesg, nvidia-spark-ota-check, lspci -vv), and no network access
# is made.
#
# Usage:
#   ./scripts/spark-capture.sh                 # writes nvcheckup-spark-capture-<stamp>.tar.gz
#   ./scripts/spark-capture.sh --out DIR       # capture into DIR (no tarball; used by linux-fieldtest.sh)
#   ./scripts/spark-capture.sh --no-sudo       # never call sudo even if it is passwordless
#
# Attach the tarball to https://github.com/thatcooperguy/NVCheckup/issues/2.
set -uo pipefail

REPO="thatcooperguy/NVCheckup"
OUT=""
USE_SUDO=1
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --no-sudo) USE_SUDO=0; shift ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

STAMP="$(date -u +%Y%m%d-%H%M%S)"
TARBALL=""
if [ -z "$OUT" ]; then
  OUT="nvcheckup-spark-capture-$STAMP"
  TARBALL="$OUT.tar.gz"
fi
mkdir -p "$OUT" || exit 1
OUT="$(cd "$OUT" && pwd)"
LOG="$OUT/00-capture-log.txt"

SUDO=""
if [ "$(id -u)" -ne 0 ] && [ "$USE_SUDO" = 1 ] && command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
  SUDO="sudo -n"
fi

note() { printf '%s\n' "$*" | tee -a "$LOG"; }

# cap FILE CMD...: run CMD, store "$ CMD", its output and exit code in FILE (appending, so one
# file can hold several related commands). Missing tools are recorded, not fatal.
cap() {
  local file="$OUT/$1"; shift
  {
    printf '$ %s\n' "$*"
    if command -v "${1#sudo -n }" >/dev/null 2>&1 || [ "$1" = "sudo" ] || [ "$1" = "sh" ]; then
      timeout 120 "$@" 2>&1
      printf '[exit=%s]\n\n' "${PIPESTATUS[0]:-$?}"
    else
      printf '[not installed]\n\n'
    fi
  } >>"$file"
  note "captured: $(basename "$file") <- $*"
}
# capsudo FILE CMD...: like cap but through sudo when available (read-only tools that need root).
capsudo() {
  local file="$1"; shift
  if [ -n "$SUDO" ]; then cap "$file" $SUDO "$@"; else cap "$file" "$@"; note "  (no passwordless sudo: $1 ran unprivileged and may be incomplete)"; fi
}
# capfile FILE PATH...: contents of files (spec 12: cat /etc/dgx-release etc.).
capfile() {
  local file="$OUT/$1"; shift
  for p in "$@"; do
    { printf '$ cat %s\n' "$p"; if [ -e "$p" ]; then cat "$p" 2>&1 | tr -d '\000'; else echo "[absent]"; fi; printf '\n\n'; } >>"$file"
  done
  note "captured: $(basename "$file") <- $*"
}

note "NVCheckup spark-capture $STAMP (read-only); sudo: ${SUDO:-none}"
note "host: $(uname -m), $(. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME")"

# --- identity of the platform (spec 3.1 rows 4, 5, 10; section 12) ---------
capfile 01-dgx-release.txt /etc/dgx-release /etc/fastos-release /etc/nv_tegra_release
capfile 02-os-release.txt /etc/os-release /proc/version
cap 03-uname.txt uname -a
for k in system-manufacturer system-product-name system-version system-family bios-vendor bios-version bios-release-date baseboard-manufacturer baseboard-product-name; do
  capsudo 04-dmidecode.txt dmidecode -s "$k"
done
capfile 05-dmi-sysfs.txt /sys/class/dmi/id/sys_vendor /sys/class/dmi/id/product_name /sys/class/dmi/id/product_version /sys/class/dmi/id/product_family /sys/class/dmi/id/bios_vendor /sys/class/dmi/id/bios_version /sys/class/dmi/id/bios_date /sys/class/dmi/id/board_vendor /sys/class/dmi/id/board_name
cap 06-device-tree.txt ls -la /proc/device-tree/model
capfile 06-device-tree.txt /proc/device-tree/model /proc/device-tree/compatible

# --- CPU and memory (spec 2.1 MemTotal, 3.1 lscpu/MIDR, 3.3 arithmetic) ------
cap 07-lscpu.txt lscpu
capfile 08-cpuinfo.txt /proc/cpuinfo
capfile 09-meminfo.txt /proc/meminfo /proc/swaps /proc/sys/vm/swappiness /proc/pressure/memory
cap 09-meminfo.txt sh -c "grep -E 'MemTotal|MemAvailable|Swap|HugePages' /proc/meminfo"
cap 09-meminfo.txt sh -c "grep -E '^(pswpin|pswpout|oom_kill) ' /proc/vmstat"
cap 10-thermal.txt sh -c 'for z in /sys/class/thermal/thermal_zone*; do echo "$z $(cat $z/type 2>/dev/null) $(cat $z/temp 2>/dev/null)"; done'

# --- PCI (spec 3.1 row 5 [10de:2e12] at 000f:01:00.0; 2.1 ConnectX-7 15b3:1021) --
cap 11-lspci-nvidia.txt lspci -nn -D -d 10de:
capsudo 11-lspci-nvidia.txt lspci -nn -D -vv -d 10de:
cap 12-lspci-mellanox.txt lspci -nn -D -d 15b3:
capsudo 12-lspci-mellanox.txt lspci -nn -D -vv -d 15b3:
cap 13-lspci-all.txt lspci -nn -D

# --- nvidia-smi (spec 2.1; every field list the collectors use, incl. compute_cap) --
cap 14-nvidia-smi-L.txt nvidia-smi -L
cap 15-nvidia-smi-table.txt nvidia-smi
for q in \
  "index,driver_version,pci.bus_id,memory.total,memory.free,memory.used,temperature.gpu,power.draw" \
  "index,pcie.link.gen.current,pcie.link.gen.max,pcie.link.width.current,pcie.link.width.max,pstate,utilization.gpu" \
  "index,temperature.gpu,pstate,clocks.current.graphics,clocks.max.graphics,power.limit,power.draw,fan.speed,utilization.gpu" \
  "index,clocks_event_reasons.active" \
  "index,clocks_throttle_reasons.active" \
  "index,compute_cap" \
  "index,name,uuid,clocks.max.graphics,clocks.max.sm,clocks.max.memory,clocks.current.memory,power.limit,enforced.power.limit,power.default_limit,power.min_limit,power.max_limit,fan.speed,persistence_mode,compute_mode" \
  "index,pcie.link.gen.gpucurrent,pcie.link.gen.gpumax,pcie.link.gen.hostmax,memory.reserved,utilization.memory,temperature.memory,power.draw.average,power.draw.instant"; do
  cap 16-nvidia-smi-query.txt nvidia-smi "--query-gpu=$q" --format=csv,noheader,nounits
  cap 16-nvidia-smi-query.txt nvidia-smi "--query-gpu=$q" --format=csv
done
cap 17-nvidia-smi-compute-apps.txt nvidia-smi --query-compute-apps=pid,process_name,used_memory,gpu_uuid --format=csv
cap 18-nvidia-smi-q.txt nvidia-smi -q
cap 19-nvidia-smi-q-sections.txt nvidia-smi -q -d MEMORY,PERFORMANCE,CLOCK,POWER,TEMPERATURE
cap 19-nvidia-smi-q-sections.txt nvidia-smi -q -d SUPPORTED_CLOCKS

# --- DGX OS tooling (spec 2.1 updates row; 5 dgx-spark-ota-torn, -dashboard-unhealthy, -firmware-behind) --
for sub in summary torn-score installed-name is-ota-available; do
  capsudo 20-ota-check.txt nvidia-spark-ota-check "$sub"
done
cap 21-fwupdmgr.txt fwupdmgr --version
cap 21-fwupdmgr.txt fwupdmgr get-devices
cap 21-fwupdmgr.txt fwupdmgr get-updates
cap 22-fwupdmgr-devices.json fwupdmgr get-devices --json
cap 23-dpkg.txt dpkg -l 'nvidia-dkms*' 'nvidia-driver*' 'nvidia-kernel*' 'nvidia-firmware*' 'linux-modules-nvidia*' 'linux-nvidia*' 'nvidia-persistenced' 'nvidia-container-toolkit' 'dgx*' 'nvidia-spark*' 'nvidia-dgx*' 'nvidia-system*' 'libnvidia-gl*' 'cuda-toolkit*' 'fwupd' 'libfwupd*'
cap 23-dpkg.txt dpkg-query -W -f='${Package} ${Version} ${db:Status-Status}\n' 'nvidia-*' 'dgx-*' 'linux-*nvidia*'
cap 24-systemd.txt systemctl is-active dgx-dashboard.service dgx-dashboard-admin.service fwupd.service nvidia-persistenced.service gb10-clock-cap.service avahi-daemon.service ufw.service docker.service ollama.service
cap 24-systemd.txt systemctl is-enabled dgx-dashboard.service dgx-dashboard-admin.service fwupd.service nvidia-persistenced.service gb10-clock-cap.service
cap 24-systemd.txt sh -c "systemctl list-units --type=service --all --no-pager --plain | grep -E 'dgx|nvidia|fwupd|avahi|ufw|docker|ollama|gb10'"
cap 25-listening-ports.txt ss -ltn
cap 25-listening-ports.txt sh -c "grep -c . /proc/net/tcp; head -20 /proc/net/tcp"

# --- ConnectX-7 (spec 2.1 networking row; section 9) ------------------------
cap 26-ibdev2netdev.txt ibdev2netdev
cap 26-ibdev2netdev.txt ibdev2netdev -v
cap 27-ibstat.txt ibstat
cap 28-ip.txt ip -br link
cap 28-ip.txt ip -br addr
cap 28-ip.txt ip -4 route
cap 29-sysfs-net.txt sh -c 'for d in /sys/class/infiniband/*; do n=$(basename $d); echo "$n state=$(cat $d/ports/1/state 2>/dev/null) phys=$(cat $d/ports/1/phys_state 2>/dev/null) rate=$(cat $d/ports/1/rate 2>/dev/null) link_layer=$(cat $d/ports/1/link_layer 2>/dev/null) net=$(ls $d/device/net 2>/dev/null | tr "\n" ",") pci=$(basename $(readlink -f $d/device) 2>/dev/null)"; done'
cap 29-sysfs-net.txt sh -c 'for d in /sys/class/net/*; do n=$(basename $d); [ "$n" = lo ] && continue; echo "$n operstate=$(cat $d/operstate 2>/dev/null) carrier=$(cat $d/carrier 2>/dev/null) speed=$(cat $d/speed 2>/dev/null) mtu=$(cat $d/mtu 2>/dev/null) driver=$(basename $(readlink -f $d/device/driver) 2>/dev/null) pci=$(basename $(readlink -f $d/device) 2>/dev/null) vendor=$(cat $d/device/vendor 2>/dev/null) device=$(cat $d/device/device 2>/dev/null)"; done'
capfile 30-netplan-ufw.txt /etc/nvidia/cx7-hotplug-enabled /etc/ufw/ufw.conf
cap 30-netplan-ufw.txt sh -c 'ls -la /etc/netplan; cat /etc/netplan/*.yaml 2>/dev/null'
cap 31-env-nccl.txt sh -c "env | grep -E '^(NCCL_|UCX_|TRITON_PTXAS_PATH|CUDA_|LD_LIBRARY_PATH)' | sort"

# --- kernel modules and logs (spec 3.2 GSP strings, 6 failure strings) -------
cap 32-lsmod.txt lsmod
cap 33-modinfo.txt sh -c "modinfo nvidia | grep -Ev '^(sig|alias|depends|filename)'"
cap 33-modinfo.txt sh -c "modinfo nvidia_peermem 2>&1 | head -5"
capsudo 34-dmesg.txt sh -c "dmesg | grep -iE 'nvidia|nvrm|gsp|xid|sec2|mlx5|thermal|pcie|badf|oom|swap' | tail -400"
cap 35-journal-prev-boot.txt sh -c "journalctl -k -b -1 --no-pager 2>&1 | tail -200"
cap 35-journal-prev-boot.txt sh -c "journalctl --list-boots --no-pager 2>&1 | tail -10"
cap 35-journal-prev-boot.txt sh -c "ls -la /sys/fs/pstore 2>&1"
cap 36-journal-fwupd-dashboard.txt sh -c "journalctl -u fwupd -u dgx-dashboard -u dgx-dashboard-admin -b --no-pager 2>&1 | tail -80"

# --- ecosystem (spec 3.2 ecosystem strings; 7.7 prerequisites) --------------
cap 37-python-torch.txt sh -c "python3 -c 'import torch,sys; print(torch.__version__); print(torch.version.cuda); print(torch.cuda.get_arch_list())' 2>&1 | tail -5"
cap 37-python-torch.txt sh -c "python3 -m pip list 2>/dev/null | grep -iE 'torch|triton|flash|onnx|nvidia|cuda|vllm|sglang|transformers' | head -40"
cap 38-docker.txt sh -c "docker version --format '{{.Server.Version}} {{.Server.Arch}}' 2>&1"
cap 38-docker.txt sh -c "docker info --format '{{json .Runtimes}} default={{.DefaultRuntime}} cdi={{json .CDISpecDirs}}' 2>&1"
cap 38-docker.txt sh -c "ls -la /etc/cdi /var/run/cdi 2>&1; snap list docker 2>&1 | head -3"
cap 38-docker.txt sh -c "docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | grep -iE 'nvcr|pytorch|vllm|sglang|tensorrt|ollama|cuda' | head -30"
cap 39-cuda-toolkit.txt sh -c "ls -la /usr/local/cuda* 2>&1 | head; /usr/local/cuda/bin/nvcc --version 2>&1 | tail -2; /usr/local/cuda/bin/ptxas --version 2>&1 | tail -1"
cap 39-cuda-toolkit.txt sh -c "ldconfig -p | grep -E 'libcuda|libcudart|libnvidia-ml|libnccl' "

# --- redaction ---------------------------------------------------------------
# Serial numbers, GUIDs and GPU UUIDs identify the unit; MACs, IPs, hostname,
# user and home identify the network and the person. Version strings such as
# 580.159.03 or 6.17.0-1026 must survive, so IPv4 needs exactly four octets
# in 0-255 not adjacent to more dots or digits.
redact() {
  # DGX OS ships python3; the fallback to plain "python" only matters when the script is
  # exercised on a developer box whose python3 is a store stub.
  local py=python3
  python3 -c pass >/dev/null 2>&1 || py=python
  "$py" - "$OUT" "$(hostname)" "$(id -un)" "${HOME:-/nonexistent}" <<'PY' 2>/dev/null
import os, re, sys
out, host, user, home = sys.argv[1:5]
ipv4 = re.compile(r'(?<![\d.])(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?![\d.])')
rules = [
    (re.compile(r'(?i)(DGX_SERIAL_NUMBER=)"[^"]*"'), r'\1"<serial>"'),
    (re.compile(r'(?i)(Serial Number\s*:\s*)\S.*'), r'\1<serial>'),
    (re.compile(r'(?i)("Serial"\s*:\s*)"[^"]*"'), r'\1"<serial>"'),
    (re.compile(r'(?i)(product_serial|board_serial|chassis_serial)\s*\n[^\n]*'), r'\1\n<serial>'),
    (re.compile(r'GPU-[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'), '<gpu-uuid>'),
    (re.compile(r'(?i)((?:Node|Port|System image)\s+GUID\s*:\s*)0x[0-9a-f]+'), r'\1<guid>'),
    (re.compile(r'(?i)(DeviceId"\s*:\s*)"[0-9a-f]{40}"'), r'\1"<device-id>"'),
    (re.compile(r'(?i)(Device ID:\s*)[0-9a-f]{40}'), r'\1<device-id>'),
    (re.compile(r'\b(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}\b'), '<mac>'),
    (re.compile(r'\b(?:[0-9a-fA-F]{2}:){7}[0-9a-fA-F]{2}\b'), '<mac>'),
    (re.compile(r'(?i)\b(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{1,4}(?:/\d{1,3})?\b(?=.*(?:inet6|scope|fe80|::))'), '<ipv6>'),
    (re.compile(r'(?i)(ssid|psk|password|token)\s*[:=]\s*\S+'), r'\1=<redacted>'),
]
keep_ip = {"127.0.0.1", "0.0.0.0", "1.1.1.1", "8.8.8.8", "255.255.255.255"}


def ip_sub(m):
    return m.group(0) if m.group(0) in keep_ip else "<ip>"


counts = {}
for root, _, files in os.walk(out):
    for name in files:
        path = os.path.join(root, name)
        try:
            text = open(path, encoding="utf-8", errors="replace").read()
        except OSError:
            continue
        orig = text
        for rx, rep in rules:
            text, n = rx.subn(rep, text)
            counts[rx.pattern[:30]] = counts.get(rx.pattern[:30], 0) + n
        text, n = ipv4.subn(ip_sub, text)
        counts["ipv4"] = counts.get("ipv4", 0) + n
        if home and home != "/" and home != "/nonexistent":
            text = text.replace(home, "<home>")
        if host:
            text = re.sub(r'(?<![\w.-])' + re.escape(host) + r'(?![\w-])', "<host>", text)
        # Also the bare account of a domain user (DOMAIN+name, DOMAIN\name) and the home directory's last component.
        names = {user, user.split("+")[-1], user.split("\\")[-1], os.path.basename(home.rstrip("/"))}
        for u in sorted(names, key=len, reverse=True):
            if u and u not in ("root", "nonexistent") and len(u) > 2:
                text = re.sub(r'(?<![\w-])' + re.escape(u) + r'(?![\w-])', "<user>", text)
        if text != orig:
            with open(path, "w", encoding="utf-8", newline="\n") as f:
                f.write(text)
with open(os.path.join(out, "00-redaction.txt"), "w", encoding="utf-8") as f:
    f.write("redaction pass: tokens <serial> <gpu-uuid> <guid> <device-id> <mac> <ip> <ipv6> <host> <user> <home>\n")
    for k, v in sorted(counts.items()):
        if v:
            f.write("%-32s %d\n" % (k, v))
print("redacted")
PY
}
if ! redact; then
  note "python3 unavailable: falling back to sed redaction (hostname, user, home, MACs, serial lines only)"
  h="$(hostname)"; u="$(id -un)"
  for f in "$OUT"/*.txt "$OUT"/*.json; do
    [ -f "$f" ] || continue
    sed -i -E \
      -e 's/(DGX_SERIAL_NUMBER=)"[^"]*"/\1"<serial>"/I' \
      -e 's/(Serial Number[[:space:]]*:[[:space:]]*).*/\1<serial>/I' \
      -e 's/GPU-[0-9a-fA-F-]{36}/<gpu-uuid>/g' \
      -e 's/\b([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}\b/<mac>/g' \
      -e "s#${HOME:-/nonexistent}#<home>#g" \
      -e "s/\b${h}\b/<host>/g" \
      -e "s/\b${u}\b/<user>/g" "$f"
  done
fi

# --- checklist: which open question does each file answer? -------------------
cat >"$OUT/CHECKLIST.md" <<'EOF'
# DGX Spark capture: what each file answers

Spec: `docs/roadmap/spark-support.md` section 12 (open questions only hardware
can answer) and the values it marks *unconfirmed* / *placeholder* in sections
2.1, 3.1, 3.2 and 10. Everything below was captured read-only and redacted
(`00-redaction.txt` lists the replacement counts).

| File | Open question / spec item it answers |
|---|---|
| `01-dgx-release.txt` | Exact `/etc/dgx-release` key list and values (3.1 row 4; is `DGX_PLATFORM="DGX Server for KVM"` really there; does an OEM unit differ, 2.1 OEM row); `/etc/fastos-release` `NAME="DGX SPARK FASTOS"`; confirms no `/etc/nv_tegra_release` |
| `02-os-release.txt`, `03-uname.txt` | Kernel flavour string for 3.1 row 11 (`^\d+\.\d+\.\d+-\d+-nvidia`), Ubuntu point release |
| `04-dmidecode.txt`, `05-dmi-sysfs.txt` | DMI strings of 3.1 row 10 (`NVIDIA` / `NVIDIA_DGX_Spark` / `A.7` / `5.36_0ACUM0xx`) or the OEM equivalents; whether sysfs and dmidecode agree |
| `06-device-tree.txt` | Whether DGX Spark exposes `/proc/device-tree/model` at all (12; the gb10 fixture leaves it absent) |
| `07-lscpu.txt`, `08-cpuinfo.txt` | `Model name:` lines (`Cortex-X925` / `Cortex-A725`), `Vendor ID: ARM`, `Stepping: r0p1`, `CPU(s): 20`; MIDR parts `0xd85` / `0xd87` (3.1 last paragraph) |
| `09-meminfo.txt` | `MemTotal` on this unit (125,513,944 kB in 2025, ~121.7 GiB in 2026 per 2.1), swap size and device shipped by DGX OS (fixture placeholder), `HugePages_*` (3.3) |
| `10-thermal.txt` | Which ACPI thermal zones exist and their idle temperature (5 `gb10-acpi-thermal-zone-hot` threshold inference) |
| `11-lspci-nvidia.txt` | `[10de:2e12]` at `000f:01:00.0` (3.1 row 5), `LnkCap`/`LnkSta` of the misreported link (2.1 `GEN 1@ 1x`) |
| `12-lspci-mellanox.txt`, `13-lspci-all.txt` | Four ConnectX-7 functions `0000:01:00.0/.1` and `0002:01:00.0/.1` `[15b3:1021]` (2.1); Realtek `10ec:8127` and MediaTek `14c3:7925` ids |
| `14-nvidia-smi-L.txt`, `15-nvidia-smi-table.txt` | Name `NVIDIA GB10`, Bus-Id `0000000F:01:00.0`, `Not Supported` Memory-Usage, `N/A` fan and cap (2.1); exact table layout used by the shim |
| `16-nvidia-smi-query.txt` | Every collector query field list with and without units: does `compute_cap` succeed (4, `GPUCapQueryFields`); which fields print `[N/A]`; are `clocks.max.memory` and the power limits `[N/A]` (2.1) |
| `17-nvidia-smi-compute-apps.txt` | Whether per-process memory works on unified memory (2.1) |
| `18-nvidia-smi-q.txt`, `19-nvidia-smi-q-sections.txt` | `-q` layout: power limits `N/A`, `Max Clocks Graphics 3003 MHz`, memory clock `N/A`, `Supported Clocks N/A`, T.Limit lines, `GPU Max Operating T.Limit Temp 0 C` (2.1); `-d PERFORMANCE` counter names for `ThermalInfo.EventCounters` (4) |
| `20-ota-check.txt` | Exact `nvidia-spark-ota-check` output shapes for `summary`, `torn-score`, `installed-name`, `is-ota-available` (2.1, 5 `dgx-spark-ota-torn`); requires root |
| `21-fwupdmgr.txt`, `22-fwupdmgr-devices.json` | Device names and version formats for EC / SoC / USB PD (5 `dgx-spark-firmware-behind`: hex vs dotted); `get-updates` exit code when nothing is pending |
| `23-dpkg.txt` | Is `nvidia-dkms-580-open` installed on a stock unit (3.2 placeholder, 10 fixture); exact versions of `nvidia-driver-580-open`, `linux-modules-nvidia-580-open-<kernel>`, `dgx-release`, `dgx-dashboard`, `nvidia-spark-ota-check`, `fwupd` / `libfwupd` |
| `24-systemd.txt`, `25-listening-ports.txt` | Unit names and states (`dgx-dashboard*.service`, `fwupd`, `nvidia-persistenced`, `gb10-clock-cap.service`), dashboard port 11000 (2.1, 5 `dgx-spark-dashboard-unhealthy`) |
| `26-ibdev2netdev.txt`, `27-ibstat.txt`, `28-ip.txt`, `29-sysfs-net.txt` | RDMA and netdev names of the twins, sysfs `state` / `phys_state` / `rate` strings (fixture placeholder `200 Gb/sec (4X HDR)`), `speed` when cabled / uncabled, MTU, IPv4 per twin (2.1 networking row, 9) |
| `30-netplan-ufw.txt`, `31-env-nccl.txt` | `/etc/nvidia/cx7-hotplug-enabled` presence, netplan MTU keys, ufw default, `NCCL_*` / `UCX_NET_DEVICES` defaults (9) |
| `32-lsmod.txt`, `33-modinfo.txt` | Module set and `license:` string of the open modules; whether `nvidia_peermem` exists (2.1 CUDA memory row) |
| `34-dmesg.txt` | Benign `mlx5_pcie_event ... 27W` line (3.2); any GSP / Xid / `0xbadf5600` lines (3.2, 6); OOM or swap events (5 unified-memory rules) |
| `35-journal-prev-boot.txt` | Clean-shutdown markers of the previous boot and pstore contents (5 `gb10-logless-hard-poweroff`) |
| `36-journal-fwupd-dashboard.txt` | `libfwupd version ... does not match daemon ...` (6), dashboard unit health (5) |
| `37-python-torch.txt` | torch build tag (`+cu130`), `get_arch_list()` containing `sm_120`/`sm_121` (3.2 ecosystem, 7.7) |
| `38-docker.txt` | Docker runtimes (`nvidia`), CDI spec dirs, snap docker, image tags and architectures (5 `docker-*`, `arm64-container-amd64-image`, `sm121-ngc-image-too-old`) |
| `39-cuda-toolkit.txt` | Toolkit version (`V13.0.88` / 13.0.2), bundled `ptxas`, `libcudart.so.12` vs `.13` presence (3.2, 5 `sm121-triton-ptxas-stale`, `arm64-cuda12-wheel-on-cuda13`) |

Not run on purpose: `sudo nvidia-bug-report.sh` (writes a large archive; run it
separately if NVIDIA support asks), `partnerdiag` (needs Secure Boot off),
`fwupdmgr refresh/upgrade`, `apt`, anything that starts or stops a service.
EOF
note "checklist: CHECKLIST.md"

if [ -n "$TARBALL" ]; then
  ( cd "$(dirname "$OUT")" && tar czf "$TARBALL" "$(basename "$OUT")" ) && note "bundle: $(dirname "$OUT")/$TARBALL"
  echo
  echo "Bundle: $(dirname "$OUT")/$TARBALL"
  echo "Everything inside is redacted (serials, GPU UUIDs, GUIDs, MACs, IPs, hostname, user, home)."
  echo "Attach it to https://github.com/$REPO/issues/2 together with CHECKLIST.md answers you can add by hand."
fi
