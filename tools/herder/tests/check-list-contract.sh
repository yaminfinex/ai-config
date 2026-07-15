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
#                    retired and lost sessions are hidden unless --all.
#   modes          — table (default), --all, --json, --raw, --guid (found +
#                    missing), missing registry, herdr-list failure,
#                    unresolved continuation surfacing + acknowledgement.
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
# Determinism: HOME is a fixed fake path (never touched), and the registry is a
# committed fixture.

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
# live but re-keyed to p_99 (working, drifted from stored p_20); term_CCC and
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

# Context snapshot scenario: scratch-only state, never the live registry/bus.
CTX_STATE="$ROOT/list-ctx-state"
CTX_HCOM="$ROOT/ctx-hcom"
mkdir -p "$CTX_STATE" "$CTX_HCOM/statusline"
cat > "$CTX_STATE/registry.jsonl" <<CTX_REGISTRY
{"guid":"guid-fresh-0000","short_guid":"fresh","label":"fresh","role":"worker","agent":"claude","terminal_id":"term_AAA","pane_id":"p_10","team":"","hcom_dir":"$CTX_HCOM","hcom_name":"fresh-rive","hcom_tag":"worker","status":"active"}
{"guid":"guid-stale-0000","short_guid":"stale","label":"stale","role":"worker","agent":"claude","terminal_id":"term_CCC","pane_id":"p_30","team":"","hcom_dir":"$CTX_HCOM","hcom_name":"stale-rive","hcom_tag":"worker","status":"active"}
{"guid":"guid-unknown-0000","short_guid":"unknown","label":"unknown","role":"reviewer","agent":"codex","terminal_id":"term_BBB","pane_id":"p_20","team":"","hcom_dir":"$CTX_HCOM","hcom_name":"unknown-rive","hcom_tag":"reviewer","status":"active"}
CTX_REGISTRY
now_ts="$(date +%s)"
cat > "$CTX_HCOM/statusline/fresh-rive.env" <<CTX_FRESH
HCOM_UNREAD=0
HCOM_LAST_TS=$now_ts
HCOM_LAST_AGE_S=0
CTX_PCT=24
CTX_TOKENS=61768
CTX_SIZE=258400
CTX_TS=$now_ts
CTX_FRESH
cat > "$CTX_HCOM/statusline/stale-rive.env" <<'CTX_STALE'
HCOM_UNREAD=0
HCOM_LAST_TS=1
HCOM_LAST_AGE_S=999999
CTX_PCT=91
CTX_TOKENS=230000
CTX_SIZE=258400
CTX_TS=1
CTX_STALE

# Failed-continuation scenarios: scratch-only durable state, including one
# foreign record that must warn without hiding the valid failure.
CONT_STATE="$ROOT/list-continuations-state"
mkdir -p "$CONT_STATE/continuations"
cp "$FIX/registry.jsonl" "$CONT_STATE/registry.jsonl"
cat > "$CONT_STATE/continuations/compact-then-beta-42.json" <<'CONT_FAILURE'
{
  "schema": "herder.continuation.v1",
  "id": "compact-then-beta-42",
  "status": "failed",
  "target": "beta-rive",
  "updated_at": "2026-07-12T12:00:00Z",
  "reason": "delivery budget exhausted after 3 attempts",
  "log_path": "/hfake/state/compact-then/compact-then-beta-42.log",
  "recovery_command": "herder send beta-rive -- continue",
  "lifecycle": [
    {"status":"armed","timestamp":"2026-07-12T11:59:00Z"},
    {"status":"failed","timestamp":"2026-07-12T12:00:00Z","reason":"delivery budget exhausted after 3 attempts"}
  ]
}
CONT_FAILURE
printf '{}\n' > "$CONT_STATE/continuations/foreign.json"
cat > "$CONT_STATE/observer.status.json" <<'OBSERVER_STATUS'
{
  "schema": "herder.observer.v1",
  "advice": true,
  "last_sweep_summary": {"applied": 0, "noop": 0, "refused": 0},
  "protocol_compatible": true,
  "flags": [
    {
      "guid": "guid-beta-0000",
      "label": "beta",
      "type": "failed-continuation",
      "severity": "warning",
      "detail": "detached continuation delivery failed",
      "suggested": "run the recorded recovery command"
    }
  ]
}
OBSERVER_STATUS

# An unresolved failure remains a JSONL document record even when no session
# row survives reconciliation or filtering.
CONT_EMPTY_STATE="$ROOT/list-continuations-empty-state"
mkdir -p "$CONT_EMPTY_STATE/continuations"
: > "$CONT_EMPTY_STATE/registry.jsonl"
cp "$CONT_STATE/continuations/compact-then-beta-42.json" "$CONT_EMPTY_STATE/continuations/"

CONT_NO_REGISTRY_STATE="$ROOT/list-continuations-no-registry-state"
mkdir -p "$CONT_NO_REGISTRY_STATE/continuations"
cp "$CONT_STATE/continuations/compact-then-beta-42.json" "$CONT_NO_REGISTRY_STATE/continuations/"

# scenario name | mock scenario | state dir | args
SCENARIOS=(
  "table|normal|$FIX|"
  "ctx|normal|$CTX_STATE|"
  "table_all|normal|$FIX|--all"
  "json|normal|$FIX|--json"
  "raw|normal|$FIX|--raw"
  "guid_full|normal|$FIX|--guid guid-beta-0000"
  "guid_short|normal|$FIX|--guid dupe"
  "guid_missing|normal|$FIX|--guid nope"
  "noregistry|normal|/hfake/absent-state|"
  "livefail|fail|$FIX|"
  "provenance_raw|normal|$TESTS_DIR/fixtures/list-provenance|--raw"
  "provenance_json|normal|$TESTS_DIR/fixtures/list-provenance|--json"
  "v2_json|normal|$TESTS_DIR/fixtures/list-v2|--json"
  "guid_fork_shadow|normal|$TESTS_DIR/fixtures/list-guid-fork-shadow|--guid guid-parent-0000"
  "archive_all|normal|$TESTS_DIR/fixtures/list-archives|--all"
  "archive_json|normal|$TESTS_DIR/fixtures/list-archives|--json --all"
  "continuations_table|normal|$CONT_STATE|"
  "continuations_json|normal|$CONT_STATE|--json"
  "continuations_json_empty|normal|$CONT_EMPTY_STATE|--json"
  "continuations_json_no_registry|normal|$CONT_NO_REGISTRY_STATE|--json"
  "continuations_ack|normal|$CONT_STATE|--ack-continuation compact-then-beta-42"
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
