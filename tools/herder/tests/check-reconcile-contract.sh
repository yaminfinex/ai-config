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
    if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "backfill" ]]; then
      jq -n '{result:{agents:[
        {pane_id:"p_repair", terminal_id:"term_REPAIR", agent:"claude", agent_status:"idle", name:"repair", cwd:"/work/repair"}
      ]}}'
      exit 0
    fi
    if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "field" ]]; then
      jq -n '{result:{agents:[
        {pane_id:"p_heal", terminal_id:"term_HEAL", agent:"claude", agent_status:"idle", name:"stale-tracker", cwd:"/work/heal"},
        {pane_id:"p_dup", terminal_id:"term_D11DUP", agent:"claude", agent_status:"idle", name:"stale-duplicate", cwd:"/work/duplicate"},
        {pane_id:"p_live_foreign", terminal_id:"term_D11FP", agent:"claude", agent_status:"idle", name:"stale-foreign-pane", cwd:"/work/foreign-pane"},
        {pane_id:"p_ft_stored", terminal_id:"term_D11FT", agent:"claude", agent_status:"idle", name:"intruder", cwd:"/work/foreign-terminal"},
        {pane_id:"p_abs", terminal_id:"term_D11ABS", agent:"claude", agent_status:"idle", name:"stale-absent", cwd:"/work/absent"}
      ]}}'
      exit 0
    fi
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
    if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "backfill" ]]; then
      jq -n '{result:{panes:[{pane_id:"p_repair",terminal_id:"term_REPAIR"}]}}'
      exit 0
    fi
    if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "field" ]]; then
      jq -n '{result:{panes:[
        {pane_id:"p_heal",terminal_id:"term_HEAL"},
        {pane_id:"p_dup",terminal_id:"term_D11DUP"},
        {pane_id:"p_live_foreign",terminal_id:"term_D11FP"},
        {pane_id:"p_ft_stored",terminal_id:"term_D11FT"},
        {pane_id:"p_abs",terminal_id:"term_D11ABS"}
      ]}}'
      exit 0
    fi
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

cat > "$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "backfill" ]]; then
    jq -cn '[{name:"repair-bus",session_id:"",joined:true,launch_context:{}}]'
    exit 0
  fi
  if [[ "${MOCK_RECONCILE_SCENARIO:-}" == "field" ]]; then
    jq -cn '[
      {name:"live-reclaimed",session_id:"sid-heal",joined:true,launch_context:{pane_id:"p_heal"}},
      {name:"duplicate-one",session_id:"sid-duplicate",joined:true,launch_context:{pane_id:"p_dup"}},
      {name:"duplicate-two",session_id:"sid-duplicate",joined:true,launch_context:{pane_id:"p_dup"}},
      {name:"foreign-pane-bus",session_id:"sid-foreign-pane",joined:true,launch_context:{pane_id:"p_elsewhere"}},
      {name:"foreign-terminal-bus",session_id:"sid-foreign-terminal",joined:true,launch_context:{pane_id:"p_ft_live"}}
    ]'
    exit 0
  fi
  jq -cn '[
    {name:"beta-live",joined:true,launch_context:{pane_id:"p_99"}},
    {name:"delta-live",joined:true,launch_context:{pane_id:"p_55"}}
  ]'
  exit 0
