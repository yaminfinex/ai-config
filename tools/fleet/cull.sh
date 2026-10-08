#!/usr/bin/env bash
# Courtesy-stop one hcom seat and prove its exact herdr pane is gone. The
# fallback closes only a unique exact name/tool label match; ambiguity is a
# refusal, never a guess. Every close of a pane that survives the kill goes
# through one guarded path: the pane must still be this seat's (no conflicting
# label, no other seat claiming it) and must hold only its idle shell, proven
# twice. A pane running anything else is kept and named. cull never closes a
# tab: herdr removes a tab with its last pane, and the output reports tab=gone
# or tab=kept.

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

# Report the seat's tab after its pane closed: gone once herdr removed it,
# kept otherwise (including when the tab is unknown).
tab_state() {
  local tab=$1
  if [[ -n $tab ]] && ! herdr tab get "$tab" >/dev/null 2>&1; then
    printf 'gone\n'
  else
    printf 'kept\n'
  fi
}

# Close a pane that survived the kill, or die keeping it. Sets closed_cwd and
# closed_cmd for the report.
guarded_close() {
  local target=$1 pane_output label process_output first_pid recheck_output roster claimants idle_rc
  pane_output=$(herdr pane get "$target") || die "cannot read the remaining pane: $target"
  closed_cwd=$(jq -r '.result.pane.foreground_cwd // .result.pane.cwd // "unknown"' <<<"$pane_output")
  [[ -n $target_tab ]] || target_tab=$(jq -r '.result.pane.tab_id // empty' <<<"$pane_output")
  label=$(jq -r '.result.pane.label // empty' <<<"$pane_output")

  process_output=$(herdr pane process-info --pane "$target") \
    || die "remaining pane $target kept: cannot inspect its processes (cwd=$closed_cwd)"
  closed_cmd=$(fleet_foreground_names "$process_output")
  idle_rc=0
  fleet_idle_shell "$process_output" || idle_rc=$?
  case $idle_rc in
    0) ;;
    1) die "remaining pane $target kept: not an idle shell (cwd=$closed_cwd foreground=$closed_cmd)" ;;
    *) die "remaining pane $target kept: idle shell unproven, process info malformed (cwd=$closed_cwd foreground=$closed_cmd)" ;;
  esac
  first_pid=$(fleet_shell_pid "$process_output")

  # Ownership: another agent's label, or another live seat launched into this
  # pane, means it is no longer ours to close.
  if [[ -n $label ]] && jq -en --arg label "$label" --arg mine "$full_name [$tool]" '
      ($label | test("^[◉▶■○◦] .+ \\[[^]]+\\]$")) and ([ "◉", "▶", "■", "○", "◦" ] | all(. + " " + $mine != $label))
    ' >/dev/null; then
    die "remaining pane $target kept: it carries another agent's label: $label (cwd=$closed_cwd)"
  fi
  roster=$(hcom list --json) || die "remaining pane $target kept: cannot read hcom seats to confirm no one claimed it"
  claimants=$(jq -r --arg pane "$target" --arg mine "$full_name" '
      if type == "array" then [.[] | select(.name != $mine and .launch_context.pane_id? == $pane) | .name] | join(",")
      else error("not a seat list") end
    ' <<<"$roster" 2>/dev/null) || die "remaining pane $target kept: hcom seat list is malformed"
  [[ -z $claimants ]] || die "remaining pane $target kept: claimed by seat $claimants (cwd=$closed_cwd)"

  # herdr has no atomic close-if-idle. Re-read just before closing so a
  # launcher that took the pane since the first read is seen; a launch that
  # lands between this read and the close below is still lost.
  recheck_output=$(herdr pane process-info --pane "$target") \
    || die "remaining pane $target kept: cannot re-inspect its processes (cwd=$closed_cwd)"
  idle_rc=0
  fleet_idle_shell "$recheck_output" || idle_rc=$?
  if ((idle_rc != 0)) || [[ $(fleet_shell_pid "$recheck_output") != "$first_pid" ]]; then
    die "remaining pane $target kept: it changed while being checked (cwd=$closed_cwd foreground=$(fleet_foreground_names "$recheck_output"))"
  fi

  herdr pane close "$target" >/dev/null || die "pane close failed: $target (cwd=$closed_cwd)"
  if herdr pane get "$target" >/dev/null 2>&1; then
    die "pane still exists after close: $target"
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
  printf 'culled name=%s pane=%s close=managed tab=%s\n' "$full_name" "$candidate" "$(tab_state "$candidate_tab")"
  exit 0
fi

panes_after=$(herdr pane list) || die "cannot verify herdr panes after hcom kill"
fallback_panes=$(label_matches "$panes_after")
fallback_count=$(jq 'length' <<<"$fallback_panes")
fallback_pane=$(jq -r 'if length == 1 then .[0] else empty end' <<<"$fallback_panes")

if [[ $fallback_count -gt 1 ]]; then
  die "managed close failed and multiple exact labels remain; refusing to guess"
fi
if [[ -n $candidate ]]; then
  if [[ -n $fallback_pane && $fallback_pane != "$candidate" ]]; then
    die "expected pane $candidate remains but the exact label is now on $fallback_pane; refusing to cull either"
  fi
  target=$candidate
  target_tab=$candidate_tab
elif [[ -n $fallback_pane ]]; then
  target=$fallback_pane
  target_tab=$(pane_tab "$panes_after" "$fallback_pane")
else
  die "cannot verify a managed close and no exact label match exists; no pane was closed"
fi

close_kind=idle-shell
[[ $target != "$fallback_pane" ]] || close_kind="label-fallback"
guarded_close "$target"
register_event culled --name "$full_name" --pane "$target" --close "$close_kind"
printf 'culled name=%s pane=%s close=%s tab=%s cwd=%s foreground=%s\n' \
  "$full_name" "$target" "$close_kind" "$(tab_state "$target_tab")" "$closed_cwd" "$closed_cmd"
