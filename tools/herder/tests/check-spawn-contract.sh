#!/usr/bin/env bash
# check-spawn-contract.sh — lock the herder spawn contract with committed golden
# fixtures (P0 characterization for the Go port: goldens are generated FROM the
# bash implementation and are immutable during Go work).
#
# Drives the FULL spawn path against hermetic mocks (mock-herdr-spawn +
# mock-hcom-spawn; no live session, no live bus), covering:
#   argv        — the exact `herdr agent start` argv: login-shell wrapping
#                 ($SHELL -lic 'export HERDER_*…; exec …'), --no-login-shell env
#                 form, herder launch routing with the role as --tag, HCOM_DIR
#                 team-bus pinning, HERDER_BIN/HERDER_NOTIFY_TO exports.
#   permissions — per-agent autonomous-mode flag injection (claude/codex),
#                 suppression under --safe or an explicit caller perm flag.
#   readiness   — trust-modal clearing (Enter) vs --safe refusal; the
#                 status+stable ready reason.
#   new-tab     — tab create, root-pane identity check + close, pane_id
#                 re-resolution by terminal_id after compaction; the rootguard
#                 refusal when the root reports the agent's terminal.
#   delivery    — prompt handoff via the verified send leg; codex brief staging
#                 (multi-line brief → file + one-line pointer on the wire).
#   capture     — hcom name capture by frozen launch pane_id, tag+cwd fallback
#                 (newest wins), and the best-effort failure path.
#   registry    — the appended JSONL record (identity + bus coordinate fields).
#
# Usage:
#   check-spawn-contract.sh            # verify current worktree herder spawn vs goldens
#   check-spawn-contract.sh --write    # (re)generate goldens from $HERDER_SPAWN_BIN
#   HERDER_SPAWN_BIN=/path/to/herder spawn check-spawn-contract.sh [--write]
#
# HERDER_SPAWN_BIN may point at ANY executable honouring the herder spawn CLI.
# The default drives `bin/herder spawn` directly.
#
# Determinism: HOME/state are per-case tempdirs normalized to <CASE>, repo paths
# to <REPO>, the generated uuid/short-guid to <GUID>/<SHORT>, and the started_at
# timestamp to <TS>. `sleep` is a no-op on the mock PATH (herder spawn's poll
# loops advance by iteration counters, so this only removes dead wall-clock
# time; a Go implementation sleeping internally passes identically, just
# slower).

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd)"
GOLDENS="$TESTS_DIR/goldens/spawn"
HSP=("$REPO_ROOT/bin/herder" spawn)
[[ -n "${HERDER_SPAWN_BIN:-}" ]] && HSP=("$HERDER_SPAWN_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

ln -s "$TESTS_DIR/mock-herdr-spawn" "$MOCKBIN/herdr"
ln -s "$TESTS_DIR/mock-hcom-spawn" "$MOCKBIN/hcom"
# No-op sleep: every wait loop in herder spawn/herder send advances by iteration
# counter, not wall clock, so this is pure speed-up with identical behavior.
printf '#!/usr/bin/env bash\nexit 0\n' >"$MOCKBIN/sleep"
chmod +x "$MOCKBIN/sleep"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0

# run_spawn <herdr scenario> <agent kind> <hcom scenario> <args...>
# Populates CASE (per-scenario dir) and prints nothing; the caller reads the
# outputs via block_for.
run_spawn() {
  local herdr_scen="$1" agent_kind="$2" hcom_scen="$3"; shift 3
  mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
  # Optional pre-seed so a scenario can give the spawner a registry identity
  # (e.g. a bus-bound orchestrator row that --notify resolves against).
  [[ -n "${SPAWN_SEED_REGISTRY:-}" ]] && printf '%s\n' "$SPAWN_SEED_REGISTRY" >"$CASE/state/registry.jsonl"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_orch \
    HERDER_GUID="${SPAWN_HERDER_GUID:-}" \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_SPAWN_SHELL=/bin/zsh \
    MOCK_SPAWN_SCENARIO="$herdr_scen" MOCK_SPAWN_AGENT="$agent_kind" \
    MOCK_SPAWN_STATE="$CASE/mock" MOCK_PROBE_DIR="$CASE/probe" \
    MOCK_HCOM_SPAWN_SCENARIO="$hcom_scen" \
    "${HSP[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {  # assemble + normalize the golden block for the current CASE
  local block guid short
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC")"
  block+="$(printf '\n=== AGENT START ARGV ===\n%s' "$(cat "$CASE/probe/agent_start_argv" 2>/dev/null)")"
  block+="$(printf '\n=== HERDR MUTATING CALLS ===\n%s' "$(cat "$CASE/probe/calls" 2>/dev/null)")"
  block+="$(printf '\n=== HCOM DIR ===\n%s' "$(cat "$CASE/probe/hcom_dir" 2>/dev/null)")"
  block+="$(printf '\n=== REGISTRY ===\n%s' "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)")"
  block+="$(printf '\n=== BRIEF ===\n%s' "$(cat "$CASE/state/briefs/"*.md 2>/dev/null)")"

  block="${block//$CASE/<CASE>}"
  block="${block//$REPO_ROOT/<REPO>}"
  guid="$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' <<<"$block" | head -n1 || true)"
  if [[ -n "$guid" ]]; then
    short="${guid:0:8}"
    block="${block//$guid/<GUID>}"
    block="${block//$short/<SHORT>}"
  fi
  # started_at / closed_at ISO timestamps
  block="$(sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g' <<<"$block")"
  printf '%s' "$block"
}

