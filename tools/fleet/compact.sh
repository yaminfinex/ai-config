#!/usr/bin/env bash
# Compact ANOTHER seat and prove it happened. Foreground; the caller reads
# the exit code. Sibling of selfcompact.sh (which is for the caller's own
# composer and cannot verify anything because the caller itself compacts).
#
# Why the guards exist (2026-09-14, riko on moki): a `/compact …` typed into
# a composer within seconds of a bus delivery is submitted as a plain prompt
# and answered as a steer; the seat never compacts and its context keeps
# climbing. The retry after an empty idle composer compacted normally. So:
#   1. wait until the seat is listening AND hcom's screen dump says the
#      composer is ready and empty AND the screen has not changed for a
#      settle window (no delivery, no typing);
#   2. inject "/compact " on its own, then the steer, then enter (Claude Code
#      2.1.257+ paste parsing; same split as selfcompact.sh);
#   3. latch busy-then-listening;
#   4. require the status line's context figure to drop (Claude "NNNk / NNNk"
#      or Codex "Context NN% left") before the continuation is sent; without
#      a drop nothing is sent and the exit code says so, because a
#      continuation into an uncompacted seat is just another prompt.
#
# usage: compact.sh <hcom-name> <steer-text> <continuation-text>
# env:   FLEET_COMPACT_SETTLE_SECONDS (30)  quiet composer required before typing
#        FLEET_COMPACT_WAIT_SECONDS   (600) bound on waiting for that quiet
#        FLEET_COMPACT_LATCH_SECONDS  (1800) bound on the busy-then-listening latch
#        FLEET_COMPACT_DROP_SECONDS   (120) bound on seeing the context drop
#        FLEET_COMPACT_POLL_SECONDS   (2)   poll cadence

set -euo pipefail

die() {
  printf 'fleet compact: %s\n' "$*" >&2
  exit 1
}

log_line() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

positive_int() {
  [[ $2 =~ ^[1-9][0-9]*$ ]] || die "$1 must be a positive integer"
}

[[ $# -eq 3 ]] || die "usage: compact.sh <hcom-name> <steer-text> <continuation-text>"
name=$1
steer=$2
continuation=$3
[[ -n $name && -n $steer && -n $continuation ]] || die "all three arguments must be non-empty"
[[ $name =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] || die "hcom name contains unsupported characters: $name"
command -v hcom >/dev/null || die "hcom is required"
command -v jq >/dev/null || die "jq is required"

settle=${FLEET_COMPACT_SETTLE_SECONDS:-30}
wait_bound=${FLEET_COMPACT_WAIT_SECONDS:-600}
latch_bound=${FLEET_COMPACT_LATCH_SECONDS:-1800}
drop_bound=${FLEET_COMPACT_DROP_SECONDS:-120}
poll=${FLEET_COMPACT_POLL_SECONDS:-2}
positive_int FLEET_COMPACT_SETTLE_SECONDS "$settle"
positive_int FLEET_COMPACT_WAIT_SECONDS "$wait_bound"
positive_int FLEET_COMPACT_LATCH_SECONDS "$latch_bound"
positive_int FLEET_COMPACT_DROP_SECONDS "$drop_bound"
positive_int FLEET_COMPACT_POLL_SECONDS "$poll"

seat_status() {
  hcom list "$name" status 2>/dev/null || true
}

# Screen dump fields: ready, prompt_empty, input_text, lines[].
screen_json=
read_screen() {
  screen_json=$(hcom term "$name" --json 2>/dev/null) || screen_json=
  [[ -n $screen_json ]] && jq -e 'type == "object"' <<<"$screen_json" >/dev/null 2>&1
}

composer_idle() {
  jq -e '.ready == true and .prompt_empty == true and ((.input_text // "") | length) == 0' <<<"$screen_json" >/dev/null 2>&1
}

screen_text() {
  jq -r '(.lines // []) | join("\n")' <<<"$screen_json"
}

# Context figure on the status line. Prints "<kind> <value>" or nothing:
#   claude <used-k>   from "NNNk / NNNk" (used before the slash; a drop = smaller)
#   codex <left-pct>  from "Context NN% left"          (a drop = larger)
context_figure() {
  local text=$1 used left
  used=$(grep -oE '[0-9]+k */ *[0-9]+k' <<<"$text" | tail -n 1 | sed -E 's/^([0-9]+)k.*/\1/')
  if [[ -n $used ]]; then
    printf 'claude %s\n' "$used"
    return 0
  fi
  left=$(grep -oE 'Context [0-9]+% left' <<<"$text" | tail -n 1 | sed -E 's/Context ([0-9]+)% left/\1/')
  if [[ -n $left ]]; then
    printf 'codex %s\n' "$left"
    return 0
  fi
  return 1
}

dropped() {
  local kind=$1 before=$2 after=$3
  case $kind in
    claude) ((after < before)) ;;
    codex) ((after > before)) ;;
    *) return 1 ;;
  esac
}

