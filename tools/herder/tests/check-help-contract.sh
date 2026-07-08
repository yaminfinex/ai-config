#!/usr/bin/env bash
# check-help-contract.sh — smoke-assert every subcommand's --help text.
#
# Not byte-pinned: help text is prose that carries operational doctrine and is
# expected to evolve. This suite asserts only the invariants a caller relies on:
#   1. `herder <cmd> --help` exits 0.
#   2. It names itself ("herder <cmd>") so the reader knows what they invoked.
#   3. It carries NO leaked bash-script corpse (`#!/usr/bin/env bash`,
#      `set -euo pipefail`) — help is plain CLI text, not a script header.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
trap 'rm -rf "$ROOT"' EXIT

for tool in herdr jq hcom; do
  cat >"$MOCKBIN/$tool" <<'MOCK'
#!/usr/bin/env bash
exit 0
MOCK
  chmod +x "$MOCKBIN/$tool"
done
cat >"$MOCKBIN/uuidgen" <<'MOCK_UUIDGEN'
#!/usr/bin/env bash
printf '00000000-0000-0000-0000-000000000000\n'
MOCK_UUIDGEN
chmod +x "$MOCKBIN/uuidgen"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin:/sbin"

run_help() {
  local cmd="$1"
  mkdir -p "$ROOT/home/.local/state/herder" "$ROOT/cache" "$ROOT/gocache"
  : >"$ROOT/home/.local/state/herder/registry.jsonl"
  env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$ROOT/home" \
    XDG_CACHE_HOME="$ROOT/cache" \
    GOCACHE="$ROOT/gocache" \
    HERDR_ENV=1 \
    HERDR_PANE_ID=p_help \
    "$REPO_ROOT/bin/herder" $cmd --help 2>&1
}

fail=0
assert_help() {
  local cmd="$1" out code
  out="$(run_help "$cmd")"
  code=$?
  if [[ "$code" -ne 0 ]]; then
    printf 'FAIL  %-8s --help exited %s (want 0)\n' "$cmd" "$code"; fail=1; return
  fi
  if ! grep -qF "herder $cmd" <<<"$out"; then
    printf 'FAIL  %-8s --help does not mention "herder %s"\n' "$cmd" "$cmd"; fail=1; return
  fi
  if grep -qF '#!/usr/bin/env bash' <<<"$out"; then
    printf 'FAIL  %-8s --help leaks a shebang line\n' "$cmd"; fail=1; return
  fi
  if grep -qF 'set -euo pipefail' <<<"$out"; then
    printf 'FAIL  %-8s --help leaks "set -euo pipefail"\n' "$cmd"; fail=1; return
  fi
  printf 'PASS  %s\n' "$cmd"
}

for cmd in spawn send list wait cull enroll rename retire reopen fork resume compact node launch sidecar; do
  assert_help "$cmd"
done

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — every subcommand --help is clean.\n'
  exit 0
else
  printf '\nHELP CONTRACT FAILED — see failures above.\n'
  exit 1
fi
