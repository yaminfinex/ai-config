#!/usr/bin/env bash
# Write the canonical launch notes (docs/hcom-launch-notes.txt) into hcom's
# `[launch] notes`, the text every hcom-launched agent reads at session start.
# The repo file is the source of truth; __AI_CONFIG_ROOT__ is replaced with this
# checkout's absolute path so the doctrine links resolve on this machine.
# Usage: apply-hcom-notes.sh [--check]   (--check: exit 1 if hcom differs, write nothing)

set -euo pipefail

die() {
  printf 'fleet apply-hcom-notes: %s\n' "$*" >&2
  exit 1
}

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
source_file=$root/docs/hcom-launch-notes.txt
[[ -s $source_file ]] || die "missing or empty $source_file"

expected=$(sed "s|__AI_CONFIG_ROOT__|$root|g" "$source_file")
[[ -n $expected ]] || die "rendered notes are empty"
[[ $expected != *__AI_CONFIG_ROOT__* ]] || die "unrendered placeholder left in notes"

if [[ ${1:-} == --check ]]; then
  # `hcom config notes` prints only the first line back, so read the stored
  # value from hcom's own config file.
  config=${HCOM_DIR:-$HOME/.hcom}/config.toml
  [[ -r $config ]] || die "cannot read $config"
  current=$(python3 - "$config" <<'PY'
import sys, tomllib
with open(sys.argv[1], 'rb') as handle:
    print(tomllib.load(handle).get('launch', {}).get('notes', ''), end='')
PY
  ) || die "cannot parse $config"
  if [[ $current == "$expected" ]]; then
    printf 'hcom launch notes match %s\n' "$source_file"
    exit 0
  fi
  printf 'hcom launch notes differ from %s (run tools/fleet/apply-hcom-notes.sh)\n' "$source_file" >&2
  exit 1
fi

hcom config notes "$expected" >/dev/null || die "hcom config notes failed"
printf 'hcom launch notes set from %s (%d chars)\n' "$source_file" "${#expected}"
