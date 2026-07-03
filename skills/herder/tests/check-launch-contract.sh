#!/usr/bin/env bash
# check-launch-contract.sh — lock hcom-launch behavior with committed goldens.
#
# The --write pass runs the live bash hcom-launch wrapper. After the Go port and
# shim flip, the same suite verifies scripts/hcom-launch by default, with
# HERDER_LAUNCH_BIN available for explicit binary/shim checks.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOLDENS="$TESTS_DIR/goldens/launch"
HL="${HERDER_LAUNCH_BIN:-$TESTS_DIR/../scripts/hcom-launch}"

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
BASEBIN="$ROOT/basebin"
MOCKBIN="$ROOT/mockbin"
mkdir -p "$BASEBIN" "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/argv"
{
  printf 'HCOM_LAUNCH_INFLIGHT=%s\n' "${HCOM_LAUNCH_INFLIGHT-}"
  printf 'CLAUDE_CONFIG_DIR=%s\n' "${CLAUDE_CONFIG_DIR-}"
  printf 'CODEX_HOME=%s\n' "${CODEX_HOME-}"
  printf 'GEMINI_CLI_HOME=%s\n' "${GEMINI_CLI_HOME-}"
  printf 'HCOM_DIR=%s\n' "${HCOM_DIR-}"
} >"$PROBE/env"
exit "${MOCK_HCOM_RC:-0}"
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"
cat >"$BASEBIN/go" <<'MOCK_GO'
#!/usr/bin/env bash
exec /opt/homebrew/bin/go "$@"
MOCK_GO
chmod +x "$BASEBIN/go"

PATH_BASE="$BASEBIN:/usr/bin:/bin:/sbin"
PATH_WITH_HCOM="$MOCKBIN:$PATH_BASE"

run_case() {
  local name="$1" path_mode="$2" home="$3" hcom_dir="$4" extra_env="$5"
  shift 5
  local case_dir="$ROOT/$name" err out code path
  mkdir -p "$case_dir/home" "$case_dir/probe" "$case_dir/team"
  [[ "$home" == "<case-home>" ]] && home="$case_dir/home"
  [[ "$hcom_dir" == "<case-team>" ]] && hcom_dir="$case_dir/team"
  [[ "$hcom_dir" == "<case-home>/.hcom" ]] && hcom_dir="$home/.hcom"
  path="$PATH_WITH_HCOM"
  [[ "$path_mode" == "no-hcom" ]] && path="$PATH_BASE"

  err="$case_dir/stderr"
  out="$(env -i \
    PATH="$path" \
    HOME="$home" \
    PROBE="$case_dir/probe" \
    HCOM_DIR="$hcom_dir" \
    $extra_env \
    "$HL" "$@" 2>"$err")"
  code=$?

  {
    printf '=== STDERR ===\n%s\n' "$(cat "$err")"
    printf '=== STDOUT ===\n%s\n' "$out"
    printf '=== EXIT ===\n%s\n' "$code"
    printf '=== HCOM ARGV ===\n%s\n' "$(cat "$case_dir/probe/argv" 2>/dev/null)"
    printf '=== ENV PROBES ===\n%s\n' "$(cat "$case_dir/probe/env" 2>/dev/null)"
  } | sed "s|$case_dir|<CASE>|g"
}

fail=0
scenario() {
  local name="$1"
  shift
  block="$(run_case "$name" "$@")"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"
    fail=1
    return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >"/tmp/hcom_launch_diff.$$" 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    cat "/tmp/hcom_launch_diff.$$"
    fail=1
  fi
  rm -f "/tmp/hcom_launch_diff.$$"
}

scenario no_tool          with-hcom '<case-home>' ''          ''                                     
scenario leading_dash     with-hcom '<case-home>' ''          ''                                     --model opus
scenario hcom_missing     no-hcom   '<case-home>' ''          ''                                     claude --model opus
scenario plain_claude     with-hcom '<case-home>' ''          ''                                     claude --model opus
scenario tag_space        with-hcom '<case-home>' ''          ''                                     claude --model opus --tag worker
scenario tag_equals       with-hcom '<case-home>' ''          ''                                     claude --tag=worker --model opus
scenario tag_missing      with-hcom '<case-home>' ''          ''                                     claude --tag
scenario double_dash      with-hcom '<case-home>' ''          ''                                     claude -- --tag worker
scenario pin_claude       with-hcom '<case-home>' '<case-team>' ''                                  claude
scenario pin_codex        with-hcom '<case-home>' '<case-team>' ''                                  codex
scenario pin_gemini       with-hcom '<case-home>' '<case-team>' ''                                  gemini
scenario preset_claude    with-hcom '<case-home>' '<case-team>' 'CLAUDE_CONFIG_DIR=/custom/claude'  claude
scenario global_unset     with-hcom '<case-home>' ''          ''                                     claude
scenario global_home      with-hcom '<case-home>' '<case-home>/.hcom' ''                            claude
scenario unknown_tool     with-hcom '<case-home>' '<case-team>' ''                                  pi --flag

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "$HL"
  exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — launch contract matches goldens.\n'
  exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'
  exit 1
fi
