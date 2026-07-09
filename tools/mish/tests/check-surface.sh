#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

git_init_repo "$MISSIONS_REPO_DIR"
new_mission surface-a --authority hera
run_mish "$INVOKE_DIR" "surface-task" backlog --mission surface-a task create "Surface task"
assert_status 0
git_commit_all "$MISSIONS_REPO_DIR" "seed surface"
origin="$WORK/surface-origin.git"
git init -q --bare "$origin"
git -C "$origin" symbolic-ref HEAD refs/heads/main
git -C "$MISSIONS_REPO_DIR" remote add origin "$origin"
git -C "$MISSIONS_REPO_DIR" push -q -u origin HEAD:main

step "AC-11 allowlist and passthrough surface"
for sub in init config agents future-subcommand; do
  run_mish "$INVOKE_DIR" "deny-$sub" backlog --mission surface-a "$sub" ${sub/config/set}
  assert_status 1
  assert_contains "$LAST_ERR" "is not allowed"
  assert_contains "$LAST_ERR" "task, tasks, draft, board"
done
run_mish "$INVOKE_DIR" "board-pass" backlog --mission surface-a board
assert_status 0
assert_contains "$LAST_OUT" "Surface task"
run_mish "$INVOKE_DIR" "edit-pass" backlog --mission surface-a task edit TASK-1 -s "In Progress"
assert_status 0
assert_contains "$(single_task_file surface-a)" "status: In Progress"
run_mish "$INVOKE_DIR" "bare-backlog" backlog
assert_status 0
assert_contains "$LAST_OUT" "Allowed subcommands:"
run_mish "$INVOKE_DIR" "task-help" backlog --mission surface-a task --help
assert_status 0
assert_contains "$LAST_OUT" "Usage: backlog task"

step "AC-12 status detail, warnings, staleness, and read-only hash"
before=$(tree_hash "$(mission_dir surface-a)")
run_mish "$(mission_dir surface-a)" "status-clean" status
assert_status 0
assert_contains "$LAST_OUT" "mission: surface-a"
assert_contains "$LAST_OUT" "board:"
assert_contains "$LAST_OUT" "artifacts:"
after=$(tree_hash "$(mission_dir surface-a)")
assert_eq "$after" "$before" "status subtree hash"
replace_in_file "$(mission_dir surface-a)/backlog/config.yml" "auto_commit: false" "auto_commit: true"
run_mish "$(mission_dir surface-a)" "status-pin-warning" status
assert_status 0
assert_contains "$LAST_OUT" "warning: pinned board key drift: auto_commit"
replace_in_file "$(mission_dir surface-a)/backlog/config.yml" "auto_commit: true" "auto_commit: false"
replace_in_file "$(mission_dir surface-a)/mission.md" "mission: surface-a" "mission: wrong-surface"
run_mish "$(mission_dir surface-a)" "status-name-warning" status
assert_status 0
assert_contains "$LAST_OUT" "warning: mission frontmatter"
replace_in_file "$(mission_dir surface-a)/mission.md" "mission: wrong-surface" "mission: surface-a"
rm -rf "$(mission_dir surface-a)/artifacts"
run_mish "$(mission_dir surface-a)" "status-artifacts-warning" status
assert_status 0
assert_contains "$LAST_OUT" "warning: artifacts missing"
mkdir -p "$(mission_dir surface-a)/artifacts"
printf 'dirty\n' >"$(mission_dir surface-a)/artifacts/dirty.txt"
run_mish "$(mission_dir surface-a)" "status-stale-warning" status
assert_status 0
assert_contains "$LAST_OUT" "warning: mission subtree has uncommitted or unpushed changes"

step "AC-13 status overview"
new_mission surface-closed --authority hera
replace_in_file "$(mission_dir surface-closed)/mission.md" "status: active" "status: closed"
run_mish "$MISSIONS_REPO_DIR" "overview-root" status
assert_status 0
assert_contains "$LAST_OUT" "surface-a"
assert_contains "$LAST_OUT" "surface-closed"
outside="$WORK/outside"
mkdir -p "$outside"
run_mish "$outside" "overview-refuse-outside" status
assert_status 1
assert_contains "$LAST_ERR" "--mission"

all_green
