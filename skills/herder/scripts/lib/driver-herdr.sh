#!/usr/bin/env bash
# driver-herdr.sh — keystroke-based delivery driver (herdr agent send + pane send-keys)
#
# Implements the driver interface for the herdr native transport: registry-based
# resolution, herdr agent send + Enter, and sigil-heuristic delivery verification.
# This is the current herder-send logic extracted BEHAVIOR-PRESERVING (R2, R5): the
# send/submit/verify/report path below is a faithful port of pristine `herder-send`
# on `main`. Golden fixtures under skills/herder/tests/ lock it byte-for-byte.

set -euo pipefail

# Shared trust-modal patterns (single source of truth; see trust-modals.sh).
# Used by _hd_preflight_blocked_reason below and by herder-spawn's modal clearing.
source "$(dirname "${BASH_SOURCE[0]}")/trust-modals.sh"

# Shared state and config
HERDR_STATE_DIR="${HERDER_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herder}"
HERDR_REGISTRY="$HERDR_STATE_DIR/registry.jsonl"

# ---- resolution (drift-proof) ----
# Internal helper: resolve without setting globals
# Outputs: "pane_id|resolved_via|drifted|drift_note" on stdout for parsing
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
    local drifted=false
    drift_note=""
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
  # Parse output format: "pane_id|resolved_via|drifted|drift_note"
  HERDR_RESOLVED_VIA="$(printf '%s' "$output" | cut -d'|' -f2)"
  printf '%s' "$(printf '%s' "$output" | cut -d'|' -f1)"
}

# ============================================================================
# Send-path helpers — faithful port of pristine herder-send. They read driver
# globals set by herdr_send:  PANE_ID  MSG_PROBE  SIGIL  PRE_STATUS  PRE_BLOBS
#                             TIMEOUT_MS  STEP_MS
# ============================================================================

_hd_read_pane() {
  herdr agent read "$PANE_ID" --source recent-unwrapped --lines 80 2>&1 \
    | jq -r '.result.read.text // empty' 2>/dev/null || true
}

_hd_detect_kind() {
  herdr agent list 2>/dev/null \
    | jq -r --arg pid "$PANE_ID" \
        '.result.agents[]? | select(.pane_id==$pid) | .agent // empty' \
    | head -n1
}

_hd_detect_status() {
  herdr agent list 2>/dev/null \
    | jq -r --arg pid "$PANE_ID" \
        '.result.agents[]? | select(.pane_id==$pid) | .agent_status // empty' \
    | head -n1
}

# Strip ANSI escape sequences and zero-width chars before pattern matching.
_hd_strip_chrome() {
  python3 -c '
import re, sys
t = sys.stdin.read()
t = re.sub(r"\x1b\[[0-9;]*[A-Za-z]", "", t)
t = re.sub(r"\x1b\][^\x07]*\x07", "", t)
sys.stdout.write(t)
' 2>/dev/null || cat
}

# Pre-flight state check. Refuses to send if the pane is in a state where input
# would be unsafe. Patterns are deliberately narrow.
_hd_preflight_blocked_reason() {
  local text="$1"
  if grep -qE '(Conversation interrupted|Interrupted by user)' <<<"$text"; then
    printf 'agent is in "Conversation interrupted" state; recover it first (focus pane and press Enter, or --force)'
    return 0
  fi
  if grep -qE "$HERDER_TRUST_MODAL_ERE" <<<"$text"; then
    printf 'first-run directory-trust prompt is open; accept it (focus pane + Enter) before sending'
    return 0
  fi
  if grep -qE '(Sandbox approval|Approve command\?|Allow this command\?)' <<<"$text"; then
    printf 'codex approval modal is open; resolve it manually before sending'
    return 0
  fi
  if grep -qE '(Do you want to allow|Permission required)' <<<"$text"; then
    printf 'claude permission prompt is open; resolve it manually before sending'
    return 0
  fi
  return 1
}

