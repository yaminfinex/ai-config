#!/usr/bin/env bash
# driver-hcom.sh — hcom bus delivery driver (hcom send)
#
# Implements the driver interface for the hcom message bus: registry-driven
# resolution, send via hcom CLI scoped to the peer's recorded team bus, and
# delivery verification via hcom events. Codex is a first-class hcom target.
#
# W3 identity resolution: the user addresses a peer by guid/short_guid/label; the
# spawn registry maps that to the peer's bus coordinate — its hcom_name (the
# hcom-assigned bus name) and hcom_dir (the team bus, i.e. the HCOM_DIR the peer
# joined). The send is scoped to that HCOM_DIR and addressed to @<hcom_name>, which
# is the proven cross-team external-send bridge (orchestrator sends into any team
# bus by pinning HCOM_DIR + naming the recipient), now automatic.
# Hard dependency: source via delivery-driver.sh so _registry_record_for is defined.

set -euo pipefail

# ---- registry → bus coordinate ----
# Map a user target → "hcom_name<TAB>hcom_dir" from the spawn registry. Returns
# non-zero when the target has no registry row at all (e.g. an ad-hoc raw bus name
# under a forced HERDER_BUS=hcom); callers decide how to treat that. A row that
# exists but carries no hcom_name (a non-bus peer) yields an EMPTY name field.
# Relies on _registry_record_for from delivery-driver.sh (sourced before dispatch).
_hcom_coord_for() {
  local target="$1" rec
  if ! declare -f _registry_record_for >/dev/null 2>&1; then
    printf 'driver-hcom.sh: _registry_record_for missing; source delivery-driver.sh before driver-hcom.sh\n' >&2
    return 64
  fi
  rec="$(_registry_record_for "$target" 2>/dev/null || true)"
  [[ -n "$rec" ]] || return 1
  printf '%s\t%s' \
    "$(printf '%s' "$rec" | jq -r '.hcom_name // ""' 2>/dev/null || printf '')" \
    "$(printf '%s' "$rec" | jq -r '.hcom_dir  // ""' 2>/dev/null || printf '')"
}

# ---- resolution (map target → effective bus name) ----
# Prints the effective hcom bus name for a target. Exit 0 with the name on success;
# exit 2 when a registry row exists but the peer is NOT bus-bound (empty hcom_name)
# — the clean "fall back to herdr" signal. With no registry row (forced-hcom ad-hoc
# addressing), the target is treated as a literal bus name.
hcom_resolve() {
  local target="$1" coord name
  if ! coord="$(_hcom_coord_for "$target")"; then
    printf '%s' "$target"   # no row → literal bus name (forced-hcom path)
    return 0
  fi
  name="${coord%%$'\t'*}"
  if [[ -n "$name" && "$name" != "null" ]]; then
    printf '%s' "$name"
    return 0
  fi
  return 2   # recorded but not bus-bound → herdr
}

# ---- send (via hcom with delivery verification) ----
# Opts (forwarded through driver_dispatch): --timeout sets the delivery-verify poll
# window; --json affects output. The keystroke-only flags (--no-enter/--no-verify/
# --force) have no meaning on a message bus and are accepted-and-ignored for interface
# symmetry with the herdr driver.
# Return contract (NOT exit — dispatch captures the return code; an exit would kill
# the sourcing script):
#   0 = delivered / queued (busy → runs next),  1 = real delivery failure,
#   2 = target not joined on the bus (→ selection would have fallen back to herdr).

