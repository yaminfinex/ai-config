#!/usr/bin/env bash
# check-launcher-doctor.sh - hermetic contract for ai-doctor's login-shell
# launcher checks and the mise-owned duplicate-vendor check.
#
# The launcher generation replaced global PATH-shim interception with shell
# functions (lib/launchers.sh via the ai-setup rc block): a function wins name
# resolution over every PATH entry, so mise hook-env re-fronting its shims dir
# on a config-boundary cd can no longer reroute a hand-typed claude. The
# doctor's job therefore changed shape, and this test pins it:
#   1. rc block ABSENT + a PATH claude present -> doctor warns that claude
#      resolves to a path instead of the launcher function.
#   2. rc block INSTALLED -> no launcher warning, even with a shadow dir
#      prepended ahead of everything (the pre-function failure mode).
#   3. a `mish` copy shadowing the bin/ wrapper is still flagged (bin tools
#      remain PATH-resolved).
#   4. a claude inside a mise-managed node install is flagged as a duplicate
#      vendor copy; a clean mise tree is not.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene: herder-spawned agents export AI_CONFIG_ROOT pointing at the
# spawner's checkout — pin it to THIS tree so the doctor under test is ours.
unset HERDER_BIN
AI_DOCTOR="$REPO/bin/ai-doctor"

# Canonicalize so fixture paths match the doctor's abs_path() answers
# (macOS /tmp symlink).
ROOT="$(cd "$(mktemp -d)" && pwd -P)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) ok "$name" ;;
    *) bad "$name" "missing [$needle]" ;;
  esac
}

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) bad "$name" "unexpected [$needle]" ;;
    *) ok "$name" ;;
  esac
}

for dep in git mktemp timeout; do
  command -v "$dep" >/dev/null 2>&1 || {
    printf 'FAIL  harness dependency missing: %s\n' "$dep" >&2
    exit 1
  }
done

FIXTURE="$ROOT/repo"
HOME_DIR="$ROOT/home"
XDG_CONFIG="$ROOT/config"
MISE_DATA="$ROOT/mise-data"
SHADOW_DIR="$HOME_DIR/shadow-bin"
HERDER_SHIMS="$FIXTURE/tools/herder/shims"
mkdir -p "$FIXTURE/bin" "$FIXTURE/lib" "$HERDER_SHIMS" \
  "$HOME_DIR/.claude" "$XDG_CONFIG/mise/conf.d" "$MISE_DATA/installs" "$SHADOW_DIR"

cp "$REPO/lib/common.sh" "$REPO/lib/mise-path.sh" "$REPO/lib/grok-health.sh" \
  "$REPO/lib/launchers.sh" "$REPO/lib/shell-rc.sh" "$FIXTURE/lib/"
for tool in herder mish; do
  printf '%s\n' '#!/bin/sh' 'exit 0' > "$FIXTURE/bin/$tool"
done
# Launch-scoped shim files still ship (the spawner injects the dir per-spawn);
# the grok doctor check requires the grok one to exist.
for tool in claude codex grok hcom; do
  printf '%s\n' '#!/bin/sh' '# herder-path-shim' 'exit 0' > "$HERDER_SHIMS/$tool"
done
# Shadows: a raw `claude` and a stale `mish` copy ahead of everything.
printf '%s\n' '#!/bin/sh' 'exit 0' > "$SHADOW_DIR/claude"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$SHADOW_DIR/mish"
chmod +x "$FIXTURE"/bin/* "$HERDER_SHIMS"/* "$SHADOW_DIR"/*

printf '%s\n' '{"statusLine":{"command":"$HOME/.claude/statusline.sh"}}' > "$HOME_DIR/.claude/settings.json"
{
  printf '%s\n' '# Managed by ai-config. Remove with: bin/ai-setup --shims remove'
  printf '%s\n' '[env]'
  printf '_.path = ["%s"]\n' "$FIXTURE/bin"
} > "$XDG_CONFIG/mise/conf.d/ai-config.toml"

git init -b main "$FIXTURE" >/dev/null 2>&1
git -C "$FIXTURE" config user.name fixture
git -C "$FIXTURE" config user.email fixture@example.invalid
git -C "$FIXTURE" add . >/dev/null 2>&1
git -C "$FIXTURE" commit -m fixture >/dev/null 2>&1

# The shadow dir sits FIRST on the base PATH — the exact position that beat
# the shim generation. Functions must not care.
PATH_VALUE="$SHADOW_DIR:$FIXTURE/bin:/usr/bin:/bin"
BASE_ENV=(
  "PATH=$PATH_VALUE"
  "HOME=$HOME_DIR"
  "SHELL=/bin/bash"
  "XDG_CONFIG_HOME=$XDG_CONFIG"
  "MISE_DATA_DIR=$MISE_DATA"
  "AI_CONFIG_ROOT=$FIXTURE"
  "XAI_API_KEY=present"
)

doctor() {
  (cd "$FIXTURE" && env -i "${BASE_ENV[@]}" bash "$AI_DOCTOR" --quick 2>&1)
}

# --- 1: no rc block -> claude resolves to a path, doctor flags it ------------
: > "$HOME_DIR/.profile"
: > "$HOME_DIR/.bashrc"
OUT_NOBLOCK="$(doctor)"
assert_contains "no rc block: claude flagged" "$OUT_NOBLOCK" "login shell resolves 'claude' to $SHADOW_DIR/claude instead of the launcher function"
assert_contains "no rc block: remedy names ai-setup" "$OUT_NOBLOCK" "Run bin/ai-setup to install the rc block"
# mish bin wrapper shadowing is a separate, still-live check.
assert_contains "no rc block: mish shadow flagged" "$OUT_NOBLOCK" "login shell resolves 'mish' to $SHADOW_DIR/mish, not the ai-config checkout ($FIXTURE/bin/mish)"

# --- 2: rc block installed -> functions win, no launcher warnings ------------
# Install via the real shell_rc library against the fixture HOME/root, and
# mirror Ubuntu's profile->bashrc sourcing so a login+interactive probe loads it.
env -i "${BASE_ENV[@]}" bash -c '
  set -e
  cd "$AI_CONFIG_ROOT"
  source lib/common.sh
  source lib/shell-rc.sh
  dry_run=0 shell_rc_install >/dev/null
'
printf '%s\n' '. "$HOME/.bashrc"' > "$HOME_DIR/.profile"
OUT_BLOCK="$(doctor)"
assert_not_contains "rc block: no claude warning" "$OUT_BLOCK" "instead of the launcher function"
assert_contains "rc block: mish shadow still flagged" "$OUT_BLOCK" "login shell resolves 'mish'"

# --- 3: mise-owned duplicate vendor copy is flagged --------------------------
NODE_BIN="$MISE_DATA/installs/node/25.9.0/bin"
mkdir -p "$NODE_BIN"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$NODE_BIN/claude"
chmod +x "$NODE_BIN/claude"
OUT_DUP="$(doctor)"
assert_contains "duplicate: mise-owned claude flagged" "$OUT_DUP" "mise-owned copy of claude shadows the vendor CLI: $NODE_BIN/claude"
rm -rf "$MISE_DATA/installs/node"
OUT_CLEAN="$(doctor)"
assert_not_contains "duplicate: clean tree quiet" "$OUT_CLEAN" "mise-owned copy of claude"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - launcher doctor contract holds.\n'
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above.\n'
exit 1
