#!/usr/bin/env bash
# driver-herdr.sh — keystroke-based delivery driver (herdr agent send + pane send-keys)
#
# Implements the driver interface for the herdr native transport: registry-based
# resolution, herdr agent send + Enter, and sigil-heuristic delivery verification.
# This is the current herder-send logic extracted unchanged (R2, R5).

set -euo pipefail

# Shared state and config
HERDR_STATE_DIR="${HERDER_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herder}"
HERDR_REGISTRY="$HERDR_STATE_DIR/registry.jsonl"
HERDR_TIMEOUT_MS="${HERDER_TIMEOUT_MS:-3000}"

# ---- resolution (drift-proof) ----
# Internal helper: resolve without setting globals
# Outputs: "pane_id|resolved_via" on stdout for parsing
_herdr_resolve_internal() {
  local target="$1"
  local pane_id resolved_via drift_note
  local pane_list_json

  # Snapshot the live pane→terminal map once
  pane_list_json="$(herdr pane list 2>/dev/null || true)"
  resolved_via="verbatim"
  drift_note=""

  # A bare terminal_id (term_*) resolves DIRECTLY to its current pane
  # (no drift possible; term_* is the direct live handle)
  if [[ "$target" == term_* ]]; then
    pane_id="$(printf '%s' "$pane_list_json" | jq -r --arg t "$target" \
      '.result.panes[]? | select(.terminal_id==$t) | .pane_id' 2>/dev/null | head -n1)"
    if [[ -n "$pane_id" ]]; then
      printf '%s|terminal_id(direct)|false|' "$pane_id"
      return 0
    fi
    local npanes
    npanes="$(printf '%s' "$pane_list_json" | jq '[.result.panes[]?] | length' 2>/dev/null || echo 0)"
    if [[ "${npanes:-0}" -eq 0 ]]; then
      printf 'herdr_resolve: could not read live pane list; not resolving %s\n' "$target" >&2
      return 1
    fi
    printf 'herdr_resolve: terminal %s is not live — agent gone or culled\n' "$target" >&2
    return 2
  fi

  # Registry lookup: guid/label/short_guid
  local rec term stored label live npanes
  if [[ -f "$HERDR_REGISTRY" ]]; then
    rec="$(jq -sc --arg v "$target" '
      group_by(.guid) | map(.[-1])
      | map(select(.guid==$v or .short_guid==$v or .label==$v))
      | last // empty' "$HERDR_REGISTRY")"
  fi
  if [[ -z "${rec:-}" ]]; then
    # No registry record; treat as verbatim pane_id (no drift possible)
    printf '%s|verbatim|false|' "$target"
    return 0
  fi

  term="$(printf '%s' "$rec" | jq -r '.terminal_id // empty')"
  stored="$(printf '%s' "$rec" | jq -r '.pane_id // empty')"
  label="$(printf '%s' "$rec" | jq -r '.label // empty')"
  if [[ -z "$term" ]]; then
    # Legacy record without terminal_id; best effort with stored pane (no drift check possible)
    printf '%s|stored_pane(no terminal_id)|false|' "$stored"
    return 0
  fi

  # Look up terminal_id in live pane list (drift-proof)
  live="$(printf '%s' "$pane_list_json" | jq -r --arg t "$term" \
    '.result.panes[]? | select(.terminal_id==$t) | .pane_id' 2>/dev/null | head -n1)"
  if [[ -n "$live" ]]; then
    # Detect pane drift: if stored pane != live pane, the terminal moved
    local drifted=false drift_note=""
    if [[ -n "$stored" && "$live" != "$stored" ]]; then
      drifted=true
      drift_note="pane drifted — ${label:-$target} spawned at $stored, terminal $term now at $live"
    fi
    printf '%s|terminal_id|%s|%s' "$live" "$drifted" "$drift_note"
    return 0
  fi

  # terminal_id is not live
  npanes="$(printf '%s' "$pane_list_json" | jq '[.result.panes[]?] | length' 2>/dev/null || echo 0)"
  if [[ "${npanes:-0}" -eq 0 ]]; then
    printf 'herdr_resolve: could not read live pane list; not resolving %s\n' "$target" >&2
    return 1
  fi
  printf 'herdr_resolve: %s (terminal %s) is not live — agent gone or culled\n' "${label:-$target}" "$term" >&2
  return 2
}

# Public wrapper for external use (e.g., from herder-send --dry-run)
herdr_resolve() {
  local output
  output="$(_herdr_resolve_internal "$1")" || return $?
  # Parse output format: "pane_id|resolved_via"
  HERDR_RESOLVED_VIA="${output##*|}"
  printf '%s' "${output%|*}"
}

# ---- send (with delivery verification) ----

