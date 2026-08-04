#!/usr/bin/env bash
# check-launchers.sh - hermetic contract for lib/launchers.sh: the interactive
# claude/codex/grok launcher functions.
#
# Pins the properties the design depends on:
#   - functions route to "$AI_CONFIG_ROOT/bin/herder" launch with baked
#     default args, env-overridable (including the empty-string ask-mode
#     override), user args appended;
#   - a PATH imposter prepended ahead of everything loses to the function
#     (the mise hook-env re-front scenario that beat the shim generation);
#   - a missing/unresolvable AI_CONFIG_ROOT fails loud (rc 127) and never
#     falls back to a raw PATH launch.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    ok "$name"
  else
    bad "$name" "got [$got] want [$want]"
  fi
}

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) ok "$name" ;;
    *) bad "$name" "missing [$needle] in [$haystack]" ;;
  esac
}

FIXROOT="$ROOT/root"
mkdir -p "$FIXROOT/bin" "$FIXROOT/lib" "$ROOT/decoy"
cp "$REPO/lib/launchers.sh" "$FIXROOT/lib/"
# Recorder herder: prints exactly one argv line per token so word-splitting
# bugs (a quoted user arg breaking apart) are visible.
cat > "$FIXROOT/bin/herder" <<'EOF'
#!/bin/sh
for a in "$@"; do printf 'ARG[%s]\n' "$a"; done
EOF
printf '%s\n' '#!/bin/sh' 'echo RAW-DECOY-RAN' > "$ROOT/decoy/claude"
chmod +x "$FIXROOT/bin/herder" "$ROOT/decoy/claude"

run_case() { # run_case <extra-env...> -- <shell-body>
  local envs=()
  while [ "$1" != "--" ]; do envs+=("$1"); shift; done
  shift
  env -i PATH="$ROOT/decoy:/usr/bin:/bin" HOME="$ROOT" AI_CONFIG_ROOT="$FIXROOT" "${envs[@]}" \
    bash --noprofile --norc -c "source \"\$AI_CONFIG_ROOT/lib/launchers.sh\"; $1" 2>&1
}

# 1. Defaults baked in, user args appended, quoting preserved.
OUT="$(run_case -- 'claude --resume "two words"')"
assert_eq "claude default+args" "$OUT" 'ARG[launch]
ARG[claude]
ARG[--dangerously-skip-permissions]
ARG[--resume]
ARG[two words]'

OUT="$(run_case -- 'codex')"
assert_eq "codex default" "$OUT" 'ARG[launch]
ARG[codex]
ARG[--dangerously-bypass-approvals-and-sandbox]'

OUT="$(run_case -- 'grok')"
assert_eq "grok no default args" "$OUT" 'ARG[launch]
ARG[grok]'

# 2. Env overrides: custom flags, and the empty-string ask-mode override.
OUT="$(run_case HERDER_SHIM_ARGS_CLAUDE=--custom-flag -- 'claude')"
assert_eq "claude env override" "$OUT" 'ARG[launch]
ARG[claude]
ARG[--custom-flag]'

OUT="$(run_case HERDER_SHIM_ARGS_CLAUDE= -- 'claude -p hi')"
assert_eq "claude empty override (ask mode)" "$OUT" 'ARG[launch]
ARG[claude]
ARG[-p]
ARG[hi]'

# 3. The function wins over a PATH imposter fronted ahead of everything.
OUT="$(run_case -- 'type -t claude; claude')"
assert_contains "function beats PATH decoy: type" "$OUT" "function"
assert_contains "function beats PATH decoy: routes to herder" "$OUT" "ARG[launch]"
case "$OUT" in
  *RAW-DECOY-RAN*) bad "function beats PATH decoy: decoy never runs" "decoy ran" ;;
  *) ok "function beats PATH decoy: decoy never runs" ;;
esac

# 4. Missing root fails loud with rc 127; the decoy must NOT run as fallback.
OUT="$(env -i PATH="$ROOT/decoy:/usr/bin:/bin" HOME="$ROOT" AI_CONFIG_ROOT="$ROOT/nonexistent" \
  bash --noprofile --norc -c "source \"$FIXROOT/lib/launchers.sh\"; claude; echo rc=\$?" 2>&1)"
assert_contains "missing root: loud error" "$OUT" "missing or not executable"
assert_contains "missing root: rc 127" "$OUT" "rc=127"
case "$OUT" in
  *RAW-DECOY-RAN*) bad "missing root: no raw fallback" "decoy ran" ;;
  *) ok "missing root: no raw fallback" ;;
esac

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - launcher function contract holds.\n'
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above.\n'
exit 1
