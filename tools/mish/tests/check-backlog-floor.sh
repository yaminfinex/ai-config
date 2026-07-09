#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

step "Backlog floor AC-5 nested isolation"
git_init_repo "$MISSIONS_REPO_DIR"
(cd "$MISSIONS_REPO_DIR" && backlog init root-board --defaults --integration-mode none --backlog-dir backlog --config-location folder --no-git --auto-open-browser false >/dev/null)
(cd "$MISSIONS_REPO_DIR" && backlog task create "Root task" >/dev/null)
new_mission floor-demo --authority hera
run_mish "$INVOKE_DIR" "floor-task" backlog --mission floor-demo task create "Floor task"
assert_status 0
assert_eq "$(find "$MISSIONS_REPO_DIR/backlog/tasks" -type f -name '*.md' | wc -l | awk '{print $1}')" "1" "root count"
assert_eq "$(task_files floor-demo | wc -l | awk '{print $1}')" "1" "mission count"

step "Backlog floor AC-6 no ancestor fallthrough"
rm -f "$(mission_dir floor-demo)/backlog/config.yml"
run_mish "$INVOKE_DIR" "floor-missing-board" backlog --mission floor-demo task list
assert_status 1
assert_contains "$LAST_ERR" "board missing"

step "Backlog floor AC-7 branch-scan pin"
new_mission floor-branch --authority hera
run_mish "$INVOKE_DIR" "floor-branch-task" backlog --mission floor-branch task create "Branch-safe floor task"
assert_status 0
git_commit_all "$MISSIONS_REPO_DIR" "floor seed"
git -C "$MISSIONS_REPO_DIR" checkout -q -b floor-side-a
git -C "$MISSIONS_REPO_DIR" checkout -q -b floor-side-b
git -C "$MISSIONS_REPO_DIR" checkout -q floor-side-a
run_mish "$INVOKE_DIR" "floor-branch-list" backlog --mission floor-branch task list
assert_status 0
assert_contains "$LAST_OUT" "Branch-safe floor task"

step "Backlog floor AC-19 references"
run_mish "$INVOKE_DIR" "floor-ref-set" backlog --mission floor-branch task edit TASK-1 --ref "repo@abc123" --ref "session:floor"
assert_status 0
task_file=$(single_task_file floor-branch)
assert_contains "$task_file" "repo@abc123"
assert_contains "$task_file" "session:floor"
run_mish "$INVOKE_DIR" "floor-ref-unrelated" backlog --mission floor-branch task edit TASK-1 -s "In Progress" --comment "still referenced"
assert_status 0
assert_contains "$task_file" "repo@abc123"
run_mish "$INVOKE_DIR" "floor-ref-plain" backlog --mission floor-branch task TASK-1 --plain
assert_status 0
assert_contains "$LAST_OUT" "repo@abc123"
run_mish "$INVOKE_DIR" "floor-ref-replace" backlog --mission floor-branch task edit TASK-1 --ref "pr:99"
assert_status 0
assert_contains "$task_file" "pr:99"
assert_not_contains "$task_file" "repo@abc123"

all_green
