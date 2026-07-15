#!/usr/bin/env bash
# check-node-contract.sh — lock the herder node init command contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
GOLDENS="$TESTS_DIR/goldens/node"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

toolchain_fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
mkdir -p "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

GO_MOD="$REPO_ROOT/tools/herder/go.mod"
GO_VERSION="$(awk '$1 == "go" {print $2; exit}' "$GO_MOD")"
[ -n "$GO_VERSION" ] || toolchain_fail "cannot read the toolchain pin ('go X.Y.Z') from $GO_MOD"
TOOLCHAIN="$(awk '$1 == "toolchain" {print $2; exit}' "$GO_MOD")"
[ -z "$TOOLCHAIN" ] || [ "$TOOLCHAIN" = "go$GO_VERSION" ] ||
  toolchain_fail "go.mod declares toolchain ${TOOLCHAIN} but pins go ${GO_VERSION}; the go directive is the authority — align or drop the toolchain directive"
GO_ROOT="$(mise where "go@$GO_VERSION" 2>/dev/null)" ||
  toolchain_fail "go ${GO_VERSION} is not installed; fix: mise install go@${GO_VERSION}"
GO_BIN="$GO_ROOT/bin"
GO_HAVE="$(env -u GOROOT GOTOOLCHAIN=local "$GO_BIN/go" env GOVERSION 2>/dev/null)" ||
  toolchain_fail "cannot execute the pinned go toolchain at $GO_BIN/go"
GO_HAVE="${GO_HAVE#go}"
[ "$GO_HAVE" = "$GO_VERSION" ] ||
  toolchain_fail "go toolchain resolves to ${GO_HAVE:-unknown}, but go.mod pins go ${GO_VERSION}"
PATH_HERMETIC="$GO_BIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
HN=("$REPO_ROOT/bin/herder" node)
HRN=("$REPO_ROOT/bin/herder" rename)
NODE_A="11111111-1111-4111-8111-111111111111"
NODE_B="22222222-2222-4222-8222-222222222222"
NODE_C="33333333-3333-4333-8333-333333333333"
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

run_rename() {
  local case_dir="$1"; shift
  RUN_ERR_F="$case_dir/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$case_dir/home" \
    HERDER_STATE_DIR="$case_dir/state" \
    GOTOOLCHAIN=local \
    "${HRN[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

normalize() {
  sed -E "s|$ROOT|<ROOT>|g; s/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<NODE>/g"
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
  printf '%s\n' "$NODE_A" >"$case_dir/state/node_id"
  run_node "$case_dir" init
  check_one repair_marker_only
}

scenario_repair_row_only() {
  local case_dir="$ROOT/repair_row_only"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:00Z"}\n' "$NODE_B" >"$case_dir/state/registry.jsonl"
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

scenario_gate_refuses_marker_only() {
  local case_dir="$ROOT/gate_refuses_marker_only"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '%s\n' "$NODE_A" >"$case_dir/state/node_id"
  printf '{"guid":"guid-alpha","label":"alpha","status":"active"}\n' >"$case_dir/state/registry.jsonl"
  run_rename "$case_dir" alpha beta
  check_one gate_refuses_marker_only
}

scenario_gate_refuses_disagree() {
  local case_dir="$ROOT/gate_refuses_disagree"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '%s\n' "$NODE_A" >"$case_dir/state/node_id"
  printf '{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:00Z"}\n{"guid":"guid-alpha","label":"alpha","status":"active"}\n' "$NODE_B" >"$case_dir/state/registry.jsonl"
  run_rename "$case_dir" alpha beta
  check_one gate_refuses_disagree
}

scenario_init_refuses_malformed_marker() {
  local case_dir="$ROOT/init_refuses_malformed_marker"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf 'not-a-node-id\n' >"$case_dir/state/node_id"
  printf '{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:00Z"}\n' "$NODE_B" >"$case_dir/state/registry.jsonl"
  run_node "$case_dir" init
  check_one init_refuses_malformed_marker
}

scenario_init_new_refuses_malformed_marker_with_registry() {
  local case_dir="$ROOT/init_new_refuses_malformed_marker_with_registry"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf 'not-a-node-id\n' >"$case_dir/state/node_id"
  printf '{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:00Z"}\n' "$NODE_B" >"$case_dir/state/registry.jsonl"
  run_node "$case_dir" init --new
  check_one init_new_refuses_malformed_marker_with_registry
}

scenario_init_refuses_conflict_multiple_nodes() {
  local case_dir="$ROOT/init_refuses_conflict_multiple_nodes"
  mkdir -p "$case_dir/home" "$case_dir/state"
  printf '%s\n' "$NODE_A" >"$case_dir/state/node_id"
  printf '{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:00Z"}\n{"kind":"node","event":"node_registered","node_id":"%s","recorded_at":"2026-07-08T00:00:01Z"}\n' "$NODE_B" "$NODE_C" >"$case_dir/state/registry.jsonl"
  run_node "$case_dir" init
  check_one init_refuses_conflict_multiple_nodes
}

scenario_init_empty
scenario_idempotent
scenario_repair_marker_only
scenario_repair_row_only
scenario_repair_empty_marker
scenario_repair_empty_marker_new
scenario_clone_new
scenario_gate_refuses_marker_only
scenario_gate_refuses_disagree
scenario_init_refuses_malformed_marker
scenario_init_new_refuses_malformed_marker_with_registry
scenario_init_refuses_conflict_multiple_nodes

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HN[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — node init contract matches goldens.\n'; exit 0
else
  printf '\nNODE CONTRACT DRIFT — see diffs above.\n'; exit 1
fi
