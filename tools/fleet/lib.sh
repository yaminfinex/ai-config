#!/usr/bin/env bash

# Print hcom's current seat name when a direct fleet invocation can prove it.
# A serve-provided launcher is already authoritative, and lookup failure is
# deliberately silent so herder register can apply its own best-effort default.
fleet_self_name() {
  local name

  [[ -z ${FLEET_LAUNCHER:-} ]] || return 0
  [[ -n ${HCOM_PROCESS_ID:-} ]] || return 0
  name=$(timeout --foreground 2s hcom list self --json 2>/dev/null \
    | jq -r '.name // empty' 2>/dev/null) || return 0
  printf '%s\n' "$name"
}

# Classify a `herdr pane process-info` JSON document. Returns 0 when the pane
# holds only its idle shell (every foreground process is the shell itself, or
# there is none), 1 when anything else runs in the foreground, and 2 when the
# document omits shell_pid so idleness cannot be proven.
fleet_idle_shell() {
  local process_output=$1

  jq -e '.result.process_info.shell_pid // empty' <<<"$process_output" >/dev/null 2>&1 || return 2
  if jq -e '
      .result.process_info as $info
      | any(($info.foreground_processes // [])[];
          . as $process
          | $process.pid != $info.shell_pid
            or (["bash", "dash", "fish", "ksh", "nu", "sh", "xonsh", "zsh"] | index($process.name)) == null)
    ' <<<"$process_output" >/dev/null; then
    return 1
  fi
  return 0
}

# Print the foreground command names of a process-info document, comma-joined,
# or "none" when the pane reports no foreground process.
fleet_foreground_names() {
  jq -r '[.result.process_info.foreground_processes[]?.name] | if length == 0 then "none" else join(",") end' <<<"$1"
}

# Print how many panes a `herdr pane list` document places in one tab.
fleet_tab_pane_count() {
  local panes=$1 tab=$2

  jq -r --arg tab "$tab" '[.result.panes[]? | select(.tab_id == $tab)] | length' <<<"$panes"
}
