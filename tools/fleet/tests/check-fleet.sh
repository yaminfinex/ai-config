#!/usr/bin/env bash
# Hermetic contract checks for the fleet scripts. Live hcom/herdr round-trips
# remain the authoritative integration gate; this suite pins parsing, quoting,
# config preservation, and required launch flags.

set -euo pipefail

unset HCOM_PROCESS_ID

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
if [[ -n ${FLEET_TEST_CULL_MODE:-} ]]; then
  # The p-seat modes keep the seat pane p-seat after hcom kill with its label
  # gone; idle and emptytab leave a bare shell there, busy a dev server.
  # emptytab models a herdr that leaves the emptied tab behind. moved puts the
  # exact label on p-moved after kill, foreign gives p-seat another agent's
  # label, claimed has another hcom seat launched into p-seat, malformed and
  # zeropid return process info that cannot prove an idle shell, and flip
  # turns busy on the second process read. fallback-busy is fallback with a
  # dev server in the labelled pane.
  case "${1:-} ${2:-}" in
    'pane list')
      case "$FLEET_TEST_CULL_MODE" in
        managed)
          # FLEET_TEST_OTHER_PANE_DIR adds an unlabelled pane sitting in that dir.
          jq -c --arg other "${FLEET_TEST_OTHER_PANE_DIR:-}" '
            if $other == "" then . else .result.panes += [{"pane_id":"p-shell","tab_id":"t-shell","label":null,"cwd":$other}] end
          ' <<<'{"result":{"panes":[{"pane_id":"p-managed","tab_id":"t-seat","label":"◉ gate-vava [codex]"}]}}'
          ;;
        fallback | fallback-busy) printf '%s\n' '{"result":{"panes":[{"pane_id":"p-fallback","tab_id":"t-seat","label":"▶ gate-vava [codex]"}]}}' ;;
        ambiguous) printf '%s\n' '{"result":{"panes":[{"pane_id":"p-one","label":"◉ gate-vava [codex]"},{"pane_id":"p-two","label":"○ gate-vava [codex]"}]}}' ;;
        *)
          if [[ -e $FLEET_TEST_CULL_STATE/killed && $FLEET_TEST_CULL_MODE == moved ]]; then
            printf '%s\n' '{"result":{"panes":[{"pane_id":"p-seat","tab_id":"t-seat","label":null},{"pane_id":"p-moved","tab_id":"t-moved","label":"◉ gate-vava [codex]"}]}}'
          elif [[ -e $FLEET_TEST_CULL_STATE/killed ]]; then
            printf '%s\n' '{"result":{"panes":[{"pane_id":"p-seat","tab_id":"t-seat","label":null},{"pane_id":"p-other","tab_id":"t-other","label":"◉ gate-kemo [claude]"}]}}'
          else
            printf '%s\n' '{"result":{"panes":[{"pane_id":"p-seat","tab_id":"t-seat","label":"◉ gate-vava [codex]"},{"pane_id":"p-other","tab_id":"t-other","label":"◉ gate-kemo [claude]"}]}}'
          fi
          ;;
      esac
      ;;
    'pane get')
      if [[ $FLEET_TEST_CULL_MODE == managed && -e $FLEET_TEST_CULL_STATE/killed ]] || \
         [[ $FLEET_TEST_CULL_MODE != managed && -e $FLEET_TEST_CULL_STATE/closed ]]; then
        exit 1
      fi
      label=null
      case $FLEET_TEST_CULL_MODE in
        fallback | fallback-busy) label='"▶ gate-vava [codex]"' ;;
        foreign) label='"◉ gate-kemo [claude]"' ;;
      esac
      printf '%s\n' '{"result":{"pane":{"pane_id":"'"${3:-}"'","tab_id":"t-seat","label":'"$label"',"cwd":"/srv/seat","foreground_cwd":"/srv/seat/app"}}}'
      ;;
    'pane process-info')
      reads=0
      [[ ! -e $FLEET_TEST_CULL_STATE/reads ]] || reads=$(<"$FLEET_TEST_CULL_STATE/reads")
      reads=$((reads + 1)); printf '%s' "$reads" >"$FLEET_TEST_CULL_STATE/reads"
      shape=idle
      case $FLEET_TEST_CULL_MODE in
        busy | fallback-busy) shape=busy ;;
        malformed) shape=malformed ;;
        zeropid) shape=zeropid ;;
        flip) ((reads < 2)) || shape=busy ;;
      esac
      case $shape in
        busy) printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":77,"name":"node"}]}}}' ;;
        malformed) printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":false}}}' ;;
        zeropid) printf '%s\n' '{"result":{"process_info":{"shell_pid":0,"foreground_processes":[{"pid":0,"name":"zsh"}]}}}' ;;
        *) printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"zsh"}]}}}' ;;
      esac
      ;;
    'pane close')
      : >"$FLEET_TEST_CULL_STATE/closed"
      ;;
    'tab get')
      if [[ $FLEET_TEST_CULL_MODE == emptytab && -e $FLEET_TEST_CULL_STATE/closed && ! -e $FLEET_TEST_CULL_STATE/tab-closed ]]; then
        printf '%s\n' '{"result":{"tab":{"tab_id":"'"${3:-}"'","pane_count":0}}}'
      else
        exit 1
      fi
      ;;
    'tab close')
      : >"$FLEET_TEST_CULL_STATE/tab-closed"
      ;;
  esac
  exit 0
fi
case "$1 $2" in
  'tab create')
    if [[ ${3:-} == --workspace && ${4:-} == w-wt ]]; then
      [[ ${FLEET_TEST_TAB_CREATE:-} != fail ]] || exit 1
      printf '%s\n' '{"result":{"tab":{"tab_id":"t-wt-fresh"},"root_pane":{"pane_id":"p-wt-fresh","cwd":"/tmp"}}}'
    elif [[ ${FLEET_TEST_DEFAULT_PLACEMENT:-} == 1 ]]; then
      printf '%s\n' '{"result":{"tab":{"tab_id":"tab-default"},"root_pane":{"pane_id":"p-default","cwd":"/tmp"}}}'
    else
      printf '%s\n' '{"result":{"tab":{"tab_id":"tab-left-behind"}}}'
    fi
    ;;
  'pane get')
    if [[ ${FLEET_TEST_UNKNOWN_SPLIT:-} == 1 && ${3:-} == p-source ]]; then
      exit 1
    fi
    # FLEET_TEST_ROOT_TAB=unknown hides the worktree root pane's tab.
    tab='"t-'"${3:-p-test}"'"'
    [[ ${FLEET_TEST_ROOT_TAB:-} != unknown || ${3:-} != p-wt ]] || tab=null
    printf '%s\n' '{"result":{"pane":{"pane_id":"'"${3:-p-test}"'","tab_id":'"$tab"',"workspace_id":"w-test","cwd":"'"${FLEET_TEST_PANE_CWD:-/tmp}"'"}}}'
    ;;
  'pane list')
    # Each known pane sits alone in tab t-<pane>, except FLEET_TEST_TAB_SHARED,
    # whose tab also holds p-neighbour.
    panes=
    for listed in p-test p-wt p-self p-split; do
      panes="$panes{\"pane_id\":\"$listed\",\"tab_id\":\"t-$listed\"},"
      [[ $listed != "${FLEET_TEST_TAB_SHARED:-}" ]] || panes="$panes{\"pane_id\":\"p-neighbour\",\"tab_id\":\"t-$listed\"},"
    done
    printf '{"result":{"panes":[%s]}}\n' "${panes%,}"
    ;;
  'worktree list')
    printf '%s\n' '{"result":{"source":{"source_checkout_path":"/tmp"}}}'
    ;;
  'worktree create')
    printf '%s\n' '{"result":{"workspace":{"workspace_id":"w-wt","worktree":{"checkout_path":"/tmp"}},"root_pane":{"pane_id":"p-wt","cwd":"/tmp"}}}'
    ;;
  'pane split')
    printf '%s\n' '{"result":{"pane":{"pane_id":"p-split","cwd":"/tmp"}}}'
    ;;
  'pane current')
    [[ ${FLEET_TEST_UNKNOWN_CURRENT:-} != 1 ]] || exit 1
    printf '%s\n' '{"result":{"pane":{"pane_id":"p-self","workspace_id":"w-current","cwd":"/tmp"}}}'
    ;;
  'pane process-info')
    # FLEET_TEST_ROOT_SEQ scripts the worktree root pane's reads in order
    # (busy, unknown, idle, respawned), then FLEET_TEST_ROOT_AFTER for the rest.
    if [[ -n ${FLEET_TEST_ROOT_SEQ:-} && ${4:-} == p-wt ]]; then
      read -ra root_seq <<<"$FLEET_TEST_ROOT_SEQ"
      root_read=$(($(cat "$FLEET_TEST_ROOT_COUNT" 2>/dev/null || printf 0) + 1))
      printf '%s\n' "$root_read" >"$FLEET_TEST_ROOT_COUNT"
      root_state=${root_seq[root_read - 1]:-${FLEET_TEST_ROOT_AFTER:-idle}}
      case $root_state in
        busy) printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"bash"},{"pid":43,"name":"mise"}]}}}' ;;
        unknown) printf '%s\n' '{"result":{"process_info":{"foreground_processes":[]}}}' ;;
        respawned) printf '%s\n' '{"result":{"process_info":{"shell_pid":44,"foreground_processes":[{"pid":44,"name":"bash"}]}}}' ;;
        *) printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"bash"}]}}}' ;;
      esac
    elif [[ ${FLEET_TEST_PROCESS_SHAPE:-} == no-shell-pid ]]; then
      printf '%s\n' '{"result":{"process_info":{"foreground_processes":[{"pid":42,"name":"bash"}]}}}'
    elif [[ ${FLEET_TEST_PROCESS_SHAPE:-} == malformed ]]; then
      printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":false}}}'
    else
      printf '%s\n' '{"result":{"process_info":{"shell_pid":42,"foreground_processes":[{"pid":42,"name":"bash"}]}}}'
    fi
    ;;
esac
EOF
cat >"$TEST_ROOT/bin/hcom" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'hcom FLEET_PANE=%q FLEET_TOOL=%q HCOM_TERMINAL=%q HCOM_NOTES_SET=%q PERSIST=%q' \
  "${FLEET_PANE:-}" "${FLEET_TOOL:-}" "${HCOM_TERMINAL:-}" "${HCOM_NOTES+x}" \
  "${CLAUDE_CODE_FORCE_SESSION_PERSISTENCE:-}" >>"$FLEET_TEST_CALLS"
printf ' %q' "$@" >>"$FLEET_TEST_CALLS"
printf '\n' >>"$FLEET_TEST_CALLS"
if [[ ${1:-} == list && ${2:-} == self && ${3:-} == --json ]]; then
  case ${FLEET_TEST_SELF_MODE:-} in
    name) printf '%s\n' '{"name":"ziru"}' ;;
    fail) exit 1 ;;
    forbid) exit 99 ;;
    *) exit 64 ;;
  esac
  exit 0
fi
if [[ -n ${FLEET_TEST_CULL_MODE:-} ]]; then
  case "${1:-} ${2:-}" in
    'list --json')
      if [[ $FLEET_TEST_CULL_MODE == managed ]]; then
        roster='[{"name":"gate-vava","base_name":"vava","tool":"codex","launch_context":{"pane_id":"p-managed"}}]'
      elif [[ $FLEET_TEST_CULL_MODE == claimed ]]; then
        roster='[{"name":"gate-vava","base_name":"vava","tool":"codex","launch_context":{}},{"name":"gate-kemo","base_name":"kemo","tool":"claude","launch_context":{"pane_id":"p-seat"}}]'
      else
        roster='[{"name":"gate-vava","base_name":"vava","tool":"codex","launch_context":{}}]'
      fi
      # FLEET_TEST_SEAT_DIR gives gate-vava a directory; FLEET_TEST_OTHER_SEAT_DIR
      # adds a second live seat there.
      jq -c --arg dir "${FLEET_TEST_SEAT_DIR:-}" --arg other "${FLEET_TEST_OTHER_SEAT_DIR:-}" '
        map(if .name == "gate-vava" and $dir != "" then .directory = $dir else . end)
        + (if $other == "" then [] else [{"name":"gate-mura","base_name":"mura","tool":"claude","directory":$other,"launch_context":{}}] end)
      ' <<<"$roster"
      ;;
    'send @gate-vava') ;;
    'kill gate-vava')
      : >"$FLEET_TEST_CULL_STATE/killed"
      [[ $FLEET_TEST_CULL_MODE == managed ]] || exit 1
      ;;
    *) exit 64 ;;
  esac
  exit 0