# Delivery is confirmed if the agent moved into `working` from a non-working
# pre-state — i.e. our submit kicked off a turn.
_hd_status_confirms_delivery() {
  local now; now="$(_hd_detect_status || true)"
  [[ "$now" == "working" && "$PRE_STATUS" != "working" ]]
}

_hd_pasted_blob_count() {  # count of paste-collapse placeholders in the given text
  local n
  n="$(grep -cE '\[Pasted (Content|text)' <<<"$1" 2>/dev/null)" || n=0
  printf '%s' "${n:-0}"
}

_hd_new_paste_blob() {  # a fresh paste-collapse placeholder appeared since we started
  [[ "$(_hd_pasted_blob_count "$1")" -gt "$PRE_BLOBS" ]]
}

_hd_msg_present() {  # message text visible anywhere in the pane (input or transcript)
  local text="$1"
  [[ -n "$MSG_PROBE" ]] || return 0
  grep -qF -- "$MSG_PROBE" <<<"$text"
}

_hd_msg_trailing_sigil() {  # message text is sitting in the input buffer, not yet submitted
  local text="$1" last_sigil last_line
  [[ -n "$MSG_PROBE" ]] || return 1
  if [[ -n "$SIGIL" ]]; then
    last_sigil="$(grep -F "$SIGIL " <<<"$text" | tail -n1 || true)"
    [[ -n "$last_sigil" ]] || return 1
    grep -qF -- "$MSG_PROBE" <<<"$last_sigil"
  else
    last_line="$(printf '%s\n' "$text" | awk 'NF{l=$0} END{print l}')"
    grep -qF -- "$MSG_PROBE" <<<"$last_line"
  fi
}

_hd_verify_delivered() {
  local text="$1"
  [[ -n "$MSG_PROBE" ]] || return 0               # empty message: nothing to verify
  if _hd_msg_present "$text" && ! _hd_msg_trailing_sigil "$text"; then
    return 0                                       # text moved out of the buffer → submitted
  fi
  return 1
}

_hd_poll_delivered() {  # poll up to $TIMEOUT_MS for positive delivery evidence
  local elapsed=0 post
  while [[ $elapsed -lt $TIMEOUT_MS ]]; do
    sleep "$(awk -v ms="$STEP_MS" 'BEGIN{printf "%.3f", ms/1000}')"
    _hd_status_confirms_delivery && return 0
    post="$(_hd_read_pane | _hd_strip_chrome)"
    _hd_verify_delivered "$post" && return 0
    elapsed=$((elapsed + STEP_MS))
  done
  return 1
}

_hd_blob_submitted() { [[ "$(_hd_pasted_blob_count "$(_hd_read_pane | _hd_strip_chrome)")" -le "$PRE_BLOBS" ]]; }
_hd_wait_blob_submitted() {  # poll ~2.4s for the blob to leave the composer
  local i
  for i in 1 2 3 4 5 6 7 8; do _hd_blob_submitted && return 0; sleep 0.3; done
  return 1
}

