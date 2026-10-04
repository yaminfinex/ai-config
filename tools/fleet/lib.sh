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

# Classify a `herdr pane process-info` JSON document, failing closed. Returns 0
# only for proven idleness: shell_pid is a positive integer and every
# foreground process (objects with a numeric pid and string name, or bare
# pids) is that shell, or foreground_processes is absent/null/empty. Returns 1
# when something else holds the foreground, and 2 for anything unprovable:
# missing or invalid shell_pid, a malformed foreground collection or element,
# or a document jq cannot parse or evaluate.
fleet_idle_shell() {
  local verdict

  verdict=$(jq -r '
    def shells: ["bash", "dash", "fish", "ksh", "nu", "sh", "xonsh", "zsh"];
    .result.process_info as $info
    | if ($info | type) != "object" then "unknown"
      else $info.shell_pid as $shell
      | if ($shell | type) != "number" or $shell <= 0 or ($shell | floor) != $shell then "unknown"
        else $info.foreground_processes as $fg
        | if $fg == null then "idle"
          elif ($fg | type) != "array" then "unknown"
          else [$fg[]
              | if type == "number" then
                  (if . == $shell then "idle" else "busy" end)
                elif type == "object" and (.pid | type) == "number" and (.name | type) == "string" then
                  (. as $process
                   | if $process.pid == $shell and any(shells[]; . == $process.name) then "idle" else "busy" end)
                else "unknown" end]
            | if any(.[]; . == "unknown") then "unknown"
              elif any(.[]; . == "busy") then "busy"
              else "idle" end
          end
        end
      end
  ' <<<"$1" 2>/dev/null) || return 2
  case $verdict in
    idle) return 0 ;;
    busy) return 1 ;;
    *) return 2 ;;
  esac
}

# Print a process-info document's shell_pid, or nothing when it is absent.
fleet_shell_pid() {
  jq -r '.result.process_info.shell_pid // empty' <<<"$1" 2>/dev/null || true
}

# Print the foreground command names of a process-info document, comma-joined,
# "none" when the pane reports no foreground process, or "unknown" when the
# document is malformed.
fleet_foreground_names() {
  jq -r '
    .result.process_info.foreground_processes
    | if . == null or . == [] then "none"
      elif type == "array" then map(if type == "object" then (.name // "?") | tostring else tostring end) | join(",")
      else "unknown" end
  ' <<<"$1" 2>/dev/null || printf 'unknown\n'
}

# Print how many panes a `herdr pane list` document places in one tab. Fails
# when the document carries no panes array, so a malformed list never reads
# as an empty tab.
fleet_tab_pane_count() {
  local panes=$1 tab=$2

  jq -er --arg tab "$tab" '
    if (.result.panes | type) == "array" then [.result.panes[] | select(.tab_id == $tab)] | length
    else error("no panes array") end
  ' <<<"$panes" 2>/dev/null
}
