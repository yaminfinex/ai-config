#!/usr/bin/env bash
# check-context-nudge.sh - Claude advisory hook threshold and dedupe contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HOOK="$REPO/claude/hooks/context-nudge.sh"
ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

fail=0
passes=0
pass() { printf 'PASS  %s\n' "$1"; passes=$((passes + 1)); }
fail() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }
assert_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "got [$2], want [$3]"; fi
}
assert_contains() {
  case "$2" in *"$3"*) pass "$1" ;; *) fail "$1" "missing [$3]" ;; esac
}

for dep in bash jq mktemp timeout sha256sum; do
  command -v "$dep" >/dev/null 2>&1 || { printf 'FAIL  missing dependency: %s\n' "$dep"; exit 1; }
done

HOME_DIR="$ROOT/home"
STATE_DIR="$ROOT/state"
BIN_DIR="$ROOT/bin"
TOKEN_DIR="$ROOT/tokens"
mkdir -p "$HOME_DIR" "$STATE_DIR" "$BIN_DIR" "$TOKEN_DIR"

cat >"$BIN_DIR/herder" <<'STUB'
#!/usr/bin/env bash
set -u
[ "${1:-}" = show ] && [ "${2:-}" = --session ] && [ "${4:-}" = --json ] || exit 1
case "$3" in
  11111111-1111-1111-1111-111111111111) ;;
  22222222-2222-2222-2222-222222222222) ;;
  eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee) exit 1 ;;
  dddddddd-dddd-dddd-dddd-dddddddddddd) sleep 3; exit 0 ;;
  bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb) printf '{garbage\n'; exit 0 ;;
  *) exit 1 ;;
esac
token_file="$FAKE_TOKEN_DIR/$3"
token="$(cat "$token_file")" || exit 1
jq -cn --argjson used "$token" '{vitals:{context_usage:{used_tokens:$used}}}'
STUB
chmod +x "$BIN_DIR/herder" "$HOOK"

SID_A=11111111-1111-1111-1111-111111111111
SID_B=22222222-2222-2222-2222-222222222222
MARKER_A="$STATE_DIR/ai-config/context-nudge/claude-$SID_A.bands"
MARKER_B="$STATE_DIR/ai-config/context-nudge/claude-$SID_B.bands"

set_token() { printf '%s\n' "$2" >"$TOKEN_DIR/$1"; }
run_hook() {
  local sid="$1" event="$2" source="${3:-}" bands="${4-200000,250000}"
  local payload
  payload="$(jq -cn --arg sid "$sid" --arg event "$event" --arg source "$source" \
    '{session_id:$sid,hook_event_name:$event} + if $source == "" then {} else {source:$source} end')"
  ERR="$ROOT/stderr"
  OUT="$(printf '%s\n' "$payload" | env -u AI_CONFIG_ROOT -u HERDER_BIN \
    PATH="$BIN_DIR:/usr/bin:/bin" HOME="$HOME_DIR" XDG_STATE_HOME="$STATE_DIR" \
    FAKE_TOKEN_DIR="$TOKEN_DIR" AI_CONTEXT_NUDGE_BANDS="$bands" \
    "$HOOK" 2>"$ERR")"
  RC=$?
}
marker_sum() { [ -f "$MARKER_A" ] && sha256sum "$MARKER_A" | awk '{print $1}' || printf absent; }
reset_a() { rm -rf "$STATE_DIR/ai-config"; mkdir -p "$STATE_DIR"; }

# Boundary walk reddens > instead of >=, event drift, and missing marker dedupe.
reset_a
set_token "$SID_A" 199999
run_hook "$SID_A" UserPromptSubmit
assert_eq "boundary: 199999 is silent" "$OUT" ""
set_token "$SID_A" 200000
run_hook "$SID_A" UserPromptSubmit
assert_contains "boundary: 200000 warns inclusively" "$OUT" "crossed your 200000-token warning band"
set_token "$SID_A" 249999
run_hook "$SID_A" PostToolUse
assert_eq "boundary: 249999 is silent across events" "$OUT" ""
set_token "$SID_A" 250000
run_hook "$SID_A" PostToolUse
assert_contains "boundary: 250000 emits second nudge" "$OUT" "crossed your 250000-token warning band"
run_hook "$SID_A" UserPromptSubmit
assert_eq "boundary: both bands remain deduped" "$OUT" ""
assert_eq "boundary: marker records both exact bands" "$(cat "$MARKER_A")" $'200000\n250000'

