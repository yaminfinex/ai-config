#!/usr/bin/env bash
# lib/launchers.sh - interactive agent launchers: claude/codex/grok as shell
# functions resolving and executing the vendor CLI directly.
#
# Sourced by the managed rc block bin/ai-setup writes into ~/.bashrc (and
# ~/.zshrc when present). Shell FUNCTIONS, not PATH entries: a function wins
# name resolution over every PATH participant unconditionally, so no PATH
# writer — mise hook-env re-fronting its shims dir on a config-boundary `cd`,
# an installer prepend, stale __MISE_* state inherited from a pane server —
# can reroute a hand-typed `claude` to a version-manager imposter. The
# previous generation intercepted via tools/herder/shims + global PATH
# ordering and lost that race repeatedly (off-bus agents without autonomous
# flags); see docs/launcher-design.md.
#
# Each invocation walks PATH once, skipping old herder shims and every
# mise-owned candidate (including symlink targets), then execs the absolute
# vendor entry point in a child process. Global hcom hooks and herdr's session
# integration bind Claude/Codex automatically; this file adds no registration.
# Scope: interactive shells only. Escape hatch that bypasses default args:
#   command claude ...
#
# Default args are baked here with env override, NOT read from mise [env]
# alone: a shell where mise env never applied must still launch autonomous.
# Export HERDER_SHIM_ARGS_CLAUDE="" (empty) on an ask-mode machine.

_aic_mise_owned() {
  case "$1" in
    */mise/shims/*|*/mise/installs/*) return 0 ;;
  esac
  return 1
}

_aic_realpath() {
  local path="$1" target hops=0
  while [ -L "$path" ] && [ "$hops" -lt 40 ]; do
    target=$(command readlink "$path" 2>/dev/null) || return 1
    case "$target" in
      /*) path=$target ;;
      *) path="$(dirname -- "$path")/$target" ;;
    esac
    hops=$((hops + 1))
  done
  [ "$hops" -lt 40 ] || return 1
  printf '%s/%s\n' "$(cd -- "$(dirname -- "$path")" 2>/dev/null && pwd -P)" "$(basename -- "$path")"
}

_aic_resolve_vendor() {
  local tool="$1" rest="${PATH-}" dir candidate absolute resolved last=0
  [ -n "$tool" ] || return 1
  while [ "$last" -eq 0 ]; do
    case "$rest" in
      *:*) dir=${rest%%:*}; rest=${rest#*:} ;;
      *) dir=$rest; rest=; last=1 ;;
    esac
    [ -n "$dir" ] || dir=.
    candidate="$dir/$tool"
    [ -f "$candidate" ] && [ -x "$candidate" ] || continue
    case "$candidate" in
      /*) absolute=$candidate ;;
      *) absolute="$(cd -- "$(dirname -- "$candidate")" 2>/dev/null && pwd -P)/$(basename -- "$candidate")" ;;
    esac
    [ -n "$absolute" ] || continue
    _aic_mise_owned "$absolute" && continue
    resolved=$(_aic_realpath "$absolute" 2>/dev/null || printf '%s' "$absolute")
    _aic_mise_owned "$resolved" && continue
    if LC_ALL=C command dd if="$absolute" bs=512 count=1 2>/dev/null | LC_ALL=C command grep -aqF 'herder-path-shim'; then
      continue
    fi
    printf '%s\n' "$absolute"
    return 0
  done
  printf "ai-config launcher: no vendor '%s' on PATH after skipping herder shims and mise-owned copies; install the vendor CLI and retry\n" "$tool" >&2
  return 127
}

_aic_launch() {
  local tool="$1" vendor
  shift
  vendor=$(_aic_resolve_vendor "$tool") || return $?
  local args=""
  case "$tool" in
    claude) args="${HERDER_SHIM_ARGS_CLAUDE---dangerously-skip-permissions}" ;;
    codex) args="${HERDER_SHIM_ARGS_CODEX---dangerously-bypass-approvals-and-sandbox}" ;;
    grok) args="${HERDER_SHIM_ARGS_GROK-}" ;;
  esac
  # Deliberate whitespace split: HERDER_SHIM_ARGS_* is a simple flag string
  # (same contract the shim generation used).
  # The subshell keeps the user's interactive shell alive when the tool exits;
  # exec still ensures the launched child is the resolved vendor, with no
  # wrapper or second PATH lookup left in the process chain.
  # shellcheck disable=SC2086
  (exec "$vendor" $args "$@")
}

claude() { _aic_launch claude "$@"; }
codex() { _aic_launch codex "$@"; }
grok() { _aic_launch grok "$@"; }