fi
if [[ ${1:-} == 1 ]]; then
  printf '%s\n' 'Started the launch process' 'Names: vava' 'Batch id: batch-test'
  case ${FLEET_TEST_LAUNCH_MODE:-} in
    descendant-stdout) /bin/sleep 3 & ;;
    blocking) /bin/sleep 5 ;;
  esac
  exit 2
fi
if [[ -n ${FLEET_TEST_COMPACT_MODE:-} ]]; then
  # compact.sh fixture: the seat is listening with a quiet composer until the
  # /compact is submitted, then goes active twice and listens again. The
  # status line carries the context figure that must drop.
  injected=0
  grep -q 'term inject vava --enter' "$FLEET_TEST_CALLS" && injected=1
  screen() { printf '{"ready":true,"prompt_empty":%s,"input_text":"%s","lines":["› ","","  %s"]}\n' "$1" "$2" "$3"; }
  case "${1:-} ${2:-} ${3:-}" in
    'list vava status')
      if ((injected == 0)); then
        printf 'listening\n'
      else
        n=$(<"$FLEET_TEST_COMPACT_STATE/status"); n=$((n + 1)); printf '%s' "$n" >"$FLEET_TEST_COMPACT_STATE/status"
        if ((n <= 2)); then printf 'active\n'; else printf 'listening\n'; fi
      fi
      ;;
    'term vava --json')
      case $FLEET_TEST_COMPACT_MODE in
        busy) screen false 'half-typed message' '120k / 200k' ;;
        happy) if ((injected)); then screen true '' '9k / 200k'; else screen true '' '120k / 200k'; fi ;;
        codex) if ((injected)); then screen true '' 'gpt-5.6-sol low · Context 88% left · unit'; else screen true '' 'gpt-5.6-sol low · Context 31% left · unit'; fi ;;
        nodrop) screen true '' '120k / 200k' ;;
        *) exit 64 ;;
      esac
      ;;
    'term inject vava') ;;
    'send @vava --intent') ;;
    *) exit 64 ;;
  esac
  exit 0
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
      printf '%s\n' '[{"base_name":"vava","hooks_bound":true,"name":"gate-vava","session_id":"session-test"}]'
    else
      printf '%s\n' '[{"base_name":"vava","hooks_bound":false,"name":"gate-vava","session_id":"session-test"}]'
    fi
    ;;
esac
EOF
cat >"$TEST_ROOT/bin/herder" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'herder FLEET_LAUNCHER=%q FLEET_LAUNCHER_KIND=%q' "${FLEET_LAUNCHER:-}" "${FLEET_LAUNCHER_KIND:-}" >>"$FLEET_TEST_CALLS"
printf ' %q' "$@" >>"$FLEET_TEST_CALLS"
printf '\n' >>"$FLEET_TEST_CALLS"
case ${FLEET_TEST_REGISTER_MODE:-} in
  exit3) printf 'store unavailable\n' >&2; exit 3 ;;
  sleep) exec /bin/sleep 20 ;;
esac
if [[ ${1:-} == show && ${3:-} == --json ]]; then
  if [[ ${FLEET_TEST_SHOW_MODE:-} == fail ]]; then
    printf 'store unavailable\n' >&2
    exit 3
  fi
  printf '%s\n' '{"name":"'"${2:-}"'","assignment":{"group":"'"${FLEET_TEST_PARENT_GROUP:-}"'"}}'
  exit 0
fi
if [[ ${1:-} == register && ${2:-} == launch-requested ]]; then
  printf '%s\n' 'id=018f0000-0000-7000-8000-000000000001' 'request=018f0000-0000-7000-8000-000000000001'
else
  printf '%s\n' 'id=018f0000-0000-7000-8000-000000000002'
fi
EOF
cat >"$TEST_ROOT/bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_ROOT/bin/herdr" "$TEST_ROOT/bin/hcom" "$TEST_ROOT/bin/herder" "$TEST_ROOT/bin/sleep"

# A fleet seat running this battery exports its own launch context.
unset FLEET_PANE FLEET_TOOL FLEET_LAUNCHER
export FLEET_TEST_CALLS=$TEST_ROOT/calls
export FLEET_TEST_ROOT_COUNT=$TEST_ROOT/root-reads
# Root-pane settle reads need no real delay against the fake herdr.
export FLEET_ROOT_SETTLE_INTERVAL=0
HCOM_NOTES=stale PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test --prompt hello >"$TEST_ROOT/spawn.out"
grep -Fx 'name=gate-vava' "$TEST_ROOT/spawn.out" >/dev/null || fail "spawn did not print full hcom name"
grep -F 'FLEET_PANE=p-test FLEET_TOOL=codex HCOM_TERMINAL=fleet' "$FLEET_TEST_CALLS" >/dev/null || fail "spawn omitted fleet tool env contract"
grep -F "HCOM_NOTES_SET=''" "$FLEET_TEST_CALLS" >/dev/null \
  || fail "spawn passed inherited HCOM_NOTES to hcom"
grep -E 'hcom .* 1 codex .*--dir /tmp.*--hcom-prompt hello.*--dangerously-bypass-approvals-and-sandbox.*--no-run-here.*--go' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "codex launch omitted a required flag"
if grep -F 'model_reasoning_effort' "$FLEET_TEST_CALLS" >/dev/null || grep -F -- '--effort' "$FLEET_TEST_CALLS" >/dev/null; then
  fail "spawn added reasoning effort when none was requested"
fi
requested_line=$(grep -n 'herder .*register launch-requested' "$FLEET_TEST_CALLS" | head -n1 | cut -d: -f1)
launch_line=$(grep -n 'hcom .* 1 codex' "$FLEET_TEST_CALLS" | head -n1 | cut -d: -f1)
ready_line=$(grep -n 'herder .*register launch-ready' "$FLEET_TEST_CALLS" | head -n1 | cut -d: -f1)
[[ $requested_line -lt $launch_line && $ready_line -gt $launch_line ]] || fail "spawn registration calls are misordered"
grep -F 'register launch-ready --request 018f0000-0000-7000-8000-000000000001 --name gate-vava --batch batch-test --pane p-test --cwd /tmp --session session-test' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "spawn ready registration lost request or launch facts"
pass "spawn pins placement, cwd, readiness, and Codex autonomy"

: >"$FLEET_TEST_CALLS"
env -u FLEET_LAUNCHER -u FLEET_LAUNCHER_KIND HCOM_PROCESS_ID=seat-test \
  FLEET_TEST_SELF_MODE=name PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test --group '  builders  ' \
  --title '  payload builder  ' >"$TEST_ROOT/spawn-group-title.out"
grep -Fx 'herder FLEET_LAUNCHER='"''"' FLEET_LAUNCHER_KIND='"''"' assign gate-vava --group builders --by ziru --by-kind agent' \
  "$FLEET_TEST_CALLS" >/dev/null || fail "spawn did not write the exact explicit group assignment"
grep -Fx 'herder FLEET_LAUNCHER='"''"' FLEET_LAUNCHER_KIND='"''"' register annotate --name gate-vava --title payload\ builder --by ziru --by-kind agent' \
  "$FLEET_TEST_CALLS" >/dev/null || fail "spawn did not write the exact title annotation"
requested_line=$(grep -n 'herder .*register launch-requested' "$FLEET_TEST_CALLS" | cut -d: -f1)
launch_line=$(grep -n 'hcom .* 1 codex' "$FLEET_TEST_CALLS" | cut -d: -f1)
assign_line=$(grep -n 'herder .* assign gate-vava --group builders' "$FLEET_TEST_CALLS" | cut -d: -f1)
annotate_line=$(grep -n 'herder .* register annotate --name gate-vava' "$FLEET_TEST_CALLS" | cut -d: -f1)
ready_line=$(grep -n 'herder .*register launch-ready' "$FLEET_TEST_CALLS" | cut -d: -f1)
[[ $requested_line -lt $launch_line && $launch_line -lt $assign_line && $assign_line -lt $annotate_line && $annotate_line -lt $ready_line ]] \
  || fail "spawn group/title events are misordered"
pass "spawn writes explicit group and title before launch-ready"

: >"$FLEET_TEST_CALLS"
HCOM_PROCESS_ID=seat-test HCOM_TAG=impl FLEET_TEST_SELF_MODE=name FLEET_TEST_PARENT_GROUP=builders \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/spawn-inherited-group.out"
grep -Fx 'herder FLEET_LAUNCHER='"''"' FLEET_LAUNCHER_KIND='"''"' assign gate-vava --group builders --by ziru --by-kind agent' \
  "$FLEET_TEST_CALLS" >/dev/null || fail "spawn did not write the exact inherited group assignment"
[[ $(grep -c 'herder .* show impl-ziru --json' "$FLEET_TEST_CALLS") -eq 1 ]] \
  || fail "spawn did not query its own group exactly once"
pass "spawn inherits its launching agent's group once"

: >"$FLEET_TEST_CALLS"
HCOM_PROCESS_ID=seat-test HCOM_TAG=impl FLEET_TEST_SELF_MODE=name FLEET_TEST_PARENT_GROUP=builders \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test --group '' \
  >"$TEST_ROOT/spawn-empty-group.out"
! grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' assign gate-vava --group ' >/dev/null \
  || fail "explicit empty group wrote an assignment"
! grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' show impl-ziru --json' >/dev/null \
  || fail "explicit empty group queried inheritance"
pass "explicit empty group suppresses assignment and inheritance"

: >"$FLEET_TEST_CALLS"
env -u HCOM_PROCESS_ID -u FLEET_LAUNCHER -u FLEET_LAUNCHER_KIND HCOM_TAG=impl \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/spawn-human-group.out"
! grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' assign gate-vava --group ' >/dev/null \
  || fail "human launch wrote a group assignment"
! grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' show ' >/dev/null \
  || fail "human launch queried group inheritance"
pass "human launch inherits and writes no group"

: >"$FLEET_TEST_CALLS"
env -u HCOM_TAG HCOM_PROCESS_ID=seat-test FLEET_TEST_SELF_MODE=name \
  FLEET_TEST_PARENT_GROUP=builders \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/spawn-no-self-tag.out" 2>"$TEST_ROOT/spawn-no-self-tag.err"
[[ $(grep -c 'herder .* show ziru --json' "$FLEET_TEST_CALLS") -eq 1 ]] \
  || fail "untagged launcher did not query its own group exactly once"
grep -Fx 'herder FLEET_LAUNCHER='"''"' FLEET_LAUNCHER_KIND='"''"' assign gate-vava --group builders --by ziru --by-kind agent' \
  "$FLEET_TEST_CALLS" >/dev/null || fail "untagged launcher did not inherit its group"
pass "untagged launcher inherits its group"

: >"$FLEET_TEST_CALLS"
HCOM_PROCESS_ID=seat-test HCOM_TAG=impl FLEET_TEST_SELF_MODE=name FLEET_TEST_SHOW_MODE=fail \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/spawn-inherit-fail.out" 2>"$TEST_ROOT/spawn-inherit-fail.err"
[[ $(grep -c 'fleet spawn: group inheritance skipped: store unavailable' "$TEST_ROOT/spawn-inherit-fail.err") -eq 1 ]] \
  || fail "failed inheritance lookup did not warn exactly once"
! grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' assign gate-vava --group ' >/dev/null \
  || fail "failed inheritance lookup wrote a group"
pass "failed group inheritance is warned and fail-open"