# Jump reddens per-band drip and multiple outputs for one observation.
reset_a
set_token "$SID_A" 260000
run_hook "$SID_A" PostToolUse
assert_contains "jump: one advisory names both bands" "$OUT" "crossed your 200000,250000-token warning band"
assert_eq "jump: output contains one hook object" "$(grep -o 'hookSpecificOutput' <<<"$OUT" | wc -l | tr -d ' ')" "1"
run_hook "$SID_A" PostToolUse
assert_eq "jump: repeat is silent" "$OUT" ""

# Dip reddens implementations that rearm when usage falls.
reset_a
set_token "$SID_A" 200000
run_hook "$SID_A" UserPromptSubmit
set_token "$SID_A" 100000
run_hook "$SID_A" PostToolUse
assert_eq "dip: below-band observation is silent" "$OUT" ""
set_token "$SID_A" 210000
run_hook "$SID_A" UserPromptSubmit
assert_eq "dip: rise does not rearm" "$OUT" ""

# SessionStart reddens reset-on-any-start and missing compact reset.
run_hook "$SID_A" SessionStart startup
set_token "$SID_A" 260000
run_hook "$SID_A" PostToolUse
assert_contains "reset: startup preserves first-band dedupe" "$OUT" "crossed your 250000-token warning band"
run_hook "$SID_A" SessionStart compact
assert_eq "reset: compact emits no output" "$OUT" ""
run_hook "$SID_A" UserPromptSubmit
assert_contains "reset: compact rearms both bands" "$OUT" "crossed your 200000,250000-token warning band"

# Fail-open cases redden nonzero exits, output on failure, and marker mutation.
before="$(marker_sum)"
for spec in \
  'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee|herder exit 1' \
  'dddddddd-dddd-dddd-dddd-dddddddddddd|herder timeout' \
  'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb|garbage herder JSON'; do
  sid="${spec%%|*}"; label="${spec#*|}"
  run_hook "$sid" PostToolUse
  assert_eq "failure: $label exits zero" "$RC" "0"
  assert_eq "failure: $label is silent" "$OUT" ""
  assert_eq "failure: $label leaves marker untouched" "$(marker_sum)" "$before"
done
ERR="$ROOT/stderr"
OUT="$(printf '%s\n' '{"hook_event_name":"PostToolUse"}' | env -u AI_CONFIG_ROOT -u HERDER_BIN \
  PATH="$BIN_DIR:/usr/bin:/bin" HOME="$HOME_DIR" XDG_STATE_HOME="$STATE_DIR" FAKE_TOKEN_DIR="$TOKEN_DIR" "$HOOK" 2>"$ERR")"
RC=$?
assert_eq "failure: missing session id exits zero" "$RC" "0"
assert_eq "failure: missing session id is silent" "$OUT" ""
assert_eq "failure: all cases keep stderr empty" "$(cat "$ERR")" ""
assert_eq "failure: missing id leaves marker untouched" "$(marker_sum)" "$before"
BAD_STATE="$ROOT/not-a-directory"
printf occupied >"$BAD_STATE"
OUT="$(jq -cn --arg sid "$SID_A" '{session_id:$sid,hook_event_name:"PostToolUse"}' | \
  env -u AI_CONFIG_ROOT -u HERDER_BIN PATH="$BIN_DIR:/usr/bin:/bin" HOME="$HOME_DIR" \
  XDG_STATE_HOME="$BAD_STATE" FAKE_TOKEN_DIR="$TOKEN_DIR" "$HOOK" 2>"$ERR")"
RC=$?
assert_eq "failure: unwritable state exits zero" "$RC" "0"
assert_eq "failure: unwritable state is silent" "$OUT" ""
assert_eq "failure: unwritable state leaves marker untouched" "$(marker_sum)" "$before"

# Shape reddens wrong event, extra keys/stdout, blocking fields, and rendered-token drift.
reset_a
set_token "$SID_A" 210000
run_hook "$SID_A" UserPromptSubmit
shape="$(jq -c 'if keys == ["hookSpecificOutput"] and (.hookSpecificOutput | keys == ["additionalContext","hookEventName"]) and .hookSpecificOutput.hookEventName == "UserPromptSubmit" and (.hookSpecificOutput.additionalContext | contains("last completed request used 210000 input-context tokens")) and ([.. | objects | has("decision") or has("continue")] | any | not) then "ok" else "bad" end' <<<"$OUT")"
assert_eq "shape: exact advisory object and raw token integer" "$shape" '"ok"'
# The expected output deliberately keeps $AI_CONFIG_ROOT literal.
# shellcheck disable=SC2016
expected='Context reminder: the last completed request used 210000 input-context tokens and crossed your 200000-token warning band. Compact now or notify your orchestrator now with your token count and next safe boundary. If you are orchestrating, first rewrite your state/handoff to current truth, then run $AI_CONFIG_ROOT/tools/fleet/selfcompact.sh <own-hcom-name> '\''<what to preserve>'\'' '\''<explicit continuation>'\'', end this turn so it can run, and resume from the continuation. If you are a worker, compact at the next safe boundary using /compact, or finish the current bounded unit and immediately report for compaction or pickup. If someone manages you, send that manager an hcom update now; use your current manager, otherwise the launcher'\''s reply-to instruction. If no orchestrator is known, own the compaction yourself. This reminder does not block work: do not stop to wait for permission, and do not abandon the task. This band is warned once per compaction cycle.'
assert_eq "shape: reminder text stays verbatim" "$(jq -r '.hookSpecificOutput.additionalContext' <<<"$OUT")" "$expected"
assert_eq "shape: success keeps stderr empty" "$(cat "$ERR")" ""

