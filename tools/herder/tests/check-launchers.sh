#!/usr/bin/env bash
# Hermetic contract for the managed interactive launcher functions: resolve
# the vendor once past retired herder shims and mise-owned candidates, then
# launch ON-BUS in the current pane via `hcom <tool> --run-here` with the
# vendor pinned on the child PATH and ambient identity env scrubbed.
# Direct vendor exec survives only for grok and claude -p/--print one-shots.

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT

fail() { printf 'FAIL  %s\n' "$1" >&2; exit 1; }
pass() { printf 'PASS  %s\n' "$1"; }

mkdir -p "$TEST_ROOT/herder-bin" "$TEST_ROOT/mise/shims" \
  "$TEST_ROOT/mise/installs/node/fixture/bin" "$TEST_ROOT/symlink-bin" \
  "$TEST_ROOT/vendor-bin" "$TEST_ROOT/hcom-bin" "$TEST_ROOT/cache"
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
for tool in claude codex grok; do
  cat >"$TEST_ROOT/vendor-bin/$tool" <<'EOF'
#!/usr/bin/env bash
{
  printf 'exe=%s\n' "$0"
  printf 'arg=%s\n' "$@"
  printf 'env_process_id=%s\n' "${HCOM_PROCESS_ID-UNSET}"
} >"$LAUNCHER_TEST_LOG"
exit 23
EOF
done
cat >"$TEST_ROOT/hcom-bin/hcom" <<'EOF'
#!/usr/bin/env bash
{
  printf 'exe=%s\n' "$0"
  printf 'arg=%s\n' "$@"
  printf 'path_head=%s\n' "${PATH%%:*}"
  printf 'env_process_id=%s\n' "${HCOM_PROCESS_ID-UNSET}"
  printf 'env_herder_guid=%s\n' "${HERDER_GUID-UNSET}"
  printf 'env_herdr_pane=%s\n' "${HERDR_PANE_ID-UNSET}"
  printf 'env_hcom_dir=%s\n' "${HCOM_DIR-UNSET}"
  printf 'env_inflight=%s\n' "${HCOM_LAUNCH_INFLIGHT-UNSET}"
} >"$LAUNCHER_TEST_LOG"
exit 55
EOF
chmod +x "$TEST_ROOT/herder-bin/claude" "$TEST_ROOT/mise/shims/claude" \
  "$TEST_ROOT/mise/installs/node/fixture/bin/claude" \
  "$TEST_ROOT/vendor-bin/claude" "$TEST_ROOT/vendor-bin/codex" \
  "$TEST_ROOT/vendor-bin/grok" "$TEST_ROOT/hcom-bin/hcom"

export LAUNCHER_TEST_LOG="$TEST_ROOT/launch.log"
export XDG_CACHE_HOME="$TEST_ROOT/cache"
PIN_DIR="$XDG_CACHE_HOME/ai-config/vendorbin"
BUS_PATH="$TEST_ROOT/herder-bin:$TEST_ROOT/mise/shims:$TEST_ROOT/symlink-bin:$TEST_ROOT/vendor-bin:$TEST_ROOT/hcom-bin:/usr/bin:/bin"
PATH="$BUS_PATH"
# Ambient identity a hand-typed launch from an agent shell would inherit;
# the launcher must scrub it (docs/hazards/agent-cli-identity-hijack.md).
export HCOM_PROCESS_ID="stale-caller-row"
export HERDER_GUID="stale-guid"
export HERDR_PANE_ID="stale-pane"
export HCOM_DIR="$TEST_ROOT/busdir"
# shellcheck source=../../../lib/launchers.sh
source "$ROOT/lib/launchers.sh"

log_has() { grep -Fx "$1" "$LAUNCHER_TEST_LOG" >/dev/null; }

set +e
claude --model test-model
rc=$?
set -e
[[ $rc -eq 55 ]] || fail "on-bus launch did not return the hcom process status (rc=$rc)"
log_has "exe=$TEST_ROOT/hcom-bin/hcom" || fail "on-bus launch did not exec hcom"
head -n 6 "$LAUNCHER_TEST_LOG" | grep -Fx 'arg=claude' >/dev/null \
  || fail "hcom was not asked to launch claude"
