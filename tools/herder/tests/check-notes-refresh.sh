#!/usr/bin/env bash
# check-notes-refresh.sh - Claude SessionStart notes re-injection contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
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
assert_absent() {
  case "$2" in *"$3"*) fail "$1" "unexpected [$3]" ;; *) pass "$1" ;; esac
}

for dep in bash jq mktemp readlink; do
  command -v "$dep" >/dev/null 2>&1 || { printf 'FAIL  missing dependency: %s\n' "$dep"; exit 1; }
done

# A fake checkout holding the real hook and a fixture notes file, and a fake HOME
# whose ~/.claude/hooks entry is a symlink into it, as ai-setup installs it.
CHECKOUT="$ROOT/checkout"
HOME_DIR="$ROOT/home"
mkdir -p "$CHECKOUT/claude/hooks" "$CHECKOUT/docs" "$HOME_DIR/.claude/hooks"
cp "$REPO/claude/hooks/notes-refresh.sh" "$CHECKOUT/claude/hooks/"
chmod +x "$CHECKOUT/claude/hooks/notes-refresh.sh"
NOTES="$CHECKOUT/docs/hcom-launch-notes.txt"
printf '%s\n' 'Fleet doctrine: __AI_CONFIG_ROOT__/docs/session-context-fleet.md' \
  'Chat fencing: <status>…</status> and "quotes" \ backslash; __AI_CONFIG_ROOT__/tools/fleet' >"$NOTES"
ln -s "$CHECKOUT/claude/hooks/notes-refresh.sh" "$HOME_DIR/.claude/hooks/notes-refresh.sh"
HOOK="$HOME_DIR/.claude/hooks/notes-refresh.sh"

run_hook() {
  local event="$1" source="${2:-}" launched="${3-1}"
  local payload
  payload="$(jq -cn --arg event "$event" --arg source "$source" \
    '{session_id:"11111111-1111-1111-1111-111111111111",hook_event_name:$event} + if $source == "" then {} else {source:$source} end')"
  run_raw "$payload" "$launched"
}
run_raw() {
  local launched="${2-1}"
  local -a launch_env=(-u HCOM_LAUNCHED)
  [ -n "$launched" ] && launch_env=(HCOM_LAUNCHED="$launched")
  OUT="$(printf '%s\n' "$1" | env -u AI_CONFIG_ROOT "${launch_env[@]}" \
    PATH="/usr/bin:/bin" HOME="$HOME_DIR" "$HOOK" 2>"$ROOT/stderr")"
  RC=$?
  ERR="$(<"$ROOT/stderr")"
}

for source in compact resume; do
  run_hook SessionStart "$source"
  assert_eq "$source: exit 0" "$RC" 0
  assert_eq "$source: no stderr" "$ERR" ""
  assert_eq "$source: valid SessionStart JSON" \
    "$(jq -r '.hookSpecificOutput.hookEventName' <<<"$OUT" 2>/dev/null)" SessionStart
  text="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$OUT" 2>/dev/null)"
  assert_contains "$source: header names the source" "$text" "after $source"
  assert_contains "$source: header says it supersedes" "$text" "supersedes any earlier NOTES section"
  assert_contains "$source: checkout path rendered" "$text" "$CHECKOUT/docs/session-context-fleet.md"
  assert_contains "$source: second placeholder rendered" "$text" "$CHECKOUT/tools/fleet"
  assert_absent "$source: no placeholder left" "$text" "__AI_CONFIG_ROOT__"
  assert_contains "$source: quotes and backslash survive" "$text" '"quotes" \ backslash'
  expected_body="$(sed "s|__AI_CONFIG_ROOT__|$CHECKOUT|g" "$NOTES")"
  assert_eq "$source: header, blank line, notes verbatim" "$(tail -n +3 <<<"$text")" "$expected_body"
  assert_eq "$source: blank second line" "$(sed -n 2p <<<"$text")" ""
done

# Symlink resolution reddens a hook that looks beside $HOME instead of the checkout.
assert_absent "symlink: resolves to checkout, not HOME" "$text" "$HOME_DIR"

for source in startup clear bogus ""; do
  run_hook SessionStart "$source"
  assert_eq "source [$source]: exit 0" "$RC" 0
  assert_eq "source [$source]: silent" "$OUT$ERR" ""
done

for event in UserPromptSubmit PostToolUse; do
  run_hook "$event" compact
  assert_eq "event $event: silent" "$RC:$OUT$ERR" "0:"
done

run_hook SessionStart compact ""
assert_eq "HCOM_LAUNCHED unset: silent" "$RC:$OUT$ERR" "0:"
run_hook SessionStart compact 0
assert_eq "HCOM_LAUNCHED=0: silent" "$RC:$OUT$ERR" "0:"

for garbage in '' '{garbage' '[]' '{"hook_event_name":7,"source":"compact"}' 'plain text' \
  '{"hook_event_name":"SessionStart","source":"compact"} garbage' \
  '{"hook_event_name":"SessionStart","source":"compact"}{'; do
  run_raw "$garbage"
  assert_eq "garbage [$garbage]: silent" "$RC:$OUT$ERR" "0:"
