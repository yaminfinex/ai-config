#!/usr/bin/env bash
# delivery-driver.sh — pluggable driver interface for herder message delivery
#
# The driver abstraction maps herder's delivery intent (send, ring, resolve, join)
# to a selected transport. `herder-send` and `herder-spawn` dispatch through this
# interface; callers remain transport-agnostic.
#
# Public interface:
#   driver_dispatch <op> <target> [msg] [opts...]
#   - op: resolve|send|ring|join
#   - returns exit code per the contract: 0/1/2/64
#   - stdout: --json output on success, error message on stderr
#
# Driver protocol:
#   Each driver (herdr, hcom, etc.) implements these functions:
#   - herdr_resolve <target> → PANE_ID (exit 0/2)
#   - herdr_send <target> <msg> [opts...] → JSON record (exit 0/1/2)
#   - herdr_ring <target> <msg> → exit 0/2
#   - herdr_join <agent> → exit 0
#   - (similar for hcom_*, etc.)
#
# Selection:
#   - HERDER_BUS env (auto|herdr|hcom) selects driver
#   - default: auto (capability detection)
#   - auto: pick hcom if available and target usable, else herdr
#   - herdr is always available (fallback)

set -euo pipefail

# Source all available driver implementations. Each driver file defines its
# driver_<op> functions (e.g. herdr_resolve, herdr_send, herdr_ring, herdr_join).
# Driver files are sourced in priority order; first to define a function wins.
# (In v1, only herdr and conditionally hcom are available.)

_driver_dir="$(dirname "${BASH_SOURCE[0]}")"

# Always source herdr driver (always available fallback)
if [[ ! -f "$_driver_dir/driver-herdr.sh" ]]; then
  printf 'delivery-driver.sh: herdr driver not found at %s/driver-herdr.sh\n' "$_driver_dir" >&2
  exit 64
fi
source "$_driver_dir/driver-herdr.sh"

# Conditionally source hcom driver if present
if [[ -f "$_driver_dir/driver-hcom.sh" ]]; then
  source "$_driver_dir/driver-hcom.sh"
fi

# ---- driver selection (KTD3: capability detection + fallback) -----

select_driver() {
  local target="$1" bus="${HERDER_BUS:-auto}"

  # Explicit override takes priority (herdr or hcom)
  if [[ "$bus" == "herdr" ]]; then
    printf '%s' "herdr"
    return 0
  fi
  if [[ "$bus" == "hcom" ]]; then
    printf '%s' "hcom"
    return 0
  fi

  # Auto-detection: hcom if available and target resolves as usable, else herdr
  if command -v hcom >/dev/null 2>&1 && declare -f hcom_resolve >/dev/null 2>&1; then
    # Try to resolve via hcom; if it succeeds, use hcom driver
    if hcom_resolve "$target" >/dev/null 2>&1; then
      printf 'hcom'
      return 0
    fi
  fi

  # Fallback to herdr (always available)
  printf 'herdr'
  return 0
}

# ---- driver dispatch -----

driver_dispatch() {
  local op="$1" target="${2:-}" msg="${3:-}" driver
  local exit_code=0

  if [[ -z "$op" ]]; then
    printf 'delivery-driver: op required\n' >&2
    return 64
  fi

  # Select the driver for this target
  driver="$(select_driver "$target")"

  # Call the driver's op function
  local func_name="${driver}_${op}"
  if ! declare -f "$func_name" >/dev/null 2>&1; then
    printf 'delivery-driver: %s driver does not implement %s\n' "$driver" "$op" >&2
    return 64
  fi

  # Call the driver function; capture its exit code
  if [[ -n "$msg" ]]; then
    "$func_name" "$target" "$msg" || exit_code=$?
  else
    "$func_name" "$target" || exit_code=$?
  fi

  return "$exit_code"
}

# ---- helpers (used by driver implementations) -----

# Return JSON string suitable for --json output, preserving the contract shape
driver_json_resolve() {
  local pane="$1" target="$2" via="$3" drifted="${4:-false}"
  jq -nc \
    --arg pane "$pane" \
    --arg target "$target" \
    --arg via "$via" \
    --argjson drifted "$drifted" \
    '{pane_id:$pane, target:$target, resolved_via:$via, drifted:$drifted, dry_run:true}'
}

driver_json_send() {
  local pane="$1" kind="$2" target="$3" via="$4" submitted="$5" verify="$6" preview="$7"
  jq -nc \
    --arg pane "$pane" \
    --arg kind "$kind" \
    --arg target "$target" \
    --arg via "$via" \
    --argjson submitted "$submitted" \
    --arg verify "$verify" \
    --arg preview "$preview" \
    '{pane_id:$pane, agent:$kind, target:$target, resolved_via:$via, submitted:$submitted,
      verify:$verify, message_preview:$preview}'
}