fi
exit 64
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

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
    [[ ! -f "$state/node_id" ]] || cp "$state/node_id" "$run_state/node_id"
  fi
  err="$(mktemp)"
  out="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="/hfake" \
    HCOM_DIR="${MOCK_RECONCILE_HCOM_DIR:-}" \
    HERDER_STATE_DIR="$run_state" \
    MOCK_RECONCILE_SCENARIO="${MOCK_RECONCILE_SCENARIO:-}" \
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

  block="$(run_one "$state" "$mutable" ${args[@]+"${args[@]}"} | sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<GUID>/g; s/[0-9a-f]{32}/<GEN>/g; s/"hostname":"[^"]*"/"hostname":"<HOST>"/g; s/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g')"
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

if [[ "$WRITE" -eq 0 ]]; then
  field_block="$(MOCK_RECONCILE_SCENARIO=field run_one "$FIX/reconcile-field" 1 --apply)"
  if grep -q 'rightful.*re-confirm' <<<"$field_block" \
    && grep -q 'tracker name.*dominated by exact terminal+pane and unique joined bus' <<<"$field_block" \
    && grep -q '"hcom_name":"live-reclaimed"' <<<"$field_block"; then
    printf 'PASS  D11 exact seat plus unique SID/pane bus proof heals stale tracker/name\n'
  else
    printf 'FAIL  D11 exact seat plus unique SID/pane bus proof heals stale tracker/name\n%s\n' "$field_block"
    fail=1
  fi
  for label in duplicate foreign-pane foreign-terminal absent; do
    if grep -Eq "${label}[[:space:]]+conflict" <<<"$field_block" \
      && ! grep -Eq "${label}[[:space:]]+re-confirm" <<<"$field_block"; then
      printf 'PASS  D11 negative remains conflict: %s\n' "$label"
    else
      printf 'FAIL  D11 negative remains conflict: %s\n%s\n' "$label" "$field_block"
      fail=1
    fi
  done

  backfill_dir="$ROOT/backfill-hcom"
  mkdir -p "$backfill_dir"
  python3 - "$backfill_dir/hcom.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.executescript('''
CREATE TABLE instances(name TEXT PRIMARY KEY, launch_context TEXT DEFAULT '');
CREATE TABLE process_bindings(process_id TEXT PRIMARY KEY, instance_name TEXT, updated_at REAL NOT NULL);
PRAGMA user_version=17;
INSERT INTO instances(name, launch_context) VALUES ('repair-bus', '{}');
INSERT INTO process_bindings(process_id, instance_name, updated_at) VALUES ('process-repair', 'repair-bus', 1);
''')
db.commit()
PY
  backfill_dry="$(MOCK_RECONCILE_SCENARIO=backfill MOCK_RECONCILE_HCOM_DIR="$backfill_dir" run_one "$FIX/reconcile-backfill" 0)"
  if grep -q 'launch context backfill pending from exact live terminal+pane and unique joined bus' <<<"$backfill_dry" \
    && python3 -c 'import sqlite3,sys; raise SystemExit(0 if sqlite3.connect(sys.argv[1]).execute("select launch_context from instances where name=\"repair-bus\"").fetchone()[0] == "{}" else 1)' "$backfill_dir/hcom.db"; then
    printf 'PASS  reconcile dry-run reports launch-context write without mutating hcom\n'
  else
    printf 'FAIL  reconcile dry-run reports launch-context write without mutating hcom\n%s\n' "$backfill_dry"
    fail=1
  fi
  backfill_apply="$(MOCK_RECONCILE_SCENARIO=backfill MOCK_RECONCILE_HCOM_DIR="$backfill_dir" run_one "$FIX/reconcile-backfill" 1 --apply)"
  if grep -q 'launch context backfill completed before registry append' <<<"$backfill_apply" \
    && grep -q '=== EXIT ==='$'\n''0' <<<"$backfill_apply" \
    && python3 -c '
import json,sqlite3,sys
ctx=json.loads(sqlite3.connect(sys.argv[1]).execute("select launch_context from instances where name=\"repair-bus\"").fetchone()[0])
raise SystemExit(0 if ctx.get("pane_id")=="p_repair" and ctx.get("process_id")=="process-repair" else 1)
' "$backfill_dir/hcom.db"; then
    printf 'PASS  reconcile --apply writes and confirms missing launch coordinates\n'
  else
    printf 'FAIL  reconcile --apply writes and confirms missing launch coordinates\n%s\n' "$backfill_apply"
    fail=1
  fi

  python3 - "$backfill_dir/hcom.db" <<'PY'
import sqlite3, sys
db=sqlite3.connect(sys.argv[1])
db.execute("update instances set launch_context='{}' where name='repair-bus'")
db.execute("pragma user_version=18")
db.commit()
PY
  backfill_refuse="$(MOCK_RECONCILE_SCENARIO=backfill MOCK_RECONCILE_HCOM_DIR="$backfill_dir" run_one "$FIX/reconcile-backfill" 1 --apply)"
  if grep -q 'completion refused \[launch_context_schema_mismatch\]' <<<"$backfill_refuse" \
    && grep -q '=== EXIT ==='$'\n''1' <<<"$backfill_refuse" \
    && python3 -c 'import sqlite3,sys; raise SystemExit(0 if sqlite3.connect(sys.argv[1]).execute("select launch_context from instances where name=\"repair-bus\"").fetchone()[0] == "{}" else 1)' "$backfill_dir/hcom.db"; then
    printf 'PASS  reconcile schema mismatch is typed and writes nothing\n'
  else
    printf 'FAIL  reconcile schema mismatch is typed and writes nothing\n%s\n' "$backfill_refuse"
    fail=1
  fi
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HR[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — reconcile contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
