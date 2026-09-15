#!/usr/bin/env bash
# check-fence-display.sh - contract for the Claude Code MessageDisplay hook
# claude/hooks/fence-display.sh: status and internal fences are redrawn on the
# terminal, everything else passes through, every failure is silent exit 0.
#
# Batches are fed by hand through stdin with a private XDG_RUNTIME_DIR; no
# network, no writes outside the tmp root.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HOOK="$REPO/claude/hooks/fence-display.sh"
SETTINGS="$REPO/claude/settings.shared.json"
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

for dep in jq mktemp date; do
  command -v "$dep" >/dev/null 2>&1 || {
    printf 'FAIL  harness dependency missing: %s\n' "$dep" >&2
    exit 1
  }
done
[ -x "$HOOK" ] || { printf 'FAIL  hook not executable: %s\n' "$HOOK"; exit 1; }

RUNTIME="$ROOT/runtime"
mkdir -p "$RUNTIME"
STATE_DIR="$RUNTIME/fence-display-$(id -u)"

# batch <message_id> <index> <final:true|false> <delta>  -> prints hook stdout
batch() {
  local mid="$1" idx="$2" fin="$3" delta="$4"
  jq -cn --arg m "$mid" --argjson i "$idx" --argjson f "$fin" --arg d "$delta" \
    '{session_id:"s",transcript_path:"/dev/null",cwd:"/",hook_event_name:"MessageDisplay",
      turn_id:"t",message_id:$m,index:$i,final:$f,delta:$d}' \
    | env XDG_RUNTIME_DIR="$RUNTIME" HOME="$ROOT/home" bash "$HOOK"
}
shown() { jq -r '.hookSpecificOutput.displayContent' <<<"$1"; }

# (a) single-line status
OUT="$(batch a 0 true $'<status>reading the brief</status>\n')"
assert_eq "a: status draws as dotted line" "$(shown "$OUT")" "· reading the brief"
assert_eq "a: hook event name echoed" "$(jq -r '.hookSpecificOutput.hookEventName' <<<"$OUT")" "MessageDisplay"
OUT="$(batch a2 0 true $'  <status>padded</status>  \n')"
assert_eq "a: surrounding whitespace tolerated" "$(shown "$OUT")" "· padded"

# (b) empty status
OUT="$(batch b 0 true $'<status></status>\n')"
assert_eq "b: empty status draws placeholder" "$(shown "$OUT")" "· status"

# (c) internal block whole in one batch
OUT="$(batch c 0 true $'before\n<internal>\nline one\nline two\nline three\n</internal>\nafter\n')"
assert_eq "c: one note line with count, body and closer absent" "$(shown "$OUT")" $'before\n▸ internal note · 3 lines\nafter'

# (d) internal block across batches 0..3
OUT="$(batch d 0 false $'<internal>\n')"
assert_eq "d0: opener draws once without count" "$(shown "$OUT")" "▸ internal note"
if [ -f "$STATE_DIR/d" ]; then ok "d0: state file created"; else bad "d0: state file created" "missing $STATE_DIR/d"; fi
OUT="$(batch d 1 false $'body a\nbody b\n')"
assert_eq "d1: body batch draws nothing" "$(shown "$OUT")" ""
OUT="$(batch d 2 false $'body c\n')"
assert_eq "d2: body batch draws nothing" "$(shown "$OUT")" ""
OUT="$(batch d 3 false $'</internal>\nplain after\n')"
assert_eq "d3: closer draws nothing, following text kept" "$(shown "$OUT")" "plain after"
OUT="$(batch d 4 true '')"
assert_eq "d4: empty final batch is silent" "$OUT" ""
if [ -e "$STATE_DIR/d" ]; then bad "d4: state file removed on final" "still present"; else ok "d4: state file removed on final"; fi
if [ -d "$STATE_DIR" ]; then
  assert_eq "d: state dir mode 0700" "$(stat -c %a "$STATE_DIR")" "700"
fi

# (e) backticked tag and tag inside a code fence pass through unchanged
E_DELTA=$'Write `<status>x</status>` bare.\n```\n<status>inside fence</status>\n```\n<status>real</status>\n'
OUT="$(batch e 0 true "$E_DELTA")"
assert_eq "e: backticked and fenced tags literal, bare one drawn" "$(shown "$OUT")" \
  $'Write `<status>x</status>` bare.\n```\n<status>inside fence</status>\n```\n· real'
OUT="$(batch e2 0 true $'<Status>caps</Status>\n<status foo="1">attr</status>\n<status>open only\n')"
assert_eq "e: misspelt, attributed and unclosed status are not redrawn" "$OUT" ""
OUT="$(batch e3 0 false $'```\n')"
OUT="$(batch e3 1 true $'<internal>\nnot a note\n</internal>\n')"
assert_eq "e: code fence state carries across batches" "$OUT" ""

