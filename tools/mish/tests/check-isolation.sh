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

run_mish_with_path() {
  local cwd=$1 name=$2 extra_path=$3
  shift 3
  LAST_OUT="$WORK/${name}.out"
  LAST_ERR="$WORK/${name}.err"
  set +e
  (cd "$cwd" && env -i HOME="$HOME_DIR" USER="mish-u10" PATH="$extra_path:$BIN:$ORIG_PATH" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null MISSIONS_REPO="$MISSIONS_REPO_DIR" SESSION_OWNER="plain-owner" mish "$@" >"$LAST_OUT" 2>"$LAST_ERR")
  LAST_STATUS=$?
  set -e
}

shim=$(make_git_shim)

step "AC-14 plain non-git mission works with no git invocation"
: >"$WORK/git.log"
run_mish_with_path "$INVOKE_DIR" "plain-new" "$shim" new plain-run --authority hera
assert_status 0
run_mish_with_path "$INVOKE_DIR" "plain-task" "$shim" backlog --mission plain-run task create "Plain task"
assert_status 0
run_mish_with_path "$(mission_dir plain-run)" "plain-status" "$shim" status
assert_status 0
[ ! -s "$WORK/git.log" ] || fail "non-git run invoked git"
assert_not_contains "$LAST_OUT" "stale"

step "AC-14 git-backed status uses only read-only git commands"
git_init_repo "$MISSIONS_REPO_DIR"
git_commit_all "$MISSIONS_REPO_DIR" "plain seed"
origin="$WORK/isolation-origin.git"
git_init_bare_origin "$origin"
git -C "$MISSIONS_REPO_DIR" remote add origin "$origin"
git -C "$MISSIONS_REPO_DIR" push -q -u origin HEAD:main
printf 'unpushed under shim\n' >"$(mission_dir plain-run)/artifacts/git-backed-unpushed.txt"
git_commit_all "$MISSIONS_REPO_DIR" "local unpushed mission change"
: >"$WORK/git.log"
run_mish_with_path "$(mission_dir plain-run)" "git-status" "$shim" status
assert_status 0
assert_contains "$LAST_OUT" '"warnings":["mission subtree has uncommitted or unpushed changes"]'
if [ ! -s "$WORK/git.log" ]; then
  fail "git-backed status did not perform the expected read-only staleness query"
fi
assert_contains "$WORK/git.log" "status --porcelain -- missions/plain-run"
assert_contains "$WORK/git.log" "rev-list --count @{u}..HEAD -- missions/plain-run"
while IFS= read -r line; do
  case "$line" in
    rev-parse*|status*|rev-list*) ;;
    *) fail "git shim observed non-read-only command: $line" ;;
  esac
done <"$WORK/git.log"

all_green
