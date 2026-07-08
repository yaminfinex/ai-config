#!/usr/bin/env bash
# check-reconcile-contract.sh — lock herder reconcile's handoff-repair contract
# with committed golden fixtures and a hermetic mock herdr.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
FIX="$TESTS_DIR/fixtures"
GOLDENS="$TESTS_DIR/goldens/reconcile"
HR=("$REPO_ROOT/bin/herder" reconcile)
[[ -n "${HERDER_RECONCILE_BIN:-}" ]] && HR=("$HERDER_RECONCILE_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat > "$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "agent list")
    jq -n '{result:{agents:[
      {pane_id:"p_10", terminal_id:"term_AAA", agent:"claude", agent_status:"idle", name:"alpha", cwd:"/work/alpha"},
      {pane_id:"p_99", terminal_id:"term_BBB", agent:"codex", agent_status:"working", name:"beta", cwd:"/work/beta"},
      {pane_id:"p_conflict", terminal_id:"term_CON", agent:"claude", agent_status:"idle", name:"intruder", cwd:"/work/gamma"},
      {pane_id:"p_55", terminal_id:"term_NEW", agent:"claude", agent_status:"idle", name:"delta", cwd:"/work/delta"},
      {pane_id:"p_dup", terminal_id:"term_DUPLIVE", agent:"claude", agent_status:"idle", name:"duplabel", cwd:"/work/dupe"},
      {pane_id:"p_amb1", terminal_id:"term_AMB1", agent:"codex", agent_status:"idle", name:"amb", cwd:"/work/one"},
      {pane_id:"p_amb2", terminal_id:"term_AMB2", agent:"codex", agent_status:"idle", name:"amb", cwd:"/work/two"}
    ]}}'
    ;;
  "pane list")
    jq -n '{result:{panes:[
      {pane_id:"p_10", terminal_id:"term_AAA"},
      {pane_id:"p_99", terminal_id:"term_BBB"},
      {pane_id:"p_conflict", terminal_id:"term_CON"},
      {pane_id:"p_55", terminal_id:"term_NEW"},
      {pane_id:"p_dup", terminal_id:"term_DUPLIVE"},
      {pane_id:"p_amb1", terminal_id:"term_AMB1"},
      {pane_id:"p_amb2", terminal_id:"term_AMB2"},
      {pane_id:"p_60", terminal_id:"term_UND"}
    ]}}'
    ;;
  *)
    printf 'mock herdr (reconcile suite): unhandled: %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# scenario name | fixture dir | mutable registry? | args
SCENARIOS=(
  "table_mixed|$FIX/reconcile|0|"
  "json_mixed|$FIX/reconcile|0|--json"
  "apply_mixed_refuses_writes|$FIX/reconcile|1|--apply"
  "duplicate_label|$FIX/reconcile-duplicate-label|1|"
  "duplicate_label_apply|$FIX/reconcile-duplicate-label|1|--apply"
  "dryrun_apply_fixture|$FIX/reconcile-apply|1|"
  "apply_fixture|$FIX/reconcile-apply|1|--apply"
  "help|$FIX/reconcile|0|--help"
  "noregistry|/hfake/absent-state|0|"
)

run_one() {
  local state="$1" mutable="$2"; shift 2
  local run_state="$state" err out code
  if [[ "$mutable" == "1" ]]; then
    run_state="$ROOT/state-$RANDOM"
    mkdir -p "$run_state"
    cp "$state/registry.jsonl" "$run_state/registry.jsonl"
  fi
  err="$(mktemp)"
  out="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="/hfake" \
    HERDER_STATE_DIR="$run_state" \
    "${HR[@]}" "$@" 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$err")" "$out" "$code"
  if [[ "$mutable" == "1" ]]; then
    printf '=== REGISTRY ===\n%s\n' "$(cat "$run_state/registry.jsonl")"
  fi
  rm -f "$err"
}

fail=0
for row in "${SCENARIOS[@]}"; do
  IFS='|' read -r name state mutable argstr <<<"$row"
  # shellcheck disable=SC2206
  args=($argstr)

  block="$(run_one "$state" "$mutable" ${args[@]+"${args[@]}"} | sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g')"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    continue
  fi

  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hr_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hr_diff.$$; fail=1
  fi
  rm -f /tmp/hr_diff.$$
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HR[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — reconcile contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
