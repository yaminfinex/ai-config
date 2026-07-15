#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

step "AC-15 clean union across two clones"
origin="$WORK/origin.git"
seed="$WORK/seed"
git_init_bare_origin "$origin"
mkdir -p "$seed"
git_init_repo "$seed"
MISSIONS_REPO_DIR="$seed" new_mission union-demo --authority hera
MISSIONS_REPO_DIR="$seed" run_mish "$INVOKE_DIR" "union-task-a" backlog --mission union-demo task create "Node A task"
assert_status 0
MISSIONS_REPO_DIR="$seed" run_mish "$INVOKE_DIR" "union-task-b" backlog --mission union-demo task create "Node B task"
assert_status 0
git_commit_all "$seed" "seed union mission"
git -C "$seed" remote add origin "$origin"
git -C "$seed" push -q -u origin HEAD:main

git clone -q "$origin" "$WORK/node-a"
git clone -q "$origin" "$WORK/node-b"
git -C "$WORK/node-a" config user.email node-a@example.invalid
git -C "$WORK/node-a" config user.name "node a"
git -C "$WORK/node-b" config user.email node-b@example.invalid
git -C "$WORK/node-b" config user.name "node b"

MISSIONS_REPO_DIR="$WORK/node-a" run_mish "$WORK/node-a/missions/union-demo" "node-a-edit" backlog task edit TASK-1 -s "In Progress"
assert_status 0
mkdir -p "$WORK/node-a/missions/union-demo/artifacts/a"
printf 'node a\n' >"$WORK/node-a/missions/union-demo/artifacts/a/result.txt"
git_commit_all "$WORK/node-a" "mission(union-demo): harvest node-a"
git -C "$WORK/node-a" push -q origin HEAD:main

MISSIONS_REPO_DIR="$WORK/node-b" run_mish "$WORK/node-b/missions/union-demo" "node-b-edit" backlog task edit TASK-2 -s "In Progress"
assert_status 0
mkdir -p "$WORK/node-b/missions/union-demo/artifacts/b"
printf 'node b\n' >"$WORK/node-b/missions/union-demo/artifacts/b/result.txt"
git_commit_all "$WORK/node-b" "mission(union-demo): harvest node-b"
git -C "$WORK/node-b" pull -q --no-rebase origin main
git -C "$WORK/node-b" push -q origin HEAD:main
git -C "$WORK/node-a" pull -q --no-rebase origin main

assert_file "$WORK/node-a/missions/union-demo/artifacts/a/result.txt"
assert_file "$WORK/node-a/missions/union-demo/artifacts/b/result.txt"
assert_file "$WORK/node-b/missions/union-demo/artifacts/a/result.txt"
assert_file "$WORK/node-b/missions/union-demo/artifacts/b/result.txt"
if grep -R -n -E '^(<<<<<<<|=======|>>>>>>>)' "$WORK/node-a/missions/union-demo" "$WORK/node-b/missions/union-demo"; then
  fail "merged union tree contains conflict markers"
fi
MISSIONS_REPO_DIR="$WORK/node-a" run_mish "$WORK/node-a/missions/union-demo" "union-status" status
assert_status 0
assert_contains "$LAST_OUT" '"total":2'
MISSIONS_REPO_DIR="$WORK/node-b" run_mish "$WORK/node-b/missions/union-demo" "union-status-node-b" status
assert_status 0
assert_contains "$LAST_OUT" '"total":2'
MISSIONS_REPO_DIR="$WORK/node-b" run_mish "$WORK/node-b/missions/union-demo" "union-list-node-b" backlog task list
assert_status 0
assert_contains "$LAST_OUT" "Node A task"
assert_contains "$LAST_OUT" "Node B task"

all_green
