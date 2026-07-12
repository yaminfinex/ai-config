#!/usr/bin/env bash
# check-enroll-contract.sh — lock the herder enroll registry/provenance contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
GOLDENS="$TESTS_DIR/goldens/enroll"
HEN=("$REPO_ROOT/bin/herder" enroll)
[[ -n "${HERDER_ENROLL_BIN:-}" ]] && HEN=("$HERDER_ENROLL_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "pane get")
    jq -n '{result:{pane:{pane_id:"p_self", terminal_id:"term_SELF", workspace_id:"ws_self", cwd:"/mock/cwd"}}}';;
  *)
    printf 'mock herdr (enroll suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
rows="${MOCK_HCOM_ROWS:-[]}"
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "$rows"
  exit 0
fi
if [[ "${1:-}" == "list" && -n "${2:-}" ]]; then
  jq -e --arg name "$2" 'map(select(.name==$name and (.joined // true))) | length == 1' <<<"$rows" >/dev/null
  exit
fi
exit 64
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

run_case() {
  local name="$1"; shift
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/state"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  local block guid short
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== REGISTRY ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)")"
  guid="$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' <<<"$block" | head -n1 || true)"
  if [[ -n "$guid" ]]; then
    short="${guid:0:8}"
    block="${block//$guid/<GUID>}"
    block="${block//$short/<SHORT>}"
  fi
  block="$(sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<GUID>/g; s/"hostname":"[^"]*"/"hostname":"<HOST>"/g' <<<"$block")"
  block="$(sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g' <<<"$block")"
  printf '%s' "$block"
}

check_one() {
  local name="$1" block gold
  block="$(block_for)"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hen_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hen_diff.$$; fail=1
  fi
  rm -f /tmp/hen_diff.$$
}

scenario_default() {
  run_case default "$@"
  check_one default
}

scenario_ambient() {
  run_case ambient env \
    HERDER_GUID=guid-existing-0000 HERDER_LABEL=env-label HERDER_ROLE=env-role \
    HCOM_INSTANCE_NAME=stale-launch-name HCOM_SESSION_ID=sess-live \
    MOCK_HCOM_ROWS='[{"name":"bus-live","session_id":"sess-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    HCOM_DIR=/hfake/.hcom HCOM_TAG=env-role HCOM_LAUNCH_BATCH_ID=batch-9 \
    "$@"
  check_one ambient
}

scenario_reenroll_spawned() {
  CASE="$ROOT/reenroll_spawned"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-spawned-0000","short_guid":"guid","label":"spawned-old","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-spawn","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-spawned-0000 HERDER_ROLE=worker \
    HCOM_SESSION_ID=sess-spawn \
    MOCK_HCOM_ROWS='[{"name":"spawned-live","session_id":"sess-spawn","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label spawned-new --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_spawned
}

# TASK-035 AC#1: a reused pane accumulates a stale manual identity per prior
# session; re-enrolling that pane must RETIRE (close) the prior active rows for
# it so a dead session's row stops lingering as LIVE=working. Seed two stale
# manual rows on p_self (the pane the mock reports) plus one on a DIFFERENT
# pane that must be left untouched; enroll fresh and assert both p_self rows
# gain a closed record while the other-pane row stays active.
scenario_reenroll_reused_pane() {
  CASE="$ROOT/reenroll_reused_pane"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-stale1-000","short_guid":"stale1","label":"stale-a","role":"manual","agent":"claude","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-a-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-01T00:00:00Z"}}
{"guid":"guid-stale2-000","short_guid":"stale2","label":"stale-b","role":"manual","agent":"claude","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-b-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-02T00:00:00Z"}}
{"guid":"guid-other-pane0","short_guid":"otherp","label":"other-pane","role":"manual","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","hcom_name":"other-bus","status":"active"}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-fresh-0000 \
    "${HEN[@]}" --label fresh-session --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_reused_pane
}

# TASK-035 P1-b: herdr COMPACTS pane ids, so a still-live session that moved can
# keep an old pane_id that a NEW unrelated session now reuses. Retirement keys on
# pane_id AND the durable terminal_id — a prior row whose terminal_id differs from
# the enrolling pane's is a different session and must NOT be closed. Seed a row
# on p_self but terminal_id=term_ELSEWHERE; enroll (pane terminal=term_SELF) and
# assert that row is left ACTIVE (no closed record) while a same-terminal stale
# row IS retired.
scenario_reenroll_compacted_pane() {
  CASE="$ROOT/reenroll_compacted_pane"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-live-else0","short_guid":"livels","label":"live-elsewhere","role":"manual","agent":"claude","terminal_id":"term_ELSEWHERE","pane_id":"p_self","hcom_name":"live-else-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-01T00:00:00Z"}}
{"guid":"guid-stalehere0","short_guid":"staleh","label":"stale-here","role":"manual","agent":"claude","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-here-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-02T00:00:00Z"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-fresh-0000 \
    "${HEN[@]}" --label fresh-session --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_compacted_pane
}

scenario_default "${HEN[@]}" --json
scenario_ambient "${HEN[@]}" --label cli-label --role cli-role --json
scenario_reenroll_spawned
scenario_reenroll_reused_pane
scenario_reenroll_compacted_pane

if [[ "$WRITE" -eq 0 ]]; then
  RUN_ERR_F="$ROOT/outside-stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$ROOT/home" HERDER_STATE_DIR="$ROOT/state" "${HEN[@]}" 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'HERDR_ENV/HERDR_PANE_ID required' "$RUN_ERR_F"; then
    printf 'PASS  guard: requires herdr pane\n'
  else
    printf 'FAIL  guard: requires herdr pane — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi

  CASE="$ROOT/collision"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-other-0000","short_guid":"other","label":"taken","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","status":"active"}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    "${HEN[@]}" --label taken 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'label "taken" already belongs to active guid guid-other-0000' "$RUN_ERR_F"; then
    printf 'PASS  guard: active label collision refused\n'
  else
    printf 'FAIL  guard: active label collision refused — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi

  CASE="$ROOT/guid-mismatch"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-existing-0000","short_guid":"existing","label":"existing","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","hcom_name":"old-bus","status":"active","provenance":{"mechanism":"enroll","tool_session_id":"sess-old"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
    HCOM_SESSION_ID=sess-new \
    MOCK_HCOM_ROWS='[{"name":"new-bus","session_id":"sess-new","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label existing 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'refused to re-enroll guid-existing-0000' "$RUN_ERR_F"; then
    printf 'PASS  guard: inherited guid cannot re-key another session\n'
  else
    printf 'FAIL  guard: inherited guid cannot re-key another session — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HEN[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — enroll contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
