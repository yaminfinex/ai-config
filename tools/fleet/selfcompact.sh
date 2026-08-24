#!/usr/bin/env bash
# Detached self-compaction helper. This deliberately DOUBLE-INJECTS into the
# caller's own composer: first `/compact <steer>`, then the continuation. A
# self-addressed hcom send reroutes to the owner's mailbox, so it cannot carry
# this wake-up. The composer queue carries delivery through compaction; the
# busy-then-listening latch only orders the two injections.
# Mid-turn precondition: launch this helper, then end your turn; that turn-end
# supplies the busy phase needed to latch before compaction settles.

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
  local status seen_busy=0 attempts=0 empty_reads=0

  inject_continuation() {
    local reason=$1
    log_line "$reason; injecting continuation"
    if hcom term inject "$name" "$continuation" --enter; then
      log_line "continuation injected"
      return 0
    fi
    log_line "continuation injection failed"
    return 1
  }

  log_line "injecting compact request for $name"
  hcom term inject "$name" "/compact $steer" --enter

  while ((attempts < 900)); do
    status=$(hcom list "$name" status 2>/dev/null || true)
    log_line "status=$status"
    if [[ -z $status ]]; then
      empty_reads=$((empty_reads + 1))
      if ((empty_reads >= 3)); then
        inject_continuation "three consecutive empty status reads" || true
        die "agent status remained empty before the latch completed"
      fi
    elif [[ $status == active ]]; then
      empty_reads=0
      seen_busy=1
    elif ((seen_busy == 1)) && [[ $status == listening ]]; then
      inject_continuation "busy-then-listening latch complete" \
        || die "continuation injection failed after the latch completed"
      return 0
    elif [[ $status == blocked || $status == inactive ]]; then
      inject_continuation "agent entered terminal status '$status'" || true
      die "agent entered terminal status '$status' before the latch completed"
    else
      empty_reads=0
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  inject_continuation "timed out waiting for busy-then-listening latch" || true
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
