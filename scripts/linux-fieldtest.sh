#!/usr/bin/env bash
# NVCheckup Linux field kit.
#
# One command to run on a Linux box with a real NVIDIA GPU (or a Jetson) and
# produce a bundle we can look at. Everything it runs is read-only; the two
# `fix` invocations are --dry-run and change nothing. Reports are redacted by
# default, so the bundle is safe to attach to
# https://github.com/thatcooperguy/NVCheckup/issues/2
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/thatcooperguy/NVCheckup/main/scripts/linux-fieldtest.sh | bash
#   ./scripts/linux-fieldtest.sh                 # downloads the latest release for this CPU
#   ./scripts/linux-fieldtest.sh --local ./nvcheckup   # use a binary you built yourself
#   ./scripts/linux-fieldtest.sh --network       # also run the opt-in ping/traceroute/DNS probes
set -uo pipefail

REPO="thatcooperguy/NVCheckup"
BIN=""
NETWORK=""
while [ $# -gt 0 ]; do
  case "$1" in
    --local) BIN="$2"; shift 2 ;;
    --network) NETWORK="--network"; shift ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

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
command -v nvidia-smi >/dev/null && run nvidia-smi -L || echo "nvidia-smi: not on PATH (expected on Jetson)" | tee -a "$NOTES"

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
for e in j.get("collector_errors") or []:
    print("collector note:", e["collector"], "->", e["error"])
PY

cd ..
tar czf "$WORK.tar.gz" "$WORK"
echo
echo "Bundle: $WORK.tar.gz"
echo "Everything inside is redacted (hostname, user, home, IPs). Attach it to"
echo "https://github.com/$REPO/issues/2 along with anything that looked wrong."