log_has 'arg=--run-here' || fail "launch was not routed into the current pane (--run-here missing)"
log_has 'arg=--go' || fail "launch stopped at hcom's preview gate (--go missing)"
log_has 'arg=--dangerously-skip-permissions' || fail "launcher omitted the Claude default autonomy flag"
log_has 'arg=--model' && log_has 'arg=test-model' || fail "launcher lost caller arguments"
log_has "path_head=$PIN_DIR/claude" || fail "vendor pin dir does not front the child PATH"
[[ "$(readlink "$PIN_DIR/claude/claude")" == "$TEST_ROOT/vendor-bin/claude" ]] \
  || fail "pin symlink does not target the resolved vendor (imposters won)"
pass "hand-typed claude launches on-bus via hcom --run-here with the vendor pinned"

log_has 'env_process_id=UNSET' || fail "ambient HCOM_PROCESS_ID leaked into the launch (identity hijack)"
log_has 'env_herder_guid=UNSET' || fail "ambient HERDER_GUID leaked into the launch"
log_has 'env_herdr_pane=UNSET' || fail "ambient HERDR_PANE_ID leaked into the launch"
log_has "env_hcom_dir=$TEST_ROOT/busdir" || fail "HCOM_DIR (bus location) was not preserved"
log_has 'env_inflight=1' || fail "HCOM_LAUNCH_INFLIGHT guard not set"
[[ "${HCOM_PROCESS_ID-}" == "stale-caller-row" ]] \
  || fail "scrub mutated the interactive shell's own environment"
pass "ambient identity env is scrubbed in the child only; HCOM_DIR survives"

set +e
claude -p 'one shot'
rc=$?
set -e
[[ $rc -eq 23 ]] || fail "print one-shot did not return the vendor status (rc=$rc)"
log_has "exe=$TEST_ROOT/vendor-bin/claude" \
  || fail "claude -p was routed through hcom (task-010: the answer would never return)"
log_has 'arg=-p' || fail "print bypass lost the -p flag"
log_has 'env_process_id=UNSET' || fail "print bypass leaked ambient identity"
pass "claude -p bypasses hcom and execs the vendor directly"

set +e
HERDER_SHIM_ARGS_CODEX='' codex exec hello
rc=$?
set -e
[[ $rc -eq 55 ]] || fail "codex did not launch through hcom (rc=$rc)"
if grep -F -- '--dangerously-bypass-approvals-and-sandbox' "$LAUNCHER_TEST_LOG" >/dev/null; then
  fail "empty override did not disable default Codex args"
fi
head -n 6 "$LAUNCHER_TEST_LOG" | grep -Fx 'arg=codex' >/dev/null || fail "hcom was not asked to launch codex"
log_has 'arg=--run-here' || fail "codex launch missing --run-here"
log_has 'arg=exec' && log_has 'arg=hello' || fail "Codex override lost caller args"
pass "codex routes on-bus; empty launcher override preserves ask-mode behavior"

set +e
grok --model test
rc=$?
set -e
[[ $rc -eq 23 ]] || fail "grok did not exec the vendor directly (rc=$rc)"
log_has "exe=$TEST_ROOT/vendor-bin/grok" || fail "grok was routed through hcom"
pass "grok remains a direct vendor exec"

PATH="$TEST_ROOT/herder-bin:$TEST_ROOT/mise/shims:$TEST_ROOT/hcom-bin:/usr/bin:/bin"
set +e
claude >"$TEST_ROOT/missing.out" 2>"$TEST_ROOT/missing.err"
rc=$?
set -e
[[ $rc -eq 127 ]] || fail "missing vendor did not fail with rc 127"
grep -F "no vendor 'claude' on PATH" "$TEST_ROOT/missing.err" >/dev/null \
  || fail "missing vendor failure was not actionable"
pass "missing vendor fails loud without falling back"

PATH="$TEST_ROOT/vendor-bin:/usr/bin:/bin"
set +e
claude >"$TEST_ROOT/nohcom.out" 2>"$TEST_ROOT/nohcom.err"
rc=$?
set -e
[[ $rc -eq 127 ]] || fail "missing hcom did not fail with rc 127"
grep -F "hcom not on PATH" "$TEST_ROOT/nohcom.err" >/dev/null \
  || fail "missing hcom failure was not actionable"
grep -F "command claude" "$TEST_ROOT/nohcom.err" >/dev/null \
  || fail "missing hcom failure did not name the deliberate bypass"
pass "missing hcom fails loud instead of launching raw off-bus"

printf 'ALL GREEN - interactive launchers route on-bus through hcom --run-here.\n'
