#!/usr/bin/env bash
# Hermetic contract for the managed interactive launcher functions: resolve
# once past retired herder shims and mise-owned candidates, then exec the
# absolute vendor entry point with the configured default args.

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT

fail() { printf 'FAIL  %s\n' "$1" >&2; exit 1; }
pass() { printf 'PASS  %s\n' "$1"; }

mkdir -p "$TEST_ROOT/herder-bin" "$TEST_ROOT/mise/shims" \
  "$TEST_ROOT/mise/installs/node/fixture/bin" "$TEST_ROOT/symlink-bin" "$TEST_ROOT/vendor-bin"
cat >"$TEST_ROOT/herder-bin/claude" <<'EOF'
#!/usr/bin/env bash
# herder-path-shim
exit 91
EOF
cat >"$TEST_ROOT/mise/shims/claude" <<'EOF'
#!/usr/bin/env bash
exit 92
EOF
cat >"$TEST_ROOT/mise/installs/node/fixture/bin/claude" <<'EOF'
#!/usr/bin/env bash
exit 93
EOF
ln -s "$TEST_ROOT/mise/installs/node/fixture/bin/claude" "$TEST_ROOT/symlink-bin/claude"
cat >"$TEST_ROOT/vendor-bin/claude" <<'EOF'
#!/usr/bin/env bash
printf 'exe=%s\n' "$0" >"$LAUNCHER_TEST_LOG"
printf 'arg=%s\n' "$@" >>"$LAUNCHER_TEST_LOG"
exit 23
EOF
cat >"$TEST_ROOT/vendor-bin/codex" <<'EOF'
#!/usr/bin/env bash
printf 'exe=%s\n' "$0" >"$LAUNCHER_TEST_LOG"
printf 'arg=%s\n' "$@" >>"$LAUNCHER_TEST_LOG"
EOF
chmod +x "$TEST_ROOT/herder-bin/claude" "$TEST_ROOT/mise/shims/claude" \
  "$TEST_ROOT/mise/installs/node/fixture/bin/claude" \
  "$TEST_ROOT/vendor-bin/claude" "$TEST_ROOT/vendor-bin/codex"

export LAUNCHER_TEST_LOG="$TEST_ROOT/launch.log"
PATH="$TEST_ROOT/herder-bin:$TEST_ROOT/mise/shims:$TEST_ROOT/symlink-bin:$TEST_ROOT/vendor-bin:/usr/bin:/bin"
# shellcheck source=../../../lib/launchers.sh
source "$ROOT/lib/launchers.sh"

set +e
claude --model test-model
rc=$?
set -e
[[ $rc -eq 23 ]] || fail "launcher did not return the vendor process status"
grep -Fx "exe=$TEST_ROOT/vendor-bin/claude" "$LAUNCHER_TEST_LOG" >/dev/null \
  || fail "launcher did not exec the absolute vendor entry point"
grep -Fx 'arg=--dangerously-skip-permissions' "$LAUNCHER_TEST_LOG" >/dev/null \
  || fail "launcher omitted the Claude default autonomy flag"
grep -Fx 'arg=--model' "$LAUNCHER_TEST_LOG" >/dev/null \
  || fail "launcher lost caller arguments"
grep -Fx 'arg=test-model' "$LAUNCHER_TEST_LOG" >/dev/null \
  || fail "launcher lost caller argument values"
pass "launcher skips herder/mise imposters and execs the vendor once"

HERDER_SHIM_ARGS_CODEX='' codex exec hello
if grep -F -- '--dangerously-bypass-approvals-and-sandbox' "$LAUNCHER_TEST_LOG" >/dev/null; then
  fail "empty override did not disable default Codex args"
fi
grep -Fx 'arg=exec' "$LAUNCHER_TEST_LOG" >/dev/null || fail "Codex override lost caller args"
grep -Fx 'arg=hello' "$LAUNCHER_TEST_LOG" >/dev/null || fail "Codex override lost caller values"
pass "empty launcher override preserves ask-mode behavior"

PATH="$TEST_ROOT/herder-bin:$TEST_ROOT/mise/shims:/usr/bin:/bin"
set +e
claude >"$TEST_ROOT/missing.out" 2>"$TEST_ROOT/missing.err"
rc=$?
set -e
[[ $rc -eq 127 ]] || fail "missing vendor did not fail with rc 127"
grep -F "no vendor 'claude' on PATH" "$TEST_ROOT/missing.err" >/dev/null \
  || fail "missing vendor failure was not actionable"
pass "missing vendor fails loud without falling back"

printf 'ALL GREEN - interactive launchers resolve and exec vendors directly.\n'
