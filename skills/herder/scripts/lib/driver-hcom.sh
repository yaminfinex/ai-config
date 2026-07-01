#!/usr/bin/env bash
# driver-hcom.sh — hcom bus delivery driver (hcom send + hcom join)
#
# Implements the driver interface for the hcom message bus: bus-aware
# resolution, send via hcom CLI, and delivery verification via hcom events.
# Codex is a first-class hcom target (not special-cased to herdr).

set -euo pipefail

# hcom env setup
HCOM_DIR="${HCOM_DIR:-$HOME/.hcom}"

# ---- resolution (check if target is joined on the bus) ----

hcom_resolve() {
  local target="$1"

  # Use `hcom list <target>` to check if the instance is joined and usable.
  # Exit 0 if found, exit 2 if not found (clean fallback signal).
  if hcom list "$target" >/dev/null 2>&1; then
    printf '%s' "$target"
    return 0
  fi

  # Not found on the bus — signal fallback to herdr
  return 2
}

# ---- send (via hcom with delivery verification) ----
# Opts (forwarded through driver_dispatch) are accepted for interface symmetry with
# the herdr driver; only --json affects hcom output (keystroke-only flags like
# --no-enter/--force/--timeout have no meaning on a message bus and are ignored).
# Return contract (NOT exit — so hcom_ring's degrade branch stays live, S3):
#   0 = delivered / queued (busy → runs next),  1 = real delivery failure,
#   2 = target not joined on the bus (→ selection would have fallen back to herdr).

hcom_send() {
  local target="$1" message="$2"; shift 2 || shift $#
  local json_out=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --json)      json_out="${2:-1}"; shift 2;;
      --no-enter|--no-verify|--force|--timeout) shift 2;;
      *)           shift;;
    esac
  done

  local verify_result="not_attempted" submitted=false pane_id=""

  # Pre-flight: check if target is joined and usable
  if ! hcom list "$target" >/dev/null 2>&1; then
    printf 'hcom_send: target %s not found on bus (not joined or does not exist)\n' "$target" >&2
    return 2
  fi

  # Orchestrator is the implicit sender (external identity; need not be joined)
  local sender="${HERDER_AGENT_LABEL:-orchestrator}"

  # Baseline timestamp BEFORE the send so the delivery probe only counts an ack
  # correlated to THIS send (S1: no crediting a stale deliver: from a prior send).
  local start_iso
  start_iso="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  # Send the message via hcom. The target was confirmed joined above, so a non-zero
  # here is a REAL delivery failure (not a "not joined" fallback case) → exit 1.
  if ! hcom send --from "$sender" "@$target" -- "$message" >/dev/null 2>&1; then
    verify_result="not_delivered"
  else
    submitted=true

    # Verify delivery by polling hcom's event stream for a `deliver:` ack addressed
    # to THIS target and emitted AFTER our send timestamp. `--context`/`--after` are
    # passed as hcom filter VALUES (no shell-interpolated ERE — S1 regex-injection
    # fixed) and `--after` correlates the ack to this send, not any earlier one.
    # hcom coalesces bursts into one ordered, lossless injection; the deliver: event
    # is the receipt. No ack within the window ⇒ queued (mid-turn inject next boundary).
    local start_time elapsed acked=0
    start_time=$(date +%s)
    while true; do
      elapsed=$(( $(date +%s) - start_time ))
      [[ $elapsed -ge 3 ]] && break
      if [[ "$(hcom events --last 50 --context "deliver:$target" --after "$start_iso" 2>/dev/null \
                | jq 'length' 2>/dev/null || echo 0)" -gt 0 ]]; then
        acked=1; break
      fi
      sleep 0.25
    done
    if [[ "$acked" -eq 1 ]]; then verify_result="delivered"; else verify_result="queued"; fi
  fi

  # ---- report: human summary to STDERR always (matches herdr driver contract) ----
  {
    printf 'sent %d chars to %s (hcom)' "${#message}" "$target"
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
      --arg resolved_via "hcom_list" \
      --argjson submitted "$submitted" \
      --arg verify "$verify_result" \
      --arg preview "$(printf '%s' "$message" | head -c 120)" \
      '{pane_id:$pane, agent:$kind, target:$target, resolved_via:$resolved_via, submitted:$submitted,
        verify:$verify, message_preview:$preview}'
  fi

  # Return codes: 0 = delivered/queued, 1 = real delivery failure, 2 = not joined.
  case "$verify_result" in
    delivered|queued) return 0;;
    *)                return 1;;
  esac
}

# ---- ring (best-effort doorbell via hcom) ----

hcom_ring() {
  local target="$1" message="$2"

  # Ring is a best-effort send through hcom: refuse (2) on a target that isn't on
  # the bus, otherwise queue (0). hcom_send RETURNS its code (not exit), so this
  # degrade branch is live (S3).
  hcom_send "$target" "$message" || {
    local rc=$?
    [[ $rc -eq 2 ]] && return 2 || return 0
  }
  return 0
}

# ---- join (start hcom with the given label in child) ----

hcom_join() {
  local agent_label="$1"

  # Child process joins the hcom bus with the given label.
  # The name must be pinned to the herder label for resolution to work.
  # HCOM_DIR must be in the child's process env at launch (injected by herder-spawn).
  # Exit 0 on success.

  if ! command -v hcom >/dev/null 2>&1; then
    printf 'hcom_join: hcom not available; falling back to herdr\n' >&2
    return 1
  fi

  # Start hcom with instance name pinned to the herder label
  hcom start --as "$agent_label" >/dev/null 2>&1 || {
    printf 'hcom_join: failed to join hcom bus as %s\n' "$agent_label" >&2
    return 1
  }

  exit 0
}