# Session isolation reddens shared or hcom-name-based marker keys.
set_token "$SID_B" 210000
run_hook "$SID_B" PostToolUse
assert_contains "isolation: second session warns independently" "$OUT" "crossed your 200000-token warning band"
assert_eq "isolation: first UUID marker exists" "$(test -f "$MARKER_A" && printf yes)" yes
assert_eq "isolation: second UUID marker exists" "$(test -f "$MARKER_B" && printf yes)" yes

# Invalid preferences redden permissive parsing and accidental state writes.
before_a="$(marker_sum)"
before_count="$(find "$STATE_DIR/ai-config/context-nudge" -type f | wc -l | tr -d ' ')"
for bands in '250000,200000' '200000,200000' '0,200000' 'nope' '200000,'; do
  run_hook "$SID_A" PostToolUse "" "$bands"
  assert_eq "bands [$bands]: exits zero" "$RC" "0"
  assert_eq "bands [$bands]: stays silent" "$OUT" ""
  assert_eq "bands [$bands]: leaves marker untouched" "$(marker_sum)" "$before_a"
done
assert_eq "bands: invalid values create no files" "$(find "$STATE_DIR/ai-config/context-nudge" -type f | wc -l | tr -d ' ')" "$before_count"

# Scratch merge reddens replacement, duplicates, and loss of existing hcom hooks.
MERGE_HOME="$ROOT/merge-home"
MERGE_REPO="$ROOT/merge-repo"
mkdir -p "$MERGE_HOME/.claude" "$MERGE_REPO/claude" "$MERGE_REPO/lib"
cp "$REPO/claude/settings.shared.json" "$MERGE_REPO/claude/"
cp "$REPO/lib/claude-settings.sh" "$MERGE_REPO/lib/"
printf '%s\n' '{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"hcom user"}]}],"PostToolUse":[{"hooks":[{"type":"command","command":"hcom post"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"hcom start"}]}]}}' >"$MERGE_HOME/.claude/settings.json"
for run in 1 2; do
  # Expansion is intentionally deferred to the scratch merge subprocess.
  # shellcheck disable=SC2016
  env -u AI_CONFIG_ROOT -u HERDER_BIN PATH="/usr/bin:/bin" HOME="$MERGE_HOME" AI_CONFIG_ROOT="$MERGE_REPO" \
    CLAUDE_SETTINGS_LIVE="$MERGE_HOME/.claude/settings.json" AI_CONFIG_BACKUP_DIR="$ROOT/backups" \
    AI_CONFIG_TIMESTAMP="run-$run" bash -c 'log_info(){ :; }; log_warn(){ :; }; log_error(){ :; }; . "$AI_CONFIG_ROOT/lib/claude-settings.sh"; claude_settings_apply 0' >/dev/null
done
settings="$MERGE_HOME/.claude/settings.json"
for event in UserPromptSubmit PostToolUse SessionStart; do
  hcom_command="hcom $(case "$event" in UserPromptSubmit) printf user ;; PostToolUse) printf post ;; *) printf start ;; esac)"
  assert_eq "merge: $event preserves hcom" "$(jq --arg e "$event" --arg c "$hcom_command" '[.hooks[$e][] | select(any(.hooks[]; .command == $c))] | length' "$settings")" 1
  assert_eq "merge: $event has one nudge after two runs" "$(jq --arg e "$event" '[.hooks[$e][] | select(any(.hooks[]; .command == "$HOME/.claude/hooks/context-nudge.sh"))] | length' "$settings")" 1
done

printf '\n'
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - context nudge contract holds (%s checks).\n' "$passes"
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above (%s checks passed).\n' "$passes"
exit 1
