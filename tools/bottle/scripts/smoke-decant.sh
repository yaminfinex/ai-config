#!/usr/bin/env bash
# smoke-decant.sh — live-harness smoke test for the bottle decant mechanic.
#
# Verifies that `claude --resume` accepts foreign-written session files: copies
# a fixture, rewrites every sessionId to a fresh UUID, plants it under the
# encoded project dir for a throwaway /tmp cwd, resumes headless via
# `claude -p`, and asserts context recall against the fixture's known
# codewords (ALPHA..ECHO — synthetic lab content baked into the fixtures).
#
# Variants exercised (per the bottle plan, U1):
#   1. plain fixture, untouched          — baseline foreign-write resume
#   2. plain fixture, truncated          — cut after an earlier completed turn
#   3. multi-compact, cut at boundary    — boundary + summary kept, rest gone
#   4. dangling tool_use tail            — last entry is an unresolved tool_use
#
# Costs a few small API calls. Re-run after Claude Code version bumps.
# Findings this script encodes are recorded in ../testdata/README.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURES="${1:-$SCRIPT_DIR/../testdata}"
PROJECTS="$HOME/.claude/projects"
RUN_TAG="bottle-smoke-$$"

CREATED_PATHS=()
cleanup() {
  local p
  for p in "${CREATED_PATHS[@]:-}"; do
    [[ -n "$p" && "$p" == *"$RUN_TAG"* ]] && rm -rf "$p"
  done
}
trap cleanup EXIT

encode_cwd() { printf '%s' "$1" | tr -c 'A-Za-z0-9' '-'; }

new_uuid() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr 'A-Z' 'a-z'
  else
    python3 -c 'import uuid; print(uuid.uuid4())'
  fi
}

# rewrite_session_ids <new-id>  (stdin: JSONL, stdout: JSONL)
rewrite_session_ids() {
  python3 -c '
import json, sys
nid = sys.argv[1]
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    e = json.loads(line)
    if "sessionId" in e:
        e["sessionId"] = nid
    print(json.dumps(e))
' "$1"
}

PASS=0
FAIL=0

# run_case <name> <fixture> <head_lines|all> <prompt> <expect_regex> [absent_regex]
run_case() {
  local name="$1" fixture="$2" head_lines="$3" prompt="$4" expect="$5" absent="${6:-}"
  local cwd="/tmp/$RUN_TAG-$name"
  local enc proj id seed out
  mkdir -p "$cwd"
  CREATED_PATHS+=("$cwd")
  enc="$(encode_cwd "$cwd")"
  proj="$PROJECTS/$enc"
  mkdir -p "$proj"
  chmod 700 "$proj"
  CREATED_PATHS+=("$proj")
  id="$(new_uuid)"
  seed="$proj/$id.jsonl"
  if [[ "$head_lines" == "all" ]]; then
    rewrite_session_ids "$id" <"$FIXTURES/$fixture" >"$seed"
  else
    head -n "$head_lines" "$FIXTURES/$fixture" | rewrite_session_ids "$id" >"$seed"
  fi

  echo "== $name: resume $id (fixture=$fixture lines=$head_lines)"
  if ! out="$(cd "$cwd" && claude --resume "$id" -p "$prompt" </dev/null 2>&1)"; then
    echo "   FAIL: claude --resume exited non-zero"
    printf '   output: %s\n' "$out"
    FAIL=$((FAIL + 1))
    return 0
  fi
  printf '   reply: %s\n' "$out"
  if ! grep -Eq "$expect" <<<"$out"; then
    echo "   FAIL: expected /$expect/ in reply"
    FAIL=$((FAIL + 1))
    return 0
  fi
  if [[ -n "$absent" ]] && grep -Eq "$absent" <<<"$out"; then
    echo "   FAIL: did not expect /$absent/ in reply"
    FAIL=$((FAIL + 1))
    return 0
  fi
  echo "   PASS"
  PASS=$((PASS + 1))
}

ASK="Which codewords do you know? Answer with just the codewords, comma-separated."

# 1. Baseline: full plain fixture (ALPHA, BRAVO, CHARLIE turns all present).
run_case baseline plain.jsonl all "$ASK" 'ALPHA.*BRAVO.*CHARLIE'

# 2. Truncated: plain fixture cut after the BRAVO turn completed (entry tree
#    ends at the BRAVO assistant reply + its valid last-prompt trailer).
#    CHARLIE must be unknown — proves the cut actually trimmed context.
run_case truncated plain.jsonl 16 "$ASK" 'BRAVO' 'CHARLIE'

# 3. Truncated at a compact boundary: multi-compact fixture cut just after
#    compact boundary #1's summary block (boundary + isCompactSummary entry +
#    command echoes + trailers). Codewords survive only via the summary text;
#    ECHO (after boundary #2) must be gone.
run_case at-compact-boundary multi-compact.jsonl 44 "$ASK" 'CHARLIE' 'ECHO'

# 4. Dangling tool_use tail: fixture ends with an assistant tool_use that has
#    no tool_result. CC 2.1.170 tolerates this (parents the next user entry to
#    the dangling entry's parent, orphaning it as a dead branch).
run_case dangling-tool-use dangling-tool-use.jsonl all "$ASK" 'ALPHA.*BRAVO'

echo
echo "smoke-decant: $PASS passed, $FAIL failed (claude $(claude --version 2>/dev/null || echo '?'))"
[[ "$FAIL" -eq 0 ]]
