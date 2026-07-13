#!/usr/bin/env bash
# check-grok-doctor.sh - hermetic Grok setup/doctor drift checks.
#
# Uses only mock Grok binaries and throwaway HOME/state/config/cache roots. The
# mocks fail if the launch gate exposes credential-shaped env or probes a live home.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_DOCTOR="$REPO/bin/ai-doctor"
GO_BIN="$(mise where go@1.26.5)/bin"
ROOT="$(mktemp -d)"
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

HOME_DIR="$ROOT/home"
LIVE_GROK="$HOME_DIR/.grok"
STATE_DIR="$ROOT/state/herder"
MANAGED_GROK="$STATE_DIR/grok-home"
XDG_CONFIG="$ROOT/config"
XDG_CACHE="$ROOT/cache"
MOCKBIN="$ROOT/mockbin"
WRONGBIN="$ROOT/wrongbin"
PINNED_DIR="$ROOT/pinned"
PINNED="$PINNED_DIR/grok-linux-x86_64"
mkdir -p "$LIVE_GROK" "$STATE_DIR" "$XDG_CONFIG/mise/conf.d" "$XDG_CACHE" \
  "$MOCKBIN" "$WRONGBIN" "$PINNED_DIR"
printf '%s\n' 'live-user-state' > "$LIVE_GROK/sentinel"
ln -s "$LIVE_GROK" "$MANAGED_GROK"

make_mock_grok() {
  local path="$1" version="$2"
  printf '%s\n' \
    '#!/bin/sh' \
    'set -eu' \
    'if [ "${XAI_API_KEY+x}" = x ] || [ "${OPENAI_API_KEY+x}" = x ] || [ "${ANTHROPIC_API_KEY+x}" = x ]; then exit 91; fi' \
    'dir=$(dirname "$0")' \
    'printf "%s\n" "$HOME" > "$dir/probe-home"' \
    'printf "%s\n" "$GROK_HOME" > "$dir/probe-grok-home"' \
    'case " $* " in' \
    "  *' --version '*) printf '%s\\n' 'grok $version (mock)' ;;" \
    "  *' --help '*) printf '%s\\n' '--no-subagents --session-id --rules' ;;" \
    '  *) exit 92 ;;' \
    'esac' > "$path"
  chmod +x "$path"
}

make_mock_grok "$PINNED" "0.2.93"
make_mock_grok "$WRONGBIN/grok" "0.2.99"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$MOCKBIN/mise"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$MOCKBIN/hcom"
chmod +x "$MOCKBIN/mise" "$MOCKBIN/hcom"

cat > "$XDG_CONFIG/mise/conf.d/ai-config.toml" <<EOF
# Managed by ai-config. Remove with: bin/ai-setup --shims remove
[env]
_.path = ["$REPO/bin", "$REPO/tools/herder/shims"]
[tools]
"github:aannoo/hcom" = "0.7.23"
EOF

PATH_VALUE="$GO_BIN:$REPO/bin:$REPO/tools/herder/shims:$WRONGBIN:$MOCKBIN:/usr/bin:/bin"
OUT="$(env -i \
  PATH="$PATH_VALUE" HOME="$HOME_DIR" XDG_CONFIG_HOME="$XDG_CONFIG" \
  XDG_STATE_HOME="$ROOT/state" XDG_CACHE_HOME="$XDG_CACHE" \
  AI_CONFIG_ROOT="$REPO" HERDER_BIN="$REPO/bin/herder" HERDER_STATE_DIR="$STATE_DIR" \
  HERDER_GROK_BIN="$PINNED" GROK_HOME="$LIVE_GROK" \
  bash "$AI_DOCTOR" --quick 2>&1)"
RC=$?

if [ "$RC" -eq 0 ]; then
  ok "doctor drift case: warnings do not auto-fix"
else
  bad "doctor drift case: warnings do not auto-fix" "rc=$RC output=$OUT"
fi
assert_contains "doctor: auth precondition reported by name" "$OUT" "XAI_API_KEY is absent or empty"
assert_contains "doctor: auth remedy names login profile" "$OUT" 'login-shell profile such as $HOME/.profile'
assert_contains "doctor: live-home confusion reported" "$OUT" "GROK_HOME points at the live user home"
assert_contains "doctor: managed-home symlink reported" "$OUT" "managed Grok home is a symlink"
assert_contains "doctor: PATH wrong version reported" "$OUT" "Grok vendor binary on PATH is outside launch support"
assert_contains "doctor: observed planted wrong version" "$OUT" "0.2.99"
assert_contains "doctor: names supported version" "$OUT" "0.2.93"

HERDER_DOC="$(cat "$REPO/tools/herder/README.md")"
assert_contains "docs: managed home named" "$HERDER_DOC" '<herder-state>/grok-home'
assert_contains "docs: every-launch atomic rewrite" "$HERDER_DOC" 'Every launch takes the seed lock and atomically'
assert_contains "docs: three deliberate drifts" "$HERDER_DOC" '| Home | Uses the vendor'
assert_contains "docs: owner verification path" "$HERDER_DOC" 'manual-verification path is `herder launch grok`'
assert_contains "docs: shared-home contrast" "$HERDER_DOC" "Claude and Codex share the user's live homes"
assert_contains "docs: future isolation option" "$HERDER_DOC" 'could provide multi-account isolation'
assert_contains "docs: manual guest is not a registered spawn" "$HERDER_DOC" 'bounded manual guest, not a registered spawn'
assert_contains "docs: explicit raw-vendor escape" "$HERDER_DOC" 'GROK=/absolute/path/to/grok grok'
if [[ "$HERDER_DOC" != *"Grok and Pi use the fully herder-managed model"* ]]; then
  ok "docs: no invented Pi managed-home claim"
else
  bad "docs: no invented Pi managed-home claim" "stale Pi family claim remains"
fi

if [ -L "$MANAGED_GROK" ] && [ "$(cat "$LIVE_GROK/sentinel")" = "live-user-state" ]; then
  ok "doctor: live and managed homes untouched"
else
  bad "doctor: live and managed homes untouched" "doctor changed throwaway planted state"
fi

for dir in "$PINNED_DIR" "$WRONGBIN"; do
  probe_home="$(cat "$dir/probe-home" 2>/dev/null || true)"
  probe_grok_home="$(cat "$dir/probe-grok-home" 2>/dev/null || true)"
  if [ -n "$probe_home" ] && [ "$probe_home" != "$HOME_DIR" ] && [ "$probe_grok_home" != "$LIVE_GROK" ]; then
    ok "doctor gate: isolated probe roots for $(basename "$dir")"
  else
    bad "doctor gate: isolated probe roots for $(basename "$dir")" "HOME=$probe_home GROK_HOME=$probe_grok_home"
  fi
done

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - Grok doctor contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
