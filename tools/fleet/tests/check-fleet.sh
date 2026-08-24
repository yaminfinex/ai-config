#!/usr/bin/env bash
# Hermetic contract checks for the fleet scripts. Live hcom/herdr round-trips
# remain the authoritative integration gate; this suite pins parsing, quoting,
# config preservation, and required launch flags.

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
FLEET=$ROOT/tools/fleet
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT

pass() {
  printf 'PASS  %s\n' "$1"
}

fail() {
  printf 'FAIL  %s\n' "$1" >&2
  exit 1
}

mkdir -p -- "$TEST_ROOT/hcom"
cat >"$TEST_ROOT/hcom/config.toml" <<'EOF'
get = "terminal"
[terminal]
active = "herdr"

[terminal.presets.fleet]
open = "stale"

[[unrelated.items]]
name = "keep-me"
EOF

HCOM_DIR="$TEST_ROOT/hcom" "$FLEET/preset-install.sh" >/dev/null
first_sum=$(sha256sum "$TEST_ROOT/hcom/config.toml" | awk '{print $1}')
HCOM_DIR="$TEST_ROOT/hcom" "$FLEET/preset-install.sh" >/dev/null
second_sum=$(sha256sum "$TEST_ROOT/hcom/config.toml" | awk '{print $1}')
[[ $first_sum == "$second_sum" ]] || fail "preset install is not idempotent"
grep -Fx 'active = "herdr"' "$TEST_ROOT/hcom/config.toml" >/dev/null || fail "preset changed active terminal"
grep -Fx '[[unrelated.items]]' "$TEST_ROOT/hcom/config.toml" >/dev/null || fail "preset removed array table"
grep -Fx 'name = "keep-me"' "$TEST_ROOT/hcom/config.toml" >/dev/null || fail "preset removed unrelated config"
pass "preset is idempotent and preserves unrelated TOML"

backslash_fleet=$TEST_ROOT/'fleet\path'
mkdir -p -- "$backslash_fleet" "$TEST_ROOT/backslash-hcom"
cp -- "$FLEET/preset-install.sh" "$FLEET/spawn-pane.sh" "$backslash_fleet/"
HCOM_DIR="$TEST_ROOT/backslash-hcom" "$backslash_fleet/preset-install.sh" >/dev/null
grep -F 'fleet\\path' "$TEST_ROOT/backslash-hcom/config.toml" >/dev/null \
  || fail "preset did not TOML-escape a backslash in the helper path"
pass "preset TOML-escapes backslashes in helper paths"

mkdir -p -- "$TEST_ROOT/bin"
cat >"$TEST_ROOT/bin/herdr" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'herdr' >>"$FLEET_TEST_CALLS"
printf ' %q' "$@" >>"$FLEET_TEST_CALLS"
printf '\n' >>"$FLEET_TEST_CALLS"
case "$1 $2" in
  'tab create')
    printf '%s\n' '{"result":{"tab":{"tab_id":"tab-left-behind"}}}'
    ;;
  'pane get')
    printf '%s\n' '{"result":{"pane":{"pane_id":"p-test","cwd":"/tmp"}}}'
    ;;
  'pane process-info')
    if [[ ${FLEET_TEST_PROCESS_SHAPE:-} == no-shell-pid ]]; then
      printf '%s\n' '{"result":{"process_info":{"foreground_processes":[{"pid":42,"name":"bash"}]}}}'
    else
      printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"bash"}]}}}'
    fi
    ;;
esac
EOF
cat >"$TEST_ROOT/bin/hcom" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'hcom FLEET_PANE=%q HCOM_TERMINAL=%q' "${FLEET_PANE:-}" "${HCOM_TERMINAL:-}" >>"$FLEET_TEST_CALLS"
printf ' %q' "$@" >>"$FLEET_TEST_CALLS"
printf '\n' >>"$FLEET_TEST_CALLS"
if [[ ${1:-} == 1 ]]; then
  printf '%s\n' 'Started the launch process' 'Names: vava' 'Batch id: batch-test'
  exit 2
fi
if [[ ${1:-} == list && ${3:-} == status ]]; then
  case ${FLEET_TEST_STATUS_MODE:-} in
    transient)
      count=$(<"$FLEET_TEST_STATUS_COUNT")
      count=$((count + 1))
      printf '%s\n' "$count" >"$FLEET_TEST_STATUS_COUNT"
      case $count in
        1) exit 1 ;;
        2) printf '%s\n' active ;;
        *) printf '%s\n' listening ;;
      esac
      ;;
    terminal) printf '%s\n' inactive ;;
    timeout) printf '%s\n' listening ;;
  esac
  exit 0
fi
case "${1:-} ${2:-}" in
  'events launch')
    printf '%s\n' '{"batch_id":"batch-test","blocked":0,"expected":1,"failed":0,"instances":["vava"],"ready":1,"status":"ready"}'
    ;;
  'list --json')
    hooks_bound=${FLEET_TEST_HOOKS_BOUND:-1}
    if [[ $hooks_bound == delayed ]]; then
      count=$(<"$FLEET_TEST_HOOKS_COUNT")
      count=$((count + 1))
      printf '%s\n' "$count" >"$FLEET_TEST_HOOKS_COUNT"
      ((count >= 2)) && hooks_bound=1 || hooks_bound=0
    fi
    if [[ $hooks_bound == 1 ]]; then
      printf '%s\n' '[{"base_name":"vava","hooks_bound":true,"name":"gate-vava"}]'
    else
      printf '%s\n' '[{"base_name":"vava","hooks_bound":false,"name":"gate-vava"}]'
    fi
    ;;
