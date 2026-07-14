#!/usr/bin/env bash

if [ -n "${AI_CONFIG_GROK_HEALTH_SH:-}" ]; then
  return 0
fi
AI_CONFIG_GROK_HEALTH_SH=1

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

grok_invoked_path() {
  local path="$1" dir base
  case "$path" in
    /*) ;;
    *) path="$PWD/$path" ;;
  esac
  dir="$(dirname "$path")"
  base="$(basename "$path")"
  dir="$(cd "$dir" 2>/dev/null && pwd -P)" || return 1
  printf '%s/%s\n' "$dir" "$base"
}

# Locate the vendor candidate a shell reaches after every herder shim. This is
# the same PATH rule as launch and deliberately never executes the candidate.
grok_vendor_on_path() {
  local entry candidate canonical invoked old_ifs="$IFS"
  IFS=:
  for entry in ${PATH:-}; do
    [ -n "$entry" ] || entry="."
    candidate="$entry/grok"
    [ -x "$candidate" ] && [ ! -d "$candidate" ] || continue
    canonical="$(grok_canonical_path "$candidate" 2>/dev/null || true)"
    [ -n "$canonical" ] || continue
    grok_is_herder_shim "$canonical" && continue
    invoked="$(grok_invoked_path "$candidate" 2>/dev/null || true)"
    [ -n "$invoked" ] || continue
    IFS="$old_ifs"
    printf '%s\n' "$invoked"
    return 0
  done
  IFS="$old_ifs"
  return 1
}