# (f) plain markdown: no displayContent at all
OUT="$(batch f 0 true $'# Title\n\n- one\n- two\n')"
assert_eq "f: plain markdown emits nothing" "$OUT" ""
if [ -e "$STATE_DIR/f" ]; then bad "f: no state file for a plain message" "present"; else ok "f: no state file for a plain message"; fi

# (g) missing jq on PATH
NOJQ="$ROOT/nojq"; mkdir -p "$NOJQ"
for tool in bash cat id mkdir chmod find rm; do
  src="$(command -v "$tool")" && ln -s "$src" "$NOJQ/$tool"
done
OUT="$(printf '%s' '{"hook_event_name":"MessageDisplay","message_id":"g","final":true,"delta":"<status>x</status>\n"}' \
  | env -i PATH="$NOJQ" XDG_RUNTIME_DIR="$RUNTIME" HOME="$ROOT/home" bash "$HOOK")"
RC=$?
assert_eq "g: missing jq exits 0" "$RC" "0"
assert_eq "g: missing jq prints nothing" "$OUT" ""

# (h) malformed JSON
OUT="$(printf '%s' '{"hook_event_name": "MessageDisplay", "delta": ' | env XDG_RUNTIME_DIR="$RUNTIME" bash "$HOOK")"
RC=$?
assert_eq "h: malformed JSON exits 0" "$RC" "0"
assert_eq "h: malformed JSON prints nothing" "$OUT" ""
OUT="$(printf '' | env XDG_RUNTIME_DIR="$RUNTIME" bash "$HOOK")"
assert_eq "h: empty stdin prints nothing" "$OUT" ""
OUT="$(printf '%s' '{"hook_event_name":"MessageDisplay","message_id":"../x","final":true,"delta":"<status>x</status>\n"}' | env XDG_RUNTIME_DIR="$RUNTIME" bash "$HOOK")"
assert_eq "h: unsafe message_id prints nothing" "$OUT" ""
RO="$ROOT/ro"; mkdir -p "$RO/fence-display-$(id -u)"; chmod 0500 "$RO/fence-display-$(id -u)"
OUT="$(printf '%s' '{"hook_event_name":"MessageDisplay","message_id":"ro","final":false,"delta":"<internal>\n"}' | env XDG_RUNTIME_DIR="$RO" bash "$HOOK")"
if [ "$(id -u)" = 0 ]; then ok "h: read-only state dir (skipped as root)"; else assert_eq "h: read-only state dir prints nothing" "$OUT" ""; fi
chmod 0700 "$RO/fence-display-$(id -u)"

# (i) timing: 20 sequential calls under 200 ms each
slow=0; worst=0
for i in $(seq 1 20); do
  t0=$(date +%s%N)
  batch "timing$i" 0 true $'<status>tick</status>\nplain\n<internal>\nx\n</internal>\n' >/dev/null
  t1=$(date +%s%N)
  ms=$(( (t1 - t0) / 1000000 ))
  [ "$ms" -gt "$worst" ] && worst=$ms
  [ "$ms" -lt 200 ] || slow=1
done
if [ "$slow" = 0 ]; then ok "i: 20 calls each under 200 ms (worst ${worst} ms)"; else bad "i: 20 calls each under 200 ms" "worst ${worst} ms"; fi

# stale state pruning
OLD="$STATE_DIR/stale"; printf 'open 1 0\n' >"$OLD"; touch -d '2 days ago' "$OLD"
batch prune 0 true $'plain\n' >/dev/null
if [ -e "$OLD" ]; then bad "prune: day-old state file removed" "still present"; else ok "prune: day-old state file removed"; fi

# source guard: settings.shared.json carries exactly one MessageDisplay entry
if jq -e . "$SETTINGS" >/dev/null 2>&1; then
  ok "settings: parses"
  assert_eq "settings: one MessageDisplay group" "$(jq '.hooks.MessageDisplay | length' "$SETTINGS")" "1"
  assert_eq "settings: one command in the group" "$(jq '[.hooks.MessageDisplay[].hooks[]] | length' "$SETTINGS")" "1"
  # shellcheck disable=SC2016 # the expected value is a literal settings string
  assert_eq "settings: command targets fence-display.sh" \
    "$(jq -r '.hooks.MessageDisplay[0].hooks[0].command' "$SETTINGS")" \
    '[ -x "$HOME/.claude/hooks/fence-display.sh" ] && exec "$HOME/.claude/hooks/fence-display.sh" || exit 0'
  assert_eq "settings: type command" "$(jq -r '.hooks.MessageDisplay[0].hooks[0].type' "$SETTINGS")" "command"
  assert_eq "settings: timeout 3" "$(jq '.hooks.MessageDisplay[0].hooks[0].timeout' "$SETTINGS")" "3"
else
  bad "settings: parses" "$SETTINGS is not valid JSON"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - fence-display hook contract holds.\n'
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above.\n'
exit 1
