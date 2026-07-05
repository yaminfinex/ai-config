#!/usr/bin/env bash
# check-help-contract.sh — lock command --help text against committed goldens.
#
# Verification drives current executable paths (or HERDER_*_BIN overrides) through
# the same guarded environment and compares stdout/stderr/exit byte-for-byte.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd)"
GOLDENS="$TESTS_DIR/goldens/help"

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
exit 0
MOCK_HERDR
cat >"$MOCKBIN/jq" <<'MOCK_JQ'
#!/usr/bin/env bash
exit 0
MOCK_JQ
cat >"$MOCKBIN/uuidgen" <<'MOCK_UUIDGEN'
#!/usr/bin/env bash
printf '00000000-0000-0000-0000-000000000000\n'
MOCK_UUIDGEN
chmod +x "$MOCKBIN/herdr" "$MOCKBIN/jq" "$MOCKBIN/uuidgen"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin:/sbin"

bin_for() {
  local cmd="$1"
  case "$cmd" in
    send)  printf '%s' "${HERDER_CMD_SEND_BIN:-$REPO_ROOT/bin/herder send}" ;;
    spawn) printf '%s' "${HERDER_SPAWN_BIN:-$REPO_ROOT/bin/herder spawn}" ;;
    list)  printf '%s' "${HERDER_LIST_BIN:-$REPO_ROOT/bin/herder list}" ;;
    wait)  printf '%s' "${HERDER_WAIT_BIN:-$REPO_ROOT/bin/herder wait}" ;;
    cull)  printf '%s' "${HERDER_CULL_BIN:-$REPO_ROOT/bin/herder cull}" ;;
    fork)  printf '%s' "${HERDER_FORK_BIN:-$REPO_ROOT/bin/herder fork}" ;;
    resume) printf '%s' "${HERDER_RESUME_BIN:-$REPO_ROOT/bin/herder resume}" ;;
  esac
}

run_help() {
  local bin="$1" err out code
  local bin_argv=()
  read -r -a bin_argv <<<"$bin"
  err="$(mktemp)"
  mkdir -p "$ROOT/home/.local/state/herder" "$ROOT/cache" "$ROOT/gocache"
  : >"$ROOT/home/.local/state/herder/registry.jsonl"
  out="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$ROOT/home" \
    XDG_CACHE_HOME="$ROOT/cache" \
    GOCACHE="$ROOT/gocache" \
    HERDR_ENV=1 \
    HERDR_PANE_ID=p_help \
    "${bin_argv[@]}" --help 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$err")" "$out" "$code"
  rm -f "$err"
}

fail=0
for cmd in send spawn list wait cull fork resume; do
  bin="$(bin_for "$cmd")"
  block="$(run_help "$bin")"
  gold="$GOLDENS/$cmd.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$cmd"
    continue
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$cmd"
    fail=1
    continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >"/tmp/herder_help_diff.$$" 2>&1; then
    printf 'PASS  %s\n' "$cmd"
  else
    printf 'FAIL  %s\n' "$cmd"
    cat "/tmp/herder_help_diff.$$"
    fail=1
  fi
  rm -f "/tmp/herder_help_diff.$$"
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from current bin/herder subcommands.\n'
  exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — help contract matches goldens.\n'
  exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'
  exit 1
fi
