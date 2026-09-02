#!/usr/bin/env bash
# NVCheckup Linux field kit.
#
# One command to run on a Linux box with a real NVIDIA GPU (a workstation, a
# Jetson, or a DGX Spark / GB10 on aarch64) and produce a bundle we can look
# at. Everything it runs is read-only; the two `fix` invocations are --dry-run
# and change nothing. Reports are redacted by default, so the bundle is safe
# to attach to https://github.com/thatcooperguy/NVCheckup/issues/2
#
# On a DGX Spark (/etc/dgx-release present) it also runs scripts/spark-capture.sh,
# which saves the raw tool outputs the open questions of
# docs/roadmap/spark-support.md section 12 need, redacted, into the same bundle.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/thatcooperguy/NVCheckup/main/scripts/linux-fieldtest.sh | bash
#   ./scripts/linux-fieldtest.sh                 # downloads the latest release for this CPU (x86_64 or aarch64)
#   ./scripts/linux-fieldtest.sh --local ./nvcheckup   # use a binary you built yourself
#   ./scripts/linux-fieldtest.sh --network       # also run the opt-in ping/traceroute/DNS probes
#   ./scripts/linux-fieldtest.sh --no-capture    # skip the DGX Spark raw capture
set -uo pipefail

REPO="thatcooperguy/NVCheckup"
BIN=""
NETWORK=""
CAPTURE=1
while [ $# -gt 0 ]; do
  case "$1" in
    --local) BIN="$2"; shift 2 ;;
    --network) NETWORK="--network"; shift ;;
    --no-capture) CAPTURE=0; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"

STAMP="$(date -u +%Y%m%d-%H%M%S)"
WORK="nvcheckup-fieldtest-$STAMP"
mkdir -p "$WORK"
cd "$WORK" || exit 1
NOTES="notes.txt"

say() { printf '\n== %s ==\n' "$*" | tee -a "$NOTES"; }
run() { printf '$ %s\n' "$*" | tee -a "$NOTES"; "$@" 2>&1 | tee -a "$NOTES"; return "${PIPESTATUS[0]}"; }

say "host"
{ uname -a; (. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME"); id -un; date -u; } | tee -a "$NOTES"
[ -f /etc/nv_tegra_release ] && { echo "Jetson / L4T:"; head -1 /etc/nv_tegra_release; } | tee -a "$NOTES"
# spec docs/roadmap/spark-support.md 3.1 row 4: DGX OS carries /etc/dgx-release (DGX_NAME="DGX Spark"); serial is not echoed.
IS_SPARK=0
if [ -f /etc/dgx-release ]; then
  IS_SPARK=1
  { echo "DGX OS:"; grep -E '^DGX_(NAME|PRETTY_NAME|SWBUILD_VERSION|OTA_VERSION|OTA_DATE)=' /etc/dgx-release; } | tee -a "$NOTES"
fi
command -v nvidia-smi >/dev/null && run nvidia-smi -L || echo "nvidia-smi: not on PATH (expected on Jetson)" | tee -a "$NOTES"
# spec 3.1 row 5: a GB10 is a DGX Spark even without DGX OS.
if [ "$IS_SPARK" = 0 ] && command -v nvidia-smi >/dev/null && nvidia-smi -L 2>/dev/null | grep -q "NVIDIA GB10"; then IS_SPARK=1; fi

if [ -z "$BIN" ]; then
  case "$(uname -m)" in
    x86_64) ASSET="nvcheckup-linux-amd64" ;;
    aarch64|arm64) ASSET="nvcheckup-linux-arm64" ;;
    *) echo "unsupported CPU: $(uname -m)" >&2; exit 2 ;;
  esac
  say "download latest release ($ASSET)"
  BASE="https://github.com/$REPO/releases/latest/download"
  curl -fsSL -o "$ASSET" "$BASE/$ASSET" && curl -fsSL -o "$ASSET.sha256" "$BASE/$ASSET.sha256" || { echo "download failed" >&2; exit 1; }
  sha256sum -c "$ASSET.sha256" | tee -a "$NOTES" || { echo "checksum mismatch; refusing to run" >&2; exit 1; }
  chmod +x "$ASSET"
  BIN="./$ASSET"
  if command -v gh >/dev/null 2>&1; then
    say "provenance (optional)"
    gh attestation verify "$ASSET" --owner "${REPO%%/*}" 2>&1 | tail -3 | tee -a "$NOTES" || true
  fi