# ---- send (with delivery verification) ----
# Faithful port of pristine herder-send send/submit/verify/report path.
# Opts (forwarded through driver_dispatch): --no-enter/--no-verify/--force/--timeout/--json,
# each as "--flag <value>" where value is 0/1 (or the timeout ms / json flag).
herdr_send() {
  local target="$1" message="$2"; shift 2 || shift $#
  local NO_ENTER=0 NO_VERIFY=0 FORCE=0 JSON_OUT=0
  TIMEOUT_MS="${HERDER_TIMEOUT_MS:-3000}"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --no-enter)  NO_ENTER="${2:-1}"; shift 2;;
      --no-verify) NO_VERIFY="${2:-1}"; shift 2;;
      --force)     FORCE="${2:-1}"; shift 2;;
      --timeout)   TIMEOUT_MS="${2:-3000}"; shift 2;;
      --json)      JSON_OUT="${2:-1}"; shift 2;;
      *)           shift;;
    esac
  done

  # ---- resolve target → LIVE pane_id (drift-proof, via terminal_id) ----
  local resolve_output rc resolved_via drift_note
  resolve_output="$(_herdr_resolve_internal "$target")" || {
    rc=$?
    [[ $rc -eq 1 ]] && return 1 || return 2
  }
  PANE_ID="$(printf '%s' "$resolve_output" | cut -d'|' -f1)"
  resolved_via="$(printf '%s' "$resolve_output" | cut -d'|' -f2)"
  drift_note="$(printf '%s' "$resolve_output" | cut -d'|' -f4-)"
  [[ -n "$drift_note" ]] && printf 'herder-send: %s\n' "$drift_note" >&2

  # ---- detect agent kind → prompt sigil ----
  local KIND
  KIND="$(_hd_detect_kind || true)"
  case "$KIND" in
    codex)  SIGIL='›';;
    claude) SIGIL='❯';;
    *)      SIGIL='';;
  esac

  # ---- pre-flight state check (--force bypasses) ----
  local PRE_TEXT_RAW PRE_TEXT REASON
  PRE_TEXT_RAW="$(_hd_read_pane)"
  PRE_TEXT="$(printf '%s' "$PRE_TEXT_RAW" | _hd_strip_chrome)"
  PRE_STATUS="$(_hd_detect_status || true)"

  if [[ "$FORCE" -ne 1 ]]; then
    if REASON="$(_hd_preflight_blocked_reason "$PRE_TEXT")"; then
      printf 'herder-send: refusing to send to %s: %s\n' "$PANE_ID" "$REASON" >&2
      return 2
    fi
  fi

  # ---- message probe (tail-keyed; guard sub-45-char trim) ----
  MSG_PROBE="$(printf '%s' "$message" | grep -v '^[[:space:]]*$' | tail -n1)"
  [[ "${#MSG_PROBE}" -gt 45 ]] && MSG_PROBE="${MSG_PROBE: -45}"

  PRE_BLOBS="$(_hd_pasted_blob_count "$PRE_TEXT")"

  # ---- send (ensure it lands; re-paste once if not ready) ----
  local LANDED=0 PASTE_COLLAPSED=0 SEND_ATTEMPTS=0 W POST
  if [[ -z "$MSG_PROBE" ]]; then LANDED=1; fi  # empty message: nothing to land
  while [[ $LANDED -eq 0 && $SEND_ATTEMPTS -lt 2 ]]; do
    SEND_ATTEMPTS=$((SEND_ATTEMPTS + 1))
    if ! herdr agent send "$PANE_ID" "$message" >/dev/null 2>&1; then
      sleep 0.4; continue
    fi
    W=0
    while [[ $W -lt 2500 ]]; do
      sleep 0.25
      if _hd_status_confirms_delivery; then LANDED=1; break; fi
      POST="$(_hd_read_pane | _hd_strip_chrome)"
      if _hd_msg_present "$POST"; then LANDED=1; break; fi
      if _hd_new_paste_blob "$POST"; then LANDED=1; PASTE_COLLAPSED=1; break; fi
      W=$((W + 250))
    done
  done

  if [[ $LANDED -eq 0 ]]; then
    printf 'herder-send: message never landed in %s after %d paste attempts (agent not accepting input?)\n' \
      "$PANE_ID" "$SEND_ATTEMPTS" >&2
  fi

  # ---- submit (unless --no-enter) ----
  local SUBMITTED=false
  if [[ "$NO_ENTER" -eq 0 && $LANDED -eq 1 && -n "$MSG_PROBE" ]]; then
    sleep 0.2
    herdr pane send-keys "$PANE_ID" Enter >/dev/null 2>&1 || true
    SUBMITTED=true
  fi

  # ---- verify delivery ----
  local VERIFY_RESULT="not_attempted" EXTRA_ENTER_SENT=false
  STEP_MS=250

  if [[ $LANDED -eq 0 ]]; then
    VERIFY_RESULT="not_landed"
  elif [[ "$NO_ENTER" -eq 1 ]]; then
    VERIFY_RESULT="placed"
  elif [[ "$NO_VERIFY" -eq 1 ]]; then
    VERIFY_RESULT="not_verified"
  elif [[ "$SUBMITTED" == "true" ]]; then
    local DELIVERED=0 QUEUED=0
    if [[ "$PASTE_COLLAPSED" -eq 1 ]]; then
      # A collapsed blob needs another Enter to submit; confirm via blob-disappearance.
      if _hd_wait_blob_submitted; then
        DELIVERED=1
      else
        herdr pane send-keys "$PANE_ID" Enter >/dev/null 2>&1 || true
        EXTRA_ENTER_SENT=true
        if _hd_wait_blob_submitted; then
          DELIVERED=1
        else
          herdr pane send-keys "$PANE_ID" Enter >/dev/null 2>&1 || true
          _hd_wait_blob_submitted && DELIVERED=1
        fi
      fi
    elif _hd_poll_delivered; then
      DELIVERED=1
    elif [[ "$PRE_STATUS" == "working" ]]; then
      # Target was ALREADY mid-turn — claude/codex QUEUES the message (runs next).
      # Treat as queued=success; do NOT fire extra-Enter recovery (would stack dups).
      QUEUED=1; DELIVERED=1
    else
      # Recovery for the non-collapsed, idle-agent case: codex absorbs the first
      # Enter when leaving "Conversation interrupted" — send one more if still buffered.
      if _hd_msg_trailing_sigil "$(_hd_read_pane | _hd_strip_chrome)"; then
        herdr pane send-keys "$PANE_ID" Enter >/dev/null 2>&1 || true
        EXTRA_ENTER_SENT=true
        _hd_poll_delivered && DELIVERED=1
      fi
    fi

    if [[ "$QUEUED" -eq 1 ]]; then VERIFY_RESULT="queued"
    elif [[ $DELIVERED -eq 1 ]]; then VERIFY_RESULT="delivered"
    else VERIFY_RESULT="not_delivered"; fi
  fi

  # ---- report: human summary to STDERR always ----
  {
    printf 'sent %d chars to %s' "${#message}" "$PANE_ID"
    [[ -n "$KIND" ]] && printf ' (%s)' "$KIND"
    if [[ "$SUBMITTED" == "true" ]]; then
      printf ', submitted'
    elif [[ "$NO_ENTER" -eq 1 ]]; then
      printf ', not submitted (--no-enter)'
    fi
    printf ', verify=%s' "$VERIFY_RESULT"
    [[ "$VERIFY_RESULT" == "queued" ]] && printf ' (target was busy; message queued to run next — do NOT resend)'
    [[ "$PASTE_COLLAPSED" -eq 1 ]] && printf ' (codex collapsed paste to blob)'
    [[ "$EXTRA_ENTER_SENT" == "true" ]] && printf ' (extra Enter sent)'
    [[ "$SEND_ATTEMPTS" -gt 1 ]] && printf ' (re-pasted x%d)' "$SEND_ATTEMPTS"
    printf '\n'
  } >&2

  # ---- JSON record to STDOUT only under --json ----
  if [[ "$JSON_OUT" -eq 1 ]]; then
    jq -nc \
      --arg pane "$PANE_ID" \
      --arg kind "$KIND" \
      --arg target "$target" \
      --arg resolved_via "$resolved_via" \
      --argjson submitted "$SUBMITTED" \
      --arg verify "$VERIFY_RESULT" \
      --argjson extra_enter "$EXTRA_ENTER_SENT" \
      --argjson paste_collapsed "$PASTE_COLLAPSED" \
      --arg msg_preview "$(printf '%s' "$message" | head -c 120)" \
      '{pane_id:$pane, agent:$kind, target:$target, resolved_via:$resolved_via, submitted:$submitted,
        verify:$verify, extra_enter_sent:$extra_enter, paste_collapsed:($paste_collapsed==1),
        message_preview:$msg_preview}'
  fi

  # Non-zero when we could not positively confirm the outcome the caller asked for.
  case "$VERIFY_RESULT" in
    not_landed|not_delivered) return 1;;
  esac
  return 0
}
