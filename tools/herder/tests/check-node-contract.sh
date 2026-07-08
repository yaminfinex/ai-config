#!/usr/bin/env bash
# check-node-contract.sh — lock the herder node init command contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
GOLDENS="$TESTS_DIR/goldens/node"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
mkdir -p "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

PATH_HERMETIC="/home/grace/.local/share/mise/installs/go/1.26.4/bin:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
HN=("$REPO_ROOT/bin/herder" node)
fail=0

run_node() {
  local case_dir="$1"; shift
  RUN_ERR_F="$case_dir/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$case_dir/home" \
    HERDER_STATE_DIR="$case_dir/state" \
    GOTOOLCHAIN=local \
    "${HN[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

normalize() {
  sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<NODE>/g'
}

check_one() {
  local name="$1" block gold
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" | normalize)"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/herder_node_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/herder_node_diff.$$; fail=1
  fi
  rm -f /tmp/herder_node_diff.$$
}

scenario_init_empty() {
  local case_dir="$ROOT/init_empty"
  mkdir -p "$case_dir/home" "$case_dir/state"
  run_node "$case_dir" init
  check_one init_empty
}

scenario_idempotent() {
  local case_dir="$ROOT/idempotent"
  mkdir -p "$case_dir/home" "$case_dir/state"
  run_node "$case_dir" init >/dev/null
  run_node "$case_dir" init
  check_one idempotent
}

scenario_repair_marker_only() {
  local case_dir="$ROOT/repair_marker_only"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '11111111-1111-4111-8111-111111111111\n' >"$case_dir/state/node_id"
  run_node "$case_dir" init
  check_one repair_marker_only
}

scenario_repair_row_only() {
  local case_dir="$ROOT/repair_row_only"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '{"kind":"node","event":"node_registered","node_id":"22222222-2222-4222-8222-222222222222","recorded_at":"2026-07-08T00:00:00Z"}\n' >"$case_dir/state/registry.jsonl"
  run_node "$case_dir" init
  check_one repair_row_only
}

scenario_repair_empty_marker() {
  local case_dir="$ROOT/repair_empty_marker"
  mkdir -p "$case_dir/home" "$case_dir/state"
  : >"$case_dir/state/node_id"
  run_node "$case_dir" init
  check_one repair_empty_marker
}

scenario_repair_empty_marker_new() {
  local case_dir="$ROOT/repair_empty_marker_new"
  mkdir -p "$case_dir/home" "$case_dir/state"
  : >"$case_dir/state/node_id"
  run_node "$case_dir" init --new
  check_one repair_empty_marker_new
}

scenario_clone_new() {
  local case_dir="$ROOT/clone_new"
  mkdir -p "$case_dir/home" "$case_dir/state"
  run_node "$case_dir" init >/dev/null
  run_node "$case_dir" init --new
  check_one clone_new
}

scenario_init_empty
scenario_idempotent
scenario_repair_marker_only
scenario_repair_row_only
scenario_repair_empty_marker
scenario_repair_empty_marker_new
scenario_clone_new

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HN[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — node init contract matches goldens.\n'; exit 0
else
  printf '\nNODE CONTRACT DRIFT — see diffs above.\n'; exit 1
fi
