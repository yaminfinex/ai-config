#!/usr/bin/env bash
# check-rename-contract.sh — lock the herder rename append/collision contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
GOLDENS="$TESTS_DIR/goldens/rename"
HRN=("$REPO_ROOT/bin/herder" rename)
[[ -n "${HERDER_RENAME_BIN:-}" ]] && HRN=("$HERDER_RENAME_BIN")

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
  "agent rename")
    printf '%s\n' "$*" >>"$PROBE/herdr_rename_argv"
    if [[ "${MOCK_RENAME_SCENARIO:-ok}" == "fail" ]]; then
      printf 'mock rename failed\n' >&2
      exit 7
    fi
    jq -n '{result:{type:"ok"}}';;
  *)
    printf 'mock herdr (rename suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

seed_registry() {
  mkdir -p "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-alpha-0000","short_guid":"alpha","label":"alpha","role":"worker","agent":"codex","terminal_id":"term_ALPHA","pane_id":"p_alpha","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-alpha","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-beta-0000","short_guid":"beta","label":"beta","role":"reviewer","agent":"claude","terminal_id":"term_BETA","pane_id":"p_beta","status":"active"}
{"guid":"guid-closed-0000","short_guid":"closed","label":"old-closed","role":"reviewer","agent":"claude","terminal_id":"term_CLOSED","pane_id":"p_closed","status":"closed"}
JSONL
}

run_case() {
  local name="$1" scen="$2"; shift 2
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/probe"
  seed_registry
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDER_STATE_DIR="$CASE/state" \
    MOCK_PROBE_DIR="$CASE/probe" \
    MOCK_RENAME_SCENARIO="$scen" \
    "${HRN[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== HERDR RENAME ARGV ===\n%s\n=== REGISTRY ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" \
    "$(cat "$CASE/probe/herdr_rename_argv" 2>/dev/null)" \
    "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)"
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
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hrn_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hrn_diff.$$; fail=1
  fi
  rm -f /tmp/hrn_diff.$$
}

run_case happy ok alpha alpha-new
check_one happy
run_case herdr_fail fail alpha alpha-new
check_one herdr_fail
run_case collision ok alpha beta
check_one collision
run_case reuse_closed ok alpha old-closed
check_one reuse_closed
run_case unknown ok nope new-label
check_one unknown

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HRN[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — rename contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
