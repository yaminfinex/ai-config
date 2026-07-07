#!/usr/bin/env bash
# check-send-contract.sh — lock the herder send SEND + resolution contract with
# committed golden fixtures. This is the durable guard for R2/R5: the "byte-for-byte
# behavior-preserving" claim was only ever a live diff against pristine `main`, which
# vanishes at merge — and it only exercised --dry-run/resolve, never a real
# paste+submit. That blind spot is exactly how the send-path regressions (dead
# --no-enter/--timeout/--force, JSON-to-stdout, missing verify=queued line, lost
# extra_enter_sent/paste_collapsed) shipped. This drives herder send against a
# hermetic mock `herdr` (no live agent) and diffs BOTH the stderr human line AND the
# --json record against goldens/<scenario>.txt.
#
# Usage:
#   check-send-contract.sh            # verify current worktree herder send vs goldens
#   check-send-contract.sh --write    # (re)generate goldens from $HERDER_CMD_SEND_BIN
#   HERDER_CMD_SEND_BIN=/path/to/herder send check-send-contract.sh [--write]
#
# HERDER_CMD_SEND_BIN may point at ANY executable honouring the herder send CLI
# (the bash script or the Go `bin/herder send` shim); it is exec'd directly,
# not via `bash`, so the same suite gates either implementation.
#
# Output is fully deterministic (fixed panes, char counts, message, no timestamps),
# so no normalization is needed.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
FIX="$TESTS_DIR/fixtures"
GOLDENS="$TESTS_DIR/goldens"
HS=("$REPO_ROOT/bin/herder" send)
[[ -n "${HERDER_CMD_SEND_BIN:-}" ]] && HS=("$HERDER_CMD_SEND_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

# Hermetic bin dir: mock `herdr` first on PATH, real jq/python3/awk/grep behind it.
MOCKBIN="$(mktemp -d)"
ln -s "$TESTS_DIR/mock-herdr" "$MOCKBIN/herdr"
trap 'rm -rf "$MOCKBIN"' EXIT

MSG='ring: alpha unit DONE'

# scenario name | mock scenario | herder send args (message substituted for @MSG@)
SCENARIOS=(
  "dryrun_nodrift|idle|--dry-run --json alpha"
  "dryrun_drift|idle|--dry-run --json beta"
  "normal|normal|--json alpha @MSG@"
  "noenter|noenter|--no-enter --json alpha @MSG@"
  "busy|busy|--json alpha @MSG@"
  "bootrace|bootrace|--json alpha @MSG@"
  "force|modal|--force --json alpha @MSG@"
  "noforce_refuse|modal|--json alpha @MSG@"
  "timeout|normal|--timeout 5000 --json alpha @MSG@"
)

run_one() {  # $1=mock scenario, rest=args → prints normalized block, sets no globals
  local scen="$1"; shift
  local state err out code
  state="$(mktemp -d)"
  err="$(mktemp)"
  out="$(env -i \
    PATH="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin" \
    HOME="$HOME" \
    HERDR_ENV=1 HERDER_BUS=herdr \
    HERDER_STATE_DIR="$FIX" \
    MOCK_HERDR_SCENARIO="$scen" MOCK_HERDR_STATE="$state" \
    "${HS[@]}" "$@" 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$err")" "$out" "$code"
  rm -rf "$state" "$err"
}

fail=0
for row in "${SCENARIOS[@]}"; do
  IFS='|' read -r name scen argstr <<<"$row"
  # shellcheck disable=SC2206
  args=(); for a in $argstr; do [[ "$a" == "@MSG@" ]] && args+=("$MSG") || args+=("$a"); done

  block="$(run_one "$scen" "${args[@]}")"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    continue
  fi

  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hs_diff.$$  2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hs_diff.$$; fail=1
  fi
  rm -f /tmp/hs_diff.$$
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HS[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — send/resolution contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