hcom_send() {
  local target="$1" message="$2"; shift 2 || shift $#
  local json_out=0 TIMEOUT_MS=3000
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --json)      json_out="${2:-1}"; shift 2;;
      --timeout)   TIMEOUT_MS="${2:-3000}"; shift 2;;
      --no-enter|--no-verify|--force) shift 2;;
      *)           shift;;
    esac
  done

  local verify_result="not_attempted" submitted=false pane_id=""

  # Resolve target → bus coordinate (hcom_name @ hcom_dir) from the registry. The
  # user addresses by guid/label; the recipient on the wire is the recorded hcom_name.
  local bus_name="$target" bus_dir="" coord
  if coord="$(_hcom_coord_for "$target")"; then
    bus_name="${coord%%$'\t'*}"
    bus_dir="${coord#*$'\t'}"
    if [[ -z "$bus_name" || "$bus_name" == "null" ]]; then
      # Registry row exists but the peer is not bus-bound → selection should have
      # routed to herdr; signal fallback rather than send blind.
      printf 'hcom_send: %s has no recorded bus name (not launched through hcom)\n' "$target" >&2
      return 2
    fi
  fi
  # else: no registry row — forced HERDER_BUS=hcom addressing a literal bus name;
  # bus_name=target, bus_dir="" (inherit the ambient HCOM_DIR).

  # Scope every hcom invocation below to the peer's team bus. Empty bus_dir → inherit
  # the caller's HCOM_DIR (global default). Local to this function via a subshell-free
  # save/restore so we never leak HCOM_DIR into the sourcing script's environment.
  local _had_dir="${HCOM_DIR+set}" _prev_dir="${HCOM_DIR:-}"
  if [[ -n "$bus_dir" && "$bus_dir" != "null" ]]; then
    export HCOM_DIR="$bus_dir"
  fi
  _hcom_restore_dir() {
    if [[ "$_had_dir" == "set" ]]; then export HCOM_DIR="$_prev_dir"; else unset HCOM_DIR; fi
  }

  # Pre-flight: check the recipient is joined and usable on THIS (scoped) bus.
  if ! hcom list "$bus_name" >/dev/null 2>&1; then
    printf 'hcom_send: target %s (@%s) not found on bus (not joined or does not exist)\n' "$target" "$bus_name" >&2
    _hcom_restore_dir
    return 2
  fi

  # Orchestrator is the implicit sender (external identity; need not be joined)
  local sender="${HERDER_LABEL:-orchestrator}"

  # Baseline timestamp BEFORE the send so the delivery probe only counts an ack
  # correlated to THIS send (S1: no crediting a stale deliver: from a prior send).
  local start_iso
  start_iso="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  # Send the message via hcom to the recorded bus name. The recipient was confirmed
  # joined above, so a non-zero here is a REAL delivery failure (not "not joined") → 1.
  if ! hcom send --from "$sender" "@$bus_name" -- "$message" >/dev/null 2>&1; then
    verify_result="not_delivered"
  else
    submitted=true

    # Verify delivery by polling hcom's event stream for a `deliver:` ack addressed
    # to THIS bus name and emitted AFTER our send timestamp. `--context`/`--after` are
    # passed as hcom filter VALUES (no shell-interpolated ERE — S1 regex-injection
    # fixed) and `--after` correlates the ack to this send, not any earlier one.
    # hcom coalesces bursts into one ordered, lossless injection; the deliver: event
    # is the receipt. No ack within the window ⇒ queued (mid-turn inject next boundary).
    local start_time elapsed acked=0 window_s=$(( (TIMEOUT_MS + 999) / 1000 ))
    start_time=$(date +%s)
    while true; do
      elapsed=$(( $(date +%s) - start_time ))
      [[ $elapsed -ge $window_s ]] && break
      if [[ "$(hcom events --last 50 --context "deliver:$bus_name" --after "$start_iso" 2>/dev/null \
                | jq 'length' 2>/dev/null || echo 0)" -gt 0 ]]; then
        acked=1; break
      fi
      sleep 0.25
    done
    if [[ "$acked" -eq 1 ]]; then verify_result="delivered"; else verify_result="queued"; fi
  fi

  # Bus interaction done — restore the caller's HCOM_DIR before returning.
  _hcom_restore_dir

  # ---- report: human summary to STDERR always (matches herdr driver contract) ----
  # Report the user-facing target; note the bus name when it differs (guid → @name).
  {
    printf 'sent %d chars to %s (hcom @%s)' "${#message}" "$target" "$bus_name"
    [[ "$submitted" == "true" ]] && printf ', submitted'
    printf ', verify=%s' "$verify_result"
    [[ "$verify_result" == "queued" ]] && printf ' (target was busy; message queued to run next — do NOT resend)'
    printf '\n'
  } >&2

  # ---- JSON record to STDOUT only under --json (preserve herder-send shape) ----
  if [[ "$json_out" -eq 1 ]]; then
    jq -nc \
      --arg pane "$pane_id" \
      --arg kind "agent" \
      --arg target "$target" \
      --arg bus_name "$bus_name" \
      --arg hcom_dir "$bus_dir" \
      --arg resolved_via "registry" \
      --argjson submitted "$submitted" \
      --arg verify "$verify_result" \
      --arg preview "$(printf '%s' "$message" | head -c 120)" \
      '{pane_id:$pane, agent:$kind, target:$target, hcom_name:$bus_name, hcom_dir:$hcom_dir,
        resolved_via:$resolved_via, submitted:$submitted,
        verify:$verify, message_preview:$preview}'
  fi

  # Return codes: 0 = delivered/queued, 1 = real delivery failure, 2 = not joined.
  case "$verify_result" in
    delivered|queued) return 0;;
    *)                return 1;;
  esac
}