esac
EOF
cat >"$TEST_ROOT/bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_ROOT/bin/herdr" "$TEST_ROOT/bin/hcom" "$TEST_ROOT/bin/sleep"

export FLEET_TEST_CALLS=$TEST_ROOT/calls
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test --prompt hello >"$TEST_ROOT/spawn.out"
grep -Fx 'name=gate-vava' "$TEST_ROOT/spawn.out" >/dev/null || fail "spawn did not print full hcom name"
grep -F 'FLEET_PANE=p-test HCOM_TERMINAL=fleet' "$FLEET_TEST_CALLS" >/dev/null || fail "spawn omitted fleet env contract"
grep -E 'hcom .* 1 codex .*--dir /tmp.*--hcom-prompt hello.*--sandbox danger-full-access.*--go' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "codex launch omitted a required flag"
pass "spawn pins placement, cwd, readiness, and Codex autonomy"

printf '0\n' >"$TEST_ROOT/hooks-count"
FLEET_TEST_HOOKS_BOUND=delayed FLEET_TEST_HOOKS_COUNT="$TEST_ROOT/hooks-count" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/delayed-hooks.out"
grep -Fx 'name=gate-vava' "$TEST_ROOT/delayed-hooks.out" >/dev/null \
  || fail "spawn did not tolerate delayed hook binding after readiness"
pass "spawn waits briefly for hook binding after readiness"

if FLEET_TEST_HOOKS_BOUND=0 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/unbound.out" 2>"$TEST_ROOT/unbound.err"; then
  fail "spawn accepted a ready launch without bound hooks"
fi
grep -F 'ready launch is not hook-bound in hcom roster: gate-vava' "$TEST_ROOT/unbound.err" >/dev/null \
  || fail "spawn did not explain the unbound ready launch"
grep -F 'pane=p-test' "$TEST_ROOT/unbound.err" >/dev/null \
  || fail "spawn did not name the placement left by an unbound ready launch"
pass "spawn requires hook binding after launch readiness"

if FLEET_TEST_PROCESS_SHAPE=no-shell-pid PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/no-shell.out" 2>"$TEST_ROOT/no-shell.err"; then
  fail "spawn accepted an existing pane without a verifiable shell pid"
fi
grep -F 'cannot verify idle shell because process info omitted shell_pid' "$TEST_ROOT/no-shell.err" >/dev/null \
  || fail "spawn did not explain the missing shell_pid wire shape"
pass "spawn refuses the shell_pid-absent process-info shape honestly"

if PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --workspace w-test >"$TEST_ROOT/tab.out" 2>"$TEST_ROOT/tab.err"; then
  fail "spawn accepted a tab-create result without a root pane"
fi
grep -F 'workspace=w-test tab=tab-left-behind left for explicit cleanup' "$TEST_ROOT/tab.err" >/dev/null \
  || fail "spawn did not name the tab left behind by post-creation failure"
pass "spawn names a created tab on post-placement failure"

launch_dir=$TEST_ROOT/'path with spaces'
mkdir -p -- "$launch_dir"
launch_script=$launch_dir/launch.sh
printf '#!/usr/bin/env bash\n' >"$launch_script"
: >"$FLEET_TEST_CALLS"
first_line=$(PATH="$TEST_ROOT/bin:$PATH" FLEET_PANE=p-test "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]')
[[ $first_line == p-test ]] || fail "open helper did not print pane id first"
printf -v launch_q '%q' "$launch_script"
printf -v run_q '%q' "bash $launch_q"
grep -F "herdr pane run p-test $run_q" "$FLEET_TEST_CALLS" >/dev/null || fail "open helper lost script-path quoting"
pass "open helper preserves first-line id and script quoting"

if "$FLEET/selfcompact.sh" '../wrong' steer continue >/dev/null 2>&1; then
  fail "selfcompact accepted an unsafe hcom name"
fi
pass "selfcompact rejects unsafe log-name input"

: >"$FLEET_TEST_CALLS"
printf '0\n' >"$TEST_ROOT/status-count"
FLEET_TEST_STATUS_MODE=transient FLEET_TEST_STATUS_COUNT="$TEST_ROOT/status-count" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/selfcompact.sh" --run vava steer continue
[[ $(grep -c 'term inject vava' "$FLEET_TEST_CALLS") -eq 2 ]] \
  || fail "selfcompact did not survive one empty status read and inject continuation"
pass "selfcompact tolerates a transient empty status read"

: >"$FLEET_TEST_CALLS"
if FLEET_TEST_STATUS_MODE=terminal PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/selfcompact.sh" --run vava steer continue >/dev/null 2>&1; then
  fail "selfcompact accepted a terminal agent status"
fi
grep -F 'term inject vava continue --enter' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "selfcompact did not best-effort inject continuation on terminal status"
pass "selfcompact injects continuation before terminal-status exit"

: >"$FLEET_TEST_CALLS"
if FLEET_TEST_STATUS_MODE=timeout PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/selfcompact.sh" --run vava steer continue >/dev/null 2>&1; then
  fail "selfcompact accepted a latch timeout"
fi
grep -F 'term inject vava continue --enter' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "selfcompact did not best-effort inject continuation on timeout"
pass "selfcompact injects continuation before timeout exit"

printf 'ALL GREEN - fleet wrapper contract holds.\n'
