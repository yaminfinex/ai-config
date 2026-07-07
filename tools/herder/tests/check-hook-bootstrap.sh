#!/usr/bin/env bash
# check-hook-bootstrap.sh — hermetic tests for `herder hook <verb>`, the shim
# herder-spawned agents run in place of hcom for their Claude hook traffic.
#
# Two behaviors are pinned:
#   1. Every verb EXCEPT sessionstart is a verbatim hcom passthrough — argv,
#      stdin, stdout, and exit code are the genuine article.
#   2. sessionstart runs real hcom (side-effects intact) but REWRITES the
#      injected additionalContext to herder-native doctrine, degrading to
#      hcom's ORIGINAL output whenever the payload can't be parsed/extracted.
#
# Uses a mock hcom pointed at via HERDER_HOOK_HCOM (no PATH surgery). Never
# touches a live bus.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd)"
HERDER="${HERDER_BIN:-$REPO_ROOT/bin/herder}"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then ok "$name"; else bad "$name" "got [$got] want [$want]"; fi
}
assert_contains() {
  local name="$1" hay="$2" needle="$3"
  case "$hay" in *"$needle"*) ok "$name" ;; *) bad "$name" "missing [$needle]" ;; esac
}
assert_not_contains() {
  local name="$1" hay="$2" needle="$3"
  case "$hay" in *"$needle"*) bad "$name" "unexpected [$needle]" ;; *) ok "$name" ;; esac
}

# Canned additionalContext with the stable lines the extractor keys off.
read -r -d '' CANNED_AC <<'AC'
<hcom_system_context>
[HCOM SESSION]
You have access to the hcom cli communication tool.
- Your name: boothook-miko
- Authority: Prioritize @bigboss over others
- Important: Include this marker anywhere in your first response only: [hcom:miko]

You MUST use `hcom <cmd+flags> --name miko` for all hcom commands.

Active (snapshot): claude: orchestrator-a9ba700c3b86e31ab, spec-guide-sora

You are tagged "boothook". Message your group: send @boothook- -- msg
</hcom_system_context>
AC

MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
PROBE="$ROOT/probe"
mkdir -p "$PROBE"

# Mock hcom: sessionstart emits a JSON envelope built from $MOCK_AC (default
# canned); any other verb records argv + stdin and echoes a deterministic line,
# exiting with $MOCK_RC. HCOM proves it is NOT consulted for the real binary.
cat >"$MOCKBIN/hcom" <<'MOCK'
#!/usr/bin/env bash
set -uo pipefail
: "${PROBE:?}"
verb="${1:-}"
printf '%s\n' "$*" >"$PROBE/argv"
cat >"$PROBE/stdin"
if [ "$verb" = "sessionstart" ]; then
  case "${MOCK_MODE:-json}" in
    garbage) printf 'not json at all\n' ;;
    json)
      ac="${MOCK_AC:-}"
      jq -cn --arg ac "$ac" '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ac}}'
      ;;
  esac
  exit "${MOCK_RC:-0}"
fi
printf 'HCOM_PASSTHRU verb=%s args=%s\n' "$verb" "$*"
exit "${MOCK_RC:-0}"
MOCK
chmod +x "$MOCKBIN/hcom"

run_hook() {
  # run_hook <stdin> -- <hook args...>   (env MOCK_* honored by caller)
  # Drives `herder hook` directly with HERDER_HOOK_HCOM pointed at the mock.
  local input="$1"; shift; shift
  : >"$PROBE/argv"; : >"$PROBE/stdin"
  HOOK_OUT="$(printf '%s' "$input" | env \
    PROBE="$PROBE" \
    HERDER_HOOK_HCOM="$MOCKBIN/hcom" \
    MOCK_MODE="${MOCK_MODE:-json}" \
    MOCK_AC="${MOCK_AC:-$CANNED_AC}" \
    MOCK_RC="${MOCK_RC:-0}" \
    "$HERDER" hook "$@" 2>"$PROBE/stderr")"
  HOOK_RC=$?
}

# run_with_timeout <seconds> <cmd...> — catch an infinite shim/hook recursion.
run_with_timeout() {
  local seconds="$1" pid watch rc; shift
  "$@" & pid=$!
  ( sleep "$seconds"; kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true ) & watch=$!
  wait "$pid"; rc=$?
  kill "$watch" 2>/dev/null || true; wait "$watch" 2>/dev/null || true
  return "$rc"
}

