#!/usr/bin/env bash
# check-credential-contract.sh — pin the owner-gated credential cutover split.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT
fail=0

ok() { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

run_credential() {
  local case_dir="$1"; shift
  mkdir -p "$case_dir/home" "$case_dir/cache" "$case_dir/state"
  RUN_OUT="$(env -i \
    PATH="$PATH" \
    HOME="$case_dir/home" \
    XDG_CACHE_HOME="$case_dir/cache" \
    HERDER_STATE_DIR="$case_dir/state" \
    AI_CONFIG_ROOT="$REPO_ROOT" \
    "$REPO_ROOT/bin/herder" credential "$@" 2>"$case_dir/stderr")"
  RUN_RC=$?
  RUN_ERR="$(cat "$case_dir/stderr")"
}

seed_legacy() {
  local case_dir="$1"
  printf '%s\n' '{"kind":"session","guid":"guid-legacy","event":"seated","recorded_at":"2026-07-18T00:00:00Z","state":"seated","label":"legacy","tool":"bash","seat":{"kind":"process","pid":1}}' >"$case_dir/state/registry.jsonl"
}

seed_covered() {
  local case_dir="$1" generation="generation-covered" token_dir
  token_dir="$case_dir/state/credentials/guid-covered"
  mkdir -p "$token_dir"
  chmod 700 "$case_dir/state/credentials" "$token_dir"
  printf '%s\n' '{"kind":"session","guid":"guid-covered","event":"seated","recorded_at":"2026-07-18T00:00:00Z","state":"seated","label":"covered","tool":"bash","seat":{"kind":"process","pid":1,"credential_generation":"generation-covered"}}' >"$case_dir/state/registry.jsonl"
  printf '%s\n' '{"version":1,"guid":"guid-covered","generation":"generation-covered","token":"contract-token"}' >"$token_dir/$generation.token"
  chmod 600 "$token_dir/$generation.token"
}

below="$ROOT/below"
mkdir -p "$below/state"
seed_legacy "$below"
run_credential "$below" enable
if [[ "$RUN_RC" -eq 1 && "$RUN_OUT" == *'blocker: guid-legacy: legacy seat has no credential generation'* && "$RUN_ERR" == *'herder credential sweep'* && ! -e "$below/state/credentials/cutover-v1" ]]; then
  ok "enable refuses below 100% without creating marker"
else
  bad "enable refuses below 100% without creating marker" "rc=$RUN_RC stdout=$RUN_OUT stderr=$RUN_ERR"
fi

token_loss="$ROOT/token_loss"
mkdir -p "$token_loss/state"
seed_covered "$token_loss"
rm "$token_loss/state/credentials/guid-covered/generation-covered.token"
run_credential "$token_loss" enable
if [[ "$RUN_RC" -eq 1 && "$RUN_OUT" == *'blocker: guid-covered: current credential unavailable'* && "$RUN_OUT" == *'herder repair reissue-credential --guid guid-covered'* && "$RUN_ERR" == *'herder credential sweep'* && ! -e "$token_loss/state/credentials/cutover-v1" ]]; then
  ok "enable re-verifies current credential usability before marker creation"
else
  bad "enable re-verifies current credential usability before marker creation" "rc=$RUN_RC stdout=$RUN_OUT stderr=$RUN_ERR"
fi

covered="$ROOT/covered"
mkdir -p "$covered/state"
seed_covered "$covered"
run_credential "$covered" sweep
if [[ "$RUN_RC" -eq 0 && "$RUN_OUT" == *'herder credential enable'* && ! -e "$covered/state/credentials/cutover-v1" ]]; then
  ok "100% sweep reports enable next and leaves marker absent"
else
  bad "100% sweep reports enable next and leaves marker absent" "rc=$RUN_RC stdout=$RUN_OUT stderr=$RUN_ERR"
fi

run_credential "$covered" enable
marker="$covered/state/credentials/cutover-v1"
marker_mode="$(stat -c '%a' "$marker" 2>/dev/null || true)"
marker_body="$(cat "$marker" 2>/dev/null || true)"
if [[ "$RUN_RC" -eq 0 && "$RUN_OUT" == *'credential cutover enabled'* && "$marker_mode" == "600" && "$marker_body" == "credential-cutover-v1" ]]; then
  ok "enable at 100% creates the owner-only marker"
else
  bad "enable at 100% creates the owner-only marker" "rc=$RUN_RC mode=$marker_mode body=$marker_body stdout=$RUN_OUT stderr=$RUN_ERR"
fi

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — credential sweep/enable cutover contract holds.\n'
  exit 0
fi

printf '\nCREDENTIAL CONTRACT DRIFT — see failures above.\n'
exit 1
