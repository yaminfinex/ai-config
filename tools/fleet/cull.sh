#!/usr/bin/env bash
# Courtesy-stop one hcom seat and prove its exact herdr pane is gone. The
# fallback closes only a unique exact name/tool label match; ambiguity is a
# refusal, never a guess.

set -euo pipefail

die() {
  printf 'fleet cull: %s\n' "$*" >&2
  exit 1
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

if ! hcom send "@$full_name" --intent inform -- "your seat is closing"; then
  printf 'fleet cull: courtesy notice failed; continuing with requested cull\n' >&2
fi

set +e
kill_output=$(hcom kill "$full_name" 2>&1)
kill_rc=$?
set -e
printf '%s\n' "$kill_output" >&2

candidate=${managed_pane:-$label_pane}
if [[ -n $candidate ]] && ! herdr pane get "$candidate" >/dev/null 2>&1; then
  ((kill_rc == 0)) || printf 'fleet cull: hcom kill returned %d, but pane closure is verified\n' "$kill_rc" >&2
  printf 'culled name=%s pane=%s close=managed\n' "$full_name" "$candidate"
  exit 0
fi

panes_after=$(herdr pane list) || die "cannot verify herdr panes after hcom kill"
fallback_panes=$(label_matches "$panes_after")
fallback_count=$(jq 'length' <<<"$fallback_panes")

if [[ $fallback_count -eq 1 ]]; then
  fallback_pane=$(jq -r '.[0]' <<<"$fallback_panes")
  herdr pane close "$fallback_pane" >/dev/null || die "fallback pane close failed: $fallback_pane"
  if herdr pane get "$fallback_pane" >/dev/null 2>&1; then
    die "fallback pane still exists after close: $fallback_pane"
  fi
  printf 'culled name=%s pane=%s close=label-fallback\n' "$full_name" "$fallback_pane"
  exit 0
fi

if [[ $fallback_count -gt 1 ]]; then
  die "managed close failed and multiple exact labels remain; refusing to guess"
fi
if [[ -n $candidate ]]; then
  die "managed close failed and the expected pane remains without an exact label: $candidate"
fi
die "cannot verify a managed close and no exact label match exists; no pane was closed"
