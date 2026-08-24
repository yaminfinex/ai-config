#!/usr/bin/env bash
# Register the fleet hcom terminal preset without selecting it as the default.
#
# The preset's open helper reads FLEET_PANE, which spawn.sh sets for each
# invocation. The helper must print that target pane id as its first stdout
# line so hcom can retain it for the managed close command.

set -euo pipefail
umask 077

die() {
  printf 'fleet preset-install: %s\n' "$*" >&2
  exit 1
}

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
helper=$script_dir/spawn-pane.sh
config_dir=${HCOM_DIR:-${HOME:?HOME is required}/.hcom}
config=$config_dir/config.toml

[[ -x $helper ]] || die "open helper is not executable: $helper"
mkdir -p -- "$config_dir"
touch -- "$config"

helper_toml=${helper//\/\\}
helper_toml=${helper_toml//\"/\\\"}
fleet_block=$(printf '%s\n' \
  '[terminal.presets.fleet]' \
  "open = \"\\\"${helper_toml}\\\" \\\"{script}\\\" \\\"{pane_title}\\\"\"" \
  'close = "herdr pane close {pane_id}"' \
  'binary = "herdr"' \
  'pane_id_env = "FLEET_PANE"')

tmp=$(mktemp "${config}.tmp.XXXXXX")
trap 'rm -f -- "$tmp"' EXIT

in_fleet=0
inserted=0
while IFS= read -r line || [[ -n $line ]]; do
  if [[ $line == '[terminal.presets.fleet]' ]]; then
    if ((inserted == 0)); then
      printf '%s\n' "$fleet_block" >>"$tmp"
      inserted=1
    fi
    in_fleet=1
    continue
  fi

  if ((in_fleet == 1)); then
    if [[ $line =~ ^[[:space:]]*\[.*\][[:space:]]*$ ]]; then
      in_fleet=0
    else
      continue
    fi
  fi

  printf '%s\n' "$line" >>"$tmp"
done <"$config"

if ((inserted == 0)); then
  if [[ -s $tmp ]]; then
    printf '\n' >>"$tmp"
  fi
  printf '%s\n' "$fleet_block" >>"$tmp"
fi

if cmp -s -- "$config" "$tmp"; then
  printf 'fleet preset already registered in %s\n' "$config"
  exit 0
fi

chmod --reference="$config" "$tmp"
mv -- "$tmp" "$config"
trap - EXIT
printf 'registered fleet preset in %s (active preset unchanged)\n' "$config"
