#!/usr/bin/env bash
# Detached self-compaction helper. This deliberately DOUBLE-INJECTS into the
# caller's own composer: first `/compact <steer>`, then the continuation. A
# self-addressed hcom send reroutes to the owner's mailbox, so it cannot carry
# this wake-up. The composer queue carries delivery through compaction; the
# busy-then-listening latch only orders the two injections.

set -euo pipefail
umask 077

die() {
  printf 'fleet selfcompact: %s\n' "$*" >&2
  exit 1
}

log_line() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

run_helper() {
  local name=$1 steer=$2 continuation=$3
  local status seen_busy=0 attempts=0

  log_line "injecting compact request for $name"
  hcom term inject "$name" "/compact $steer" --enter

  while ((attempts < 900)); do
    status=$(hcom list "$name" status 2>/dev/null || true)
    log_line "status=$status"
    if [[ $status == active ]]; then
      seen_busy=1
    elif ((seen_busy == 1)) && [[ $status == listening ]]; then
      log_line "busy-then-listening latch complete; injecting continuation"
      hcom term inject "$name" "$continuation" --enter
      log_line "continuation injected"
      return 0
    elif [[ $status == blocked || $status == inactive || -z $status ]]; then
      die "agent entered terminal status '$status' before the latch completed"
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "timed out waiting for busy-then-listening latch"
}

if [[ ${1:-} == --run ]]; then
  [[ $# -eq 4 ]] || die "internal usage: selfcompact.sh --run <name> <steer> <continuation>"
  run_helper "$2" "$3" "$4"
  exit 0
fi

[[ $# -eq 3 ]] || die "usage: selfcompact.sh <own-hcom-name> <steer-text> <continuation-text>"
name=$1
steer=$2
continuation=$3
[[ -n $name && -n $steer && -n $continuation ]] || die "all three arguments must be non-empty"
[[ $name =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] || die "hcom name contains unsupported characters: $name"

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
script_path=$script_dir/$(basename -- "${BASH_SOURCE[0]}")
state_dir=${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/fleet
mkdir -p -- "$state_dir"
log=$state_dir/selfcompact-${name}-$(date -u +%Y%m%dT%H%M%SZ)-$$.log

nohup "$script_path" --run "$name" "$steer" "$continuation" >>"$log" 2>&1 </dev/null &
printf '%s\n' "$log"