# 1. Quiet composer: listening, ready, empty input, screen unchanged for $settle.
log_line "waiting for a quiet composer on $name (settle ${settle}s, bound ${wait_bound}s)"
deadline=$((SECONDS + wait_bound))
quiet_since=
quiet_screen=
baseline_text=
while ((SECONDS < deadline)); do
  status=$(seat_status)
  if [[ $status == listening ]] && read_screen && composer_idle; then
    text=$(screen_text)
    if [[ -n $quiet_since && $text == "$quiet_screen" ]]; then
      if ((SECONDS - quiet_since >= settle)); then
        baseline_text=$text
        break
      fi
    else
      quiet_since=$SECONDS
      quiet_screen=$text
    fi
  else
    quiet_since=
    quiet_screen=
    log_line "not quiet yet (status=${status:-none}, composer $(composer_idle 2>/dev/null && printf idle || printf busy))"
  fi
  sleep "$poll"
done
[[ -n $baseline_text ]] || die "$name never showed a quiet composer within ${wait_bound}s; nothing injected"

baseline_kind=
baseline_value=
if figure=$(context_figure "$baseline_text"); then
  baseline_kind=${figure%% *}
  baseline_value=${figure#* }
  log_line "baseline context: $baseline_kind $baseline_value"
else
  log_line "warning: no context figure on the status line; the drop cannot be verified, the latch alone will decide"
fi

# 2. Split injection (see selfcompact.sh for the 2.1.257 paste-parsing proof).
log_line "injecting compact request (${#steer} steer chars, split prefix)"
hcom term inject "$name" "/compact " || die "injecting the /compact prefix failed"
sleep 1
hcom term inject "$name" "$steer" || die "injecting the steer failed"
sleep 1
hcom term inject "$name" --enter || die "submitting the compact failed"

# 3. Busy-then-listening latch.
deadline=$((SECONDS + latch_bound))
seen_busy=0
empty_reads=0
latched=0
while ((SECONDS < deadline)); do
  status=$(seat_status)
  if [[ -z $status ]]; then
    empty_reads=$((empty_reads + 1))
    ((empty_reads < 3)) || die "$name status stayed empty after the inject; continuation NOT sent"
  elif [[ $status == active ]]; then
    empty_reads=0
    seen_busy=1
  elif ((seen_busy == 1)) && [[ $status == listening ]]; then
    latched=1
    break
  elif [[ $status == blocked || $status == inactive ]]; then
    die "$name entered terminal status '$status' before compaction settled; continuation NOT sent"
  else
    empty_reads=0
  fi
  sleep "$poll"
done
((latched == 1)) || die "timed out after ${latch_bound}s waiting for $name to go busy then listening; continuation NOT sent"
log_line "latch complete"

# 4. Proof: the context figure must drop before anything else is typed.
if [[ -n $baseline_kind ]]; then
  deadline=$((SECONDS + drop_bound))
  after_value=unreadable
  drop_seen=0
  while ((SECONDS < deadline)); do
    if read_screen && figure=$(context_figure "$(screen_text)"); then
      after_value=${figure#* }
      if dropped "$baseline_kind" "$baseline_value" "$after_value"; then
        drop_seen=1
        break
      fi
    fi
    sleep "$poll"
  done
  ((drop_seen == 1)) || die "no context drop on $name ($baseline_kind $baseline_value before, $after_value after ${drop_bound}s): the /compact was answered as a prompt; continuation NOT sent"
  log_line "context dropped: $baseline_kind $baseline_value -> $after_value"
fi

# 5. Continuation by bus send (another seat can receive one; only self cannot).
hcom send "@$name" --intent request -- "$continuation" >/dev/null \
  || die "compaction verified but the continuation send failed; send it yourself"
log_line "continuation sent to $name"
