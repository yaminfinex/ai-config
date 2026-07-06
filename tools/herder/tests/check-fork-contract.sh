#!/usr/bin/env bash
# check-fork-contract.sh — lock the herder fork lifecycle contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
GOLDENS="$TESTS_DIR/goldens/fork"
HFK=("$REPO/bin/herder" fork)
[[ -n "${HERDER_FORK_BIN:-}" ]] && HFK=("$HERDER_FORK_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
PROBE="${MOCK_PROBE_DIR:?}"
mkdir -p "$PROBE"
case "${1:-} ${2:-}" in
  "agent list")
    if [[ "${MOCK_LIVE_PARENT:-0}" == "1" ]]; then
      jq -n '{result:{agents:[{terminal_id:"term_PARENT", pane_id:"p_parent", name:"parent", agent_status:"idle"}]}}'
    else
      jq -n '{result:{agents:[]}}'
    fi;;
  "agent start")
    printf '%s\n' "$*" >>"$PROBE/herdr_start_argv"
    jq -n '{result:{agent:{pane_id:"p_child", terminal_id:"term_CHILD", workspace_id:"ws_child", cwd:"/mock/cwd"}}}';;
  *)
    printf 'mock herdr (fork suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
exit 0
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

# herder fork records the checkout's live git branch into child provenance;
# normalize it so goldens hold on any branch (seeded rows use fixture-branch).
LIVE_BRANCH="$(git -C "$REPO" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"

seed_registry() {
  mkdir -p "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-parent-0000","short_guid":"parent","label":"parent","role":"worker","agent":"claude","terminal_id":"term_PARENT","pane_id":"p_parent","hcom_dir":"/hcom","hcom_name":"parent-rive","hcom_tag":"worker","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-parent","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"fixture-branch","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-closed-0000","short_guid":"closed","label":"closed-parent","role":"reviewer","agent":"claude","terminal_id":"term_CLOSED","pane_id":"p_closed","hcom_dir":"/hcom","hcom_name":"closed-rive","hcom_tag":"reviewer","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-closed","tag":"reviewer","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"fixture-branch","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-closed-0000","short_guid":"closed","label":"closed-parent","role":"reviewer","agent":"claude","terminal_id":"term_CLOSED","pane_id":"p_closed","hcom_dir":"/hcom","hcom_name":"closed-rive","hcom_tag":"reviewer","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"","tag":"reviewer","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"fixture-branch","ts":"2026-07-03T00:01:00Z"}}
{"guid":"guid-nosess-0000","short_guid":"nosess","label":"no-session","role":"worker","agent":"codex","terminal_id":"term_NOSESS","pane_id":"p_nosess","hcom_dir":"/hcom","hcom_name":"","hcom_tag":"worker","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"fixture-branch","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-other-0000","short_guid":"other","label":"taken","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","status":"active"}
JSONL
}

run_case() {
  local name="$1" live="$2"; shift 2
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/probe"
  seed_registry
  RUN_ERR_F="$CASE/stderr"
  # Pin the runner cwd to $REPO so fork's os.Getwd()-derived child cwd is a
  # stable fixture value regardless of where this suite is invoked from.
  RUN_OUT="$(cd "$REPO" && env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    AI_CONFIG_ROOT="$REPO" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_LIFECYCLE_SETTLE_MS=0 \
    MOCK_PROBE_DIR="$CASE/probe" \
    MOCK_LIVE_PARENT="$live" \
    "${HFK[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  local block guid short
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== HERDR START ARGV ===\n%s\n=== REGISTRY ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" \
    "$(cat "$CASE/probe/herdr_start_argv" 2>/dev/null)" \
    "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)")"
  guid="$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' <<<"$block" | head -n1 || true)"
  if [[ -n "$guid" ]]; then
    short="${guid:0:8}"
    block="${block//$guid/<GUID>}"
    block="${block//$short/<SHORT>}"
  fi
  block="${block//$REPO/<REPO>}"
  if [[ -n "$LIVE_BRANCH" ]]; then
    block="${block//\"branch\":\"$LIVE_BRANCH\"/\"branch\":\"<BRANCH>\"}"
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
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hfk_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hfk_diff.$$; fail=1
  fi
  rm -f /tmp/hfk_diff.$$
}

run_case happy_live 1 parent --prompt "hello fork" --json
check_one happy_live
run_case closed_row 0 closed --label closed-fork --role reviewer-fork --json
check_one closed_row
run_case label_collision 1 parent --label taken
check_one label_collision
run_case unknown 0 nope
check_one unknown
run_case missing_session 0 nosess
check_one missing_session

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HFK[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — fork contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
