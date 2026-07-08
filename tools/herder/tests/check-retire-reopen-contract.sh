#!/usr/bin/env bash
# check-retire-reopen-contract.sh — lock retire/reopen state and label-release contracts.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "pane get")
    jq -n --arg pane "${3:-p_enroll}" '{result:{pane_id:$pane,terminal_id:"term_enroll",cwd:"/repo",workspace_id:"ws_1"}}';;
  "agent rename")
    jq -n '{result:{type:"ok"}}';;
  "agent list")
    jq -n '{result:{agents:[]}}';;
  "pane list")
    jq -n '{result:{panes:[]}}';;
  *)
    printf 'mock herdr (retire suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

new_case() {
  CASE="$ROOT/$1"
  mkdir -p "$CASE/state" "$CASE/home"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-08T00:00:00Z"}
{"guid":"guid-old-0000","event":"migrated_v1","recorded_at":"2026-07-08T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"trap","role":"worker","tool":"codex"}
{"guid":"guid-other-0000","event":"registered","recorded_at":"2026-07-08T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"other","role":"worker","tool":"claude"}
{"guid":"guid-seated-0000","event":"registered","recorded_at":"2026-07-08T00:00:03Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"busy","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_busy","terminal_id":"term_busy"}}
{"guid":"guid-lost-0000","event":"lost","recorded_at":"2026-07-08T00:00:04Z","node":"11111111-1111-1111-1111-111111111111","state":"lost","label":"gone","role":"worker","tool":"codex"}
JSONL
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
}

run_hr() {
  env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 \
    HERDR_PANE_ID=p_enroll \
    HCOM_INSTANCE_NAME=enrolled-bus \
    "$REPO_ROOT/bin/herder" "$@"
}

assert() {
  local name="$1"; shift
  if "$@"; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    fail=1
  fi
}

new_case label_release
retire_out="$(run_hr retire trap --json 2>"$CASE/retire.err")"; retire_rc=$?
assert "retire unseated exits 0" test "$retire_rc" -eq 0
assert "retire json is unlabelled" bash -c 'grep -q "\"event\":\"retired\"" <<<"$1" && ! grep -q "\"label\"" <<<"$1"' bash "$retire_out"

run_hr rename guid-old-0000 should-refuse >/dev/null 2>"$CASE/rename-retired.err"; rename_retired_rc=$?
assert "rename retired refuses with reopen guidance" bash -c 'test "$1" -ne 0 && grep -q "herder reopen guid-old-0000" "$2" && ! test -s "$3"' bash "$rename_retired_rc" "$CASE/rename-retired.err" "$CASE/probe/herdr_rename_argv"

rename_out="$(run_hr rename other trap 2>"$CASE/rename.err")"; rename_rc=$?
assert "rename reuses retired label" test "$rename_rc" -eq 0

new_case enroll_release
run_hr retire trap >/dev/null 2>"$CASE/retire.err"; retire_rc=$?
enroll_out="$(run_hr enroll --label trap --json 2>"$CASE/enroll.err")"; enroll_rc=$?
assert "enroll reuses retired label" test "$retire_rc" -eq 0 -a "$enroll_rc" -eq 0
assert "enroll wrote trap label" bash -c 'grep -q "\"label\":\"trap\"" <<<"$1"' bash "$enroll_out"

new_case noops
run_hr retire trap >/dev/null 2>"$CASE/retire1.err"; first_rc=$?
rows_before="$(grep -c '"event":"retired"' "$CASE/state/registry.jsonl")"
run_hr retire guid-old-0000 >/dev/null 2>"$CASE/retire2.err"; second_rc=$?
rows_after="$(grep -c '"event":"retired"' "$CASE/state/registry.jsonl")"
assert "retire twice succeeds once" test "$first_rc" -eq 0 -a "$second_rc" -eq 0 -a "$rows_before" -eq 1 -a "$rows_after" -eq 1

new_case refusals
run_hr retire busy >/dev/null 2>"$CASE/seated.err"; seated_rc=$?
run_hr retire gone >/dev/null 2>"$CASE/lost.err"; lost_rc=$?
run_hr rename gone should-refuse >/dev/null 2>"$CASE/rename-lost.err"; rename_lost_rc=$?
assert "retire seated refuses with cull guidance" bash -c 'test "$1" -ne 0 && grep -qi "cull first" "$2"' bash "$seated_rc" "$CASE/seated.err"
assert "retire lost refuses" bash -c 'test "$1" -ne 0 && grep -q "LOST sessions cannot be retired" "$2"' bash "$lost_rc" "$CASE/lost.err"
assert "rename lost refuses" bash -c 'test "$1" -ne 0 && grep -q "lost sessions cannot be renamed" "$2"' bash "$rename_lost_rc" "$CASE/rename-lost.err"

new_case same_pane_successor
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-old-pane-0000","event":"registered","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"old-pane","role":"worker","tool":"codex"}
{"guid":"guid-new-pane-0000","event":"registered","recorded_at":"2026-07-08T00:00:06Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"new-pane","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_reused","terminal_id":"term_reused"}}
JSONL
run_hr retire p_reused >/dev/null 2>"$CASE/retire-pane.err"; retire_pane_rc=$?
assert "retire by pane_id hits seated successor guard" bash -c 'test "$1" -ne 0 && grep -qi "cull first" "$2" && grep -q "guid-new-pane-0000" "$2"' bash "$retire_pane_rc" "$CASE/retire-pane.err"

new_case reopen
run_hr retire trap >/dev/null 2>"$CASE/retire.err"; retire_rc=$?
reopen_out="$(run_hr reopen guid-old-0000 --json 2>"$CASE/reopen.err")"; reopen_rc=$?
run_hr reopen other >/dev/null 2>"$CASE/reopen-open.err"; reopen_open_rc=$?
rename_after_reopen_out="$(run_hr rename guid-old-0000 trap 2>"$CASE/rename-after-reopen.err")"; rename_after_reopen_rc=$?
assert "reopen retired exits 0" test "$retire_rc" -eq 0 -a "$reopen_rc" -eq 0
assert "reopen is unseated unlabelled" bash -c 'grep -q "\"event\":\"reopened\"" <<<"$1" && grep -q "\"state\":\"unseated\"" <<<"$1" && ! grep -q "\"label\"" <<<"$1"' bash "$reopen_out"
assert "reopen non-retired refuses" test "$reopen_open_rc" -ne 0
assert "reopen then rename claims freed label" bash -c 'test "$1" -eq 0 && grep -q "renamed  -> trap (guid-old-0000)" "$2" && grep -q "\"event\":\"labelled\"" "$3" && grep -q "\"label\":\"trap\"" "$3"' bash "$rename_after_reopen_rc" "$CASE/rename-after-reopen.err" "$CASE/state/registry.jsonl"

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — retire/reopen contract holds.\n'
  exit 0
else
  printf '\nRETIRE/REOPEN CONTRACT FAILED — see failures above.\n'
  exit 1
fi
