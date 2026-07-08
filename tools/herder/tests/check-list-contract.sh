#!/usr/bin/env bash
# check-list-contract.sh — lock the herder list output contract with committed
# golden fixtures (P0 characterization for the Go port: goldens are generated
# FROM the bash implementation and are immutable during Go work).
#
# Covers, against a hermetic mock `herdr` (no live session):
#   reconciliation — records are matched to live agents by durable terminal_id
#                    (NOT the stale spawn-time pane_id): live status + current
#                    pane (drift) are reported; missing terminal ⇒ "gone".
#   collapse       — append-only registry collapses to latest-record-per-guid;
#                    non-active latest rows are hidden unless --all.
#   modes          — table (default), --all, --json, --raw, --guid (found +
#                    missing), --teams, missing registry, herdr-list failure.
#   provenance     — raw/json modes pass provenance-bearing rows through.
#
# Usage:
#   check-list-contract.sh            # verify current worktree herder list vs goldens
#   check-list-contract.sh --write    # (re)generate goldens from $HERDER_LIST_BIN
#   HERDER_LIST_BIN=/path/to/herder list check-list-contract.sh [--write]
#
# HERDER_LIST_BIN may point at ANY executable honouring the herder list CLI
# (the bash script or the Go `bin/herder list` shim); it is exec'd directly,
# not via `bash`, so the same suite gates either implementation.
#
# Determinism: HOME is a fixed fake path (never touched), the registry is a
# committed fixture, and the only tempdir that can leak into output (the teams
# root) is normalized to <ROOT> before diffing.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
FIX="$TESTS_DIR/fixtures/list"
GOLDENS="$TESTS_DIR/goldens/list"
HL=("$REPO_ROOT/bin/herder" list)
[[ -n "${HERDER_LIST_BIN:-}" ]] && HL=("$HERDER_LIST_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

# Mock herdr: herder list calls `herdr agent list` and `herdr pane list`. Topology (vs fixture
# registry): term_AAA live at its stored pane p_10 (idle, no drift); term_BBB
# live but renumbered to p_99 (working, drifted from stored p_20); term_CCC and
# term_DDD absent (gone); label reborn matches a new-epoch live agent by name;
# term_UND is alive in pane list but absent from agent list (undetected).
# MOCK_LIST_SCENARIO=fail makes the agent call fail, which herder list must
# treat as an empty live list (pane-list can still prove undetected), not an error.
cat > "$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "agent list")
    if [[ "${MOCK_LIST_SCENARIO:-normal}" == "fail" ]]; then
      printf 'mock herdr: agent list unavailable\n' >&2
      exit 1
    fi
    jq -n '{result:{agents:[
      {pane_id:"p_10", terminal_id:"term_AAA", agent:"claude", agent_status:"idle"},
      {pane_id:"p_99", terminal_id:"term_BBB", agent:"codex",  agent_status:"working"},
      {pane_id:"p_55", terminal_id:"term_NEW", agent:"claude", agent_status:"idle", name:"reborn"}
    ]}}'
    ;;
  "pane list")
    jq -n '{result:{panes:[
      {pane_id:"p_10", terminal_id:"term_AAA"},
      {pane_id:"p_99", terminal_id:"term_BBB"},
      {pane_id:"p_55", terminal_id:"term_NEW"},
      {pane_id:"p_60", terminal_id:"term_UND"}
    ]}}'
    ;;
  *)
    printf 'mock herdr (list suite): unhandled: %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# Teams root for the --teams scenario (filesystem-driven enumeration).
TEAMS_ROOT="$ROOT/teams"
mkdir -p "$TEAMS_ROOT/blue" "$TEAMS_ROOT/red"

# scenario name | mock scenario | state dir | args
SCENARIOS=(
  "table|normal|$FIX|"
  "table_all|normal|$FIX|--all"
  "json|normal|$FIX|--json"
  "raw|normal|$FIX|--raw"
  "guid_full|normal|$FIX|--guid guid-beta-0000"
  "guid_short|normal|$FIX|--guid dupe"
  "guid_missing|normal|$FIX|--guid nope"
  "teams|normal|$FIX|--teams"
  "noregistry|normal|/hfake/absent-state|"
  "livefail|fail|$FIX|"
  "provenance_raw|normal|$TESTS_DIR/fixtures/list-provenance|--raw"
  "provenance_json|normal|$TESTS_DIR/fixtures/list-provenance|--json"
  "guid_fork_shadow|normal|$TESTS_DIR/fixtures/list-guid-fork-shadow|--guid guid-parent-0000"
)

normalize() {  # make tempdir paths deterministic before diffing
  sed "s|$ROOT|<ROOT>|g"
}

run_one() {  # $1=mock scenario, $2=state dir, rest=args → prints normalized block
  local scen="$1" state="$2"; shift 2
  local err out code
  err="$(mktemp)"
  out="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="/hfake" \
    HERDER_STATE_DIR="$state" \
    HERDER_TEAMS_ROOT="$TEAMS_ROOT" \
    MOCK_LIST_SCENARIO="$scen" \
    "${HL[@]}" "$@" 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$err")" "$out" "$code" | normalize
  rm -f "$err"
}

fail=0
for row in "${SCENARIOS[@]}"; do
  IFS='|' read -r name scen state argstr <<<"$row"
  # shellcheck disable=SC2206
  args=($argstr)

  block="$(run_one "$scen" "$state" ${args[@]+"${args[@]}"})"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    continue
  fi

  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hl_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hl_diff.$$; fail=1
  fi
  rm -f /tmp/hl_diff.$$
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HL[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — list contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
