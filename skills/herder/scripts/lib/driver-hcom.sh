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

hcom_send() {
  local target="$1" message="$2"
  local verify_result="not_attempted" submitted=false pane_id=""

  # Pre-flight: check if target is joined and usable
  if ! hcom list "$target" >/dev/null 2>&1; then
    printf 'hcom_send: target %s not found on bus (not joined or does not exist)\n' "$target" >&2
    return 2
  fi

  # Orchestrator is the implicit sender (external identity; need not be joined)
  local sender="${HERDER_AGENT_LABEL:-orchestrator}"

  # Send the message via hcom
  # Exit 0 = delivered/queued, exit 2 = not found
  if ! hcom send --from "$sender" "@$target" -- "$message" >/dev/null 2>&1; then
    printf 'hcom_send: failed to send message to %s\n' "$target" >&2
    return 2
  fi

  submitted=true

  # Verify delivery by polling hcom events for a deliver: ack from the target.
  # hcom coalesces burst sends into a single atomic injection, ordered, zero drops.
  # The `deliver:` event is an ack = real delivery semantics.
  local start_time events_output delivery_acked=0
  start_time=$(date +%s)

  while true; do
    local now_time elapsed
    now_time=$(date +%s)
    elapsed=$((now_time - start_time))

    # Timeout at 3 seconds (herder standard)
    if [[ $elapsed -ge 3 ]]; then
      # Timeout: assume queued (mid-turn injection will happen at next boundary)
      verify_result="queued"
      break
    fi

    # Poll recent events for this target
    if events_output=$(hcom events --tail 10 2>/dev/null); then
      # Check for `deliver:` ack from target in the events
      if printf '%s' "$events_output" | grep -qE "deliver:.*$target"; then
        delivery_acked=1
        verify_result="delivered"
        break
      fi
    fi

    sleep 0.25
  done

  # Emit JSON record (preserve herder-send shape)
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

  # Exit codes: 0 = success (delivered/queued), 1 = delivery failure, 2 = not found
  [[ "$verify_result" == "delivered" || "$verify_result" == "queued" ]] && exit 0
  exit 1
}

# ---- ring (best-effort doorbell via hcom) ----

hcom_ring() {
  local target="$1" message="$2"

  # Ring is just a send through hcom; queue if busy (exit 0)
  hcom_send "$target" "$message" || {
    local rc=$?
    [[ $rc -eq 2 ]] && exit 2 || exit 0  # refuse on not found, queue on failure
  }
  exit 0
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
