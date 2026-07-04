#!/usr/bin/env bash
# check-enroll-contract.sh — lock the herder enroll registry/provenance contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOLDENS="$TESTS_DIR/goldens/enroll"
HEN="${HERDER_ENROLL_BIN:-$TESTS_DIR/../scripts/herder-enroll}"

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
    HCOM_INSTANCE_NAME=bus-ambient HCOM_DIR=/hfake/.hcom HCOM_TAG=env-role HCOM_LAUNCH_BATCH_ID=batch-9 \
    "$@"
  check_one ambient
}

scenario_reenroll_spawned() {
  CASE="$ROOT/reenroll_spawned"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-spawned-0000","short_guid":"guid","label":"spawned-old","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-spawned-0000 HERDER_ROLE=worker \
    "$HEN" --label spawned-new --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_spawned
}

scenario_default "$HEN" --json
scenario_ambient "$HEN" --label cli-label --role cli-role --json
scenario_reenroll_spawned

if [[ "$WRITE" -eq 0 ]]; then
  RUN_ERR_F="$ROOT/outside-stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$ROOT/home" HERDER_STATE_DIR="$ROOT/state" "$HEN" 2>"$RUN_ERR_F")"
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
    "$HEN" --label taken 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'label "taken" already belongs to active guid guid-other-0000' "$RUN_ERR_F"; then
    printf 'PASS  guard: active label collision refused\n'
  else
    printf 'FAIL  guard: active label collision refused — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "$HEN"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — enroll contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
