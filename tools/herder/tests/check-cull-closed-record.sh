#!/usr/bin/env bash
# check-cull-closed-record.sh — prove cull appends confirmed unseated close rows.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HERDER=("$REPO_ROOT/bin/herder")

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-} ${2:-}" in
  "agent list")
    jq -n '{result:{agents:[]}}'
    ;;
  "pane get")
    jq -n --arg pane "${3:-p_self}" '{result:{pane:{pane_id:$pane, terminal_id:"term_SELF", workspace_id:"ws_self", cwd:"/mock/cwd"}}}'
    ;;
  "agent rename")
    printf '%s\n' "$*" >>"${MOCK_PROBE_DIR:?}/agent_rename_argv"
    jq -n '{result:{ok:true}}'
    ;;
  "pane close")
    printf 'pane close should not be called for pane-less cull\n' >&2
    exit 64
    ;;
  *)
    printf 'mock herdr: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HERDR

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  list)
    shift
    case "${MOCK_HCOM_LIST_JOINED:-}" in
      1)
        printf '%s\n' "$*" >>"${MOCK_PROBE_DIR:?}/hcom_list_argv"
        exit 0
        ;;
      *)
        printf 'instance %s not found\n' "$*" >&2
        exit 1
        ;;
    esac
    ;;
  kill)
    shift
    printf '%s\n' "$*" >>"${MOCK_PROBE_DIR:?}/hcom_kill_argv"
    jq -n '{result:{ok:true}}'
    ;;
  *)
    printf 'mock hcom: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HCOM

chmod +x "$MOCKBIN/herdr" "$MOCKBIN/hcom"
PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

seed_unseated_holder() {
  local reg="$1" label="$2"
  cat >"$reg" <<JSONL
{"kind":"session","guid":"guid-dead-$label","event":"migrated_v1","recorded_at":"2026-07-08T00:00:00Z","state":"unseated","label":"$label","role":"worker","tool":"codex"}
JSONL
}

seed_bus_holder() {
  local reg="$1" label="$2"
  cat >"$reg" <<JSONL
{"guid":"guid-bus-$label","short_guid":"bus-$label","label":"$label","role":"worker","agent":"codex","hcom_dir":"/mock/hcom","hcom_name":"bus-$label","status":"active"}
JSONL
}

run_herder() {
  local state="$1" pane="$2" joined="$3"; shift 3
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$HOME" \
    HERDR_ENV=1 HERDR_PANE_ID="$pane" HERDER_STATE_DIR="$state" MOCK_PROBE_DIR="$state/probe" \
    MOCK_HCOM_LIST_JOINED="$joined" \
    "${HERDER[@]}" "$@" 2>&1)"
  RUN_RC=$?
}

make_case() {
  CASE="$ROOT/$1"
  REG_DIR="$CASE/state"
  mkdir -p "$REG_DIR/probe"
  REGISTRY="$REG_DIR/registry.jsonl"
}

close_count() {
  local guid="$1"
  jq -r --arg guid "$guid" 'select(.guid==$guid and .event=="unseated" and .state=="unseated" and .close_result=="already_gone") | .guid' "$REGISTRY" | wc -l | tr -d '[:space:]'
}

# 1. Dead pane-less holder -> cull confirms an appended unseated close snapshot.
make_case closed_record
seed_unseated_holder "$REGISTRY" trap
run_herder "$REG_DIR" p_culler "" cull --label trap
[[ "$RUN_RC" -eq 0 ]] && ok "closed record: cull exits 0" || bad "closed record: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
tail -n1 "$REGISTRY" | jq -e '.event=="unseated" and .state=="unseated" and .label=="trap" and .close_result=="already_gone" and .close_reason=="registry row has no pane_id or terminal_id" and (.seat|not)' >/dev/null \
  && ok "closed record: unseated close row appended" || bad "closed record: unseated close row appended" "latest=$(tail -n1 "$REGISTRY")"

# 2. Current spec truth: unseated rows still hold labels; retire/wave C will reclaim.
run_herder "$REG_DIR" p_self "" enroll --label trap --role worker
[[ "$RUN_RC" -ne 0 ]] && grep -q 'label "trap" already belongs to active guid guid-dead-trap' <<<"$RUN_OUT" \
  && ok "closed record: enroll still refuses held label" || bad "closed record: enroll still refuses held label" "rc=$RUN_RC out=$RUN_OUT"
cat >>"$REGISTRY" <<'JSONL'
{"kind":"session","guid":"guid-live-other","event":"registered","recorded_at":"2026-07-08T00:00:01Z","state":"seated","label":"other","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term_OTHER","pane_id":"p_other"}}
JSONL
run_herder "$REG_DIR" p_self "" rename other trap
[[ "$RUN_RC" -ne 0 ]] && grep -q 'label "trap" already belongs to active guid guid-dead-trap' <<<"$RUN_OUT" \
  && ok "closed record: rename still refuses held label" || bad "closed record: rename still refuses held label" "rc=$RUN_RC out=$RUN_OUT"

# 3. Repeated cull is honest: if it reports success, another close row was appended.
before="$(close_count guid-dead-trap)"
run_herder "$REG_DIR" p_culler "" cull --label trap
after="$(close_count guid-dead-trap)"
[[ "$RUN_RC" -eq 0 && "$after" -eq $((before + 1)) ]] \
  && ok "closed record: repeat cull appends confirmed row" || bad "closed record: repeat cull appends confirmed row" "rc=$RUN_RC before=$before after=$after out=$RUN_OUT"

# 4. Pane-less non-force cull must not kill a still-joined bus row.
make_case bus_guard
seed_bus_holder "$REGISTRY" live
run_herder "$REG_DIR" p_culler 1 cull --label live
[[ "$RUN_RC" -eq 0 ]] && ok "bus guard: cull exits 0" || bad "bus guard: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
grep -q 'bus: @bus-live still joined; not dropped without --force' <<<"$RUN_OUT" \
  && ok "bus guard: joined row skipped" || bad "bus guard: joined row skipped" "out=$RUN_OUT"
[[ ! -f "$REG_DIR/probe/hcom_kill_argv" ]] \
  && ok "bus guard: hcom kill not called" || bad "bus guard: hcom kill not called" "kill=$(cat "$REG_DIR/probe/hcom_kill_argv" 2>/dev/null)"

make_case bus_force
seed_bus_holder "$REGISTRY" force
run_herder "$REG_DIR" p_culler 1 cull --force --label force
[[ "$RUN_RC" -eq 0 ]] && ok "bus guard force: cull exits 0" || bad "bus guard force: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(cat "$REG_DIR/probe/hcom_kill_argv" 2>/dev/null)" = "bus-force" ]] \
  && ok "bus guard force: hcom kill allowed" || bad "bus guard force: hcom kill allowed" "kill=$(cat "$REG_DIR/probe/hcom_kill_argv" 2>/dev/null)"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN — cull closed-record contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT — see failures above.\n'
  exit 1
fi
