#!/usr/bin/env bash
# Courtesy-stop one hcom seat and prove its exact herdr pane is gone. The
# fallback closes only a unique exact name/tool label match; ambiguity is a
# refusal, never a guess. A seat pane left holding only its idle shell is
# closed too (close=idle-shell); one running anything else is kept and named.
# Then the seat's tab is closed if it is empty. herdr already removes a tab when
# its last pane closes, so that step normally reports tab=gone.

set -euo pipefail

fleet_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=tools/fleet/lib.sh
source "$fleet_dir/lib.sh"

register_disabled=0
REGISTER_OUTPUT=
attrib=()
fleet_tool=cull

register_event() {
  local kind=$1 herder_bin rc detail
  shift
  REGISTER_OUTPUT=
  ((register_disabled == 0)) || return 0
  if ! herder_bin=$(command -v herder); then
    printf 'fleet %s: register %s skipped: herder not found\n' "$fleet_tool" "$kind" >&2
    register_disabled=1
    return 0
  fi
  set +e
  REGISTER_OUTPUT=$(timeout --foreground 10s "$herder_bin" register "$kind" "$@" ${attrib[@]+"${attrib[@]}"} 2>&1)
  rc=$?
  set -e
  if ((rc != 0)); then
    detail=${REGISTER_OUTPUT//$'\n'/; }
    printf 'fleet %s: register %s skipped: %s\n' "$fleet_tool" "$kind" "${detail:-exit $rc}" >&2
    REGISTER_OUTPUT=
    register_disabled=1
  fi
}

die() {
  printf 'fleet cull: %s\n' "$*" >&2
  exit 1
}

pane_tab() {
  local panes=$1 target=$2
  jq -r --arg pane "$target" '[.result.panes[]? | select(.pane_id == $pane) | .tab_id // empty][0] // empty' <<<"$panes"
}

# Set tab_state for the seat's tab after its pane closed: gone (herdr removed
# it), closed (empty, closed here), kept (other panes remain) or unknown.
close_empty_tab() {
  local tab=$1 tab_output count panes
  tab_state=unknown
  [[ -n $tab ]] || return 0
  if ! tab_output=$(herdr tab get "$tab" 2>/dev/null); then
    tab_state=gone
    return 0
  fi
  count=$(jq -r '.result.tab.pane_count // empty' <<<"$tab_output")
  if [[ -z $count ]]; then
    panes=$(herdr pane list) || die "cannot count panes in tab $tab"
    count=$(fleet_tab_pane_count "$panes" "$tab")
  fi
  if [[ $count == 0 ]]; then
    herdr tab close "$tab" >/dev/null || die "empty tab close failed: $tab"
    tab_state=closed
  else
    tab_state=kept
  fi
}

label_matches() {
  local panes=$1
  jq -c --arg name "$full_name" --arg tool "$tool" '
    [.result.panes[]?
      | select(
          .label as $label
          | any(["◉", "▶", "■", "○", "◦"][];
              $label == (. + " " + $name + " [" + $tool + "]")))
      | .pane_id]
  ' <<<"$panes"
}

[[ $# -eq 1 ]] || die "usage: cull.sh <hcom-name>"
name=$1
command -v jq >/dev/null || die "jq is required"

self_name=$(fleet_self_name)
[[ -z $self_name ]] || attrib=(--by "$self_name" --by-kind agent)

agents=$(hcom list --json) || die "cannot read hcom agents"
matches=$(jq -c --arg name "$name" '[.[] | select(.name == $name or .base_name == $name)]' <<<"$agents")
[[ $(jq 'length' <<<"$matches") -eq 1 ]] || die "hcom name is missing or ambiguous: $name"
record=$(jq -c '.[0]' <<<"$matches")
full_name=$(jq -r '.name' <<<"$record")
tool=$(jq -r '.tool' <<<"$record" | tr '[:upper:]' '[:lower:]')
managed_pane=$(jq -r '.launch_context.pane_id // empty' <<<"$record")

panes_before=$(herdr pane list) || die "cannot read herdr panes"
label_panes=$(label_matches "$panes_before")
label_count=$(jq 'length' <<<"$label_panes")
label_pane=$(jq -r 'if length == 1 then .[0] else empty end' <<<"$label_panes")
if [[ $label_count -eq 1 ]]; then
  if [[ -n $managed_pane && $managed_pane != "$label_pane" ]]; then
    die "managed pane $managed_pane conflicts with exact label pane $label_pane; refusing to cull"
  fi
elif [[ $label_count -gt 1 ]]; then
  die "multiple panes match the exact $full_name [$tool] label; refusing to cull"
fi

candidate=${managed_pane:-$label_pane}
candidate_tab=
[[ -z $candidate ]] || candidate_tab=$(pane_tab "$panes_before" "$candidate")
requested_args=(--name "$full_name")
[[ -z $candidate ]] || requested_args+=(--pane "$candidate")
register_event cull-requested "${requested_args[@]}"

if ! hcom send "@$full_name" --intent inform -- "your seat is closing"; then
  printf 'fleet cull: courtesy notice failed; continuing with requested cull\n' >&2
fi

set +e
kill_output=$(hcom kill "$full_name" 2>&1)
kill_rc=$?
set -e
printf '%s\n' "$kill_output" >&2

if [[ -n $candidate ]] && ! herdr pane get "$candidate" >/dev/null 2>&1; then
  ((kill_rc == 0)) || printf 'fleet cull: hcom kill returned %d, but pane closure is verified\n' "$kill_rc" >&2
  register_event culled --name "$full_name" --pane "$candidate" --close managed
  close_empty_tab "$candidate_tab"
  printf 'culled name=%s pane=%s close=managed tab=%s\n' "$full_name" "$candidate" "$tab_state"
  exit 0
fi

panes_after=$(herdr pane list) || die "cannot verify herdr panes after hcom kill"
fallback_panes=$(label_matches "$panes_after")
fallback_count=$(jq 'length' <<<"$fallback_panes")

if [[ $fallback_count -eq 1 ]]; then
  fallback_pane=$(jq -r '.[0]' <<<"$fallback_panes")
  fallback_tab=$(pane_tab "$panes_after" "$fallback_pane")
  herdr pane close "$fallback_pane" >/dev/null || die "fallback pane close failed: $fallback_pane"
  if herdr pane get "$fallback_pane" >/dev/null 2>&1; then
    die "fallback pane still exists after close: $fallback_pane"
  fi
  register_event culled --name "$full_name" --pane "$fallback_pane" --close label-fallback
  close_empty_tab "$fallback_tab"
  printf 'culled name=%s pane=%s close=label-fallback tab=%s\n' "$full_name" "$fallback_pane" "$tab_state"
  exit 0
fi

if [[ $fallback_count -gt 1 ]]; then
  die "managed close failed and multiple exact labels remain; refusing to guess"
fi
if [[ -n $candidate ]]; then
  # The seat is gone but its pane survived. Close it only when nothing but its
  # shell runs there; a dev server, watcher, editor or other agent is kept.
  remain_output=$(herdr pane get "$candidate") || die "cannot read the remaining pane: $candidate"
  remain_cwd=$(jq -r '.result.pane.foreground_cwd // .result.pane.cwd // "unknown"' <<<"$remain_output")
  [[ -n $candidate_tab ]] || candidate_tab=$(jq -r '.result.pane.tab_id // empty' <<<"$remain_output")
  process_output=$(herdr pane process-info --pane "$candidate") \
    || die "managed close failed and the expected pane remains without an exact label: $candidate (cannot inspect its processes; cwd=$remain_cwd)"
  remain_cmd=$(fleet_foreground_names "$process_output")
  idle_rc=0
  fleet_idle_shell "$process_output" || idle_rc=$?
  ((idle_rc == 0)) \
    || die "managed close failed and the expected pane remains without an exact label: $candidate (not an idle shell, kept; cwd=$remain_cwd foreground=$remain_cmd)"
  herdr pane close "$candidate" >/dev/null || die "idle-shell pane close failed: $candidate (cwd=$remain_cwd)"
  if herdr pane get "$candidate" >/dev/null 2>&1; then
    die "idle-shell pane still exists after close: $candidate"
  fi
  register_event culled --name "$full_name" --pane "$candidate" --close idle-shell
  close_empty_tab "$candidate_tab"
  printf 'culled name=%s pane=%s close=idle-shell tab=%s cwd=%s foreground=%s\n' \
    "$full_name" "$candidate" "$tab_state" "$remain_cwd" "$remain_cmd"
  exit 0
fi
die "cannot verify a managed close and no exact label match exists; no pane was closed"