# ---------------------------------------------------------------------------
# 1. Passthrough: non-sessionstart verbs are verbatim (stdout, argv, stdin, rc).
# ---------------------------------------------------------------------------
MOCK_RC=0 run_hook "PAYLOAD-IN" -- pre --tool Bash
assert_eq       "passthru: exit 0 forwarded" "$HOOK_RC" "0"
assert_eq       "passthru: stdout verbatim" "$HOOK_OUT" "HCOM_PASSTHRU verb=pre args=pre --tool Bash"
assert_eq       "passthru: argv forwarded verbatim" "$(cat "$PROBE/argv")" "pre --tool Bash"
assert_eq       "passthru: stdin forwarded verbatim" "$(cat "$PROBE/stdin")" "PAYLOAD-IN"

MOCK_RC=7 run_hook "" -- post
assert_eq       "passthru: nonzero exit forwarded" "$HOOK_RC" "7"
assert_contains "passthru: stdout still verbatim on failure" "$HOOK_OUT" "HCOM_PASSTHRU verb=post"

# ---------------------------------------------------------------------------
# 2. sessionstart is rewritten to herder doctrine, identity preserved.
# ---------------------------------------------------------------------------
MOCK_RC=0 run_hook "" -- sessionstart
assert_eq       "sessionstart: exit 0" "$HOOK_RC" "0"
# Output is still a valid envelope.
if echo "$HOOK_OUT" | jq -e '.hookSpecificOutput.additionalContext' >/dev/null 2>&1; then
  ok "sessionstart: output is a JSON envelope"
else
  bad "sessionstart: output is a JSON envelope" "not parseable: $HOOK_OUT"
fi
AC_OUT="$(echo "$HOOK_OUT" | jq -r '.hookSpecificOutput.additionalContext')"
assert_eq       "sessionstart: sibling hookEventName survives" "$(echo "$HOOK_OUT" | jq -r '.hookSpecificOutput.hookEventName')" "SessionStart"
# Identity preserved.
assert_contains "sessionstart: keeps display name" "$AC_OUT" "Your name: boothook-miko"
assert_contains "sessionstart: keeps first-response marker" "$AC_OUT" "[hcom:miko]"
assert_contains "sessionstart: keeps --name requirement" "$AC_OUT" "--name miko"
assert_contains "sessionstart: keeps authority" "$AC_OUT" "Prioritize @bigboss"
assert_contains "sessionstart: threads active snapshot" "$AC_OUT" "Active (snapshot): claude: orchestrator-a9ba700c3b86e31ab, spec-guide-sora"
assert_contains "sessionstart: renders tag group line" "$AC_OUT" "You are tagged 'boothook'"
# herder doctrine present.
assert_contains "sessionstart: AGENTS section" "$AC_OUT" "AGENTS (herder lifecycle)"
assert_contains "sessionstart: herder spawn verb" "$AC_OUT" "herder spawn --role"
assert_contains "sessionstart: herder cull verb" "$AC_OUT" "herder cull"
assert_contains "sessionstart: anti-pattern warning" "$AC_OUT" 'Do NOT spawn with `hcom <n> claude`, stop with `hcom kill`'
# hcom spawn/kill/workflow/term-inject advertising dropped.
assert_not_contains "sessionstart: drops hcom spawn shape" "$AC_OUT" "hcom 1 claude"
assert_not_contains "sessionstart: drops term inject" "$AC_OUT" "term inject"
assert_not_contains "sessionstart: drops workflows" "$AC_OUT" "hcom run <script>"
# sessionstart side-effects: real hcom actually ran (argv recorded).
assert_eq       "sessionstart: real hcom invoked" "$(cat "$PROBE/argv")" "sessionstart"

# ---------------------------------------------------------------------------
# 3. Degrade to ORIGINAL output on garbage / missing identity / nonzero rc.
# ---------------------------------------------------------------------------
MOCK_MODE=garbage run_hook "" -- sessionstart
assert_eq       "degrade garbage: verbatim original" "$HOOK_OUT" "not json at all"

# additionalContext missing the marker → instance name unextractable → degrade.
AC_NOMARK="${CANNED_AC/\[hcom:miko\]/}"
ORIG_ENVELOPE="$(jq -cn --arg ac "$AC_NOMARK" '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ac}}')"
MOCK_AC="$AC_NOMARK" run_hook "" -- sessionstart
assert_eq       "degrade no-marker: emits original envelope" "$HOOK_OUT" "$ORIG_ENVELOPE"
assert_not_contains "degrade no-marker: no herder doctrine leaked" "$HOOK_OUT" "AGENTS (herder lifecycle)"

