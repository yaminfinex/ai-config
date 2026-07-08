#!/usr/bin/env bash
# check-wait-contract.sh — lock the herder wait resolution/args/output contract
# with committed golden fixtures (P0 characterization for the Go port: goldens
# are generated FROM the bash implementation and are immutable during Go work).
#
# Deliberately SHALLOW (agreed run shape): the mock `herdr wait` returns
# instantly with a scripted exit code, so no real-sleep timeout path is
# characterized here — live smokes (P5/P6) cover actual waiting. What IS locked:
#
#   resolution — guid/label → the agent's CURRENT pane via durable terminal_id
#                (drift-proof); raw pane ids verbatim; gone/unresolvable targets
#                error out before any wait call.
#   argv       — the exact `herdr wait agent-status <pane> --status S --timeout MS`
#                and `herdr pane read <pane> --source S --lines N` invocations
#                (recorded by the mock and diffed as golden sections).
#   output     — stderr messages, --read passthrough, and exit codes (1 on
#                timeout, matching `herdr wait`).
#
# Usage:
#   check-wait-contract.sh            # verify current worktree herder wait vs goldens
#   check-wait-contract.sh --write    # (re)generate goldens from $HERDER_WAIT_BIN
#   HERDER_WAIT_BIN=/path/to/herder wait check-wait-contract.sh [--write]
#
# HERDER_WAIT_BIN may point at ANY executable honouring the herder wait CLI
# (the bash script or the Go `bin/herder wait` shim); it is exec'd directly,
# not via `bash`, so the same suite gates either implementation.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
FIX="$TESTS_DIR/fixtures/wait"
GOLDENS="$TESTS_DIR/goldens/wait"
HW=("$REPO_ROOT/bin/herder" wait)
[[ -n "${HERDER_WAIT_BIN:-}" ]] && HW=("$HERDER_WAIT_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

# Mock herdr. Topology (vs fixture registry): term_AAA live at stored pane p_10;
# term_BBB live but renumbered to p_99 (stored p_20 — resolution must follow the
# terminal); term_CCC absent (gone). Scenarios:
#   MOCK_WAIT_SCENARIO  normal | emptylist (pane list has zero panes) | closed_after_wait
#   MOCK_WAIT_RC        exit code for `herdr wait agent-status` (0 ok, 1 timeout)
# Every wait/read invocation appends its argv to $MOCK_PROBE_DIR.
cat > "$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
: "${MOCK_PROBE_DIR:?}"
case "${1:-} ${2:-}" in
  "pane list")
    count_file="$MOCK_PROBE_DIR/pane_list_count"
    count="$(cat "$count_file" 2>/dev/null || printf 0)"
    count=$((count + 1))
    printf '%s\n' "$count" >"$count_file"
    if [[ "${MOCK_WAIT_SCENARIO:-normal}" == "emptylist" ]]; then
      jq -n '{result:{panes:[]}}'
    elif [[ "${MOCK_WAIT_SCENARIO:-normal}" == "closed_after_wait" && "$count" -gt 1 ]]; then
      jq -n '{result:{panes:[
        {pane_id:"p_10", terminal_id:"term_AAA"}
      ]}}'
    else
      jq -n '{result:{panes:[
        {pane_id:"p_10", terminal_id:"term_AAA"},
        {pane_id:"p_99", terminal_id:"term_BBB"}
      ]}}'
    fi
    ;;
  "agent get")
    pane="${3:-}"
    if [[ "${MOCK_WAIT_SCENARIO:-normal}" == "lost" && "$pane" == "p_99" ]]; then
      jq -n '{result:{agent:{agent_status:"unknown"}}}'
    elif [[ "${MOCK_WAIT_SCENARIO:-normal}" == "closed_after_wait" && "$pane" == "p_99" ]]; then
      exit 1
    elif [[ "$pane" == "p_10" ]]; then
      jq -n '{result:{agent:{agent:"claude", agent_status:"idle"}}}'
    elif [[ "$pane" == "p_99" ]]; then
      jq -n '{result:{agent:{agent:"codex", agent_status:"working"}}}'
    else
      jq -n '{result:{agent:{}}}'
    fi
    ;;
  "wait agent-status")
    shift 2
    printf 'wait agent-status %s\n' "$*" >>"$MOCK_PROBE_DIR/wait_argv"
    exit "${MOCK_WAIT_RC:-0}"
    ;;
  "pane read")
    shift 2
    printf 'pane read %s\n' "$*" >>"$MOCK_PROBE_DIR/read_argv"
    jq -n '{result:{read:{text:"⏺ mock transcript line 1\n❯ "}}}'
    ;;
  *)
    printf 'mock herdr (wait suite): unhandled: %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# scenario name | mock scenario | wait rc | state dir | args
SCENARIOS=(
  "label_live|normal|0|$FIX|beta"
  "label_flags|normal|0|$FIX|beta --status working --timeout 5000"
  "pane_verbatim|normal|0|$FIX|p_77"
  "gone|normal|0|$FIX|gone"
  "emptylist|emptylist|0|$FIX|beta"
  "timeout|normal|1|$FIX|alpha"
  "timeout_lost_detection|lost|1|$FIX|beta"
  "timeout_closed_after_wait|closed_after_wait|1|$FIX|beta"
  "read_defaults|normal|0|$FIX|alpha --read"
  "read_custom|normal|0|$FIX|alpha --read --lines 5 --source visible"
  "noregistry_pane|normal|0|/hfake/absent-state|p_5"
)

run_one() {  # $1=mock scenario $2=wait rc $3=state dir, rest=args → prints block
  local scen="$1" wrc="$2" state="$3"; shift 3
  local probe err out code
  probe="$(mktemp -d)"
  err="$(mktemp)"
  out="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="/hfake" \
    HERDR_ENV=1 \
    HERDER_STATE_DIR="$state" \
    MOCK_WAIT_SCENARIO="$scen" MOCK_WAIT_RC="$wrc" MOCK_PROBE_DIR="$probe" \
    "${HW[@]}" "$@" 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== HERDR CALLS ===\n%s\n' \
    "$(cat "$err")" "$out" "$code" \
    "$(cat "$probe/wait_argv" "$probe/read_argv" 2>/dev/null)"
  rm -rf "$probe" "$err"
}

fail=0
for row in "${SCENARIOS[@]}"; do
  IFS='|' read -r name scen wrc state argstr <<<"$row"
  # shellcheck disable=SC2206
  args=($argstr)

  block="$(run_one "$scen" "$wrc" "$state" ${args[@]+"${args[@]}"})"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    continue
  fi

  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hw_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hw_diff.$$; fail=1
  fi
  rm -f /tmp/hw_diff.$$
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HW[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — wait contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
