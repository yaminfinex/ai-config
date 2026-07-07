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
  SHIM_CASE="$REPO_CASE/tools/herder/shims"
  HERDER_BIN="$REPO_CASE/bin"
  REALBIN="$CASE_DIR/realbin"
  OTHERBIN="$CASE_DIR/otherbin"
  PROBE="$CASE_DIR/probe"
  mkdir -p "$SHIM_CASE" "$HERDER_BIN" "$REALBIN" "$OTHERBIN" "$PROBE"
  cp "$SHIMS_DIR/claude" "$SHIM_CASE/claude"
  cp "$SHIMS_DIR/codex" "$SHIM_CASE/codex"
  cp "$SHIMS_DIR/hcom" "$SHIM_CASE/hcom"

  cat > "$HERDER_BIN/herder" <<'MOCK_HERDER_LAUNCH'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/herder_argv"
# Record the recursion-guard handoff so tests can assert the shim resolved the
# REAL hcom (not itself) before forwarding to `herder hook`.
printf '%s\n' "${HERDER_HOOK_HCOM-}" >"$PROBE/herder_hook_hcom"
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

  cat > "$REALBIN/hcom" <<'MOCK_REAL_HCOM'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/real_hcom_argv"
MOCK_REAL_HCOM

  chmod +x "$SHIM_CASE/claude" "$SHIM_CASE/codex" "$SHIM_CASE/hcom" "$HERDER_BIN/herder" \
    "$REALBIN/claude" "$REALBIN/codex" "$REALBIN/hcom"
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

# 6. hcom shim forwards to `bin/herder hook` and exports HERDER_HOOK_HCOM to the
#    REAL hcom (found behind itself on PATH), so `herder hook` never recurses.
make_case hcom_forward
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" sessionstart --extra flag
rc=$?
assert_eq "hcom shim: exit 0" "$rc" "0"
assert_file_eq "hcom shim: forwards to herder hook with args" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook sessionstart --extra flag)"
assert_file_eq "hcom shim: exports HERDER_HOOK_HCOM to real hcom" "$PROBE/herder_hook_hcom" \
  "$(cd "$REALBIN" && pwd -P)/hcom"
assert_file_missing "hcom shim: real hcom not run directly (herder hook owns it)" "$PROBE/real_hcom_argv"

# 7. Recursion guard: even when PATH names the shim dir through a symlink, the
#    shim skips itself and resolves the real hcom rather than looping.
make_case hcom_recursion
ln -s "$SHIM_CASE" "$CASE_DIR/shimlink"
run_with_timeout 5 env -i \
  PATH="$CASE_DIR/shimlink:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" send @luna -- hi
rc=$?
assert_eq "hcom recursion: exit 0 (no loop)" "$rc" "0"
assert_file_eq "hcom recursion: still resolves real hcom past the symlink" "$PROBE/herder_hook_hcom" \
  "$(cd "$REALBIN" && pwd -P)/hcom"

# 8. hcom shim with NO real hcom on PATH still forwards (herder hook degrades to
#    exit 0 itself); HERDER_HOOK_HCOM is left empty. PATH is a coreutils-only base
#    with no hcom, so the "no real hcom" condition is hermetic.
make_case hcom_no_real
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:/usr/bin:/bin" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" post
rc=$?
assert_eq "hcom no-real: exit 0" "$rc" "0"
assert_file_eq "hcom no-real: forwards to herder hook anyway" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook post)"
assert_file_eq "hcom no-real: HERDER_HOOK_HCOM left empty" "$PROBE/herder_hook_hcom" ""

# 9. Sibling shim dirs on one PATH (worktree checkout + machine-wide main checkout):
#    the hcom shim must export HERDER_HOOK_HCOM to the REAL hcom, never to the
#    sibling shim — resolving each other made the two `herder hook`s ping-pong forever.
make_case hcom_sibling
SIBLING_REPO="$CASE_DIR/sibling"
SIBLING_SHIMS="$SIBLING_REPO/tools/herder/shims"
mkdir -p "$SIBLING_SHIMS"
cp "$SHIMS_DIR/claude" "$SHIMS_DIR/codex" "$SHIMS_DIR/hcom" "$SIBLING_SHIMS/"
chmod +x "$SIBLING_SHIMS/claude" "$SIBLING_SHIMS/codex" "$SIBLING_SHIMS/hcom"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$SIBLING_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" post
rc=$?
assert_eq "hcom sibling: exit 0" "$rc" "0"
assert_file_eq "hcom sibling: forwards to herder hook" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook post)"
assert_file_eq "hcom sibling: HERDER_HOOK_HCOM skips the sibling shim, hits real hcom" \
  "$PROBE/herder_hook_hcom" "$(cd "$REALBIN" && pwd -P)/hcom"

# 10. Same sibling layout under HCOM_LAUNCH_INFLIGHT=1: the claude shim must exec
#     the REAL claude once — exec'ing the sibling shim instead loops forever.
make_case claude_sibling
SIBLING_REPO="$CASE_DIR/sibling"
SIBLING_SHIMS="$SIBLING_REPO/tools/herder/shims"
mkdir -p "$SIBLING_SHIMS"
cp "$SHIMS_DIR/claude" "$SHIMS_DIR/codex" "$SHIMS_DIR/hcom" "$SIBLING_SHIMS/"
chmod +x "$SIBLING_SHIMS/claude" "$SIBLING_SHIMS/codex" "$SIBLING_SHIMS/hcom"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$SIBLING_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/claude" --real
rc=$?
assert_eq "claude sibling inflight: exit 0 (no loop)" "$rc" "0"
assert_file_eq "claude sibling inflight: real binary called once" "$PROBE/real_claude_count" "1"
assert_file_eq "claude sibling inflight: real argv preserved" "$PROBE/real_claude_argv" \
  "$(printf '%s\n' --real)"
assert_file_missing "claude sibling inflight: herder launch not called" "$PROBE/herder_argv"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN - shim contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