check_one() {  # $1 = scenario name; CASE outputs already populated
  local name="$1" block gold
  block="$(block_for)"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hsp_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hsp_diff.$$; fail=1
  fi
  rm -f /tmp/hsp_diff.$$
}

scenario() {  # scenario <name> <herdr scen> <agent> <hcom scen> <args...>
  local name="$1" herdr_scen="$2" agent_kind="$3" hcom_scen="$4"; shift 4
  CASE="$ROOT/$name"
  run_spawn "$herdr_scen" "$agent_kind" "$hcom_scen" "$@"
  check_one "$name"
}

MULTILINE_BRIEF=$'You are the reviewer for unit X.\nRead the plan, then the diff.\nReport findings in the run-log.'
METACHAR_LABEL_PREFIX='quote;$`&<>("* )'
METACHAR_EXTRA_ARG='arg with $dollar `tick` ; | & < > ( ) " * newline
end'

scenario bash_basic        ready claude launchctx --role worker --agent bash --json
scenario bash_nologin      ready claude launchctx --role worker --agent bash --no-login-shell --json
scenario bash_metachar     ready claude launchctx --role worker --agent bash --label-prefix "$METACHAR_LABEL_PREFIX" --extra-arg "$METACHAR_EXTRA_ARG" --json
scenario claude_prompt     ready claude launchctx --role worker --agent claude --prompt "do the thing" --json
scenario claude_modal      modal claude launchctx --role worker --agent claude --prompt "do the thing" --json
scenario claude_modal_safe modal claude launchctx --role worker --agent claude --safe --prompt "do the thing" --json
scenario claude_newtab     ready claude launchctx --role worker --agent claude --new-tab --json
scenario newtab_rootguard  rootguard claude launchctx --role worker --agent claude --new-tab --json
scenario codex_brief       ready codex launchctx --role worker --agent codex --prompt "$MULTILINE_BRIEF" --json
scenario notify            ready claude launchctx --role worker --agent claude --notify --prompt "do the thing" --json
# Bus-native notify: the spawner (HERDER_GUID) has a recorded hcom_name, so the
# --notify appendix routes completion over hcom instead of the keystroke ring.
SPAWN_HERDER_GUID="guid-orch-0000"
SPAWN_SEED_REGISTRY='{"guid":"guid-orch-0000","short_guid":"orch","label":"orchestrator","role":"orchestrator","agent":"claude","terminal_id":"term_ORCH","pane_id":"p_orch","hcom_dir":"/hcom","hcom_name":"orchestrator-bumo","hcom_tag":"orchestrator","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-orch","tag":"orchestrator","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"main","ts":"2026-07-03T00:00:00Z"}}'
scenario notify_bus        ready claude launchctx --role worker --agent claude --notify --prompt "do the thing" --json
unset SPAWN_HERDER_GUID SPAWN_SEED_REGISTRY
scenario capture_fallback  ready claude fallback --role worker --agent claude --json
scenario capture_ambiguous ready claude fallback_ambiguous --role worker --agent claude --json
scenario capture_fail      ready claude fail --role worker --agent claude --json
scenario perm_explicit     ready claude launchctx --role worker --agent claude --extra-arg --dangerously-skip-permissions --json
scenario team              ready claude launchctx --role worker --agent claude --team smoke --json
scenario start_fail        startfail claude launchctx --role worker --agent claude --json

