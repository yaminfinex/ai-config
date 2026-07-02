#!/usr/bin/env bash
# delivery-driver.sh — pluggable driver interface for herder message delivery
#
# The driver abstraction maps herder's delivery intent (resolve, send) to a
# selected transport. `herder-send` and `herder-spawn` dispatch through this
# interface; callers remain transport-agnostic.
#
# Public interface:
#   driver_dispatch <op> <target> [msg] [opts...]
#   - op: resolve|send
#   - returns exit code per the contract: 0/1/2/64
#   - stdout: --json output on success, error message on stderr
#
# Driver protocol:
#   Each driver (herdr, hcom, etc.) implements two functions:
#   - herdr_resolve <target> → PANE_ID (exit 0/2)
#   - herdr_send <target> <msg> [opts...] → JSON record (exit 0/1/2)
#   - (similar for hcom_*, etc.)
#
# Selection (W3 — registry-driven):
#   - HERDER_BUS env (auto|herdr|hcom) selects driver
#   - default: auto — resolve <target> against the spawn registry; a recorded
#     hcom_name (the peer was launched through hcom into a team bus) ⇒ hcom
#     transport, else herdr keystrokes. This REPLACES the old `hcom list <target>`
#     capability probe: the registry record already knows the transport AND its bus
#     coordinate (hcom_dir), so there is no second namespace to disambiguate.
#   - herdr is always available (fallback for non-bus peers / no record).

set -euo pipefail

# Source all available driver implementations. Each driver file defines its
# <driver>_<op> functions (e.g. herdr_resolve, herdr_send, hcom_resolve, hcom_send).
# Function names are driver-prefixed, so sourcing order never causes a collision;
# select_driver() picks the prefix and driver_dispatch calls "<driver>_<op>".
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

# ---- registry lookup (shared by selection + the hcom driver) -----
# Resolve a user-facing target (guid | short_guid | label) to its latest registry
# record. Prints the record JSON on stdout (empty if none). term_*/raw pane ids never
# match a guid/short_guid/label field, so they return empty here → herdr verbatim path.
_registry_record_for() {
  local target="$1"
  local reg="${HERDER_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herder}/registry.jsonl"
  [[ -f "$reg" ]] || return 0
  jq -sc --arg v "$target" '
    group_by(.guid) | map(.[-1])
    | map(select(.guid==$v or .short_guid==$v or .label==$v))
    | last // empty' "$reg" 2>/dev/null || true
}

# ---- driver selection (KTD3: registry-driven + hard herdr fallback) -----

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

  # Auto: the registry record decides. A recorded, non-empty hcom_name means the peer
  # is bus-bound (launched through hcom); route to the hcom driver iff the hcom CLI +
  # driver are actually present. Anything without a bus name (bash panes, term_*/pane
  # targets, records predating W2, unknown peers) stays on herdr keystrokes.
  local rec hcom_name
  rec="$(_registry_record_for "$target")"
  if [[ -n "$rec" ]]; then
    hcom_name="$(printf '%s' "$rec" | jq -r '.hcom_name // ""' 2>/dev/null || printf '')"
    if [[ -n "$hcom_name" && "$hcom_name" != "null" ]] \
        && command -v hcom >/dev/null 2>&1 && declare -f hcom_send >/dev/null 2>&1; then
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
  local op="${1:-}" driver
  local exit_code=0

  if [[ -z "$op" ]]; then
    printf 'delivery-driver: op required\n' >&2
    return 64
  fi
  shift

  # Remaining args are: <target> [msg] [opts...]. Forward ALL of them verbatim to
  # the driver so send-path flags (--no-enter/--no-verify/--force/--timeout/--json)
  # reach the driver function unmodified.
  local target="${1:-}"

  # Select the driver for this target
  driver="$(select_driver "$target")"

  # Call the driver's op function
  local func_name="${driver}_${op}"
  if ! declare -f "$func_name" >/dev/null 2>&1; then
    printf 'delivery-driver: %s driver does not implement %s\n' "$driver" "$op" >&2
    return 64
  fi

  # Call the driver function with the full remaining arg vector; capture its code.
  "$func_name" "$@" || exit_code=$?

  return "$exit_code"
}
