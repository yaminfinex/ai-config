#!/usr/bin/env bash
# lib/launchers.sh - interactive agent launchers: claude/codex/grok as shell
# functions routing through `bin/herder launch`.
#
# Sourced by the managed rc block bin/ai-setup writes into ~/.bashrc (and
# ~/.zshrc when present). Shell FUNCTIONS, not PATH entries: a function wins
# name resolution over every PATH participant unconditionally, so no PATH
# writer — mise hook-env re-fronting its shims dir on a config-boundary `cd`,
# an installer prepend, stale __MISE_* state inherited from a pane server —
# can reroute a hand-typed `claude` away from the herder launch path. The
# previous generation intercepted via tools/herder/shims + global PATH
# ordering and lost that race repeatedly (off-bus agents without autonomous
# flags); see docs/launcher-design.md.
#
# Scope: interactive shells only. Scripts and spawners must call
# `"$AI_CONFIG_ROOT/bin/herder" launch <tool>` explicitly (herder spawn
# already does). Escape hatch for a deliberate raw vendor run:
#   command claude ...
#
# Default args are baked here with env override, NOT read from mise [env]
# alone: a shell where mise env never applied must still launch autonomous.
# Export HERDER_SHIM_ARGS_CLAUDE="" (empty) on an ask-mode machine.

_aic_launch() {
  local tool="$1"
  shift
  local herder="${AI_CONFIG_ROOT:-}/bin/herder"
  if [ ! -x "$herder" ]; then
    printf 'ai-config launcher: %s missing or not executable; set AI_CONFIG_ROOT (rerun bin/ai-setup), or bypass deliberately with: command %s\n' \
      "$herder" "$tool" >&2
    return 127
  fi
  local args=""
  case "$tool" in
    claude) args="${HERDER_SHIM_ARGS_CLAUDE---dangerously-skip-permissions}" ;;
    codex) args="${HERDER_SHIM_ARGS_CODEX---dangerously-bypass-approvals-and-sandbox}" ;;
    grok) args="${HERDER_SHIM_ARGS_GROK-}" ;;
  esac
  # Deliberate whitespace split: HERDER_SHIM_ARGS_* is a simple flag string
  # (same contract the shim generation used).
  # shellcheck disable=SC2086
  "$herder" launch "$tool" $args "$@"
}

claude() { _aic_launch claude "$@"; }
codex() { _aic_launch codex "$@"; }
grok() { _aic_launch grok "$@"; }