herdr_send() {
  local target="$1" message="$2"
  local pane_id resolved_via drift_note
  local kind sigil pre_text_raw pre_text pre_status
  local msg_probe pre_blobs

  # Resolve target to pane_id
  pane_id="$(herdr_resolve "$target" 2>/dev/null)" || {
    local rc=$?
    [[ $rc -eq 1 ]] && exit 1 || exit 2
  }
  resolved_via="terminal_id"  # herdr_resolve returns terminal-resolved or verbatim; assume terminal for now
  [[ "$target" == term_* || "$target" =~ ^pane_ ]] && resolved_via="verbatim"

  # Detect agent kind and sigil
  kind="$(herdr agent list 2>/dev/null \
    | jq -r --arg pid "$pane_id" \
        '.result.agents[]? | select(.pane_id==$pid) | .agent // empty' \
    | head -n1)" || kind=""
  case "$kind" in
    codex)  sigil='›';;
    claude) sigil='❯';;
    *)      sigil='';;
  esac

  # Pre-flight check
  pre_text_raw="$(herdr agent read "$pane_id" --source recent-unwrapped --lines 80 2>&1 | jq -r '.result.read.text // empty' 2>/dev/null || true)"
  pre_text="$(printf '%s' "$pre_text_raw" | python3 -c '