# ---- usage / validation errors (direct assertions; no goldens needed) ----
if [[ "$WRITE" -eq 0 ]]; then
  ok()  { printf 'PASS  %s\n' "$1"; }
  bad() { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

  CASE="$ROOT/usage"
  run_spawn ready claude launchctx --agent bash
  [[ "$RUN_RC" -eq 1 ]] && grep -q -- '--role required' "$RUN_ERR_F" \
    && ok "usage: --role required" || bad "usage: --role required" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"

  CASE="$ROOT/usage_team"
  run_spawn ready claude launchctx --role worker --agent bash --team 'bad/slash'
  [[ "$RUN_RC" -eq 1 ]] && grep -q -- '--team must be a single safe path segment' "$RUN_ERR_F" \
    && ok "usage: unsafe --team refused" || bad "usage: unsafe --team refused" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"

  CASE="$ROOT/usage_role"
  run_spawn ready claude launchctx --role 'phase_3' --agent claude
  [[ "$RUN_RC" -eq 1 ]] && grep -q 'it becomes the hcom --tag' "$RUN_ERR_F" \
    && ok "usage: non-hcom-safe role refused for hcom agent" || bad "usage: non-hcom-safe role refused" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"

  CASE="$ROOT/usage_tab"
  run_spawn ready claude launchctx --role worker --agent bash --new-tab --tab tab_3
  [[ "$RUN_RC" -eq 1 ]] && grep -q 'use --new-tab or --tab, not both' "$RUN_ERR_F" \
    && ok "usage: --new-tab/--tab exclusive" || bad "usage: --new-tab/--tab exclusive" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"

  trailing_value_flags=(--split --workspace --from-pane --tab --cwd --label-prefix --extra-arg --wait-timeout-ms --ready-match --login-shell --team --notify-to)
  trailing_ok=1
  trailing_detail=""
  for flag in "${trailing_value_flags[@]}"; do
    CASE="$ROOT/usage_trailing_${flag#--}"
    run_spawn ready claude launchctx --role worker --agent bash "$flag"
    if [[ "$RUN_RC" -ne 1 ]] || ! grep -q "unknown arg: $flag" "$RUN_ERR_F" || grep -q 'panic:' "$RUN_ERR_F"; then
      trailing_ok=0
      trailing_detail+="$flag rc=$RUN_RC err=$(cat "$RUN_ERR_F")"$'\n'
    fi
  done
  [[ "$trailing_ok" -eq 1 ]] && ok "usage: trailing value flags refused" || bad "usage: trailing value flags refused" "$trailing_detail"

  CASE="$ROOT/usage_wait_timeout"
  run_spawn ready claude launchctx --role worker --agent bash --wait-timeout-ms 15s
  [[ "$RUN_RC" -eq 1 ]] && grep -q -- '--wait-timeout-ms must be numeric' "$RUN_ERR_F" \
    && ok "usage: --wait-timeout-ms numeric" || bad "usage: --wait-timeout-ms numeric" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HSP[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — spawn contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
