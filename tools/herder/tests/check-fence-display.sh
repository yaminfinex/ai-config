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

for dep in jq mktemp date yes head grep; do
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
assert_eq "a: surrounding whitespace is text and is kept" "$(shown "$OUT")" "  · padded  "

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

# (e) tags anywhere: backticked, fenced and mid-line tags ARE redrawn (web parser
# parity); an attribute, other casing or other spelling is text.
E_DELTA=$'Write `<status>x</status>` bare.\n```\n<status>inside fence</status>\n```\nbefore <status>mid</status> after\n'
OUT="$(batch e 0 true "$E_DELTA")"
assert_eq "e: backticked, fenced and mid-line status redrawn in place" "$(shown "$OUT")" \
  $'Write `· x` bare.\n```\n· inside fence\n```\nbefore · mid after'
OUT="$(batch e2 0 true $'<Status>caps</Status>\n<status foo="1">attr</status>\n<statu>typo</statu>\n')"
assert_eq "e: cased, attributed and misspelt tags are text" "$OUT" ""
OUT="$(batch e3 0 true $'two <status>a</status> and <status></status> here\n')"
assert_eq "e: two pairs on one line both redrawn" "$(shown "$OUT")" "two · a and · status here"
OUT="$(batch e4 0 true $'```\n<internal>\nfenced body\n</internal>\n```\n')"
assert_eq "e: internal block inside a code fence still collapses" "$(shown "$OUT")" $'```\n▸ internal note · 1 lines\n```'

# (r1) bounded loss and line granularity on malformed input
OUT="$(batch r1a 0 true $'<internal>\nkept\n<status>nested</status>\nrest\n</internal>\nafter\n')"
assert_eq "r1: nested status ends the note there, rest literal" "$(shown "$OUT")" \
  $'▸ internal note · 1 lines\n<status>nested</status>\nrest\n</internal>\nafter'
OUT="$(batch r1b 0 true $'<status>ok</status>\n</internal>\n')"
assert_eq "r1: valid status then stray closer: status drawn, closer literal" "$(shown "$OUT")" $'· ok\n</internal>'
OUT="$(batch r1c 0 true $'</internal>\n<status>ok</status>\n')"
assert_eq "r1: stray closer then valid status: closer literal, status drawn" "$(shown "$OUT")" $'</internal>\n· ok'
OUT="$(batch r1d 0 true $'<status>x\ry</status>\n')"
assert_eq "r1: carriage return inside a status body stays literal" "$OUT" ""
OUT="$(batch r1e 0 true $'<status>open\nclosed</status>\n')"
assert_eq "r1: status closer on another line stays literal" "$OUT" ""
OUT="$(batch r1f 0 true $'</status>\n<internal>\n')"
assert_eq "r1: stray status closer literal, opener still opens" "$(shown "$OUT")" $'</status>\n▸ internal note'
OUT="$(batch r1g 0 false $'<internal>\n')"
OUT="$(batch r1g 1 true $'body\n<internal>\nmore\n')"
assert_eq "r1: nested opener across batches ends the note, rest literal" "$(shown "$OUT")" $'<internal>\nmore'
OUT="$(batch r1h 0 true $'plain\r\n<status>good</status>\r\nlast')"
assert_eq "r1: CRLF endings and a final unterminated line preserved" "$(shown "$OUT")" $'plain\r\n· good\r\nlast'

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

# large input: 20 000 plain lines and 20 000 status lines, each under 1 s wall
BIG="$ROOT/big"
yes 'plain text line' | head -20000 >"$BIG.plain"
yes '<status>tick</status>' | head -20000 >"$BIG.status"
big_batch() {
  jq -cn --rawfile d "$1" '{hook_event_name:"MessageDisplay",message_id:"big",index:0,final:true,delta:$d}' \
    | env XDG_RUNTIME_DIR="$RUNTIME" HOME="$ROOT/home" bash "$HOOK"
}
t0=$(date +%s%N); OUT="$(big_batch "$BIG.plain")"; t1=$(date +%s%N); plain_ms=$(( (t1 - t0) / 1000000 ))
assert_eq "big: 20 000 plain lines emit nothing" "$OUT" ""
if [ "$plain_ms" -lt 1000 ]; then ok "big: 20 000 plain lines under 1 s (${plain_ms} ms)"; else bad "big: 20 000 plain lines under 1 s" "${plain_ms} ms"; fi
t0=$(date +%s%N); OUT="$(big_batch "$BIG.status")"; t1=$(date +%s%N); status_ms=$(( (t1 - t0) / 1000000 ))
assert_eq "big: 20 000 status lines all redrawn" "$(shown "$OUT" | grep -c '^· tick$')" "20000"
if [ "$status_ms" -lt 1000 ]; then ok "big: 20 000 status lines under 1 s (${status_ms} ms)"; else bad "big: 20 000 status lines under 1 s" "${status_ms} ms"; fi

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