for validation_case in group-long group-control title-empty title-long title-control; do
  : >"$FLEET_TEST_CALLS"
  validation_args=()
  case $validation_case in
    group-long) validation_args=(--group "$(printf 'g%.0s' {1..81})") ;;
    group-control) validation_args=(--group $'bad\tgroup') ;;
    title-empty) validation_args=(--title '   ') ;;
    title-long) validation_args=(--title "$(printf 't%.0s' {1..81})") ;;
    title-control) validation_args=(--title $'bad\ntitle') ;;
  esac
  set +e
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
    "${validation_args[@]}" >"$TEST_ROOT/validation-$validation_case.out" \
    2>"$TEST_ROOT/validation-$validation_case.err"
  validation_rc=$?
  set -e
  [[ $validation_rc -eq 2 ]] || fail "$validation_case validation did not exit 2"
  ! grep -F 'hcom ' "$FLEET_TEST_CALLS" | grep -F ' 1 codex' >/dev/null \
    || fail "$validation_case validation launched an agent"
done
pass "group and title validation refuse before launch"

: >"$FLEET_TEST_CALLS"
HERDR_WORKSPACE_ID=w-current FLEET_TEST_DEFAULT_PLACEMENT=1 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate >"$TEST_ROOT/spawn-default-placement.out"
grep -Fx 'herdr tab create --workspace w-current --no-focus' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "default placement did not create a tab in the caller workspace"
grep -Fx 'placement=tab' "$TEST_ROOT/spawn-default-placement.out" >/dev/null \
  || fail "default placement did not report a tab"
! grep -F 'herdr pane current' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "default placement consulted the focused pane"
pass "spawn defaults to a new tab in the caller workspace"

: >"$FLEET_TEST_CALLS"
set +e
env -u HERDR_WORKSPACE_ID PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate >"$TEST_ROOT/spawn-no-current.out" \
  2>"$TEST_ROOT/spawn-no-current.err"
no_current_rc=$?
set -e
[[ $no_current_rc -eq 2 ]] || fail "missing caller workspace did not exit 2"
grep -Fx 'fleet spawn: no placement flag and no current herdr pane; pass --workspace, --worktree-branch or --pane' \
  "$TEST_ROOT/spawn-no-current.err" >/dev/null || fail "missing caller workspace refusal was unclear"
! grep -F 'hcom ' "$FLEET_TEST_CALLS" | grep -F ' 1 codex' >/dev/null \
  || fail "missing caller workspace launched an agent"
pass "spawn refuses no placement outside a herdr workspace"

: >"$FLEET_TEST_CALLS"
env -u HCOM_NAME HCOM_TAG=impl HCOM_INSTANCE_NAME=fimu HCOM_PROCESS_ID=seat-test \
  FLEET_TEST_SELF_MODE=forbid FLEET_LAUNCHER=web-x FLEET_LAUNCHER_KIND=web \
  PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/spawn-web.out" 2>"$TEST_ROOT/spawn-web.err"
[[ $(grep 'herder .*register launch-' "$FLEET_TEST_CALLS" \
  | grep -c -- '--launcher-kind web .*--by web-x --by-kind web') -eq 2 ]] \
  || fail "spawn did not attribute both web registration calls"
! grep -F 'hcom ' "$FLEET_TEST_CALLS" | grep -F ' list self --json' >/dev/null \
  || fail "spawn queried hcom self despite serve attribution"
: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/spawn-direct.out" 2>"$TEST_ROOT/spawn-direct.err"
! grep -F -- '--by' "$FLEET_TEST_CALLS" >/dev/null || fail "spawn forced attribution on a direct launch"
for partial_attrib in launcher kind; do
  : >"$FLEET_TEST_CALLS"
  if [[ $partial_attrib == launcher ]]; then
    FLEET_LAUNCHER=web-x PATH="$TEST_ROOT/bin:$PATH" \
      "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/spawn-$partial_attrib.out" 2>"$TEST_ROOT/spawn-$partial_attrib.err"
  else
    FLEET_LAUNCHER_KIND=web PATH="$TEST_ROOT/bin:$PATH" \
      "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/spawn-$partial_attrib.out" 2>"$TEST_ROOT/spawn-$partial_attrib.err"
  fi
  ! grep -F -- '--by' "$FLEET_TEST_CALLS" >/dev/null \
    || fail "spawn used partial $partial_attrib attribution"
done
pass "spawn carries optional web attribution only when supplied"

for mode in exit3 absent sleep; do
  : >"$FLEET_TEST_CALLS"
  start=$SECONDS
  test_path="$TEST_ROOT/bin:$PATH"
  register_args=()
  if [[ $mode == absent ]]; then
    mv "$TEST_ROOT/bin/herder" "$TEST_ROOT/bin/herder.off"
    test_path="$TEST_ROOT/bin:/usr/bin:/bin"
  elif [[ $mode == exit3 ]]; then
    register_args=(--group builders --title 'payload builder')
  fi
  FLEET_TEST_REGISTER_MODE=$mode PATH="$test_path" \
    "$FLEET/spawn.sh" codex --tag gate --pane p-test "${register_args[@]}" \
    >"$TEST_ROOT/spawn-$mode.out" 2>"$TEST_ROOT/spawn-$mode.err"
  if [[ $mode == absent ]]; then mv "$TEST_ROOT/bin/herder.off" "$TEST_ROOT/bin/herder"; fi
  cmp -s "$TEST_ROOT/spawn.out" "$TEST_ROOT/spawn-$mode.out" || fail "register $mode changed spawn stdout"
  [[ $(grep -c 'fleet spawn: register launch-requested skipped:' "$TEST_ROOT/spawn-$mode.err") -eq 1 ]] \
    || fail "register $mode did not emit exactly one warning"
  if [[ $mode == exit3 ]]; then
    grep -F 'fleet spawn: assign gate-vava --group builders skipped:' "$TEST_ROOT/spawn-$mode.err" >/dev/null \
      || fail "assign refusal warning omitted the seat or group"
    grep -F 'fleet spawn: annotate gate-vava --title skipped:' "$TEST_ROOT/spawn-$mode.err" >/dev/null \
      || fail "annotate refusal warning omitted the seat"
    grep -F 'herder ' "$FLEET_TEST_CALLS" | grep -F ' register launch-ready ' >/dev/null \
      || fail "a herder refusal disabled launch-ready registration"
  fi
  if [[ $mode == sleep ]]; then
    elapsed=$((SECONDS - start))
    ((elapsed >= 9 && elapsed <= 12)) || fail "register timeout took ${elapsed}s instead of about 10s"
  fi
done
pass "spawn registration absence, failure, and timeout are fail-open"

mkdir -p "$TEST_ROOT/real-bin"
(cd "$ROOT/tools/herder" && go build -o "$TEST_ROOT/real-bin/herder" ./cmd/herder)
real_state=$TEST_ROOT/real-state
mkdir -p "$real_state"
: >"$FLEET_TEST_CALLS"
env -u HCOM_NAME HCOM_TAG=impl HCOM_INSTANCE_NAME=fimu HCOM_PROCESS_ID=seat-test \
  HERDER_STATE_DIR="$real_state" FLEET_TEST_SELF_MODE=name \
  PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/real-spawn.out" 2>"$TEST_ROOT/real-spawn.err"
jq -s -e 'length == 2 and .[0].kind == "launch-requested" and .[1].kind == "launch-ready"
  and .[1].request == .[0].id and all(.[]; .by == "ziru" and .by_kind == "agent")' \
  "$real_state/agents/events.jsonl" >/dev/null \
  || { sed -n '1,2p' "$real_state/agents/events.jsonl" >&2; sed -n '1,12p' "$FLEET_TEST_CALLS" >&2; fail "real register rejected the wrapper event contract"; }
HERDER_STATE_DIR="$real_state" PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" \
  "$TEST_ROOT/real-bin/herder" show gate-vava --json | jq -e '.provenance.kind == "registered"' >/dev/null \
  || fail "real show did not project the registered launch"
pass "spawn prefers hcom self over stale seat environment"

fallback_state=$TEST_ROOT/real-fallback-state
mkdir -p "$fallback_state"
: >"$FLEET_TEST_CALLS"
env -u HCOM_NAME HCOM_TAG=impl HCOM_INSTANCE_NAME=fimu HCOM_PROCESS_ID=seat-test \
  HERDER_STATE_DIR="$fallback_state" FLEET_TEST_SELF_MODE=fail \
  PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/real-fallback-spawn.out" \
  2>"$TEST_ROOT/real-fallback-spawn.err" || fail "fallback spawn exited non-zero"
cmp -s "$TEST_ROOT/real-spawn.out" "$TEST_ROOT/real-fallback-spawn.out" \
  || fail "failed hcom self lookup changed spawn stdout"
jq -s -e 'length == 2 and all(.[]; .by == "impl-fimu" and .by_kind == "agent")' \
  "$fallback_state/agents/events.jsonl" >/dev/null \
  || fail "failed hcom self lookup did not leave attribution to register default"
pass "missing hcom self is fail-open with unchanged spawn output and register fallback"
web_state=$TEST_ROOT/real-web-state
mkdir -p "$web_state"
HERDER_STATE_DIR="$web_state" FLEET_LAUNCHER=web-x FLEET_LAUNCHER_KIND=web \
  PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/real-web-spawn.out" 2>"$TEST_ROOT/real-web-spawn.err"
HERDER_STATE_DIR="$web_state" PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" \
  "$TEST_ROOT/real-bin/herder" show gate-vava --json | jq -e '.provenance.launcher == "web-x" and .provenance.launcher_kind == "web"' >/dev/null \
  || fail "real register lost web launcher attribution"
pass "real spawn and register validate requested, ready, projection, and web attribution end to end"

: >"$FLEET_TEST_CALLS"
if ! timeout 2 env FLEET_TEST_LAUNCH_MODE=descendant-stdout PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/descendant.out" 2>"$TEST_ROOT/descendant.err"; then
  fail "spawn waited on a descendant that retained launch stdout"
fi
grep -Fx 'name=gate-vava' "$TEST_ROOT/descendant.out" >/dev/null \
  || fail "spawn lost launch output captured outside a pipe"
pass "spawn capture does not hang on inherited descendant stdout"

if FLEET_TEST_LAUNCH_MODE=blocking FLEET_LAUNCH_TIMEOUT_SECONDS=1 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/blocking.out" 2>"$TEST_ROOT/blocking.err"; then
  fail "spawn accepted a launcher that exceeded its deadline"
fi
grep -F 'launcher timed out after 1s (name=vava, batch=batch-test, pane=p-test left for explicit cleanup)' "$TEST_ROOT/blocking.err" >/dev/null \
  || fail "launcher timeout did not preserve parsed name, batch, and placement"
pass "spawn caps the launcher wait and reports preserved coordinates"

: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --effort high --tag gate --pane p-test >"$TEST_ROOT/codex-effort.out"
cp "$FLEET_TEST_CALLS" "$TEST_ROOT/codex-calls"
grep -F 'model_reasoning_effort=\"high\"' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "codex effort did not reach the launch argv as a config override"

: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" claude --effort max --tag gate --pane p-test >"$TEST_ROOT/claude-effort.out"
grep -F -- '--effort max' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "claude effort did not reach the launch argv"
pass "spawn maps reasoning effort to each tool's CLI"

# A seat launched from inside Claude Code inherits CLAUDE_CODE_CHILD_SESSION,
# which turns transcript saving off; claude seats force it back on.
grep -E '^hcom .* PERSIST=1 1 claude ' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "claude launch did not force session persistence"
grep -E '^hcom .* PERSIST=1 1 codex ' "$TEST_ROOT/codex-calls" >/dev/null \
  && fail "codex launch was given the claude persistence override"
pass "spawn forces transcript saving for claude seats only"

: >"$FLEET_TEST_CALLS"
if PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --effort max --tag gate --pane p-test \
  >"$TEST_ROOT/codex-bad-effort.out" 2>"$TEST_ROOT/codex-bad-effort.err"; then
  fail "spawn accepted an invalid Codex effort"
fi
grep -F -- '--effort for codex must be one of: low, medium, high, xhigh' "$TEST_ROOT/codex-bad-effort.err" >/dev/null \
  || fail "invalid Codex effort refusal was not actionable"
