#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

make_git_shim() {
  local real_git shim_dir
  real_git=$(command -v git)
  shim_dir="$WORK/git-shim"
  mkdir -p "$shim_dir"
  cat >"$shim_dir/git" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$WORK/git.log"
exec "$real_git" "\$@"
EOF
  chmod +x "$shim_dir/git"
  echo "$shim_dir"
}

step "AC-1 scaffold format and owner/authority echo"
shim=$(make_git_shim)
: >"$WORK/git.log"
PATH="$shim:$PATH" SESSION_OWNER_VALUE="env-owner" run_mish "$INVOKE_DIR" "new-perf-regression" new perf-regression --authority hera
assert_status 0
[ ! -s "$WORK/git.log" ] || fail "AC-1 scaffold invoked git"
mission="$(mission_dir perf-regression)"
assert_dir "$mission"
assert_file "$mission/mission.md"
assert_file "$mission/backlog/config.yml"
assert_dir "$mission/backlog/tasks"
assert_dir "$mission/artifacts"
assert_file "$mission/artifacts/.gitkeep"
assert_file "$INVOKE_DIR/.mission"
assert_eq "$(cat "$INVOKE_DIR/.mission")" "perf-regression" "marker content"
assert_contains "$mission/mission.md" "mission: perf-regression"
assert_contains "$mission/mission.md" "authority: hera"
assert_contains "$mission/mission.md" "owner: env-owner"
assert_contains "$mission/mission.md" "status: active"
assert_contains "$mission/backlog/config.yml" 'project_name: "perf-regression"'
for pin in \
  "check_active_branches: false" \
  "remote_operations: false" \
  "auto_commit: false" \
  "auto_open_browser: false" \
  "filesystem_only: true"; do
  assert_contains "$mission/backlog/config.yml" "$pin"
done
assert_contains "$LAST_OUT" '"authority":"hera"'
assert_contains "$LAST_OUT" '"owner":"env-owner"'
assert_contains "$LAST_OUT" '"authority_source":"flag"'
assert_contains "$LAST_OUT" '"owner_source":"env"'
find "$MISSIONS_REPO_DIR" -name AGENTS.md -print -quit | grep -q . && fail "scaffold wrote AGENTS.md"
top_listing=$(cd "$mission" && find . -mindepth 1 -maxdepth 2 -print | sort)
assert_contains <(printf '%s\n' "$top_listing") "./artifacts"
assert_contains <(printf '%s\n' "$top_listing") "./backlog"
assert_contains <(printf '%s\n' "$top_listing") "./mission.md"

step "AC-1 owner falls back when SESSION_OWNER is unset"
SESSION_OWNER_VALUE="" new_mission owner-fallback --authority sesh
os_owner=$(id -un)
assert_contains "$(mission_dir owner-fallback)/mission.md" "owner: $os_owner"
assert_contains "$LAST_OUT" "\"owner\":\"$os_owner\""
assert_contains "$LAST_OUT" '"owner_source":"OS user"'

step "AC-2 slug rules and existing slug refusal"
run_mish "$INVOKE_DIR" "help-slug" new help --no-marker
assert_status 0
assert_dir "$(mission_dir help)"
assert_contains "$LAST_OUT" '"ok":true'
assert_contains "$LAST_OUT" '"slug":"help"'
run_mish "$INVOKE_DIR" "extra-slug-refuses" new another slug
assert_status 2
assert_contains "$LAST_ERR" "expected exactly one slug"
for slug in perf-regression Perf_Regression -x a--b x- "$(printf 'a%.0s' {1..65})"; do
  run_mish "$INVOKE_DIR" "slug-${slug//[^a-zA-Z0-9]/_}" new "$slug"
  assert_status 1
  assert_contains "$LAST_ERR" "mish new:"
done
replace_in_file "$mission/mission.md" "mission: perf-regression" "mission: old-name"
run_mish "$mission" "status-slug-mismatch" status
assert_status 0
assert_contains "$LAST_OUT" 'mission frontmatter \"old-name\" does not match directory \"perf-regression\"'
replace_in_file "$mission/mission.md" "mission: old-name" "mission: perf-regression"

step "AC-3 marker safety"
other="$WORK/other-worktree"
mkdir -p "$other/child"
write_marker "$other" other-slug
run_mish "$other/child" "marker-different-refuses" new nested
assert_status 1
assert_contains "$LAST_ERR" ".mission marker"
assert_contains "$LAST_ERR" "names other-slug, not nested"
write_marker "$other" same-slug
run_mish "$other/child" "marker-same-noop" new same-slug
assert_status 0
assert_no_file "$other/child/.mission"
mkdir -p "$WORK/no-marker"
run_mish "$WORK/no-marker" "marker-no-marker" new no-marker --no-marker
assert_status 0
assert_no_file "$WORK/no-marker/.mission"
run_mish "$MISSIONS_REPO_DIR" "marker-inside-repo" new inside-repo
assert_status 0
assert_no_file "$MISSIONS_REPO_DIR/.mission"

step "AC-4 board ready"
run_mish "$INVOKE_DIR" "task-create" backlog task create "First task"
assert_status 0
assert_file "$(single_task_file perf-regression)"

step "--text restores prior human output"
text_cwd="$WORK/text-cwd"
mkdir -p "$text_cwd"
run_mish "$text_cwd" "new-text-mode" new text-mode --authority hera --owner riley --text
assert_status 0
assert_contains "$LAST_OUT" "created mission text-mode"
assert_contains "$LAST_OUT" "authority: hera (source: flag)"
assert_contains "$LAST_OUT" "owner: riley (source: flag)"
run_mish "$(mission_dir text-mode)" "status-text-mode" status --text
assert_status 0
assert_contains "$LAST_OUT" "mission: text-mode"
assert_contains "$LAST_OUT" "board:"

all_green
