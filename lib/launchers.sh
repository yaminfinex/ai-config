#!/usr/bin/env bash
# lib/launchers.sh - interactive agent launchers: claude/codex/grok as shell
# functions. Claude and Codex launch ON-BUS in the current terminal via
# `hcom <tool> --run-here`; grok remains a direct vendor exec.
#
# Sourced by the managed rc block bin/ai-setup writes into ~/.bashrc (and
# ~/.zshrc when present). Shell FUNCTIONS, not PATH entries: a function wins
# name resolution over every PATH participant unconditionally, so no PATH
# writer — mise hook-env re-fronting its shims dir on a config-boundary `cd`,
# an installer prepend, stale __MISE_* state inherited from a pane server —
# can reroute a hand-typed `claude` to a version-manager imposter. See
# docs/launcher-design.md for the full lesson ledger; the load-bearing
# constraints each block below carries are:
#
#   - hcom >= 0.7.24 fail-closes its sessionstart hook for unknown sessions
#     (identity rows come from launching THROUGH hcom, never from a hook
#     call), and `hcom start` inside a raw session delivers messages by
#     BLOCKING the stop hook for up to HCOM_TIMEOUT (24h) — both raw-then-join
#     paths are dead ends. On-bus means launching through hcom, full stop.
#   - `claude -p/--print` must NEVER route through hcom: hcom hard-codes -p
#     as its background switch (stdin nulled, stdout to hcom logs, Stop hook
#     polling ~24h) so the one-shot's answer never returns (task-010). Codex
#     has no such flag (-p means --profile there) and stays on the hcom path.
#   - hcom resolves the tool by bare name from the PATH it inherits, so the
#     resolved vendor is pinned via a single-symlink dir fronted on the CHILD
#     PATH only (task-292: a mise shim chosen as "real claude" caused an
#     infinite exec loop; PATH order is rewritten concurrently by mise).
#   - Ambient identity env (HCOM_*/HERDER_*/HERDR_*) is scrubbed on every
#     path: a vendor CLI launched from an identity-bearing shell takes over
#     the caller's live bus row and deletes it on exit
#     (docs/hazards/agent-cli-identity-hijack.md, task-244). HCOM_DIR is
#     preserved — it locates the bus, it is not an identity.
#
# Scope: interactive shells only. Escape hatch that bypasses hcom, the
# resolver, and default args:  command claude ...
#
# Default args are baked here with env override, NOT read from mise [env]
# alone: a shell where mise env never applied must still launch autonomous.
# Export HERDER_SHIM_ARGS_CLAUDE="" (empty) on an ask-mode machine.
#
# Known upstream caveat (task-258/task-029): `hcom <tool> --run-here` has no
# launch-phase timeout; if shell init fails inside hcom's wrapper the launch
# can sit silent. In an interactive pane you can see and ^C it.

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

# Unset every ambient HCOM_*/HERDER_*/HERDR_* var in the CALLING (subshell)
# environment, keeping HCOM_DIR. Runs only inside the launch subshell so the
# user's interactive shell is never mutated.
_aic_scrub_identity() {
  local _aic_keep_dir="${HCOM_DIR-}" _aic_name
  while IFS= read -r _aic_name; do
    [ -n "$_aic_name" ] && unset "$_aic_name"
  done <<AIC_EOF
$(env 2>/dev/null | LC_ALL=C command sed -n \
    -e 's/^\(HCOM_[A-Za-z0-9_]*\)=.*/\1/p' \
    -e 's/^\(HERDER_[A-Za-z0-9_]*\)=.*/\1/p' \
    -e 's/^\(HERDR_[A-Za-z0-9_]*\)=.*/\1/p')
AIC_EOF
  if [ -n "$_aic_keep_dir" ]; then
    export HCOM_DIR="$_aic_keep_dir"
  fi
}

# Materialize a single-symlink bin dir for the resolved vendor so hcom's own
# bare-name lookup has exactly one deterministic answer on the child PATH.
# Location is ai-config-owned (~/.cache/ai-config/vendorbin), NOT the retired
# ~/.cache/herder/vendorbin dir ai-doctor flags as orphaned. The symlink
# targets the stable vendor entry point, so vendor self-updates never
# invalidate it. Failure is non-fatal: the caller warns and launches with the
# unpinned PATH (doctor owns turning imposters into findings).
_aic_pin_vendor() {
  local tool="$1" vendor="$2"
  local dir="${XDG_CACHE_HOME:-$HOME/.cache}/ai-config/vendorbin/$tool"
  command mkdir -p "$dir" 2>/dev/null || return 1
  if [ "$(command readlink "$dir/$tool" 2>/dev/null)" != "$vendor" ]; then
    command ln -sfn "$vendor" "$dir/$tool" 2>/dev/null || return 1
  fi
  printf '%s\n' "$dir"
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

  # Direct vendor exec paths: grok (fleet support retired) and claude print
  # one-shots (see header). Identity scrub still applies — a bare vendor CLI
  # inheriting HCOM_* hijacks the caller's bus row.
  local _aic_direct=0 _aic_arg
  if [ "$tool" = grok ]; then
    _aic_direct=1
  elif [ "$tool" = claude ]; then
    for _aic_arg in "$@"; do
      if [ "$_aic_arg" = "-p" ] || [ "$_aic_arg" = "--print" ]; then
        _aic_direct=1
        break
      fi
    done
  fi
  # Deliberate whitespace split below: HERDER_SHIM_ARGS_* is a simple flag
  # string (same contract the shim generation used). The subshell keeps the
  # user's interactive shell alive when the tool exits; exec ensures the
  # launched child is the resolved target with no wrapper left in the chain.
  if [ "$_aic_direct" -eq 1 ]; then
    # shellcheck disable=SC2086
    (
      _aic_scrub_identity
      export HCOM_LAUNCH_INFLIGHT=1
      exec "$vendor" $args "$@"
    )
    return $?
  fi

  # On-bus current-pane launch. hcom is a hard dependency here by doctrine:
  # never fall back silently to a raw off-bus vendor launch.
  local hcom_bin
  hcom_bin=$(command -v hcom 2>/dev/null) || {
    printf "ai-config launcher: hcom not on PATH; '%s' launches on-bus through hcom and never falls back to a raw vendor run. Run bin/ai-setup (installs hcom via mise), or bypass deliberately with: command %s ...\n" "$tool" "$tool" >&2
    return 127
  }
  local pin
  if ! pin=$(_aic_pin_vendor "$tool" "$vendor"); then
    printf "ai-config launcher: could not pin vendor '%s' for hcom's lookup; launching with unpinned PATH (imposter risk — see docs/launcher-design.md)\n" "$tool" >&2
    pin=""
  fi
  # shellcheck disable=SC2086
  (
    _aic_scrub_identity
    if [ -n "$pin" ]; then
      PATH="$pin:$PATH"
    fi
    export PATH HCOM_LAUNCH_INFLIGHT=1
    exec "$hcom_bin" "$tool" --run-here --go $args "$@"
  )
}

claude() { _aic_launch claude "$@"; }
codex() { _aic_launch codex "$@"; }
grok() { _aic_launch grok "$@"; }