[[ ! -s $FLEET_TEST_CALLS ]] || fail "invalid Codex effort reached placement or launch"

if PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" claude --effort bogus --tag gate --pane p-test \
  >"$TEST_ROOT/claude-bad-effort.out" 2>"$TEST_ROOT/claude-bad-effort.err"; then
  fail "spawn accepted an invalid Claude effort"
fi
grep -F -- '--effort for claude must be one of: low, medium, high, xhigh, max' "$TEST_ROOT/claude-bad-effort.err" >/dev/null \
  || fail "invalid Claude effort refusal was not actionable"
[[ ! -s $FLEET_TEST_CALLS ]] || fail "invalid Claude effort reached placement or launch"
pass "spawn refuses unknown effort before placement or launch"

: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --split-from p-source --force-split --prompt hello >"$TEST_ROOT/split.out"
grep -F 'herdr pane get p-source' "$FLEET_TEST_CALLS" >/dev/null || fail "split spawn did not validate its source pane"
grep -F 'herdr pane split --pane p-source --direction right --no-focus' "$FLEET_TEST_CALLS" >/dev/null || fail "split spawn did not split rightward by default (herdr 0.8 requires an explicit direction)"
grep -F 'FLEET_PANE=p-split FLEET_TOOL=codex HCOM_TERMINAL=fleet' "$FLEET_TEST_CALLS" >/dev/null || fail "split spawn did not launch into the fresh pane"
grep -Fx 'pane=p-split' "$TEST_ROOT/split.out" >/dev/null || fail "split spawn did not report the fresh pane"
pass "spawn splits beside a validated source and launches into the fresh pane"

: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --split-from self --split-direction down --force-split --prompt hello >"$TEST_ROOT/split-self.out"
grep -F 'herdr pane current' "$FLEET_TEST_CALLS" >/dev/null || fail "split-from self did not resolve the caller's own pane"
grep -F 'herdr pane split --pane p-self --direction down --no-focus' "$FLEET_TEST_CALLS" >/dev/null || fail "split-from self did not split from the resolved pane with the requested direction"
pass "spawn splits beside the caller's own pane with a chosen direction"

if PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --split-from p-source --force-split --split-direction sideways \
  >/dev/null 2>"$TEST_ROOT/split-baddir.err"; then
  fail "split spawn accepted an invalid direction"
fi
grep -F -- '--split-direction must be right or down' "$TEST_ROOT/split-baddir.err" >/dev/null \
  || fail "invalid direction refusal was not actionable"
if PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test --split-direction down \
  >/dev/null 2>"$TEST_ROOT/split-nodir.err"; then
  fail "split direction was accepted without a split placement"
fi
grep -F -- '--split-direction only applies with --split-from' "$TEST_ROOT/split-nodir.err" >/dev/null \
  || fail "direction-without-split refusal was not actionable"
pass "spawn validates split direction and its pairing"

if FLEET_TEST_UNKNOWN_SPLIT=1 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --split-from p-source --force-split >"$TEST_ROOT/split-missing.out" 2>"$TEST_ROOT/split-missing.err"; then
  fail "split spawn accepted an unknown source pane"
fi
grep -F 'fleet spawn: pane does not exist: p-source' "$TEST_ROOT/split-missing.err" >/dev/null \
  || fail "split spawn did not quote the unknown source refusal"
pass "spawn refuses an unknown split source before creating placement"

: >"$FLEET_TEST_CALLS"
set +e
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --split-from p-source \
  >"$TEST_ROOT/split-unforced.out" 2>"$TEST_ROOT/split-unforced.err"
split_unforced_rc=$?
set -e
[[ $split_unforced_rc -eq 2 ]] || fail "unforced split did not refuse with exit 2"
grep -F 'seats sharing a tab get tiny terminals and miss hcom deliveries' "$TEST_ROOT/split-unforced.err" >/dev/null \
  || fail "unforced split refusal did not say why"
grep -F -- '--force-split' "$TEST_ROOT/split-unforced.err" >/dev/null || fail "unforced split refusal did not name the override"
[[ ! -s $FLEET_TEST_CALLS ]] || fail "unforced split touched herdr, hcom or herder before refusing"
pass "spawn refuses --split-from without --force-split before acting"

: >"$FLEET_TEST_CALLS"
set +e
FLEET_TEST_TAB_SHARED=p-test PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test \
  >"$TEST_ROOT/pane-shared.out" 2>"$TEST_ROOT/pane-shared.err"
pane_shared_rc=$?
set -e
[[ $pane_shared_rc -eq 2 ]] || fail "--pane in a shared tab did not refuse with exit 2"
grep -F 'pane p-test shares tab t-p-test with 1 other pane(s); seats sharing a tab get tiny terminals and miss hcom deliveries' \
  "$TEST_ROOT/pane-shared.err" >/dev/null || fail "--pane shared-tab refusal did not say why"
! grep -E 'hcom .* 1 codex|herder ' "$FLEET_TEST_CALLS" >/dev/null || fail "--pane shared-tab refusal launched or registered"
pass "spawn refuses --pane in a multi-pane tab"

: >"$FLEET_TEST_CALLS"
FLEET_TEST_TAB_SHARED=p-test PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test --force-split \
  >"$TEST_ROOT/pane-forced.out"
grep -Fx 'pane=p-test' "$TEST_ROOT/pane-forced.out" >/dev/null || fail "--force-split did not allow a shared --pane"
! grep -F 'herdr pane list' "$FLEET_TEST_CALLS" >/dev/null || fail "--force-split still counted the tab"
pass "spawn allows a shared --pane with --force-split"

if PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --workspace w-test --force-split \
  >/dev/null 2>"$TEST_ROOT/force-nosplit.err"; then
  fail "--force-split was accepted without --split-from or --pane"
fi
grep -F -- '--force-split only applies with --split-from or --pane' "$TEST_ROOT/force-nosplit.err" >/dev/null \
  || fail "stray --force-split refusal was not actionable"
pass "spawn rejects a stray --force-split"

: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp >"$TEST_ROOT/wt-alone.out"
grep -Fx 'pane=p-wt' "$TEST_ROOT/wt-alone.out" >/dev/null || fail "worktree spawn did not use the new workspace's lone pane"
! grep -F 'herdr tab create' "$FLEET_TEST_CALLS" >/dev/null || fail "worktree spawn opened a needless extra tab"
: >"$FLEET_TEST_CALLS"
FLEET_TEST_TAB_SHARED=p-wt PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp \
  >"$TEST_ROOT/wt-shared.out"
grep -Fx 'herdr tab create --workspace w-wt --cwd /tmp --no-focus' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "worktree spawn did not open a fresh tab when its pane was shared"
grep -Fx 'pane=p-wt-fresh' "$TEST_ROOT/wt-shared.out" >/dev/null || fail "worktree spawn did not launch into the fresh tab"
grep -F 'FLEET_PANE=p-wt-fresh ' "$FLEET_TEST_CALLS" >/dev/null || fail "worktree launch did not target the fresh tab pane"
pass "worktree spawn takes its own tab even when herdr hands back a shared pane"

for unproven in 'FLEET_TEST_PROCESS_SHAPE=no-shell-pid' 'FLEET_TEST_PROCESS_SHAPE=malformed' 'FLEET_TEST_ROOT_TAB=unknown'; do
  : >"$FLEET_TEST_CALLS"
  env "$unproven" PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp \
    >"$TEST_ROOT/wt-unproven.out" || fail "worktree spawn failed with $unproven"
  grep -Fx 'herdr tab create --workspace w-wt --cwd /tmp --no-focus' "$FLEET_TEST_CALLS" >/dev/null \
    || fail "worktree spawn reused a root pane it could not prove ($unproven)"
  grep -Fx 'pane=p-wt-fresh' "$TEST_ROOT/wt-unproven.out" >/dev/null || fail "worktree spawn did not launch into the fresh tab ($unproven)"
done
pass "worktree spawn opens a fresh tab when the root pane's idleness or tab is unproven"

# A just-created root pane runs its shell's startup hooks (mise, dircolors)
# before settling; spawn waits for a steady idle shell rather than stranding it.
for settling in 'busy busy unknown idle idle idle' 'unknown unknown unknown' 'busy idle busy idle unknown' 'idle respawned'; do
  : >"$FLEET_TEST_CALLS"
  rm -f -- "$FLEET_TEST_ROOT_COUNT"
  FLEET_TEST_ROOT_SEQ=$settling PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp \
    >"$TEST_ROOT/wt-settle.out" || fail "worktree spawn failed while its root pane settled ($settling)"
  grep -Fx 'pane=p-wt' "$TEST_ROOT/wt-settle.out" >/dev/null || fail "worktree spawn stranded a root pane that settled idle ($settling)"
  ! grep -F 'herdr tab create' "$FLEET_TEST_CALLS" >/dev/null || fail "worktree spawn opened a fresh tab for a settling root pane ($settling)"
done
pass "worktree spawn reuses a root pane that settles to an idle shell after startup"