import re, sys
t = sys.stdin.read()
t = re.sub(r"\x1b\[[0-9;]*[A-Za-z]", "", t)
t = re.sub(r"\x1b\][^\x07]*\x07", "", t)
sys.stdout.write(t)
' 2>/dev/null || cat)"

  # Check for blocked states
  if grep -qE '(Conversation interrupted|Interrupted by user)' <<<"$pre_text"; then
    printf 'herdr_send: refusing to send: agent in "Conversation interrupted" state\n' >&2
    exit 2
  fi
  if grep -qE 'Do you trust the contents of this directory|Do you trust the files in this folder|Is this a project you created|Yes, I trust this folder' <<<"$pre_text"; then
    printf 'herdr_send: refusing to send: directory-trust prompt is open\n' >&2
    exit 2
  fi
  if grep -qE '(Sandbox approval|Approve command\?|Allow this command\?)' <<<"$pre_text"; then
    printf 'herdr_send: refusing to send: codex approval modal is open\n' >&2
    exit 2
  fi
  if grep -qE '(Do you want to allow|Permission required)' <<<"$pre_text"; then
    printf 'herdr_send: refusing to send: permission prompt is open\n' >&2
    exit 2
  fi

  # Snapshot pre-status for delivery detection
  pre_status="$(herdr agent list 2>/dev/null | jq -r --arg pid "$pane_id" '.result.agents[]? | select(.pane_id==$pid) | .agent_status // empty' | head -n1)" || pre_status=""

  # Prepare message probe
  msg_probe="$(printf '%s' "$message" | grep -v '^[[:space:]]*$' | tail -n1)"
  [[ "${#msg_probe}" -gt 45 ]] && msg_probe="${msg_probe: -45}"

  # Count pre-send paste blobs for codex collapse detection
  pre_blobs="$(grep -cE '\[Pasted (Content|text)' <<<"$pre_text" 2>/dev/null)" || pre_blobs=0

  # Send the message
  if ! herdr agent send "$pane_id" "$message" >/dev/null 2>&1; then
    sleep 0.4
  fi

  # Verify message landed
  local w=0 post landed=0 paste_collapsed=0
  while [[ $w -lt 2500 ]]; do
    sleep 0.25
    # Status confirms delivery (agent moved to working)
    local now
    now="$(herdr agent list 2>/dev/null | jq -r --arg pid "$pane_id" '.result.agents[]? | select(.pane_id==$pid) | .agent_status // empty' | head -n1)" || now=""
    if [[ "$now" == "working" && "$pre_status" != "working" ]]; then
      landed=1
      break
    fi
    # Message text present in pane
    post="$(herdr agent read "$pane_id" --source recent-unwrapped --lines 80 2>&1 | jq -r '.result.read.text // empty' 2>/dev/null || true)"
    if [[ -n "$msg_probe" ]] && grep -qF -- "$msg_probe" <<<"$post"; then
      landed=1
      break
    fi
    # New paste blob appeared
    local post_blobs
    post_blobs="$(grep -cE '\[Pasted (Content|text)' <<<"$post" 2>/dev/null)" || post_blobs=0
    if [[ "$post_blobs" -gt "$pre_blobs" ]]; then
      landed=1
      paste_collapsed=1
      break
    fi
    w=$((w + 250))
  done

  # Submit (press Enter)
  local submitted=false
  if [[ $landed -eq 1 && -n "$msg_probe" ]]; then
    sleep 0.2
    herdr pane send-keys "$pane_id" Enter >/dev/null 2>&1 || true
    submitted=true
  fi

  # Verify delivery
  local verify_result="not_attempted" queued=0 delivered=0 extra_enter_sent=false

  if [[ $landed -eq 0 ]]; then
    verify_result="not_landed"
  elif [[ "$submitted" == "true" ]]; then
    local step_ms=250 elapsed=0
    # Poll for delivered state
    while [[ $elapsed -lt $HERDR_TIMEOUT_MS ]]; do
      sleep "$(awk -v ms="$step_ms" 'BEGIN{printf "%.3f", ms/1000}')"
      # Status delivery confirmation
      now="$(herdr agent list 2>/dev/null | jq -r --arg pid "$pane_id" '.result.agents[]? | select(.pane_id==$pid) | .agent_status // empty' | head -n1)" || now=""
      if [[ "$now" == "working" && "$pre_status" != "working" ]]; then
        delivered=1
        break
      fi
      # Message text + not in sigil = submitted
      post="$(herdr agent read "$pane_id" --source recent-unwrapped --lines 80 2>&1 | jq -r '.result.read.text // empty' 2>/dev/null || true)"
      if [[ -n "$msg_probe" ]] && grep -qF -- "$msg_probe" <<<"$post"; then
        # Check if it's trailing the sigil (not submitted yet)
        if [[ -z "$sigil" ]] || ! grep -qF "$sigil " <<<"$post"; then
          # No sigil line or msg not on sigil line = submitted
          delivered=1
          break
        elif [[ -n "$sigil" ]]; then
          local last_sigil
          last_sigil="$(grep -F "$sigil " <<<"$post" | tail -n1 || true)"
          if ! grep -qF -- "$msg_probe" <<<"$last_sigil"; then
            delivered=1
            break
          fi
        fi
      fi
      elapsed=$((elapsed + step_ms))
    done

    # Handle codex paste collapse
    if [[ "$paste_collapsed" -eq 1 ]]; then
      local i
      for i in 1 2 3 4 5 6 7 8; do
        local blob_count
        blob_count="$(grep -cE '\[Pasted (Content|text)' <<<"$(herdr agent read "$pane_id" --source recent-unwrapped --lines 80 2>&1 | jq -r '.result.read.text // empty' 2>/dev/null || true)" 2>/dev/null)" || blob_count=0
        [[ "$blob_count" -le "$pre_blobs" ]] && { delivered=1; break; }
        sleep 0.3
      done
      if [[ $delivered -eq 0 ]]; then
        herdr pane send-keys "$pane_id" Enter >/dev/null 2>&1 || true
        extra_enter_sent=true
        for i in 1 2 3 4 5 6 7 8; do
          blob_count="$(grep -cE '\[Pasted (Content|text)' <<<"$(herdr agent read "$pane_id" --source recent-unwrapped --lines 80 2>&1 | jq -r '.result.read.text // empty' 2>/dev/null || true)" 2>/dev/null)" || blob_count=0
          [[ "$blob_count" -le "$pre_blobs" ]] && { delivered=1; break; }
          sleep 0.3
        done
      fi
    elif [[ "$pre_status" == "working" ]]; then
      # Target was already working; message is queued
      queued=1
      delivered=1
    elif [[ $delivered -eq 0 ]]; then
      # Recovery: codex absorbs first Enter on interrupt, try one more
      if [[ -n "$msg_probe" ]]; then
        local last_line
        last_line="$(printf '%s\n' "$post" | awk 'NF{l=$0} END{print l}')"
        if grep -qF -- "$msg_probe" <<<"$last_line"; then
          herdr pane send-keys "$pane_id" Enter >/dev/null 2>&1 || true
          extra_enter_sent=true
          # Re-poll
          for ((i=0; i<10; i++)); do
            sleep 0.25
            now="$(herdr agent list 2>/dev/null | jq -r --arg pid "$pane_id" '.result.agents[]? | select(.pane_id==$pid) | .agent_status // empty' | head -n1)" || now=""
            if [[ "$now" == "working" && "$pre_status" != "working" ]]; then
              delivered=1
              break
            fi
          done
        fi
      fi
    fi

    if [[ "$queued" -eq 1 ]]; then
      verify_result="queued"
    elif [[ "$delivered" -eq 1 ]]; then
      verify_result="delivered"
    else
      verify_result="not_delivered"
    fi
  fi

  # Emit JSON record (preserve herder-send shape)
  jq -nc \
    --arg pane "$pane_id" \
    --arg kind "$kind" \
    --arg target "$target" \
    --arg resolved_via "$resolved_via" \
    --argjson submitted "$submitted" \
    --arg verify "$verify_result" \
    --arg preview "$(printf '%s' "$message" | head -c 120)" \
    '{pane_id:$pane, agent:$kind, target:$target, resolved_via:$resolved_via, submitted:$submitted,
      verify:$verify, message_preview:$preview}'

  # Exit codes
  case "$verify_result" in
    not_landed|not_delivered) exit 1;;
  esac
  exit 0
}

# ---- ring (best-effort doorbell) ----

herdr_ring() {
  local target="$1" message="$2"
  # Ring is just a one-line send through herdr; queue if busy (exit 0)
  herdr_send "$target" "$message" || {
    local rc=$?
    [[ $rc -eq 2 ]] && exit 2 || exit 0  # refuse on gone target, queue on failure
  }
  exit 0
}

# ---- join (no-op for herdr) ----

herdr_join() {
  # Keystroke transport has no per-agent bus membership; this is a no-op
  exit 0
}
