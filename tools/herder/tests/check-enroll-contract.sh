#!/usr/bin/env bash
# check-enroll-contract.sh — lock the deletion-first enroll contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HEN=("$REPO_ROOT/bin/herder" enroll)

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "pane get")
    jq -n '{result:{pane:{pane_id:"p_self",terminal_id:"term_SELF",workspace_id:"ws_self",cwd:"/mock/cwd"}}}';;
  *) exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "${MOCK_HCOM_ROWS:-[]}"
  exit 0
fi
exit 64
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

pass() { printf 'PASS  %s\n' "$1"; }
fail_case() { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

new_case() {
  CASE="$ROOT/$1"
  mkdir -p "$CASE/home" "$CASE/state"
  printf '%s\n' '{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}' >"$CASE/state/registry.jsonl"
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
}

run_enroll() {
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    MOCK_HCOM_ROWS="${MOCK_HCOM_ROWS:-[]}" \
    HERDER_GUID="${HERDER_GUID:-}" HERDER_LABEL="${HERDER_LABEL:-}" \
    HCOM_SESSION_ID="${HCOM_SESSION_ID:-}" \
    "${HEN[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

new_case fresh
HERDER_LABEL=fresh HCOM_SESSION_ID=sid-live \
  MOCK_HCOM_ROWS='[{"name":"bus-live","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
  run_enroll --role worker --json
if [[ "$RUN_RC" -eq 0 ]] \
  && jq -e '.state=="seated" and .label=="fresh" and .guid!="" and .seat.pane_id=="p_self" and .seat.hcom_name=="bus-live"' <<<"$RUN_OUT" >/dev/null \
  && grep -q 'credential_generation":"[0-9a-f]' <<<"$RUN_OUT"; then
  pass "fresh enroll records a new live seat"
else
  fail_case "fresh enroll" "rc=$RUN_RC err=$(cat "$RUN_ERR_F") out=$RUN_OUT"
fi

new_case explicit
MOCK_HCOM_ROWS='[{"name":"bus-live","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
  run_enroll --label explicit --session-id sid-live --hcom-name bus-live --json
if [[ "$RUN_RC" -eq 0 ]] && jq -e '.seat.hcom_verified==true and .seat.hcom_name=="bus-live"' <<<"$RUN_OUT" >/dev/null; then
  pass "explicit bus evidence is corroborated"
else
  fail_case "explicit bus evidence" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

new_case bad-explicit
MOCK_HCOM_ROWS='[]' run_enroll --label explicit --hcom-name ghost
if [[ "$RUN_RC" -eq 1 ]] && grep -q 'explicit evidence did not corroborate' "$RUN_ERR_F"; then
  pass "unjoined explicit evidence fails closed"
else
  fail_case "unjoined explicit evidence" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

new_case existing-guid
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","pane_id":"p_old","terminal_id":"term_OLD"}}
JSONL
cp "$CASE/state/registry.jsonl" "$CASE/before.jsonl"
HERDER_GUID=guid-existing-0000 run_enroll --label replacement
if [[ "$RUN_RC" -eq 1 ]] \
  && cmp -s "$CASE/before.jsonl" "$CASE/state/registry.jsonl" \
  && grep -q 'identity repair is no longer an enroll operation' "$RUN_ERR_F" \
  && grep -q "herder adopt guid-existing-0000" "$RUN_ERR_F"; then
  pass "existing guid refuses without mutation and names adopt recovery"
else
  fail_case "existing guid refusal" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

new_case foreign-label
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-other-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"taken","role":"worker","tool":"claude","seat":{"kind":"herdr","pane_id":"p_other","terminal_id":"term_OTHER"}}
JSONL
cp "$CASE/state/registry.jsonl" "$CASE/before.jsonl"
run_enroll --label taken
if [[ "$RUN_RC" -eq 1 ]] && cmp -s "$CASE/before.jsonl" "$CASE/state/registry.jsonl" && grep -q 'already belongs' "$RUN_ERR_F"; then
  pass "foreign label owner refuses without mutation"
else
  fail_case "foreign label owner" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

new_case dead-label
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-dormant-0000","event":"unseated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"taken","role":"worker","tool":"claude"}
JSONL
run_enroll --label taken
if [[ "$RUN_RC" -eq 1 ]] && grep -q 'state unseated' "$RUN_ERR_F" && grep -q 'herder adopt guid-dormant-0000' "$RUN_ERR_F"; then
  pass "dead label owner refuses with owner-free recovery"
else
  fail_case "dead label owner" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

RUN_ERR_F="$ROOT/outside.err"
env -i PATH="$PATH_HERMETIC" HOME="$ROOT" HERDER_STATE_DIR="$ROOT/state" "${HEN[@]}" >/dev/null 2>"$RUN_ERR_F"
RUN_RC=$?
if [[ "$RUN_RC" -eq 1 ]] && grep -q 'HERDR_ENV/HERDR_PANE_ID required' "$RUN_ERR_F"; then
  pass "outside-herdr invocation refuses"
else
  fail_case "outside-herdr refusal" "rc=$RUN_RC err=$(cat "$RUN_ERR_F")"
fi

HELP_OUT="$("${HEN[@]}" --help)"
if grep -q 'creates a fresh identity' <<<"$HELP_OUT" \
  && grep -q 'does not repair' <<<"$HELP_OUT" \
  && ! grep -q 're-enroll' <<<"$HELP_OUT"; then
  pass "help reflects the surviving fresh-enroll surface"
else
  fail_case "help surface" "$HELP_OUT"
fi

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — enroll contract holds.\n'
  exit 0
fi
printf '\nCONTRACT DRIFT — see failures above.\n'
exit 1
