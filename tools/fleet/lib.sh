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
