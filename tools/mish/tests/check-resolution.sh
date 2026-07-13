#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

new_mission flag-mission --authority hera
new_mission cwd-mission --authority hera
new_mission marker-mission --authority hera
run_mish "$INVOKE_DIR" "flag-task" backlog --mission flag-mission task create "Flag task"
assert_status 0
run_mish "$INVOKE_DIR" "cwd-task" backlog --mission cwd-mission task create "Cwd task"
assert_status 0
run_mish "$INVOKE_DIR" "marker-task" backlog --mission marker-mission task create "Marker task"
assert_status 0

step "AC-9 resolution order"
work="$WORK/resolution"
mkdir -p "$work/child"
write_marker "$work" marker-mission
run_mish "$(mission_dir cwd-mission)/backlog/tasks" "flag-wins" backlog --mission flag-mission task list
assert_status 0
assert_contains "$LAST_OUT" "Flag task"
assert_not_contains "$LAST_OUT" "Cwd task"
run_mish "$(mission_dir cwd-mission)/backlog/tasks" "cwd-wins" backlog task list
assert_status 0
assert_contains "$LAST_OUT" "Cwd task"
assert_not_contains "$LAST_OUT" "Marker task"
run_mish "$work/child" "marker-resolves" backlog task list
assert_status 0
assert_contains "$LAST_OUT" "Marker task"
write_marker "$work/child" cwd-mission
run_mish "$work/child" "two-markers-refuse" backlog task list
assert_status 1
assert_contains "$LAST_ERR" "$work/.mission"
assert_contains "$LAST_ERR" "$work/child/.mission"

step "AC-10 refusal guidance"
plain="$WORK/plain"
mkdir -p "$plain"
run_mish "$plain" "no-context" backlog task list
assert_status 1
assert_contains "$LAST_ERR" "--mission"
assert_contains "$LAST_ERR" "missions/<slug>"
assert_contains "$LAST_ERR" ".mission"
write_marker "$plain" missing-slug
run_mish "$plain" "missing-marker-target" status
assert_status 1
assert_contains "$LAST_ERR" "missing-slug"
run_mish_no_repo "$plain" "unset-repo" status --mission flag-mission
assert_status 1
assert_contains "$LAST_ERR" "MISSIONS_REPO"

step "canonical slug refusals across context sources"
for slug in a--b x-; do
  mkdir -p "$MISSIONS_REPO_DIR/missions/$slug/backlog/tasks"
  printf '%s\n' "mission: $slug" >"$MISSIONS_REPO_DIR/missions/$slug/mission.md"
  run_mish "$INVOKE_DIR" "invalid-flag-${slug//-/x}" status --mission "$slug"
  assert_status 1
  assert_contains "$LAST_ERR" "invalid mission slug"
  invalid_marker="$WORK/invalid-marker-${slug//-/x}"
  mkdir -p "$invalid_marker"
  write_marker "$invalid_marker" "$slug"
  run_mish "$invalid_marker" "invalid-marker-${slug//-/x}" status
  assert_status 1
  assert_contains "$LAST_ERR" "invalid mission slug"
  run_mish "$MISSIONS_REPO_DIR/missions/$slug/backlog/tasks" "invalid-cwd-${slug//-/x}" status
  assert_status 1
  assert_contains "$LAST_ERR" "invalid mission slug"
done

all_green
