#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

new_mission refs-demo --authority hera
run_mish "$INVOKE_DIR" "refs-create" backlog --mission refs-demo task create "Referenced task"
assert_status 0

step "AC-19 references write, survive unrelated edits, render, and replace"
run_mish "$INVOKE_DIR" "refs-set" backlog --mission refs-demo task edit TASK-1 --ref "repo@example" --ref "session:agent-a"
assert_status 0
task_file=$(single_task_file refs-demo)
assert_contains "$task_file" "references:"
assert_contains "$task_file" "repo@example"
assert_contains "$task_file" "session:agent-a"
run_mish "$INVOKE_DIR" "refs-unrelated" backlog --mission refs-demo task edit TASK-1 -s "In Progress" --comment "kept refs"
assert_status 0
assert_contains "$task_file" "repo@example"
assert_contains "$task_file" "session:agent-a"
run_mish "$INVOKE_DIR" "refs-plain" backlog --mission refs-demo task TASK-1 --plain
assert_status 0
assert_contains "$LAST_OUT" "repo@example"
assert_contains "$LAST_OUT" "session:agent-a"
run_mish "$INVOKE_DIR" "refs-replace" backlog --mission refs-demo task edit TASK-1 --ref "pr:42"
assert_status 0
assert_contains "$task_file" "pr:42"
assert_not_contains "$task_file" "repo@example"
assert_not_contains "$task_file" "session:agent-a"

all_green