done

cp "$NOTES" "$ROOT/notes.bak"
printf '%s\n' '__AI_CONFIG_ROOT__' >"$NOTES"
run_hook SessionStart compact
assert_contains "placeholder-only notes render to path" "$OUT" "$CHECKOUT"
: >"$NOTES"
run_hook SessionStart compact
assert_eq "empty notes: silent" "$RC:$OUT$ERR" "0:"
rm -f "$NOTES"
run_hook SessionStart compact
assert_eq "missing notes: silent" "$RC:$OUT$ERR" "0:"
cp "$ROOT/notes.bak" "$NOTES"
if [ "$(id -u)" -eq 0 ]; then
  printf 'SKIP  unreadable notes: running as root\n'
else
  chmod 000 "$NOTES"
  run_hook SessionStart compact
  assert_eq "unreadable notes: silent, no stderr" "$RC:$OUT$ERR" "0:"
  chmod 644 "$NOTES"
fi

# A copy (not a symlink) outside a claude/hooks layout finds no notes.
mkdir -p "$ROOT/loose"
cp "$CHECKOUT/claude/hooks/notes-refresh.sh" "$ROOT/loose/notes-refresh.sh"
OUT="$(printf '%s\n' '{"hook_event_name":"SessionStart","source":"compact"}' | env HCOM_LAUNCHED=1 \
  PATH="/usr/bin:/bin" HOME="$HOME_DIR" "$ROOT/loose/notes-refresh.sh" 2>&1)"
assert_eq "loose copy outside checkout: silent" "$?:$OUT" "0:"

# No jq on PATH: bash and readlink only.
NOJQ="$ROOT/nojq"
mkdir -p "$NOJQ"
ln -s "$(command -v bash)" "$NOJQ/bash"
ln -s "$(command -v readlink)" "$NOJQ/readlink"
OUT="$(printf '%s\n' '{"hook_event_name":"SessionStart","source":"compact"}' | env HCOM_LAUNCHED=1 \
  PATH="$NOJQ" HOME="$HOME_DIR" "$NOJQ/bash" "$HOOK" 2>&1)"
assert_eq "no jq on PATH: silent" "$?:$OUT" "0:"

# Scratch merge reddens a lost matcher, duplicates, and loss of hcom's own hook.
MERGE_HOME="$ROOT/merge-home"
MERGE_REPO="$ROOT/merge-repo"
mkdir -p "$MERGE_HOME/.claude" "$MERGE_REPO/claude" "$MERGE_REPO/lib"
cp "$REPO/claude/settings.shared.json" "$MERGE_REPO/claude/"
cp "$REPO/lib/claude-settings.sh" "$MERGE_REPO/lib/"
printf '%s\n' '{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"hcom sessionstart"}]}]}}' >"$MERGE_HOME/.claude/settings.json"
for run in 1 2; do
  # Expansion is intentionally deferred to the scratch merge subprocess.
  # shellcheck disable=SC2016
  env -u AI_CONFIG_ROOT PATH="/usr/bin:/bin" HOME="$MERGE_HOME" AI_CONFIG_ROOT="$MERGE_REPO" \
    CLAUDE_SETTINGS_LIVE="$MERGE_HOME/.claude/settings.json" AI_CONFIG_BACKUP_DIR="$ROOT/backups" \
    AI_CONFIG_TIMESTAMP="run-$run" bash -c 'log_info(){ :; }; log_warn(){ :; }; log_error(){ :; }; . "$AI_CONFIG_ROOT/lib/claude-settings.sh"; claude_settings_apply 0' >/dev/null
done
settings="$MERGE_HOME/.claude/settings.json"
assert_eq "merge: hcom sessionstart preserved" \
  "$(jq '[.hooks.SessionStart[] | select(any(.hooks[]; .command == "hcom sessionstart"))] | length' "$settings")" 1
# shellcheck disable=SC2016
guarded='[ -x "$HOME/.claude/hooks/notes-refresh.sh" ] && exec "$HOME/.claude/hooks/notes-refresh.sh" || exit 0'
assert_eq "merge: one guarded notes-refresh after two runs, matcher compact|resume" \
  "$(jq -c --arg c "$guarded" '[.hooks.SessionStart[] | select(any(.hooks[]; .command == $c)) | [.matcher, .hooks[0].timeout]]' "$settings")" \
  '[["compact|resume",3]]'

# Install list reddens a hook that ai-setup never links.
# shellcheck disable=SC2016
assert_eq "install: lib/common.sh links the hook" \
  "$(grep -c '"claude/hooks/notes-refresh.sh|$HOME/.claude/hooks/notes-refresh.sh"' "$REPO/lib/common.sh")" 1

printf '\n'
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - notes refresh contract holds (%s checks).\n' "$passes"
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above (%s checks passed).\n' "$passes"
exit 1
