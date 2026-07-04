#!/usr/bin/env bash
# check-shims.sh - hermetic contract tests for the W4 claude/codex PATH shims.
#
# No live hcom, claude, codex, or herder calls. Each scenario copies the real shim scripts
# into a temp repo layout, installs mock bin/herder and real binaries, then drives the shim
# through env -i.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SHIMS_DIR="$TESTS_DIR/../shims"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

PATH_BASE="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [[ "$got" == "$want" ]]; then
    ok "$name"
  else
    bad "$name" "got [$got] want [$want]"
  fi
}

assert_file_eq() {
  local name="$1" file="$2" want="$3" got
  got="$(cat "$file" 2>/dev/null || true)"
  assert_eq "$name" "$got" "$want"
}

assert_file_missing() {
  local name="$1" file="$2"
  if [[ ! -e "$file" ]]; then
    ok "$name"
  else
    bad "$name" "unexpected file exists: $file"
  fi
}

make_case() {
  local name="$1"
  CASE_DIR="$ROOT/$name"
  REPO_CASE="$CASE_DIR/repo"
  SHIM_CASE="$REPO_CASE/skills/herder/shims"
  HERDER_BIN="$REPO_CASE/bin"
  REALBIN="$CASE_DIR/realbin"
  OTHERBIN="$CASE_DIR/otherbin"
  PROBE="$CASE_DIR/probe"
  mkdir -p "$SHIM_CASE" "$HERDER_BIN" "$REALBIN" "$OTHERBIN" "$PROBE"
  cp "$SHIMS_DIR/claude" "$SHIM_CASE/claude"
  cp "$SHIMS_DIR/codex" "$SHIM_CASE/codex"

  cat > "$HERDER_BIN/herder" <<'MOCK_HERDER_LAUNCH'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/herder_argv"
MOCK_HERDER_LAUNCH

  cat > "$REALBIN/claude" <<'MOCK_REAL_CLAUDE'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
count="$(cat "$PROBE/real_claude_count" 2>/dev/null || printf '0')"
count=$((count + 1))
printf '%s\n' "$count" >"$PROBE/real_claude_count"
printf '%s\n' "$@" >"$PROBE/real_claude_argv"
MOCK_REAL_CLAUDE

  cat > "$REALBIN/codex" <<'MOCK_REAL_CODEX'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
count="$(cat "$PROBE/real_codex_count" 2>/dev/null || printf '0')"
count=$((count + 1))
printf '%s\n' "$count" >"$PROBE/real_codex_count"
printf '%s\n' "$@" >"$PROBE/real_codex_argv"
MOCK_REAL_CODEX

  chmod +x "$SHIM_CASE/claude" "$SHIM_CASE/codex" "$HERDER_BIN/herder" \
    "$REALBIN/claude" "$REALBIN/codex"
}

run_with_timeout() {
  local seconds="$1" pid watch rc
  shift
  "$@" &
  pid=$!
  (
    sleep "$seconds"
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  ) &
  watch=$!
  wait "$pid"
  rc=$?
  kill "$watch" 2>/dev/null || true
  wait "$watch" 2>/dev/null || true
  return "$rc"
}

# 1. Plain invocation execs bin/herder launch with tool name + user args.
make_case plain
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/claude" --model opus "two words"
rc=$?
assert_eq "plain claude: exit 0" "$rc" "0"
assert_file_eq "plain claude: herder launch argv order" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch claude --model opus "two words")"

# 2. Per-tool default args are deliberately whitespace-split and prepended.
make_case defaults_claude
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HERDER_SHIM_ARGS_CLAUDE="--dangerously-skip-permissions --print" \
  "$SHIM_CASE/claude" user-arg
rc=$?
assert_eq "defaults claude: exit 0" "$rc" "0"
assert_file_eq "defaults claude: prepended before user args" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch claude --dangerously-skip-permissions --print user-arg)"

make_case defaults_codex
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HERDER_SHIM_ARGS_CODEX="--dangerously-bypass-approvals-and-sandbox" \
  "$SHIM_CASE/codex" --ask-for-approval never
rc=$?
assert_eq "defaults codex: exit 0" "$rc" "0"
assert_file_eq "defaults codex: prepended before user args" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch codex --dangerously-bypass-approvals-and-sandbox --ask-for-approval never)"

# 3. Recursion guard skips the shim even when PATH names it through a symlink.
make_case recursion
ln -s "$SHIM_CASE" "$CASE_DIR/shimlink"
run_with_timeout 3 env -i \
  PATH="$CASE_DIR/shimlink:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/claude" --real
rc=$?
assert_eq "recursion guard: exits through real binary" "$rc" "0"
assert_file_eq "recursion guard: real binary called once" "$PROBE/real_claude_count" "1"
assert_file_eq "recursion guard: real argv preserved" "$PROBE/real_claude_argv" \
  "$(printf '%s\n' --real)"
assert_file_missing "recursion guard: herder launch not called" "$PROBE/herder_argv"

# 4. Absolute invocation still uses repo-local bin/herder when shim dir is not first on PATH.
make_case not_first
env -i PATH="$OTHERBIN:$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/codex" run-here
rc=$?
assert_eq "not first on PATH: exit 0" "$rc" "0"
assert_file_eq "not first on PATH: repo launch used" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch codex run-here)"

# 5. Missing bin/herder is a loud error, not silent fallthrough.
make_case missing_sibling
rm -f "$HERDER_BIN/herder"
err="$PROBE/stderr"
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/claude" hello 2>"$err"
rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "missing bin/herder: nonzero exit"
else
  bad "missing bin/herder: nonzero exit" "rc=0"
fi
if grep -q 'missing executable bin/herder' "$err"; then
  ok "missing bin/herder: loud diagnostic"
else
  bad "missing bin/herder: loud diagnostic" "stderr=$(cat "$err" 2>/dev/null)"
fi
assert_file_missing "missing bin/herder: no real binary fallback" "$PROBE/real_claude_count"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN - shim contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
