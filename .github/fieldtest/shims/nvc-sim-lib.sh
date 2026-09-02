# shellcheck shell=bash
# Shared helpers for the bash shims in this directory (sourced, not executed).
#
# Every shim answers from the scenario JSON named by $NVC_SIM_SCENARIO (spec
# docs/roadmap/spark-support.md section 10) and never calls the real tool it
# replaces. JSON parsing is delegated to ./nvc-sim-scenario (python).
#
# Python resolution: $NVC_SIM_PYTHON if set, else python3 when it really runs
# (the Microsoft Store "python3" alias on Windows is a stub that only prints an
# install hint), else python. GitHub's Linux runners always have python3.

SIM_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SIM_PY=""

sim_py() {
  if [ -z "$SIM_PY" ]; then
    if [ -n "${NVC_SIM_PYTHON:-}" ]; then
      SIM_PY="$NVC_SIM_PYTHON"
    elif command -v python3 >/dev/null 2>&1 && python3 -c pass >/dev/null 2>&1; then
      SIM_PY="python3"
    else
      SIM_PY="python"
    fi
  fi
  "$SIM_PY" "$@"
}

sim_scn() { sim_py "$SIM_HERE/nvc-sim-scenario" "$@"; }
# sim_get KEY [DEFAULT]: scalar value; exit 1 when missing and no default.
sim_get() { sim_scn get "$@"; }
# sim_lines KEY: one list item per line; exit 1 when the key is missing.
sim_lines() { sim_scn lines "$1"; }
# sim_has KEY: exit 0 when the key exists.
sim_has() { sim_scn has "$1"; }
# sim_json KEY: JSON dump of the value.
sim_json() { sim_scn json "$1"; }
# sim_refuse: for subcommands that would change system state. Shims are part of
# a read-only tool's test rig, so they refuse instead of pretending to succeed.
sim_refuse() { echo "nvc-sim: '$*' would modify the system; the simulated shim refuses" >&2; exit 1; }
