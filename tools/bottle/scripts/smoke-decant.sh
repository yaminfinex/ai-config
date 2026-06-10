#!/usr/bin/env bash
# smoke-decant.sh — live-harness smoke + characterisation test for the bottle
# decant mechanic.
#
# Two layers, one entry point (re-run the whole script after Claude Code
# version bumps; findings are pinned to the version printed in the summary):
#
# Acceptance smoke (does the decant mechanic still work at all): copies a
# fixture, rewrites every sessionId to a fresh UUID, plants it under the
# encoded project dir for a throwaway /tmp cwd, resumes headless via
# `claude -p`, and asserts context recall against the fixture's known
# codewords (ALPHA..ECHO — synthetic lab content baked into the fixtures).
#   1. plain fixture, untouched          — baseline foreign-write resume
#   2. plain fixture, truncated          — cut after an earlier completed turn
#   3. multi-compact, cut at boundary    — boundary + summary kept, rest gone
#   4. dangling tool_use tail            — last entry is an unresolved tool_use
#
# Characterisation (do the U1 empirical answers still hold — these are the
# behaviours U3/U6 build contracts on; a FAIL here means harness drift, not
# necessarily breakage — re-read tools/bottle/testdata/README.md and re-derive):
#   5. divert-trailer  — last-prompt.leafUuid rewritten to an early leaf must
#                        NOT move the resume point (leaf = last tree entry)
#   6. stale-trailer   — leafUuid pointing at a uuid absent from the file must
#                        be tolerated (no validation hard-fail)
#   7. cwd-scope       — resume of the same id from a different cwd must fail
#                        (decant's mandatory chdir)
#   post-checks on 1+4 — `-p --resume` appends to the SAME session file (no
#                        fork: decants-map key correctness), and the dangling
#                        tool_use is branched around (next user entry parents
#                        to the dangling entry's parent; no synthetic
#                        tool_result)
#
# Costs a handful of small API calls. Findings recorded in ../testdata/README.md.

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

# --- seed transforms (fixture on stdin is not used; args: fixture-path new-id) ---

# verbatim copy, sessionIds rewritten
t_all() {
  python3 -c '
import json, sys
nid = sys.argv[2]
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    e = json.loads(line)
    if "sessionId" in e: e["sessionId"] = nid
    print(json.dumps(e))
' "$1" "$2"
}

# first N lines only (tree-consistent cut points documented in ../testdata/README.md)
t_head() {
  python3 -c '
import json, sys
nid, n = sys.argv[2], int(sys.argv[3])
for i, line in enumerate(open(sys.argv[1])):
    if i >= n: break
    e = json.loads(line)
    if "sessionId" in e: e["sessionId"] = nid
    print(json.dumps(e))
' "$1" "$2" "$3"
}
t_head16() { t_head "$1" "$2" 16; }
t_head44() { t_head "$1" "$2" 44; }

# full file, but final last-prompt.leafUuid diverted to the FIRST assistant
# tree entry — if resume honours the trailer, later turns vanish from context
t_divert_trailer() {
  python3 -c '
import json, sys
nid = sys.argv[2]
lines = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
early = next(e["uuid"] for e in lines if e.get("type") == "assistant" and e.get("uuid"))
for e in reversed(lines):
    if e.get("type") == "last-prompt":
        e["leafUuid"] = early
        break
for e in lines:
    if "sessionId" in e: e["sessionId"] = nid
    print(json.dumps(e))
' "$1" "$2"
}

# first 16 lines + a trailing last-prompt whose leafUuid points at the full
# fixture last tree uuid — absent from the truncated file (stale pointer)
t_stale_trailer() {
  python3 -c '
import json, sys
nid = sys.argv[2]
lines = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
stale = next(e["uuid"] for e in reversed(lines) if e.get("uuid"))
out = lines[:16]
assert all(e.get("uuid") != stale for e in out), "stale uuid must be past the cut"
out.append({"type": "last-prompt", "leafUuid": stale, "sessionId": nid})
for e in out:
    if "sessionId" in e: e["sessionId"] = nid
    print(json.dumps(e))
' "$1" "$2"
}

# --- post-checks (args: seed-path pre-resume-line-count project-dir) ---

# the resume must have appended to the SAME file; no second session file
# (a fork would silently break the decants-map key that rebottle relies on)
post_same_file() {
  local seed="$1" before="$2" proj="$3"
  local after files
  after="$(wc -l <"$seed")"
  files="$(find "$proj" -maxdepth 1 -name '*.jsonl' ! -name 'agent-*' | wc -l)"
  if (( after <= before )); then
    echo "   POST-FAIL: seed did not grow (before=$before after=$after) — resume forked?"
    return 1
  fi
  if (( files != 1 )); then
    echo "   POST-FAIL: expected 1 session file in project dir, found $files — resume forked?"
    return 1
  fi
  echo "   post: same-file append OK (lines $before -> $after, 1 session file)"
}