flapping=$(printf 'busy idle idle %.0s' 1 2 3 4 5)
for stuck in 'busy|busy' 'unknown|unknown' "${flapping% }|busy"; do
  : >"$FLEET_TEST_CALLS"
  rm -f -- "$FLEET_TEST_ROOT_COUNT"
  FLEET_TEST_ROOT_SEQ=${stuck%|*} FLEET_TEST_ROOT_AFTER=${stuck#*|} PATH="$TEST_ROOT/bin:$PATH" \
    "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp >"$TEST_ROOT/wt-stuck.out" \
    || fail "worktree spawn failed with a root pane that never settled ($stuck)"
  grep -Fx 'herdr tab create --workspace w-wt --cwd /tmp --no-focus' "$FLEET_TEST_CALLS" >/dev/null \
    || fail "worktree spawn reused a root pane that never settled idle ($stuck)"
  grep -Fx 'pane=p-wt-fresh' "$TEST_ROOT/wt-stuck.out" >/dev/null || fail "worktree spawn did not launch into the fresh tab ($stuck)"
  [[ $(cat "$FLEET_TEST_ROOT_COUNT") -eq 15 ]] || fail "worktree spawn did not bound its root-pane settle reads ($stuck)"
done
pass "worktree spawn opens a fresh tab after a bounded wait when the root pane never settles idle"

: >"$FLEET_TEST_CALLS"
if FLEET_TEST_TAB_SHARED=p-wt FLEET_TEST_TAB_CREATE=fail PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --worktree-branch wt --repo /tmp >"$TEST_ROOT/wt-nocreate.out" 2>"$TEST_ROOT/wt-nocreate.err"; then
  fail "worktree spawn launched although its fresh tab could not be created"
fi
grep -F 'herdr tab create failed in worktree workspace w-wt (branch=wt repo=/tmp pane=p-wt tab=t-p-wt workspace=w-wt left for explicit cleanup)' \
  "$TEST_ROOT/wt-nocreate.err" >/dev/null || fail "worktree tab-create failure did not name its coordinates"
! grep -E 'hcom .* 1 codex' "$FLEET_TEST_CALLS" >/dev/null || fail "worktree tab-create failure still launched"
pass "worktree spawn dies with coordinates when its fresh tab cannot be created"

if FLEET_TEST_PROCESS_SHAPE=malformed PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >/dev/null 2>"$TEST_ROOT/pane-malformed.err"; then
  fail "spawn reused a pane whose process info is malformed"
fi
grep -F 'cannot verify idle shell because process info is missing or malformed: p-test' "$TEST_ROOT/pane-malformed.err" >/dev/null \
  || fail "spawn did not explain the malformed process info"
pass "spawn refuses a --pane whose process info is malformed"

printf '0\n' >"$TEST_ROOT/hooks-count"
FLEET_TEST_HOOKS_BOUND=delayed FLEET_TEST_HOOKS_COUNT="$TEST_ROOT/hooks-count" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/delayed-hooks.out"
grep -Fx 'name=gate-vava' "$TEST_ROOT/delayed-hooks.out" >/dev/null \
  || fail "spawn did not tolerate delayed hook binding after readiness"
pass "spawn waits briefly for hook binding after readiness"

FLEET_TEST_HOOKS_BOUND=0 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/codex-unbound.out" 2>"$TEST_ROOT/codex-unbound.err"
grep -Fx 'name=gate-vava' "$TEST_ROOT/codex-unbound.out" >/dev/null \
  || fail "spawn did not report the ready Codex launch with pty-only binding"
grep -F 'fleet spawn: note: ready launch is not hook-bound in hcom roster: gate-vava' "$TEST_ROOT/codex-unbound.err" >/dev/null \
  || fail "spawn did not note the ready Codex launch's expected unbound hooks"
grep -Fx 'pane=p-test' "$TEST_ROOT/codex-unbound.out" >/dev/null \
  || fail "spawn did not report the ready Codex placement"
pass "spawn accepts ready Codex launch with pty-only binding"

if env -u HCOM_NAME HCOM_TAG=impl HCOM_INSTANCE_NAME=fimu HCOM_PROCESS_ID=seat-test \
  FLEET_TEST_SELF_MODE=name FLEET_TEST_HOOKS_BOUND=0 PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" claude --tag gate --pane p-test >"$TEST_ROOT/claude-unbound.out" 2>"$TEST_ROOT/claude-unbound.err"; then
  fail "spawn accepted a ready Claude launch without bound hooks"
fi
grep -F 'ready launch is not hook-bound in hcom roster: gate-vava' "$TEST_ROOT/claude-unbound.err" >/dev/null \
  || fail "spawn did not explain the unbound ready Claude launch"
grep -F 'pane=p-test' "$TEST_ROOT/claude-unbound.err" >/dev/null \
  || fail "spawn did not name the placement left by an unbound ready Claude launch"
pass "spawn still requires hook binding for ready Claude launches"
[[ $(grep -c 'herder .*register launch-failed .*--request 018f0000-0000-7000-8000-000000000001 .*--pane p-test .*--batch batch-test .*--by ziru --by-kind agent' "$FLEET_TEST_CALLS") -eq 1 ]] \
  || fail "spawn did not register the post-placement failure exactly once with coordinates"
pass "spawn keeps hcom-self attribution on launch failure"

if FLEET_TEST_PROCESS_SHAPE=no-shell-pid PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/spawn.sh" codex --tag gate --pane p-test >"$TEST_ROOT/no-shell.out" 2>"$TEST_ROOT/no-shell.err"; then
  fail "spawn accepted an existing pane without a verifiable shell pid"
fi
grep -F 'cannot verify idle shell because process info is missing or malformed' "$TEST_ROOT/no-shell.err" >/dev/null \
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
# The open helper pins the seat's git identity from the pane cwd; the tests
# never read the host's real config: a private global config stands in.
global_gitconfig=$TEST_ROOT/gitconfig
printf '[user]\n\tname = Global Owner\n\temail = owner@global.test\n' >"$global_gitconfig"
global_identity='GIT_AUTHOR_NAME=Global\ Owner GIT_AUTHOR_EMAIL=owner@global.test GIT_COMMITTER_NAME=Global\ Owner GIT_COMMITTER_EMAIL=owner@global.test'

: >"$FLEET_TEST_CALLS"
first_line=$(GIT_CONFIG_GLOBAL="$global_gitconfig" GIT_CONFIG_SYSTEM=/dev/null PATH="$TEST_ROOT/bin:$PATH" \
  FLEET_PANE=p-test FLEET_TOOL=codex "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]')
[[ $first_line == p-test ]] || fail "open helper did not print pane id first"
printf -v launch_q '%q' "$launch_script"
printf -v run_q '%q' "$global_identity HERDR_AGENT=codex bash $launch_q"
grep -F "herdr pane run p-test $run_q" "$FLEET_TEST_CALLS" >/dev/null || fail "open helper omitted the Codex marker or the identity pin"
pass "open helper marks Codex, pins the global identity, preserves first-line id and script quoting"

: >"$FLEET_TEST_CALLS"
GIT_CONFIG_GLOBAL="$global_gitconfig" GIT_CONFIG_SYSTEM=/dev/null PATH="$TEST_ROOT/bin:$PATH" \
  FLEET_PANE=p-test FLEET_TOOL=claude \
  "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [claude]' >/dev/null
printf -v run_q '%q' "$global_identity HERDR_AGENT=claude bash $launch_q"
grep -F "herdr pane run p-test $run_q" "$FLEET_TEST_CALLS" >/dev/null \
  || fail "open helper used the wrong Claude marker"
pass "open helper marks Claude with the canonical tool"

identity_repo=$TEST_ROOT/'identity repo'
mkdir -p -- "$identity_repo"
git -C "$identity_repo" init -q
git -C "$identity_repo" config user.name 'Repo Owner'
git -C "$identity_repo" config user.email 'owner@repo.test'
: >"$FLEET_TEST_CALLS"
GIT_CONFIG_GLOBAL="$global_gitconfig" GIT_CONFIG_SYSTEM=/dev/null FLEET_TEST_PANE_CWD="$identity_repo" \
  GIT_AUTHOR_EMAIL=stale@env.test PATH="$TEST_ROOT/bin:$PATH" FLEET_PANE=p-test FLEET_TOOL=codex \
  "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]' >/dev/null
printf -v run_q '%q' 'GIT_AUTHOR_NAME=Repo\ Owner GIT_AUTHOR_EMAIL=owner@repo.test GIT_COMMITTER_NAME=Repo\ Owner GIT_COMMITTER_EMAIL=owner@repo.test'" HERDR_AGENT=codex bash $launch_q"
grep -F "herdr pane run p-test $run_q" "$FLEET_TEST_CALLS" >/dev/null \
  || fail "open helper did not pin the checkout's own identity over the global one"
pass "open helper pins the pane checkout's git identity ahead of the global config"

: >"$FLEET_TEST_CALLS"
if GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null PATH="$TEST_ROOT/bin:$PATH" \
  FLEET_PANE=p-test FLEET_TOOL=codex \
  "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]' >"$TEST_ROOT/no-identity.out" 2>"$TEST_ROOT/no-identity.err"; then
  fail "open helper launched a seat with no resolvable git identity"
fi
[[ ! -s $TEST_ROOT/no-identity.out ]] || fail "open helper printed a pane id before refusing on identity"
grep -F 'no git identity resolves at /tmp' "$TEST_ROOT/no-identity.err" >/dev/null || fail "open helper did not explain the identity refusal"
if grep -E 'herdr pane (rename|run) ' "$FLEET_TEST_CALLS" >/dev/null; then
  fail "open helper touched the pane after refusing on identity"
fi
pass "open helper refuses to launch when no git identity resolves at the pane cwd"

: >"$FLEET_TEST_CALLS"
if PATH="$TEST_ROOT/bin:$PATH" FLEET_PANE=p-test \
  "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]' >/dev/null 2>&1; then
  fail "open helper accepted a missing FLEET_TOOL"
fi
[[ ! -s $FLEET_TEST_CALLS ]] || fail "open helper touched herdr without FLEET_TOOL"
pass "open helper requires the fleet tool marker"

: >"$FLEET_TEST_CALLS"
if PATH="$TEST_ROOT/bin:$PATH" FLEET_PANE=p-test FLEET_TOOL=bogus \
  "$FLEET/spawn-pane.sh" "$launch_script" '◉ gate-vava [codex]' >/dev/null 2>&1; then
  fail "open helper accepted a bogus FLEET_TOOL"
fi
[[ ! -s $FLEET_TEST_CALLS ]] || fail "open helper touched herdr with a bogus FLEET_TOOL"
pass "open helper rejects unsupported fleet tools before touching herdr"

if "$FLEET/selfcompact.sh" '../wrong' steer continue >/dev/null 2>&1; then
  fail "selfcompact accepted an unsafe hcom name"
fi
pass "selfcompact rejects unsafe log-name input"

: >"$FLEET_TEST_CALLS"
printf '0\n' >"$TEST_ROOT/status-count"
FLEET_TEST_STATUS_MODE=transient FLEET_TEST_STATUS_COUNT="$TEST_ROOT/status-count" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/selfcompact.sh" --run vava steer continue
[[ $(grep -c 'term inject vava' "$FLEET_TEST_CALLS") -eq 4 ]] \
  || fail "selfcompact did not survive one empty status read and inject continuation"
pass "selfcompact tolerates a transient empty status read"

# Claude Code 2.1.257+ treats one large injected burst as pasted content and
# never parses "/compact" out of it; the helper must inject the command word
# alone, then the steer, then the submit — never "/compact <steer>" in one burst.
inject_lines=$(grep 'term inject vava' "$FLEET_TEST_CALLS")
[[ $(sed -n 1p <<<"$inject_lines") == *'term inject vava /compact\ ' ]] \
  || fail "selfcompact did not inject the /compact prefix on its own first"
[[ $(sed -n 2p <<<"$inject_lines") == *'term inject vava steer' ]] \
  || fail "selfcompact did not inject the steer as its own burst without the prefix"
[[ $(sed -n 3p <<<"$inject_lines") == *'term inject vava --enter' ]] \
  || fail "selfcompact did not submit the compact with a bare enter"
! grep -F 'term inject vava /compact\ steer' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "selfcompact injected /compact and the steer in one burst"
pass "selfcompact splits the /compact prefix from the steer (2.1.257 paste parsing)"

# The launch notes are canonical in docs/hcom-launch-notes.txt; the apply script
# renders the checkout path in and hands the whole text to `hcom config notes`.
: >"$FLEET_TEST_CALLS"
PATH="$TEST_ROOT/bin:$PATH" "$FLEET/apply-hcom-notes.sh" >/dev/null \
  || fail "apply-hcom-notes did not run against the fake hcom"
notes_call=$(grep -F ' config notes ' "$FLEET_TEST_CALLS" || true)
[[ -n $notes_call ]] || fail "apply-hcom-notes did not call hcom config notes"
repo_root=$(cd -- "$FLEET/../.." && pwd)
printf -v root_q '%q' "$repo_root/docs/fencing-convention.md"
[[ $notes_call == *"$root_q"* ]] || fail "apply-hcom-notes did not render the checkout path into the notes"
[[ $notes_call != *__AI_CONFIG_ROOT__* ]] || fail "apply-hcom-notes left the placeholder unrendered"
[[ $notes_call == *'Fleet lifecycle:'* && $notes_call == *'Chat fencing:'* ]] \
  || fail "apply-hcom-notes did not send both the doctrine and the fencing paragraph"
pass "apply-hcom-notes renders the canonical launch notes for hcom"

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

# compact.sh (compacting ANOTHER seat) — fast bounds for the tests.
compact_env=(FLEET_COMPACT_SETTLE_SECONDS=1 FLEET_COMPACT_POLL_SECONDS=1 FLEET_COMPACT_WAIT_SECONDS=4 FLEET_COMPACT_LATCH_SECONDS=20 FLEET_COMPACT_DROP_SECONDS=3)
compact_state=$TEST_ROOT/compact-state
mkdir -p -- "$compact_state"

if "$FLEET/compact.sh" '../wrong' steer continue >/dev/null 2>&1; then
  fail "compact accepted an unsafe hcom name"
fi
pass "compact rejects unsafe hcom-name input"

: >"$FLEET_TEST_CALLS"; printf 0 >"$compact_state/status"
if env "${compact_env[@]}" FLEET_TEST_COMPACT_MODE=busy FLEET_TEST_COMPACT_STATE="$compact_state" PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/compact.sh" vava steer continue >"$TEST_ROOT/compact-busy.out" 2>"$TEST_ROOT/compact-busy.err"; then
  fail "compact typed into a busy composer"
fi
grep -F 'never showed a quiet composer' "$TEST_ROOT/compact-busy.err" >/dev/null || fail "compact did not explain the busy-composer refusal"
! grep -F 'term inject' "$FLEET_TEST_CALLS" >/dev/null || fail "compact injected into a busy composer"
! grep -F 'send @vava' "$FLEET_TEST_CALLS" >/dev/null || fail "compact sent a continuation without compacting"
pass "compact refuses to type while the composer is not empty and idle"

: >"$FLEET_TEST_CALLS"; printf 0 >"$compact_state/status"
env "${compact_env[@]}" FLEET_TEST_COMPACT_MODE=happy FLEET_TEST_COMPACT_STATE="$compact_state" PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/compact.sh" vava steer continue >"$TEST_ROOT/compact-happy.out" 2>"$TEST_ROOT/compact-happy.err" \
  || fail "compact failed on the happy path: $(<"$TEST_ROOT/compact-happy.err")"
first_inject=$(grep -n -F 'term inject vava' "$FLEET_TEST_CALLS" | head -n 1 | cut -d: -f1)
[[ $(head -n "$((first_inject - 1))" "$FLEET_TEST_CALLS" | grep -c 'term vava --json') -ge 2 ]] \
  || fail "compact typed before the settle window had two quiet screen reads"
inject_lines=$(grep 'term inject vava' "$FLEET_TEST_CALLS")
[[ $(sed -n 1p <<<"$inject_lines") == *'term inject vava /compact\ ' ]] || fail "compact did not inject the /compact prefix on its own first"
[[ $(sed -n 2p <<<"$inject_lines") == *'term inject vava steer' ]] || fail "compact did not inject the steer as its own burst"
[[ $(sed -n 3p <<<"$inject_lines") == *'term inject vava --enter' ]] || fail "compact did not submit with a bare enter"
grep -F 'context dropped: claude 120 -> 9' "$TEST_ROOT/compact-happy.out" >/dev/null || fail "compact did not report the Claude context drop"
grep -F 'send @vava --intent request -- continue' "$FLEET_TEST_CALLS" >/dev/null || fail "compact did not send the continuation after the verified drop"
[[ $(grep -c 'send @vava' "$FLEET_TEST_CALLS") -eq 1 ]] || fail "compact sent the continuation more than once"
pass "compact waits for a quiet composer, splits the inject, verifies the Claude context drop, then sends one continuation"

: >"$FLEET_TEST_CALLS"; printf 0 >"$compact_state/status"
env "${compact_env[@]}" FLEET_TEST_COMPACT_MODE=codex FLEET_TEST_COMPACT_STATE="$compact_state" PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/compact.sh" vava steer continue >"$TEST_ROOT/compact-codex.out" 2>&1 || fail "compact failed on the codex path"
grep -F 'context dropped: codex 31 -> 88' "$TEST_ROOT/compact-codex.out" >/dev/null || fail "compact did not read the Codex context figure"
pass "compact reads the Codex 'Context NN% left' figure"

: >"$FLEET_TEST_CALLS"; printf 0 >"$compact_state/status"
if env "${compact_env[@]}" FLEET_TEST_COMPACT_MODE=nodrop FLEET_TEST_COMPACT_STATE="$compact_state" PATH="$TEST_ROOT/bin:$PATH" \
  "$FLEET/compact.sh" vava steer continue >"$TEST_ROOT/compact-nodrop.out" 2>"$TEST_ROOT/compact-nodrop.err"; then
  fail "compact reported success without a context drop"
fi
grep -F 'no context drop on vava (claude 120 before, 120 after' "$TEST_ROOT/compact-nodrop.err" >/dev/null || fail "compact did not name the missing drop"
grep -F 'continuation NOT sent' "$TEST_ROOT/compact-nodrop.err" >/dev/null || fail "compact did not say the continuation was withheld"
! grep -F 'send @vava' "$FLEET_TEST_CALLS" >/dev/null || fail "compact sent a continuation into an uncompacted seat"
pass "compact withholds the continuation and fails loudly when the context does not drop"

cull_state=$TEST_ROOT/cull-state
mkdir -p "$cull_state"
: >"$FLEET_TEST_CALLS"
FLEET_TEST_CULL_MODE=managed FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-managed.out"
grep -Fx 'culled name=gate-vava pane=p-managed close=managed tab=gone' "$TEST_ROOT/cull-managed.out" >/dev/null \
  || fail "cull did not verify the managed pane close"
send_line=$(grep -n 'hcom .* send @gate-vava' "$FLEET_TEST_CALLS" | cut -d: -f1)
kill_line=$(grep -n 'hcom .* kill gate-vava' "$FLEET_TEST_CALLS" | cut -d: -f1)
[[ -n $send_line && -n $kill_line && $send_line -lt $kill_line ]] \
  || fail "cull did not send its courtesy notice before kill"
requested_line=$(grep -n 'herder .*register cull-requested --name gate-vava --pane p-managed' "$FLEET_TEST_CALLS" | cut -d: -f1)
[[ -n $requested_line && $requested_line -lt $send_line ]] || fail "cull request was not registered before courtesy and kill"
grep -F 'register culled --name gate-vava --pane p-managed --close managed' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "managed cull outcome was not registered"
if grep -F 'herdr pane close' "$FLEET_TEST_CALLS" >/dev/null; then
  fail "cull closed a pane explicitly after managed close was verified"
fi
pass "cull sends courtesy before kill and verifies managed close"

cull_attrib_state=$TEST_ROOT/real-cull-state
mkdir -p "$cull_attrib_state"
rm -f "$cull_state/killed" "$cull_state/closed"
: >"$FLEET_TEST_CALLS"
env -u HCOM_NAME HCOM_TAG=impl HCOM_INSTANCE_NAME=fimu HCOM_PROCESS_ID=seat-test \
  HERDER_STATE_DIR="$cull_attrib_state" FLEET_TEST_SELF_MODE=name \
  FLEET_TEST_CULL_MODE=managed FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/real-bin:$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava \
  >"$TEST_ROOT/cull-attrib.out"
jq -s -e 'length == 2 and .[0].kind == "cull-requested" and .[1].kind == "culled"
  and all(.[]; .by == "ziru" and .by_kind == "agent")' \
  "$cull_attrib_state/agents/events.jsonl" >/dev/null \
  || fail "cull did not carry hcom-self attribution through requested and culled events"
pass "cull prefers hcom self over stale seat environment"

for mode in exit3 sleep; do
  rm -f "$cull_state/killed" "$cull_state/closed"
  : >"$FLEET_TEST_CALLS"
  start=$SECONDS
  FLEET_TEST_REGISTER_MODE=$mode FLEET_TEST_CULL_MODE=managed FLEET_TEST_CULL_STATE="$cull_state" \
    PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-$mode.out" 2>"$TEST_ROOT/cull-$mode.err"
  cmp -s "$TEST_ROOT/cull-managed.out" "$TEST_ROOT/cull-$mode.out" \
    || fail "register $mode changed cull stdout"
  [[ $(grep -c 'fleet cull: register cull-requested skipped:' "$TEST_ROOT/cull-$mode.err") -eq 1 ]] \
    || fail "register $mode did not emit exactly one cull warning"
  if [[ $mode == sleep ]]; then
    elapsed=$((SECONDS - start))
    ((elapsed >= 9 && elapsed <= 12)) || fail "cull register timeout took ${elapsed}s instead of about 10s"
  fi
done
pass "cull registration failure and timeout are once-only and fail-open"

rm -f "$cull_state/killed" "$cull_state/closed"
: >"$FLEET_TEST_CALLS"
FLEET_TEST_CULL_MODE=fallback FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-fallback.out" 2>"$TEST_ROOT/cull-fallback.err"
grep -Fx 'culled name=gate-vava pane=p-fallback close=label-fallback tab=gone cwd=/srv/seat/app foreground=zsh' "$TEST_ROOT/cull-fallback.out" >/dev/null \
  || fail "cull did not report its unique-label fallback close"
grep -F 'herdr pane close p-fallback' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "cull did not close the unique exact-label fallback pane"
grep -F 'register culled --name gate-vava --pane p-fallback --close label-fallback' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "fallback cull outcome was not registered"
pass "cull closes the unique exact-label fallback pane once it is an idle shell"

rm -f "$cull_state/killed" "$cull_state/closed"
: >"$FLEET_TEST_CALLS"
if FLEET_TEST_CULL_MODE=ambiguous FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-ambiguous.out" 2>"$TEST_ROOT/cull-ambiguous.err"; then
  fail "cull accepted ambiguous exact-label fallback panes"
fi
grep -F 'multiple panes match the exact gate-vava [codex] label; refusing to cull' "$TEST_ROOT/cull-ambiguous.err" >/dev/null \
  || fail "cull did not explain its ambiguity refusal"
if grep -E 'hcom .* (send|kill) ' "$FLEET_TEST_CALLS" >/dev/null; then
  fail "cull acted on the seat before refusing ambiguous labels"
fi
! grep -F 'herder ' "$FLEET_TEST_CALLS" >/dev/null || fail "ambiguous cull wrote a registration event"
pass "cull refuses ambiguous exact-label matches before acting"

rm -f "$cull_state"/*
: >"$FLEET_TEST_CALLS"
FLEET_TEST_CULL_MODE=idle FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-idle.out" 2>"$TEST_ROOT/cull-idle.err"
grep -Fx 'culled name=gate-vava pane=p-seat close=idle-shell tab=gone cwd=/srv/seat/app foreground=zsh' "$TEST_ROOT/cull-idle.out" >/dev/null \
  || fail "cull did not close and report the seat's leftover idle shell"
grep -Fx 'herdr pane close p-seat' "$FLEET_TEST_CALLS" >/dev/null || fail "cull did not close the idle-shell pane"
! grep -F 'herdr pane close p-other' "$FLEET_TEST_CALLS" >/dev/null || fail "cull touched another seat's pane"
! grep -F 'herdr tab close' "$FLEET_TEST_CALLS" >/dev/null || fail "cull closed a tab herdr had already removed"
grep -F 'register culled --name gate-vava --pane p-seat --close idle-shell' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "idle-shell cull outcome was not registered"
pass "cull closes the seat's leftover idle-shell pane"

rm -f "$cull_state"/*
: >"$FLEET_TEST_CALLS"
if FLEET_TEST_CULL_MODE=busy FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-busy.out" 2>"$TEST_ROOT/cull-busy.err"; then
  fail "cull accepted a leftover pane that runs something other than its shell"
fi
grep -F 'remaining pane p-seat kept: not an idle shell (cwd=/srv/seat/app foreground=node)' \
  "$TEST_ROOT/cull-busy.err" >/dev/null || fail "busy-pane refusal did not name its cwd and foreground"
! grep -F 'herdr pane close' "$FLEET_TEST_CALLS" >/dev/null || fail "cull closed a busy pane"
! grep -F 'register culled' "$FLEET_TEST_CALLS" >/dev/null || fail "busy-pane refusal registered a cull"
pass "cull keeps a busy leftover pane and dies naming it"

rm -f "$cull_state"/*
: >"$FLEET_TEST_CALLS"
FLEET_TEST_CULL_MODE=emptytab FLEET_TEST_CULL_STATE="$cull_state" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-emptytab.out" 2>"$TEST_ROOT/cull-emptytab.err"
grep -Fx 'culled name=gate-vava pane=p-seat close=idle-shell tab=kept cwd=/srv/seat/app foreground=zsh' "$TEST_ROOT/cull-emptytab.out" >/dev/null \
  || fail "cull did not report the surviving tab as kept"
! grep -F 'herdr tab close' "$FLEET_TEST_CALLS" >/dev/null || fail "cull closed a tab"
pass "cull never closes a tab and reports one herdr kept as tab=kept"

# Every refusal after kill keeps the pane, registers no cull and names why.
for refusal in \
  'fallback-busy|remaining pane p-fallback kept: not an idle shell (cwd=/srv/seat/app foreground=node)' \
  'moved|expected pane p-seat remains but the exact label is now on p-moved; refusing to cull either' \
  'foreign|remaining pane p-seat kept: it carries another agent'"'"'s label: ◉ gate-kemo [claude]' \
  'claimed|remaining pane p-seat kept: claimed by seat gate-kemo' \
  'malformed|remaining pane p-seat kept: idle shell unproven, process info malformed (cwd=/srv/seat/app foreground=unknown)' \
  'zeropid|remaining pane p-seat kept: idle shell unproven, process info malformed' \
  'flip|remaining pane p-seat kept: it changed while being checked (cwd=/srv/seat/app foreground=node)'; do
  mode=${refusal%%|*}
  reason=${refusal#*|}
  rm -f "$cull_state"/*
  : >"$FLEET_TEST_CALLS"
  if FLEET_TEST_CULL_MODE=$mode FLEET_TEST_CULL_STATE="$cull_state" \
    PATH="$TEST_ROOT/bin:$PATH" "$FLEET/cull.sh" vava >"$TEST_ROOT/cull-$mode.out" 2>"$TEST_ROOT/cull-$mode.err"; then
    fail "cull $mode closed a pane it should have kept"
  fi
  grep -F -- "$reason" "$TEST_ROOT/cull-$mode.err" >/dev/null \
    || { cat "$TEST_ROOT/cull-$mode.err" >&2; fail "cull $mode did not explain its refusal"; }
  ! grep -F 'herdr pane close' "$FLEET_TEST_CALLS" >/dev/null || fail "cull $mode closed a pane"
  ! grep -F 'herdr tab close' "$FLEET_TEST_CALLS" >/dev/null || fail "cull $mode closed a tab"
  ! grep -F 'register culled' "$FLEET_TEST_CALLS" >/dev/null || fail "cull $mode registered a cull"
done
[[ $(grep -c 'herdr pane process-info --pane p-seat' "$FLEET_TEST_CALLS") -eq 2 ]] \
  || fail "flip cull did not re-read process info before closing"
pass "cull keeps labelled busy, moved-label, foreign, claimed, malformed and flipped panes"

# Worktree servers. Fake postmasters and Valkeys are copies of bash (so
# /proc/<pid>/comm reads postgres / valkey-server) blocked on a fifo, with
# their cwd in the data dir. The fake mise, pg_ctl and valkey-cli only ever
# signal pids listed in $db_root/pids, so no real server can be touched.
db_root=$(realpath -- "$TEST_ROOT")/db
mkdir -p -- "$db_root/fakebin" "$db_root/bin" "$db_root/tools"
cp -- "$(command -v bash)" "$db_root/fakebin/postgres"
cp -- "$(command -v bash)" "$db_root/fakebin/valkey-server"
mkfifo -- "$db_root/hold"
: >"$db_root/pids"
stop_test_servers() {
  local _kind _dir pid _port
  [[ -f ${db_root:-}/pids ]] || return 0
  while read -r _kind _dir pid _port; do kill "$pid" 2>/dev/null || true; done <"$db_root/pids"
}
trap 'stop_test_servers; rm -rf -- "$TEST_ROOT"' EXIT
# start_server KIND DIR PORT [-D ARG]: start a fake server with its cwd in DIR.
start_server() {
  local kind=$1 dir=$2 port=$3 darg=${4:-$2} pid i
  mkdir -p -- "$dir"
  if [[ $kind == postgres ]]; then
    # shellcheck disable=SC2016
    (cd -- "$dir" && exec "$db_root/fakebin/postgres" -c 'exec 3<>"$0"; read -r _ <&3' "$db_root/hold" -D "$darg" -p "$port") </dev/null >/dev/null 2>&1 &
  else
    # shellcheck disable=SC2016
    (cd -- "$dir" && exec "$db_root/fakebin/valkey-server" -c 'exec 3<>"$0"; read -r _ <&3' "$db_root/hold" "127.0.0.1:$port") </dev/null >/dev/null 2>&1 &
  fi
  pid=$!
  for ((i = 0; i < 50; i++)); do
    [[ $(cat "/proc/$pid/comm" 2>/dev/null) == "${kind/valkey/valkey-server}" ]] && break
    sleep 0.1
  done
  [[ $kind != postgres ]] || printf '%s\n%s\n0\n%s\n' "$pid" "$dir" "$port" >"$dir/postmaster.pid"
  printf '%s %s %s %s\n' "$kind" "$dir" "$pid" "$port" >>"$db_root/pids"
  printf '%s\n' "$pid"
}
running() { kill -0 "$1" 2>/dev/null; }
cat >"$db_root/kill-fake" <<'EOF'
#!/usr/bin/env bash
# kill-fake KIND FIELD VALUE: signal the listed fake whose dir (field 2) or
# port (field 4) matches; nothing else.
awk -v k="$1" -v f="$2" -v v="$3" '$1 == k && $f == v {print $3}' "$FLEET_TEST_DB_PIDS" \
  | while read -r pid; do kill "$pid" 2>/dev/null || true; done
EOF
cat >"$db_root/bin/mise" <<'EOF'
#!/usr/bin/env bash
printf 'mise cwd=%s %s\n' "$PWD" "$*" >>"$FLEET_TEST_CALLS"
case "$*" in
  'run db-stop' | 'run valkey-stop')
    kind=postgres; [[ $2 == db-stop ]] || kind=valkey
    case ${FLEET_TEST_MISE:-ok} in
      ok) "$FLEET_TEST_DB_ROOT/kill-fake" "$kind" 2 "$PWD/data/$kind" ;;
      noop) ;;
      *) exit 1 ;;
    esac
    ;;
  'which pg_ctl' | 'which valkey-cli') printf '%s\n' "$FLEET_TEST_DB_ROOT/tools/$2" ;;
  *) exit 64 ;;
esac
EOF
cat >"$db_root/tools/pg_ctl" <<'EOF'
#!/usr/bin/env bash
printf 'pg_ctl %s\n' "$*" >>"$FLEET_TEST_CALLS"
[[ ${FLEET_TEST_DB_TOOL:-ok} == ok && $1 == stop && $6 == -D ]] || exit 1
"$FLEET_TEST_DB_ROOT/kill-fake" postgres 2 "$7"
EOF
cat >"$db_root/tools/valkey-cli" <<'EOF'
#!/usr/bin/env bash
printf 'valkey-cli %s\n' "$*" >>"$FLEET_TEST_CALLS"
port=$4
case "${5:-} ${6:-} ${7:-}" in
  'config get dir') printf 'dir\n%s\n' "$(awk -v p="$port" '$1 == "valkey" && $4 == p {print $2}' "$FLEET_TEST_DB_PIDS")" ;;
  'shutdown nosave ') [[ ${FLEET_TEST_DB_TOOL:-ok} != ok ]] || "$FLEET_TEST_DB_ROOT/kill-fake" valkey 4 "$port" ;;
esac
EOF
chmod +x "$db_root/kill-fake" "$db_root/bin/mise" "$db_root/tools/pg_ctl" "$db_root/tools/valkey-cli"

repo=$db_root/repo
git init -q "$repo"
git -C "$repo" -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m init
for wt in solo shared nodb fallback fail p18 orphan gone owned; do
  git -C "$repo" worktree add -q --detach "$db_root/wt-$wt"
done
p18=$(getent passwd "$(id -u)" | cut -d: -f6)/.local/share/boomerang/postgres18
mkdir -p -- "$db_root/wt-p18/data" "$db_root/elsewhere/valkey"
ln -s -- "$p18" "$db_root/wt-p18/data/postgres"
ln -s -- "$db_root/elsewhere/valkey" "$db_root/wt-p18/data/valkey"

# db_cull OUT [ENV...]: cull the managed seat with the fakes on PATH.
db_cull() {
  local out=$1
  shift
  rm -f -- "${cull_state:?}"/*
  : >"$FLEET_TEST_CALLS"
  env FLEET_TEST_CULL_MODE=managed FLEET_TEST_CULL_STATE="$cull_state" FLEET_TEST_DB_ROOT="$db_root" \
    FLEET_TEST_DB_PIDS="$db_root/pids" "$@" PATH="$db_root/bin:$TEST_ROOT/bin:$PATH" \
    "$FLEET/cull.sh" vava >"$TEST_ROOT/$out.out" 2>"$TEST_ROOT/$out.err" \
    || { cat "$TEST_ROOT/$out.err" >&2; fail "cull $out failed"; }
  grep -Fx 'culled name=gate-vava pane=p-managed close=managed tab=gone' "$TEST_ROOT/$out.out" >/dev/null \
    || { cat "$TEST_ROOT/$out.out" >&2; fail "cull $out did not complete"; }
}

solo_pg=$(start_server postgres "$db_root/wt-solo/data/postgres" 15901)
solo_vk=$(start_server valkey "$db_root/wt-solo/data/valkey" 16901)
mkdir -p -- "$db_root/wt-solo/app"
db_cull cull-db-solo FLEET_TEST_SEAT_DIR="$db_root/wt-solo/app"
grep -Fx 'db=stopped' "$TEST_ROOT/cull-db-solo.out" >/dev/null || fail "cull did not stop the sole-owner worktree Postgres"
grep -Fx 'valkey=stopped' "$TEST_ROOT/cull-db-solo.out" >/dev/null || fail "cull did not stop the sole-owner worktree Valkey"
if running "$solo_pg" || running "$solo_vk"; then fail "cull left the sole-owner worktree servers running"; fi
stop_line=$(grep -n "mise cwd=$db_root/wt-solo run db-stop" "$FLEET_TEST_CALLS" | cut -d: -f1)
kill_line=$(grep -n 'hcom .* kill gate-vava' "$FLEET_TEST_CALLS" | cut -d: -f1)
[[ -n $stop_line && -n $kill_line && $stop_line -lt $kill_line ]] || fail "cull did not run db-stop before the pane closed"
grep -F "mise cwd=$db_root/wt-solo run valkey-stop" "$FLEET_TEST_CALLS" >/dev/null || fail "cull did not run valkey-stop"
! grep -E '^(pg_ctl|valkey-cli) ' "$FLEET_TEST_CALLS" >/dev/null || fail "cull fell back although mise stopped both servers"
pass "cull stops a sole-owner linked worktree's Postgres and Valkey through mise before the pane closes"

main_pg=$(start_server postgres "$repo/data/postgres" 15902)
db_cull cull-db-main FLEET_TEST_SEAT_DIR="$repo"
grep -Fx "db=skipped(not a linked worktree: $repo)" "$TEST_ROOT/cull-db-main.out" >/dev/null || fail "cull did not skip a main checkout's Postgres"
grep -Fx "valkey=skipped(not a linked worktree: $repo)" "$TEST_ROOT/cull-db-main.out" >/dev/null || fail "cull did not skip a main checkout's Valkey"
running "$main_pg" || fail "cull stopped a main checkout's Postgres"
! grep -E '^(mise|pg_ctl|valkey-cli) ' "$FLEET_TEST_CALLS" >/dev/null || fail "cull ran a stop in a main checkout"

shared_pg=$(start_server postgres "$db_root/wt-shared/data/postgres" 15903)
db_cull cull-db-seat FLEET_TEST_SEAT_DIR="$db_root/wt-shared" FLEET_TEST_OTHER_SEAT_DIR="$db_root/wt-shared/sub"
grep -Fx "db=skipped($db_root/wt-shared shared with seat:gate-mura)" "$TEST_ROOT/cull-db-seat.out" >/dev/null \
  || fail "cull did not skip a worktree another seat is in"
db_cull cull-db-pane FLEET_TEST_SEAT_DIR="$db_root/wt-shared" FLEET_TEST_OTHER_PANE_DIR="$db_root/wt-shared"
grep -Fx "db=skipped($db_root/wt-shared shared with pane:p-shell)" "$TEST_ROOT/cull-db-pane.out" >/dev/null \
  || fail "cull did not skip a worktree another pane is in"
running "$shared_pg" || fail "cull stopped a shared worktree's Postgres"
! grep -E '^(mise|pg_ctl|valkey-cli) ' "$FLEET_TEST_CALLS" >/dev/null || fail "cull ran a stop in a shared worktree"

db_cull cull-db-nodb FLEET_TEST_SEAT_DIR="$db_root/wt-nodb"
grep -Fx 'db=not-running' "$TEST_ROOT/cull-db-nodb.out" >/dev/null || fail "cull did not report a worktree with no Postgres"
grep -Fx 'valkey=not-running' "$TEST_ROOT/cull-db-nodb.out" >/dev/null || fail "cull did not report a worktree with no Valkey"
! grep -E '^(mise|pg_ctl|valkey-cli) ' "$FLEET_TEST_CALLS" >/dev/null || fail "cull ran a stop with no server running"
pass "cull skips a main checkout, a worktree another seat or pane is in, and a worktree with no running server"

fb_pg=$(start_server postgres "$db_root/wt-fallback/data/postgres" 15904)
fb_vk=$(start_server valkey "$db_root/wt-fallback/data/valkey" 16904)
db_cull cull-db-fallback FLEET_TEST_SEAT_DIR="$db_root/wt-fallback" FLEET_TEST_MISE=noop
grep -Fx 'db=stopped' "$TEST_ROOT/cull-db-fallback.out" >/dev/null || fail "cull did not fall back to pg_ctl"
grep -Fx 'valkey=stopped' "$TEST_ROOT/cull-db-fallback.out" >/dev/null || fail "cull did not fall back to valkey-cli"
grep -Fx "pg_ctl stop -m fast -t 30 -D $db_root/wt-fallback/data/postgres" "$FLEET_TEST_CALLS" >/dev/null \
  || fail "cull did not run the mise-resolved pg_ctl stop -m fast on the worktree data dir"
grep -Fx 'valkey-cli -h 127.0.0.1 -p 16904 config get dir' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "cull did not prove the Valkey port's data dir before shutting it down"
grep -Fx 'valkey-cli -h 127.0.0.1 -p 16904 shutdown nosave' "$FLEET_TEST_CALLS" >/dev/null || fail "cull did not shut down the Valkey"
if running "$fb_pg" || running "$fb_vk"; then fail "fallback left the worktree servers running"; fi
pass "cull falls back to pg_ctl -m fast and a dir-proven valkey-cli shutdown when mise leaves them running"

fail_pg=$(start_server postgres "$db_root/wt-fail/data/postgres" 15905)
fail_vk=$(start_server valkey "$db_root/wt-fail/data/valkey" 16905)
db_cull cull-db-fail FLEET_TEST_SEAT_DIR="$db_root/wt-fail" FLEET_TEST_MISE=fail FLEET_TEST_DB_TOOL=fail
grep -Fx "db=failed(still running pid=$fail_pg after mise run db-stop and pg_ctl)" "$TEST_ROOT/cull-db-fail.out" >/dev/null \
  || fail "cull did not report a Postgres it could not stop"
grep -Fx "valkey=failed(still running pid=$fail_vk after mise run valkey-stop and valkey-cli)" "$TEST_ROOT/cull-db-fail.out" >/dev/null \
  || fail "cull did not report a Valkey it could not stop"
grep -F 'register culled --name gate-vava --pane p-managed --close managed' "$FLEET_TEST_CALLS" >/dev/null \
  || fail "a stop failure blocked the cull"
pass "a server stop failure is reported as failed and the cull still completes"

db_cull cull-db-p18 FLEET_TEST_SEAT_DIR="$db_root/wt-p18"
grep -Fx "db=skipped(refused $db_root/wt-p18/data/postgres: not exactly the worktree data dir)" "$TEST_ROOT/cull-db-p18.out" >/dev/null \
  || fail "cull did not refuse a data/postgres that resolves to the postgres18 store"
grep -Fx "valkey=skipped(refused $db_root/wt-p18/data/valkey: not exactly the worktree data dir)" "$TEST_ROOT/cull-db-p18.out" >/dev/null \
  || fail "cull did not refuse a data/valkey that resolves out of the worktree"
! grep -E '^(mise|pg_ctl|valkey-cli) ' "$FLEET_TEST_CALLS" >/dev/null || fail "cull ran a stop against a refused data dir"
! grep -F postgres18 "$FLEET_TEST_CALLS" >/dev/null || fail "cull passed the postgres18 store to a tool"
pass "cull refuses any data dir but exactly <worktree>/data/<name>, so postgres18 is never touched"

orphan_pg=$(start_server postgres "$db_root/wt-orphan/data/postgres" 15906)
orphan_vk=$(start_server valkey "$db_root/wt-orphan/data/valkey" 16906)
gone_pg=$(start_server postgres "$db_root/wt-gone/data/postgres" 15907)
gone_vk=$(start_server valkey "$db_root/wt-gone/data/valkey" 16907)
owned_pg=$(start_server postgres "$db_root/wt-owned/data/postgres" 15908)
owned_vk=$(start_server valkey "$db_root/wt-owned/data/valkey" 16908)
p18_pg=$(start_server postgres "$db_root/wt-nodb/data/postgres" 15909 "$p18")
rm -rf -- "${db_root:?}/wt-gone"
FLEET_TEST_CULL_MODE=managed FLEET_TEST_CULL_STATE="$cull_state" FLEET_TEST_OTHER_PANE_DIR="$db_root/wt-owned/app" \
  PATH="$TEST_ROOT/bin:$PATH" "$FLEET/drift.sh" >"$TEST_ROOT/drift-db.out" 2>"$TEST_ROOT/drift-db.err" \
  || { cat "$TEST_ROOT/drift-db.err" >&2; fail "drift failed"; }
for line in \
  "orphan-db pid=$orphan_pg port=15906 data=$db_root/wt-orphan/data/postgres" \
  "orphan-valkey pid=$orphan_vk port=16906 dir=$db_root/wt-orphan/data/valkey" \
  "orphan-db pid=$gone_pg port=15907 data=$db_root/wt-gone/data/postgres (gone)" \
  "orphan-valkey pid=$gone_vk port=16907 dir=$db_root/wt-gone/data/valkey (gone)"; do
  grep -Fx -- "$line" "$TEST_ROOT/drift-db.out" >/dev/null || { cat "$TEST_ROOT/drift-db.out" >&2; fail "drift did not list: $line"; }
done
for pid in "$owned_pg" "$owned_vk" "$main_pg" "$p18_pg"; do
  ! grep -E "^orphan-(db|valkey) pid=$pid " "$TEST_ROOT/drift-db.out" >/dev/null || fail "drift listed an owned, main-checkout or protected server: pid $pid"
done
! grep -E '^orphan-.*postgres18' "$TEST_ROOT/drift-db.out" >/dev/null || fail "drift listed the postgres18 store"
if ! running "$orphan_pg" || ! running "$gone_vk"; then fail "drift stopped a server"; fi
pass "drift lists orphan and gone worktree Postgres and Valkey, and ignores owned, main-checkout and postgres18 servers"

# prune-build-cache proves its mbx settings, then only previews unless the
# caller passes --apply.
mkdir -p -- "$TEST_ROOT/mbx" "$TEST_ROOT/not-a-mount"
cat >"$TEST_ROOT/mbx/mbx" <<'EOF'
#!/usr/bin/env bash
if [[ $1 == settings && $2 == get ]]; then
  case $3 in
    cache_dir) printf '%s\n' "${FAKE_MBX_CACHE_DIR-/srv/cache/mbx}" ;;
    gc.auto) printf '%s\n' "${FAKE_MBX_GC_AUTO-false}" ;;
    gc.max_total_size) printf '%s\n' "${FAKE_MBX_MAX_TOTAL-400GiB}" ;;
    gc.min_free_size) printf '%s\n' "${FAKE_MBX_MIN_FREE-100GiB}" ;;
    *) exit 2 ;;
  esac
  exit 0
fi
printf 'mbx %s\n' "$*" >>"$FLEET_TEST_CALLS"
EOF
chmod +x "$TEST_ROOT/mbx/mbx"
prune() {
  MBX_BIN="$TEST_ROOT/mbx/mbx" CACHE_MOUNT=/ "$FLEET/prune-build-cache.sh" "$@"
}
: >"$FLEET_TEST_CALLS"
prune >/dev/null
[[ $(cat "$FLEET_TEST_CALLS") == 'mbx gc --dry-run' ]] || fail "prune without --apply did more than preview"
: >"$FLEET_TEST_CALLS"
prune --apply >/dev/null
[[ $(cat "$FLEET_TEST_CALLS") == 'mbx gc' ]] || fail "prune --apply did not run mbx gc"
for args in '--dry-run' '--apply --apply' 'apply'; do
  : >"$FLEET_TEST_CALLS"
  # shellcheck disable=SC2086
  if prune $args >/dev/null 2>&1; then
    fail "prune accepted '$args'"
  fi
  [[ ! -s $FLEET_TEST_CALLS ]] || fail "prune ran mbx for '$args'"
done
: >"$FLEET_TEST_CALLS"
if MBX_BIN="$TEST_ROOT/mbx/mbx" CACHE_MOUNT="$TEST_ROOT/not-a-mount" "$FLEET/prune-build-cache.sh" --apply >/dev/null 2>&1; then
  fail "prune ran with the cache drive unmounted"
fi
[[ ! -s $FLEET_TEST_CALLS ]] || fail "prune ran mbx with the cache drive unmounted"
for refusal in \
  '/|FAKE_MBX_CACHE_DIR=|cache_dir is unset' \
  '/proc|FAKE_MBX_CACHE_DIR=/srv/cache/mbx|cache_dir /srv/cache/mbx is not under /proc' \
  '/|FAKE_MBX_GC_AUTO=true|gc.auto is not false' \
  '/|FAKE_MBX_MAX_TOTAL=|gc.max_total_size is unset' \
  '/|FAKE_MBX_MIN_FREE=|gc.min_free_size is unset'; do
  mount=${refusal%%|*}
  setting=${refusal#*|}
  reason=${setting#*|}
  setting=${setting%%|*}
  for args in '' '--apply'; do
    : >"$FLEET_TEST_CALLS"
    # shellcheck disable=SC2086
    if env "$setting" MBX_BIN="$TEST_ROOT/mbx/mbx" CACHE_MOUNT="$mount" \
      "$FLEET/prune-build-cache.sh" $args >/dev/null 2>"$TEST_ROOT/prune.err"; then
      fail "prune ran with $setting"
    fi
    grep -F -- "$reason" "$TEST_ROOT/prune.err" >/dev/null || fail "prune did not explain refusing $setting"
    [[ ! -s $FLEET_TEST_CALLS ]] || fail "prune ran mbx gc with $setting"
  done
done
# Without MBX_BIN it asks mise for mbx, then falls back to PATH.
mkdir -p -- "$TEST_ROOT/mbx-mise" "$TEST_ROOT/mbx-path"
cat >"$TEST_ROOT/mbx-mise/mise" <<EOF
#!/usr/bin/env bash
[[ \$* == 'which mbx' && -z \${FAKE_MISE_NO_MBX:-} ]] || exit 1
printf '%s\n' "$TEST_ROOT/mbx/mbx"
EOF
chmod +x "$TEST_ROOT/mbx-mise/mise"
ln -sf -- "$TEST_ROOT/mbx/mbx" "$TEST_ROOT/mbx-path/mbx"
: >"$FLEET_TEST_CALLS"
env -u MBX_BIN PATH="$TEST_ROOT/mbx-mise:$PATH" CACHE_MOUNT=/ "$FLEET/prune-build-cache.sh" >/dev/null
[[ $(cat "$FLEET_TEST_CALLS") == 'mbx gc --dry-run' ]] || fail "prune did not find mbx through mise"
: >"$FLEET_TEST_CALLS"
env -u MBX_BIN FAKE_MISE_NO_MBX=1 PATH="$TEST_ROOT/mbx-mise:$TEST_ROOT/mbx-path:$PATH" CACHE_MOUNT=/ \
  "$FLEET/prune-build-cache.sh" >/dev/null
[[ $(cat "$FLEET_TEST_CALLS") == 'mbx gc --dry-run' ]] || fail "prune did not fall back to mbx on PATH"
: >"$FLEET_TEST_CALLS"
if env -u MBX_BIN FAKE_MISE_NO_MBX=1 PATH="$TEST_ROOT/mbx-mise:/usr/bin:/bin" CACHE_MOUNT=/ \
  "$FLEET/prune-build-cache.sh" >/dev/null 2>"$TEST_ROOT/prune.err"; then
  fail "prune ran without any mbx"
fi
grep -F 'mbx not found' "$TEST_ROOT/prune.err" >/dev/null || fail "prune did not explain a missing mbx"
[[ ! -s $FLEET_TEST_CALLS ]] || fail "prune ran mbx without finding one"
pass "prune-build-cache finds mbx through mise or PATH, proves its settings, previews by default, prunes only with --apply, and refuses bad arguments or an unmounted drive"

printf 'ALL GREEN - fleet wrapper contract holds.\n'
