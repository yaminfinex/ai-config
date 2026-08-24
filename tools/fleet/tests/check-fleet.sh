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

mkdir -p -- "$TEST_ROOT/bin"
cat >"$TEST_ROOT/bin/herdr" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'herdr' >>"$FLEET_TEST_CALLS"
printf ' %q' "$@" >>"$FLEET_TEST_CALLS"
printf '\n' >>"$FLEET_TEST_CALLS"
case "$1 $2" in
  'pane get')
    printf '%s\n' '{"result":{"pane":{"pane_id":"p-test","cwd":"/tmp"}}}'
    ;;
  'pane process-info')
    printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"bash"}]}}}'
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
case "${1:-} ${2:-}" in
  'events launch')
    printf '%s\n' '{"batch_id":"batch-test","blocked":0,"expected":1,"failed":0,"instances":["vava"],"ready":1,"status":"ready"}'
    ;;
  'list --json')
    printf '%s\n' '[{"base_name":"vava","name":"gate-vava"}]'
    ;;
esac
EOF
chmod +x "$TEST_ROOT/bin/herdr" "$TEST_ROOT/bin/hcom"

export FLEET_TEST_CALLS=$TEST_ROOT/calls
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test --prompt hello >"$TEST_ROOT/spawn.out"
grep -Fx 'name=gate-vava' "$TEST_ROOT/spawn.out" >/dev/null || fail "spawn did not print full hcom name"
grep -F 'FLEET_PANE=p-test HCOM_TERMINAL=fleet' "$FLEET_TEST_CALLS" >/dev/null || fail "spawn omitted fleet env contract"
grep -E 'hcom .* 1 codex .*--dir /tmp.*--hcom-prompt hello.*--sandbox danger-full-access.*--go' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "codex launch omitted a required flag"
pass "spawn pins placement, cwd, readiness, and Codex autonomy"

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

printf 'ALL GREEN - fleet wrapper contract holds.\n'
