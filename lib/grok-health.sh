#!/usr/bin/env bash

if [ -n "${AI_CONFIG_GROK_HEALTH_SH:-}" ]; then
  return 0
fi
AI_CONFIG_GROK_HEALTH_SH=1

grok_managed_state_dir() {
  if [ -n "${HERDER_STATE_DIR:-}" ]; then
    printf '%s\n' "$HERDER_STATE_DIR"
  elif [ -n "${XDG_STATE_HOME:-}" ]; then
    printf '%s/herder\n' "$XDG_STATE_HOME"
  else
    printf '%s/.local/state/herder\n' "$HOME"
  fi
}

grok_managed_home() {
  printf '%s/grok-home\n' "$(grok_managed_state_dir)"
}

grok_contract_binary() {
  if [ -n "${HERDER_GROK_BIN:-}" ]; then
    printf '%s\n' "$HERDER_GROK_BIN"
  else
    printf '%s/.grok/downloads/grok-linux-x86_64\n' "$HOME"
  fi
}

grok_canonical_path() {
  local path="$1" dir base target depth=0
  case "$path" in
    /*) ;;
    *) path="$PWD/$path" ;;
  esac
  while [ -L "$path" ] && [ "$depth" -lt 20 ]; do
    target="$(readlink "$path")" || return 1
    case "$target" in
      /*) path="$target" ;;
      *) path="$(dirname "$path")/$target" ;;
    esac
    depth=$((depth + 1))
  done
  dir="$(dirname "$path")"
  base="$(basename "$path")"
  [ -d "$dir" ] || return 1
  dir="$(cd "$dir" 2>/dev/null && pwd -P)" || return 1
  printf '%s/%s\n' "$dir" "$base"
}

grok_is_herder_shim() {
  LC_ALL=C head -c 512 -- "$1" 2>/dev/null | grep -Fq 'herder-path-shim'
}

# Locate the vendor candidate a shell would reach after every herder shim.
# No candidate is executed here; version/capability checks go through the
# isolated launch gate below.
grok_vendor_on_path() {
  local entry candidate canonical old_ifs="$IFS"
  IFS=:
  for entry in ${PATH:-}; do
    [ -n "$entry" ] || entry="."
    candidate="$entry/grok"
    [ -x "$candidate" ] && [ ! -d "$candidate" ] || continue
    canonical="$(grok_canonical_path "$candidate" 2>/dev/null || true)"
    [ -n "$canonical" ] || continue
    grok_is_herder_shim "$canonical" && continue
    IFS="$old_ifs"
    printf '%s\n' "$canonical"
    return 0
  done
  IFS="$old_ifs"
  return 1
}

# Run only the launch contract's existing gate. The gate allowlists its child
# environment and replaces HOME/GROK_HOME with directories under probe_root.
grok_check_binary() {
  local binary="$1" probe_root output rc herder
  # ai-doctor diagnoses the checkout it was invoked from. A herder-spawned
  # shell may inherit HERDER_BIN from a different spawning checkout, so never
  # use that override for this gate.
  herder="$AI_CONFIG_ROOT/bin/herder"
  probe_root="$(mktemp -d "${TMPDIR:-/tmp}/ai-doctor-grok.XXXXXX")" || return 1
  output="$({
    unset XAI_API_KEY OPENAI_API_KEY ANTHROPIC_API_KEY
    HERDER_GROK_BIN="$binary" "$herder" grok check --state-dir "$probe_root"
  } 2>&1)"
  rc=$?
  rm -rf "$probe_root"
  printf '%s\n' "$output"
  return "$rc"
}