# dangling tool_use mechanism: next user entry parents to the dangling entry's
# parent (branch-around); no synthetic tool_result for the dangling id
post_dangling_mechanism() {
  local seed="$1" before="$2" proj="$3"
  post_same_file "$seed" "$before" "$proj" || return 1
  python3 -c '
import json, sys
seed, before = sys.argv[1], int(sys.argv[2])
lines = [json.loads(l) for l in open(seed) if l.strip()]
orig, appended = lines[:before], lines[before:]
dang = orig[-1]
assert dang.get("type") == "assistant", "fixture must end in assistant tool_use"
tids = [b["id"] for b in dang["message"]["content"]
        if isinstance(b, dict) and b.get("type") == "tool_use"]
parent = dang.get("parentUuid")
user = next((e for e in appended if e.get("type") == "user" and e.get("uuid")), None)
assert user is not None, "no user entry appended by resume"
up = user.get("parentUuid")
if up == dang.get("uuid"):
    sys.exit("DRIFT: new user entry parents to the dangling tool_use entry itself "
             "(harness now keeps it on the live path / synthesizes results?)")
assert up == parent, (
    "DRIFT: new user parent %s is neither the dangling entry nor its parent %s" % (up, parent))
for e in appended:
    c = (e.get("message") or {}).get("content")
    if isinstance(c, list):
        for b in c:
            if isinstance(b, dict) and b.get("type") == "tool_result" and b.get("tool_use_id") in tids:
                sys.exit("DRIFT: harness synthesized a tool_result for the dangling tool_use")
print("   post: dangling branched around (no synthetic tool_result) OK")
' "$seed" "$before" || return 1
}

PASS=0
FAIL=0

# run_case <name> <fixture> <transform-fn> <prompt> <expect_regex> [absent_regex] [post_check_fn]
run_case() {
  local name="$1" fixture="$2" transform="$3" prompt="$4" expect="$5" absent="${6:-}" post="${7:-}"
  local cwd="/tmp/$RUN_TAG-$name"
  local enc proj id seed before out
  mkdir -p "$cwd"
  CREATED_PATHS+=("$cwd")
  enc="$(encode_cwd "$cwd")"
  proj="$PROJECTS/$enc"
  mkdir -p "$proj"
  chmod 700 "$proj"
  CREATED_PATHS+=("$proj")
  id="$(new_uuid)"
  seed="$proj/$id.jsonl"
  "$transform" "$FIXTURES/$fixture" "$id" >"$seed"
  before="$(wc -l <"$seed")"

  echo "== $name: resume $id (fixture=$fixture transform=$transform)"
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
  if [[ -n "$post" ]] && ! "$post" "$seed" "$before" "$proj"; then
    FAIL=$((FAIL + 1))
    return 0
  fi
  echo "   PASS"
  PASS=$((PASS + 1))
}

ASK="Which codewords do you know? Answer with just the codewords, comma-separated."

# --- acceptance smoke -------------------------------------------------------

# 1. Baseline: full plain fixture (ALPHA, BRAVO, CHARLIE turns all present).
#    Post-check piggybacks the same-file/no-fork characterisation.
run_case baseline plain.jsonl t_all "$ASK" 'ALPHA.*BRAVO.*CHARLIE' '' post_same_file

# 2. Truncated: plain fixture cut after the BRAVO turn completed. CHARLIE must
#    be unknown — proves the cut actually trimmed context.
run_case truncated plain.jsonl t_head16 "$ASK" 'BRAVO' 'CHARLIE'

# 3. Truncated at a compact boundary: boundary #1 + isCompactSummary block
#    kept, everything after gone. Codewords survive only via the summary;
#    ECHO (after boundary #2) must be gone.
run_case at-compact-boundary multi-compact.jsonl t_head44 "$ASK" 'CHARLIE' 'ECHO'

# 4. Dangling tool_use tail. Post-check characterises the branch-around
#    mechanism (no synthetic tool_result, dead-branch orphaning).
run_case dangling-tool-use dangling-tool-use.jsonl t_all "$ASK" 'ALPHA.*BRAVO' '' post_dangling_mechanism

# --- characterisation: U1 findings that U3/U6 contracts depend on -----------

# 5. Leaf selection: trailer diverted to the first assistant leaf must be
#    ignored — CHARLIE still in context. A FAIL means resume started honouring
#    last-prompt.leafUuid and the U3 truncation contract needs re-deriving.
run_case divert-trailer plain.jsonl t_divert_trailer "$ASK" 'CHARLIE'

# 6. Stale trailer: leafUuid pointing past the cut (uuid absent from file)
#    must still resume. A FAIL means resume started validating the trailer.
run_case stale-trailer plain.jsonl t_stale_trailer "$ASK" 'BRAVO' 'CHARLIE'

# 7. cwd scoping: the same session id must NOT resume from a different cwd —
#    decant's chdir-then-exec is mandatory. Drift here is benign but means the
#    materializer's cwd handling can be simplified.
char_cwd_scope() {
  local cwd="/tmp/$RUN_TAG-cwdscope" other="/tmp/$RUN_TAG-elsewhere"
  local enc proj id seed
  mkdir -p "$cwd" "$other"
  CREATED_PATHS+=("$cwd" "$other")
  enc="$(encode_cwd "$cwd")"
  proj="$PROJECTS/$enc"
  mkdir -p "$proj"
  chmod 700 "$proj"
  CREATED_PATHS+=("$proj")
  id="$(new_uuid)"
  seed="$proj/$id.jsonl"
  t_all "$FIXTURES/plain.jsonl" "$id" >"$seed"
  echo "== cwd-scope: resume $id from a foreign cwd (must fail)"
  local out
  if out="$(cd "$other" && timeout 120 claude --resume "$id" -p "Reply OK." </dev/null 2>&1)"; then
    echo "   FAIL: resume succeeded from an unrelated cwd — resume is no longer cwd-scoped"
    printf '   output: %s\n' "$out"
    FAIL=$((FAIL + 1))
  else
    echo "   PASS (refused as expected: $(head -c 100 <<<"$out"))"
    PASS=$((PASS + 1))
  fi
}
char_cwd_scope

echo
echo "smoke-decant: $PASS passed, $FAIL failed (claude $(claude --version 2>/dev/null || echo '?'))"
[[ "$FAIL" -eq 0 ]]
