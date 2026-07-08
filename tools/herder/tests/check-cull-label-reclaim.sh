#!/usr/bin/env bash
# check-cull-label-reclaim.sh — prove cull frees labels held by dead pane-less rows.

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

chmod +x "$MOCKBIN/herdr"
PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

seed_dead_holder() {
  local reg="$1" label="$2"
  cat >"$reg" <<JSONL
{"kind":"session","guid":"guid-dead-$label","event":"migrated_v1","recorded_at":"2026-07-08T00:00:00Z","state":"unseated","label":"$label","role":"worker","tool":"codex"}
JSONL
}

run_herder() {
  local state="$1" pane="$2"; shift 2
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$HOME" \
    HERDR_ENV=1 HERDR_PANE_ID="$pane" HERDER_STATE_DIR="$state" MOCK_PROBE_DIR="$state/probe" \
    "${HERDER[@]}" "$@" 2>&1)"
  RUN_RC=$?
}

make_case() {
  CASE="$ROOT/$1"
  REG_DIR="$CASE/state"
  mkdir -p "$REG_DIR/probe"
  REGISTRY="$REG_DIR/registry.jsonl"
}

# 1. Dead pane-less holder -> cull -> enroll can reclaim the same label.
make_case enroll_reclaim
seed_dead_holder "$REGISTRY" reclaim
run_herder "$REG_DIR" p_culler cull --label reclaim
[[ "$RUN_RC" -eq 0 ]] && ok "enroll reclaim: cull exits 0" || bad "enroll reclaim: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
tail -n1 "$REGISTRY" | jq -e '.event=="retired" and .state=="retired" and .label=="reclaim" and .close_result=="already_gone"' >/dev/null \
  && ok "enroll reclaim: cull appends retired row" || bad "enroll reclaim: cull appends retired row" "latest=$(tail -n1 "$REGISTRY")"
run_herder "$REG_DIR" p_self enroll --label reclaim --role worker
[[ "$RUN_RC" -eq 0 ]] && ok "enroll reclaim: label can be enrolled" || bad "enroll reclaim: label can be enrolled" "rc=$RUN_RC out=$RUN_OUT"
tail -n1 "$REGISTRY" | jq -e '.event=="seated" and .state=="seated" and .label=="reclaim" and .seat.terminal_id=="term_SELF"' >/dev/null \
  && ok "enroll reclaim: new seated holder written" || bad "enroll reclaim: new seated holder written" "latest=$(tail -n1 "$REGISTRY")"

# 2. Dead pane-less holder -> cull -> rename can move an active row onto the label.
make_case rename_reclaim
seed_dead_holder "$REGISTRY" reclaim
cat >>"$REGISTRY" <<'JSONL'
{"kind":"session","guid":"guid-live-other","event":"registered","recorded_at":"2026-07-08T00:00:01Z","state":"seated","label":"other","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term_OTHER","pane_id":"p_other"}}
JSONL
run_herder "$REG_DIR" p_culler cull --label reclaim
[[ "$RUN_RC" -eq 0 ]] && ok "rename reclaim: cull exits 0" || bad "rename reclaim: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
run_herder "$REG_DIR" p_self rename other reclaim
[[ "$RUN_RC" -eq 0 ]] && ok "rename reclaim: label can be renamed to" || bad "rename reclaim: label can be renamed to" "rc=$RUN_RC out=$RUN_OUT"
tail -n1 "$REGISTRY" | jq -e '.event=="labelled" and .guid=="guid-live-other" and .label=="reclaim" and .state=="seated"' >/dev/null \
  && ok "rename reclaim: labelled row written" || bad "rename reclaim: labelled row written" "latest=$(tail -n1 "$REGISTRY")"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN — cull frees labels held by closed rows.\n'
  exit 0
else
  printf 'CONTRACT DRIFT — see failures above.\n'
  exit 1
fi