else
  BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
fi

say "version"; run "$BIN" version
say "self-test"; run "$BIN" self-test; echo "exit=$?" | tee -a "$NOTES"

say "run --mode full (redacted)"
mkdir -p full
run "$BIN" run --mode full --json --md --zip $NETWORK --verbose --out full; echo "exit=$?" | tee -a "$NOTES"

say "run --mode ai (redacted)"
mkdir -p ai
run "$BIN" run --mode ai --json --out ai; echo "exit=$?" | tee -a "$NOTES"

say "snapshot (redacted)"
mkdir -p snap
run "$BIN" snapshot --out snap

say "fix catalog and dry-runs (nothing is changed)"
run "$BIN" fix --all
run "$BIN" fix --id update-ldconfig --dry-run --journal ./journal; echo "exit=$?" | tee -a "$NOTES"
run "$BIN" fix --id blacklist-nouveau --dry-run --journal ./journal; echo "exit=$?" | tee -a "$NOTES"
if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
  run sudo "$BIN" fix --id update-ldconfig --dry-run --journal ./journal-root; echo "exit=$?" | tee -a "$NOTES"
fi

say "collector notes and findings (what to look at first)"
python3 - <<'PY' 2>/dev/null | tee -a "$NOTES" || grep -A40 "COLLECTOR NOTES" full/report.txt | tee -a "$NOTES"
import json
j = json.load(open("full/report.json", encoding="utf-8"))
print("findings:", [(f["id"], f["severity"]) for f in j["findings"]])
print("gpus:", [(g["index"], g["name"]) for g in j["gpus"]])
print("gpu_thermal:", [(t["gpu_index"], t["temperature_c"], t["power_state"], t.get("throttle_reasons")) for t in j.get("gpu_thermal", [])])
print("gpu_pcie:", [(p["gpu_index"], p["current_speed"], p["max_speed"], p["downshifted"], p["idle_likely"]) for p in j.get("gpu_pcie", [])])
print("jetson:", j["system"].get("is_jetson"), j["system"].get("jetson_release"))
# spec section 4: platform / unified memory sections (present once the Spark collectors have landed)
p = j.get("platform") or {}
print("platform:", {k: p.get(k) for k in ("class", "vendor", "model", "product_version", "gpu_soc", "compute_cap", "unified_memory")})
um = j.get("unified_memory")
if um:
    print("unified_memory:", {k: um.get(k) for k in ("mem_total_kb", "mem_available_kb", "swap_free_kb", "allocatable_kb")})
print("gpu flags:", [(g["index"], g.get("compute_cap"), g.get("memory_reporting"), g.get("on_package")) for g in j["gpus"]])
for e in j.get("collector_errors") or []:
    print("collector note:", e["collector"], "->", e["error"])
PY

if [ "$IS_SPARK" = 1 ] && [ "$CAPTURE" = 1 ]; then
  say "DGX Spark raw capture (read-only, redacted; spec section 12)"
  CAP="$SCRIPT_DIR/spark-capture.sh"
  if [ ! -f "$CAP" ]; then
    # Piped via curl | bash: fetch the capture script from the same tree as this one.
    CAP="./spark-capture.sh"
    curl -fsSL -o "$CAP" "https://raw.githubusercontent.com/$REPO/main/scripts/spark-capture.sh" || { echo "could not download spark-capture.sh; skipping" | tee -a "$NOTES"; CAP=""; }
  fi
  if [ -n "$CAP" ]; then
    bash "$CAP" --out spark-capture 2>&1 | tail -5 | tee -a "$NOTES"
    echo "capture files: $(ls spark-capture | wc -l) (see spark-capture/CHECKLIST.md)" | tee -a "$NOTES"
  fi
fi

cd ..
tar czf "$WORK.tar.gz" "$WORK"
echo
echo "Bundle: $WORK.tar.gz"
echo "Everything inside is redacted (hostname, user, home, IPs). Attach it to"
echo "https://github.com/$REPO/issues/2 along with anything that looked wrong."
