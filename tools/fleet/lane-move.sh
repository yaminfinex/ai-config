#!/usr/bin/env bash
# One-time move from group labels to lanes. With no arguments it is a dry run:
# it reads the live hcom roster, each seat's herder record and the owner's web
# spaces, all read-only, and prints one editable line per group label that has
# a live member. The owner edits that list once; --apply FILE then creates each
# lane with its lead and moves the other members in through the herder CLI,
# and merges {"<old space id>": "<lane id>"} for each absorbed space into
# <herder state dir>/lane-move.json, which the web client reads to re-key the
# space. The spaces themselves are never written here.

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: lane-move.sh [--spaces FILE]    print the proposed list (writes nothing)
       lane-move.sh --apply FILE [--spaces FILE]
                                       create the lanes in an edited list

A list line is:  <lane name> | lead <seat> | members <seat>, <seat> | space <name or ->
Lines starting with # are ignored. --spaces defaults to the web-state user
holding the most spaces under the herder state directory.
EOF
  exit 2
}

command -v jq >/dev/null || { echo 'lane-move: jq is required' >&2; exit 1; }
command -v herder >/dev/null || { echo 'lane-move: herder is required' >&2; exit 1; }

apply_file=
spaces_file=
while (($# > 0)); do
  case $1 in
    --apply) [[ $# -ge 2 ]] || usage; apply_file=$2; shift 2 ;;
    --spaces) [[ $# -ge 2 ]] || usage; spaces_file=$2; shift 2 ;;
    *) usage ;;
  esac
done

trim() { sed 's/^[[:space:]]*//; s/[[:space:]]*$//' <<<"$1"; }

state=${HERDER_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herder}
if [[ -z $spaces_file ]]; then
  best=-1
  for candidate in "$state"/web-state/*/spaces.json; do
    [[ -f $candidate ]] || continue
    count=$(jq '[.rows[] | select(.deleted != true)] | length' "$candidate")
    ((count > best)) && best=$count spaces_file=$candidate
  done
fi
spaces='[]'
[[ -z $spaces_file ]] || spaces=$(jq -c '[.rows[] | select(.deleted != true) | {id: .value.id, name: .value.name}]' "$spaces_file")

if [[ -n $apply_file ]]; then
  # Parse every line before writing anything, so a typo writes nothing.
  names=() leads=() members=() space_ids=()
  while IFS= read -r line || [[ -n $line ]]; do
    [[ -z $(trim "$line") || $line == \#* ]] && continue
    IFS='|' read -r name lead mems space <<<"$line"
    name=$(trim "$name") lead=$(trim "${lead:-}") mems=$(trim "${mems:-}") space=$(trim "${space:-}")
    [[ -n $name && $lead == lead\ * && $mems == members* && $space == space\ * ]] \
      || { echo "lane-move: malformed line: $line" >&2; exit 2; }
    space=$(trim "${space#space}") ids=
    if [[ $space != - ]]; then
      ids=$(jq -c --arg name "$space" '[.[] | select(.name == $name) | .id]' <<<"$spaces")
      [[ $ids != '[]' ]] || { echo "lane-move: no owner space named $space (${spaces_file:-no spaces file})" >&2; exit 2; }
    fi
    names+=("$name") leads+=("$(trim "${lead#lead}")") members+=("$(trim "${mems#members}")") space_ids+=("$ids")
  done <"$apply_file"
  # Old space id -> lane id, for the web client to re-key the absorbed spaces;
  # merged into the file after each lane so a later failure keeps earlier rows.
  moves_file=$state/lane-move.json
  for i in "${!names[@]}"; do
    out=$(herder lane new "${names[$i]}" --lead "${leads[$i]}")
    id=$(sed -n 's/^lane=//p' <<<"$out")
    printf 'lane %s lead=%s id=%s\n' "${names[$i]}" "${leads[$i]}" "$id"
    IFS=',' read -ra seats <<<"${members[$i]}"
    for seat in "${seats[@]}"; do
      seat=$(trim "$seat")
      [[ -z $seat || $seat == "${leads[$i]}" ]] && continue
      herder lane move "$seat" "$id"
      printf '  member %s\n' "$seat"
    done
    [[ -n ${space_ids[$i]} ]] || continue
    previous='{}'
    [[ ! -f $moves_file ]] || previous=$(<"$moves_file")
    tmp=$(mktemp "$moves_file.XXXXXX")
    jq --argjson ids "${space_ids[$i]}" --arg lane "$id" '. + ([$ids[] | {key: ., value: $lane}] | from_entries)' \
      <<<"$previous" >"$tmp"
    mv -- "$tmp" "$moves_file"
    printf '  space %s -> %s\n' "$(jq -r 'join(", ")' <<<"${space_ids[$i]}")" "$moves_file"
  done
  exit 0
fi

# Live top-level seats (Task subagents follow their owner), each with its
# group, manager and lane from herder. A seat already in a lane is left out.
seats='[]'
while IFS=$'\t' read -r name created; do
  record=$(herder show "$name" --json 2>/dev/null) || continue
  seats=$(jq -c --argjson s "$seats" --arg created "$created" \
    '$s + [{name, group: (.assignment.group // ""), manager: (.manager // ""), lane: (.lane.id // ""), created: ($created | tonumber)}]' \
    <<<"$record")
done < <(hcom list --json 2>/dev/null | jq -r '.[] | select(.parent_name == null) | [.name, (.created_at // 0)] | @tsv')

jq -rn --argjson seats "$seats" --argjson spaces "$spaces" --arg source "${spaces_file:--}" '
  def lower: ascii_downcase;
  [$seats[] | select(.group != "" and .lane == "")] | group_by(.group) as $groups
  | ($groups | map(.[0].group)) as $labels
  | "# lane-move dry run. Edit the lines, then: lane-move.sh --apply <this file>",
    "# lead: the member managed by the operator, else the earliest created.",
    "# space: the owner space of the same name (spaces from \($source)).",
    ($groups[] | sort_by(.created) as $m
      | (([$m[] | select(.manager == "human")] + $m)[0].name) as $lead
      | .[0].group as $label
      | ([$spaces[].name | select(lower == ($label | lower))][0] // "-") as $space
      | "\($label) | lead \($lead) | members \([$m[].name] | join(", ")) | space \($space)"),
    "",
    "# near misses (not absorbed; put one in a space column to take it):",
    ($labels[] | lower as $label
      | [$spaces[].name | lower as $space | select($space != $label
          and (($space | startswith($label)) or ($label | startswith($space))))]
      | select(length > 0)
      | "#   \($label): \(join(", "))")'