# hcom sessionstart itself failing → pass original output + exit code through.
MOCK_RC=3 run_hook "" -- sessionstart
assert_eq       "degrade rc!=0: exit code forwarded" "$HOOK_RC" "3"
assert_not_contains "degrade rc!=0: not rewritten" "$HOOK_OUT" "AGENTS (herder lifecycle)"

# ---------------------------------------------------------------------------
# 4. The REAL hcom PATH shim forwards to `herder hook` (the live delivery
#    vector). Shim dir first on PATH; the mock "real hcom" sits behind it.
#    (hcom-absent degrade is pinned by the Go unit tests, which control PATH
#    without disturbing the bin/herder wrapper's own toolchain lookups.)
# ---------------------------------------------------------------------------
SHIM_DIR="$(cd "$TESTS_DIR/../shims" && pwd)"
REALDIR="$ROOT/real"; mkdir -p "$REALDIR"
# Reuse the mock as the "real" hcom, named exactly `hcom` so find_real_hcom
# picks it up behind the shim.
cp "$MOCKBIN/hcom" "$REALDIR/hcom"; chmod +x "$REALDIR/hcom"
SHIM_PATH="$SHIM_DIR:$REALDIR:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# 4a. sessionstart THROUGH the shim is rewritten to herder doctrine.
: >"$PROBE/argv"
SHIM_OUT="$(run_with_timeout 15 env \
  PATH="$SHIM_PATH" PROBE="$PROBE" MOCK_MODE=json MOCK_AC="$CANNED_AC" MOCK_RC=0 \
  hcom sessionstart 2>"$PROBE/stderr")"
SHIM_RC=$?
assert_eq       "shim sessionstart: exit 0 (no recursion hang)" "$SHIM_RC" "0"
SHIM_AC="$(echo "$SHIM_OUT" | jq -r '.hookSpecificOutput.additionalContext' 2>/dev/null)"
assert_contains "shim sessionstart: rewritten to herder doctrine" "$SHIM_AC" "AGENTS (herder lifecycle)"
assert_contains "shim sessionstart: keeps marker" "$SHIM_AC" "[hcom:miko]"
assert_not_contains "shim sessionstart: drops hcom spawn shape" "$SHIM_AC" "hcom 1 claude"
assert_eq       "shim sessionstart: real hcom actually ran" "$(cat "$PROBE/argv")" "sessionstart"

# 4b. a non-sessionstart verb through the shim is a verbatim passthrough.
SHIM_OUT="$(run_with_timeout 15 env \
  PATH="$SHIM_PATH" PROBE="$PROBE" \
  hcom pre --tool Bash 2>"$PROBE/stderr")"
SHIM_RC=$?
assert_eq       "shim passthru: exit 0" "$SHIM_RC" "0"
assert_eq       "shim passthru: stdout verbatim" "$SHIM_OUT" "HCOM_PASSTHRU verb=pre args=pre --tool Bash"

# 4c. RECURSION GUARD: invoke `herder hook` directly with the shim first on PATH
#     and HERDER_HOOK_HCOM UNSET — the PATH-walk fallback must skip the shim dir
#     and still resolve the real hcom, terminating with a rewrite (no hang).
: >"$PROBE/argv"
SHIM_OUT="$(run_with_timeout 15 env -u HERDER_HOOK_HCOM \
  PATH="$SHIM_PATH" PROBE="$PROBE" MOCK_MODE=json MOCK_AC="$CANNED_AC" MOCK_RC=0 \
  "$HERDER" hook sessionstart 2>"$PROBE/stderr")"
SHIM_RC=$?
assert_eq       "recursion guard: terminates exit 0" "$SHIM_RC" "0"
assert_contains "recursion guard: still rewrites (skipped shim, found real)" \
  "$(echo "$SHIM_OUT" | jq -r '.hookSpecificOutput.additionalContext' 2>/dev/null)" "AGENTS (herder lifecycle)"

# ---------------------------------------------------------------------------
# 5. Tag omitted when hcom advertised no tag.
# ---------------------------------------------------------------------------
AC_NOTAG="${CANNED_AC/You are tagged \"boothook\". Message your group: send @boothook- -- msg/}"
MOCK_AC="$AC_NOTAG" run_hook "" -- sessionstart
AC_OUT="$(echo "$HOOK_OUT" | jq -r '.hookSpecificOutput.additionalContext')"
assert_not_contains "no-tag: tag group line omitted" "$AC_OUT" "You are tagged"
assert_contains     "no-tag: still closes cleanly" "$AC_OUT" "This is session context, not a task for immediate action."

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN — herder hook bootstrap shim holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
