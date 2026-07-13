#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

step "AC-5 nested board isolation"
git_init_repo "$MISSIONS_REPO_DIR"
(cd "$MISSIONS_REPO_DIR" && backlog init root-board --defaults --integration-mode none --backlog-dir backlog --config-location folder --no-git --auto-open-browser false >/dev/null)
(cd "$MISSIONS_REPO_DIR" && backlog task create "Root task" >/dev/null)
new_mission nested-a --authority hera
run_mish "$INVOKE_DIR" "nested-task" backlog --mission nested-a task create "Mission task"
assert_status 0
root_count=$(find "$MISSIONS_REPO_DIR/backlog/tasks" -type f -name '*.md' | wc -l | awk '{print $1}')
mission_count=$(task_files nested-a | wc -l | awk '{print $1}')
assert_eq "$root_count" "1" "root board task count"
assert_eq "$mission_count" "1" "mission board task count"
(cd "$MISSIONS_REPO_DIR" && backlog task create "Root task two" >/dev/null)
mission_count_after=$(task_files nested-a | wc -l | awk '{print $1}')
assert_eq "$mission_count_after" "1" "mission count after root operation"

step "AC-6 missing board does not fall through"
rm -f "$(mission_dir nested-a)/backlog/config.yml"
run_mish "$INVOKE_DIR" "missing-board" backlog --mission nested-a task list
assert_status 1
assert_contains "$LAST_ERR" "board missing"
assert_contains "$LAST_ERR" "scaffold damaged or wrong mission"

step "AC-7 check_active_branches pin avoids branch scan hydration"
new_mission branch-safe --authority hera
run_mish "$INVOKE_DIR" "branch-task" backlog --mission branch-safe task create "Branch task"
assert_status 0
git_commit_all "$MISSIONS_REPO_DIR" "seed nested boards"
git -C "$MISSIONS_REPO_DIR" checkout -q -b side-a
git -C "$MISSIONS_REPO_DIR" checkout -q -b side-b
git -C "$MISSIONS_REPO_DIR" checkout -q side-a
run_mish "$INVOKE_DIR" "branch-list" backlog --mission branch-safe task list
assert_status 0
assert_contains "$LAST_OUT" "Branch task"

step "AC-8 mission subtree moves as a unit"
git -C "$MISSIONS_REPO_DIR" checkout -q side-b
git -C "$MISSIONS_REPO_DIR" mv missions/branch-safe missions/branch-moved
replace_in_file "$MISSIONS_REPO_DIR/missions/branch-moved/mission.md" "mission: branch-safe" "mission: branch-moved"
replace_in_file "$MISSIONS_REPO_DIR/missions/branch-moved/backlog/config.yml" 'project_name: "branch-safe"' 'project_name: "branch-moved"'
write_marker "$INVOKE_DIR" branch-moved
run_mish "$INVOKE_DIR" "moved-list" backlog task list
assert_status 0
assert_contains "$LAST_OUT" "Branch task"
run_mish "$INVOKE_DIR" "moved-status" status
assert_status 0
assert_contains "$LAST_OUT" "mission: branch-moved"

all_green
