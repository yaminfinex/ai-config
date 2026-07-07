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
  "pane get")
    # fork --self resolves the current pane's cwd from here (foreground_cwd first).
    jq -n '{result:{pane:{pane_id:"p_self", terminal_id:"term_SELF", workspace_id:"ws_self", foreground_cwd:"/mock/foreground", cwd:"/mock/cwd"}}}';;
  "workspace list")
    jq -n '{result:{workspaces:[]}}';;
  *)
    printf 'mock herdr (fork suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
# fork --self correlates the current pane to a registered guid via `hcom list`.
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "${MOCK_HCOM_IDENTITY:-[]}"
  exit 0
fi
exit 0
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

# Probe standing in for `herder spawn` on the --self fallback path: records the
# handoff argv instead of really re-forking the tool. fork --self resolves the
# spawn binary from $HERDER_BIN, so pointing it here captures the handoff.
cat >"$MOCKBIN/herder-spawn-probe" <<'MOCK_SPAWN'
#!/usr/bin/env bash
printf 'spawn-handoff: %s\n' "$*"
exit 0
MOCK_SPAWN
chmod +x "$MOCKBIN/herder-spawn-probe"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# fork --self detects the tool + identity from these env vars. Clear any ambient
# values (this suite is often run from inside a live claude/herder pane) so each
# --self case carries ONLY what its caller sets inline — no leakage.
unset CLAUDECODE CLAUDE_CODE_SESSION_ID CODEX_HOME AI_AGENT HERDER_GUID HERDER_SPAWNED_BY

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
    HERDER_GUID="${HERDER_GUID:-}" \
    HERDER_SPAWNED_BY="${HERDER_SPAWNED_BY:-}" \
    "${HFK[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

# fork --self detects tool + identity from the env, so its cases carry the
# tool-detect vars (CLAUDECODE / CLAUDE_CODE_SESSION_ID / CODEX_HOME / AI_AGENT),
# the pane->identity map (MOCK_HCOM_IDENTITY), and a spawn probe via HERDER_BIN.
# Callers set the relevant vars inline; unset ones default to empty.
run_self_case() {
  local name="$1" live="$2"; shift 2
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/probe"
  seed_registry
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(cd "$REPO" && env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    AI_CONFIG_ROOT="$REPO" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_LIFECYCLE_SETTLE_MS=0 \
    HERDER_BIN="$MOCKBIN/herder-spawn-probe" \
    MOCK_PROBE_DIR="$CASE/probe" \
    MOCK_LIVE_PARENT="$live" \
    CLAUDECODE="${CLAUDECODE:-}" \
    CLAUDE_CODE_SESSION_ID="${CLAUDE_CODE_SESSION_ID:-}" \
    CODEX_HOME="${CODEX_HOME:-}" \
    AI_AGENT="${AI_AGENT:-}" \
    HERDER_GUID="${HERDER_GUID:-}" \
    MOCK_HCOM_IDENTITY="${MOCK_HCOM_IDENTITY:-}" \
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
# provenance: a fork run BY a spawned session records THAT session as the child's
# spawned_by — not the inherited HERDER_SPAWNED_BY, which names the forker's own
# spawner (the child's grandparent). TASK-004.
HERDER_GUID=guid-forker-1111 HERDER_SPAWNED_BY=guid-orch-2222 \
  run_case provenance_spawned_by 1 parent --label prov-fork --json
check_one provenance_spawned_by

# --- fork --self -----------------------------------------------------------
# claude, pane correlates to a registered guid (hcom_name) -> NATIVE fork path.
CLAUDECODE=1 \
  MOCK_HCOM_IDENTITY='[{"name":"parent-rive","session_id":"sess-parent","launch_context":{"pane_id":"p_self"}}]' \
  run_self_case self_native 1 --self --prompt "hello self" --json
check_one self_native
# claude, no registered guid, orphan session id -> FALLBACK to spawn --resume.
CLAUDECODE=1 CLAUDE_CODE_SESSION_ID=sess-orphan \
  run_self_case self_fallback_claude 0 --self
check_one self_fallback_claude
# codex always falls back (native fork is claude-only); no session -> fork --last.
CODEX_HOME=/mock/codex \
  run_self_case self_fallback_codex 0 --self --split down
check_one self_fallback_codex
# no tool env -> clear refusal (exit 1), before any pane/registry lookup.
run_self_case self_unknown 0 --self
check_one self_unknown

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HFK[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — fork contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
