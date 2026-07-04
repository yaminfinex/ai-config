#!/usr/bin/env bash
# check-help-contract.sh — lock command --help text against the bash reference.
#
# Most goldens are generated FROM the historical bash scripts at d4ca54c, because the
# live scripts have already flipped to Go shims. Spawn/fork/resume goldens are generated
# from current commands for post-port contract updates. Normal verification drives the
# current executable paths (or HERDER_*_BIN overrides) through the same guarded
# environment and compares stdout/stderr/exit byte-for-byte.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd)"
GOLDENS="$TESTS_DIR/goldens/help"

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
REFBIN="$ROOT/ref"
mkdir -p "$MOCKBIN" "$REFBIN" "$GOLDENS"
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

extract_reference() {
  local cmd="$1" path="$REFBIN/herder-$cmd"
  git -C "$REPO_ROOT" show "d4ca54c:skills/herder/scripts/herder-$cmd" >"$path" || return 1
  mkdir -p "$REFBIN/lib"
  git -C "$REPO_ROOT" show "d4ca54c:skills/herder/scripts/lib/trust-modals.sh" >"$REFBIN/lib/trust-modals.sh" || return 1
  git -C "$REPO_ROOT" show "d4ca54c:skills/herder/scripts/lib/hcom-tools.sh" >"$REFBIN/lib/hcom-tools.sh" || return 1
  chmod +x "$path"
  printf '%s' "$path"
}

bin_for() {
  local cmd="$1"
  if [[ "$WRITE" -eq 1 ]]; then
    case "$cmd" in
      spawn|fork|resume) printf '%s' "$TESTS_DIR/../scripts/herder-$cmd"; return ;;
    esac
    extract_reference "$cmd"
    return
  fi
  case "$cmd" in
    send)  printf '%s' "${HERDER_SEND_BIN:-$TESTS_DIR/../scripts/herder-send}" ;;
    spawn) printf '%s' "${HERDER_SPAWN_BIN:-$TESTS_DIR/../scripts/herder-spawn}" ;;
    list)  printf '%s' "${HERDER_LIST_BIN:-$TESTS_DIR/../scripts/herder-list}" ;;
    wait)  printf '%s' "${HERDER_WAIT_BIN:-$TESTS_DIR/../scripts/herder-wait}" ;;
    cull)  printf '%s' "${HERDER_CULL_BIN:-$TESTS_DIR/../scripts/herder-cull}" ;;
    fork)  printf '%s' "${HERDER_FORK_BIN:-$TESTS_DIR/../scripts/herder-fork}" ;;
    resume) printf '%s' "${HERDER_RESUME_BIN:-$TESTS_DIR/../scripts/herder-resume}" ;;
  esac
}

run_help() {
  local bin="$1" err out code
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
    "$bin" --help 2>"$err")"
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
  printf '\nGoldens written from bash reference d4ca54c (legacy) and current post-port commands (spawn/fork/resume).\n'
  exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — help contract matches goldens.\n'
  exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'
  exit 1
fi
