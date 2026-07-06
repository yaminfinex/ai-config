#!/usr/bin/env bash
# check-hcom-hooks.sh - hermetic W5 tests for ai-setup hcom hook management.
#
# Uses a fake HOME and mock hcom only. Never touches real ~/.claude, ~/.codex,
# or ~/.hcom.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AI_SETUP="$TESTS_DIR/../../../bin/ai-setup"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

PATH_BASE="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

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

assert_file_contains() {
  local name="$1" file="$2" needle="$3"
  if grep -Fq "$needle" "$file" 2>/dev/null; then
    ok "$name"
  else
    bad "$name" "missing [$needle] in $file"
  fi
}

assert_file_not_contains_re() {
  local name="$1" file="$2" pattern="$3"
  if grep -Eq "$pattern" "$file" 2>/dev/null; then
    bad "$name" "unexpected pattern [$pattern] in $file"
  else
    ok "$name"
  fi
}

assert_exists() {
  local name="$1" path="$2"
  [ -e "$path" ] && ok "$name" || bad "$name" "missing $path"
}

write_mock_hcom() {
  local bin="$1"
  mkdir -p "$bin"
  cat > "$bin/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail

log_call() {
  : "${MOCK_HCOM_LOG:?}"
  printf 'verb=%s tool=%s hcom_dir=%s\n' "$1" "$2" "${HCOM_DIR-}" >> "$MOCK_HCOM_LOG"
}

claude_file() { printf '%s\n' "$HOME/.claude/settings.json"; }
codex_config() { printf '%s\n' "$HOME/.codex/config.toml"; }
codex_hooks() { printf '%s\n' "$HOME/.codex/hooks.json"; }
codex_rules() { printf '%s\n' "$HOME/.codex/rules/hcom.rules"; }

hooks_help() {
  cat <<'HELP'
Usage:
  hcom hooks                      Show hook status
  hcom hooks status               Same as above
  hcom hooks add [tool]           Add hooks (claude | codex | all)
  hcom hooks remove [tool]        Remove hooks (claude | codex | all)
HELP
}

add_claude() {
  local file tmp
  file="$(claude_file)"
  mkdir -p "$(dirname "$file")"
  [ -f "$file" ] || printf '{}\n' > "$file"
  tmp="$(mktemp)"
  jq '
    .env = (.env // {}) |
    .env.HCOM = "hcom" |
    .hooks = (.hooks // {}) |
    .hooks.PreToolUse = (
      ((.hooks.PreToolUse // []) | map(select(((.hooks // []) | any((.command? // "") | test("HCOM|hcom"))) | not))) +
      [{matcher:"Bash|Task|Write|Edit",hooks:[{type:"command",command:"cmd=${HCOM:-hcom}; command -v \"${cmd%% *}\" >/dev/null 2>&1 && exec $cmd pre || exit 0"}]}]
    ) |
    .permissions = (.permissions // {}) |
    .permissions.allow = (((.permissions.allow // []) | map(select(startswith("Bash(hcom") | not))) +
      ["Bash(hcom send:*)","Bash(hcom hooks:*)"])
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
}

remove_claude() {
  local file tmp
  file="$(claude_file)"
  [ -f "$file" ] || return 0
  tmp="$(mktemp)"
  jq '
    del(.env.HCOM) |
    if (.env? == {}) then del(.env) else . end |
    .hooks |= with_entries(.value |= map(select(((.hooks // []) | any((.command? // "") | test("HCOM|hcom"))) | not))) |
    .permissions.allow |= map(select(startswith("Bash(hcom") | not))
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
}

add_codex() {
  local config hooks rules
  config="$(codex_config)"
  hooks="$(codex_hooks)"
  rules="$(codex_rules)"
  mkdir -p "$(dirname "$config")" "$(dirname "$rules")"
  [ -f "$config" ] || printf 'model = "gpt-test"\n' > "$config"
  if ! grep -q 'hcom_hook_definition_hash' "$config"; then
    cat >> "$config" <<EOF

[hooks.state."$hooks:pre_tool_use:0:0"]
trusted_hash = "sha256:hcom-pre"
enabled = true
hcom_codex_cli_version = "0.142.test"
hcom_hook_definition_hash = "sha256:hcom-definition"
EOF
  fi
  cat > "$hooks" <<'EOF'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "hcom codex-pretooluse"
          }
        ]
      }
    ]
  }
}
EOF
  cat > "$rules" <<'EOF'
# hcom integration - auto-approve safe commands
prefix_rule(pattern=["hcom", "send"], decision="allow")
EOF
}

remove_codex() {
  local hooks tmp
  hooks="$(codex_hooks)"
  rm -f "$(codex_rules)"
  [ -f "$hooks" ] || return 0
  tmp="$(mktemp)"
  jq '
    .hooks |= with_entries(
      .value |= map(select(((.hooks // []) | any((.command? // "") | test("^hcom codex-"))) | not))
    ) |
    .hooks |= with_entries(select((.value | length) > 0))
  ' "$hooks" > "$tmp"
  if grep -q '"hooks"[[:space:]]*:[[:space:]]*{}' "$tmp"; then
    rm -f "$hooks" "$tmp"
  else
    mv "$tmp" "$hooks"
  fi
  # Intentionally leave config.toml hcom trust blocks behind. Real hcom 0.7.22
  # does this today; ai-setup must scrub them after delegation.
}

case "${1:-}" in
  hooks)
    case "${2:-}" in
      --help|-h|"") hooks_help ;;
      status) printf 'Claude:  mock\nCodex:  mock\n' ;;
      add)
        log_call add "${3:-}"
        case "${3:-}" in
          claude) add_claude ;;
          codex) add_codex ;;
          all) add_claude; add_codex ;;
          *) exit 2 ;;
        esac
        ;;
      remove)
        log_call remove "${3:-}"
        case "${3:-}" in
          claude) remove_claude ;;
          codex) remove_codex ;;
          all) remove_claude; remove_codex ;;
          *) exit 2 ;;
        esac
        ;;
      *) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
MOCK_HCOM
  chmod +x "$bin/hcom"
}

make_case() {
  local name="$1"
  CASE_DIR="$ROOT/$name"
  HOME_DIR="$CASE_DIR/home"
  XDG_DIR="$CASE_DIR/xdg"
  MOCKBIN="$CASE_DIR/bin"
  BACKUPS="$CASE_DIR/backups"
  MOCK_LOG="$CASE_DIR/hcom.log"
  mkdir -p "$HOME_DIR/.claude" "$HOME_DIR/.codex/rules" "$MOCKBIN" "$BACKUPS" "$XDG_DIR/mise"
  : > "$MOCK_LOG"
  write_mock_hcom "$MOCKBIN"
}

write_user_only_fixtures() {
  cat > "$HOME_DIR/.claude/settings.json" <<'JSON'
{
  "env": {
    "USER_KEEP": "yes"
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/keep/dcg"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "bash '$HOME/.claude/hooks/herdr-agent-state.sh' session"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(echo:*)",
      "Read(*)"
    ]
  }
}
JSON

  cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
model = "gpt-test"

[hooks.state."/tmp/user/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:user"
enabled = true
TOML

  cat > "$HOME_DIR/.codex/hooks.json" <<'JSON'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "user keep"
          }
        ]
      }
    ]
  }
}
JSON
}

run_setup() {
  RUN_OUT="$(env -i \
    PATH="$MOCKBIN:$PATH_BASE" HOME="$HOME_DIR" SHELL=/bin/zsh \
    XDG_CONFIG_HOME="$XDG_DIR" \
    AI_CONFIG_BACKUP_DIR="$BACKUPS" AI_CONFIG_TIMESTAMP=20260702T000000 \
    MOCK_HCOM_LOG="$MOCK_LOG" HCOM_DIR="$CASE_DIR/local-bus" \
    bash "$AI_SETUP" "$@" 2>&1)"
  RUN_RC=$?
}

user_markers() {
  {
    jq -r '.hooks.PreToolUse[]?.hooks[]?.command | select(. == "/keep/dcg")' "$HOME_DIR/.claude/settings.json" || true
    jq -r '.hooks.SessionStart[]?.hooks[]?.command | select(test("herdr-agent-state.sh"))' "$HOME_DIR/.claude/settings.json" || true
    jq -r '.permissions.allow[]? | select(. == "Bash(echo:*)" or . == "Read(*)")' "$HOME_DIR/.claude/settings.json" || true
    awk '/^\[hooks.state."\/tmp\/user\/hooks.json:pre_tool_use:0:0"\]/{p=1} p{print} p && /^enabled = true$/{p=0}' "$HOME_DIR/.codex/config.toml"
    jq -r '.hooks.PreToolUse[]?.hooks[]?.command | select(. == "user keep")' "$HOME_DIR/.codex/hooks.json" 2>/dev/null || true
  }
}

# 1. install is explicit, delegated, idempotent, and global-only.
make_case install
write_user_only_fixtures
run_setup --hcom-hooks install
assert_eq "install: exit 0" "$RUN_RC" "0"
assert_file_contains "install: Claude hcom command present" "$HOME_DIR/.claude/settings.json" 'HCOM:-hcom'
assert_file_contains "install: Claude hcom permission present" "$HOME_DIR/.claude/settings.json" 'Bash(hcom send:*)'
assert_file_contains "install: Codex hcom state present" "$HOME_DIR/.codex/config.toml" 'hcom_hook_definition_hash'
assert_file_contains "install: Codex hooks.json present" "$HOME_DIR/.codex/hooks.json" 'hcom codex-pretooluse'
assert_file_contains "install: Codex rules present" "$HOME_DIR/.codex/rules/hcom.rules" 'hcom integration'
assert_exists "install: backs up Claude settings" "$BACKUPS/20260702T000000/.claude/settings.json"
assert_exists "install: backs up Codex config" "$BACKUPS/20260702T000000/.codex/config.toml"
assert_file_not_contains_re "install: native calls do not inherit HCOM_DIR" "$MOCK_LOG" 'hcom_dir=.'

run_setup --hcom-hooks install
assert_eq "install twice: exit 0" "$RUN_RC" "0"
assert_eq "install twice: one Claude hcom permission" "$(grep -c 'Bash(hcom send:\*)' "$HOME_DIR/.claude/settings.json")" "1"
assert_eq "install twice: one Codex hcom state block" "$(grep -c 'hcom_hook_definition_hash' "$HOME_DIR/.codex/config.toml")" "1"

run_setup --hcom-hooks status
assert_contains "status installed: overall" "$RUN_OUT" "hcom hooks: installed"
assert_contains "status installed: Claude" "$RUN_OUT" "Claude: installed"
assert_contains "status installed: Codex" "$RUN_OUT" "Codex:  installed"

# 2. remove delegates, scrubs residuals, preserves user entries, and is idempotent.
before_user="$(user_markers)"
run_setup --hcom-hooks remove
assert_eq "remove: exit 0" "$RUN_RC" "0"
assert_file_not_contains_re "remove: Claude hcom state gone" "$HOME_DIR/.claude/settings.json" 'HCOM|hcom|Bash\(hcom'
assert_file_not_contains_re "remove: Codex config hcom state gone" "$HOME_DIR/.codex/config.toml" 'hcom_hook_definition_hash|hcom_codex_cli_version'
assert_file_not_contains_re "remove: Codex hooks hcom state gone" "$HOME_DIR/.codex/hooks.json" 'hcom codex-'
if [ ! -e "$HOME_DIR/.codex/rules/hcom.rules" ]; then
  ok "remove: Codex hcom rules removed"
else
  bad "remove: Codex hcom rules removed" "file still exists"
fi
after_user="$(user_markers)"
assert_eq "remove: user parts preserved" "$after_user" "$before_user"

call_count_before="$(wc -l < "$MOCK_LOG" | tr -d ' ')"
run_setup --hcom-hooks remove
assert_eq "remove absent: exit 0" "$RUN_RC" "0"
call_count_after="$(wc -l < "$MOCK_LOG" | tr -d ' ')"
assert_eq "remove absent: no native call" "$call_count_after" "$call_count_before"

run_setup --hcom-hooks status
assert_contains "status absent: overall" "$RUN_OUT" "hcom hooks: absent"

# 3. partial status catches Codex native-remove residue.
make_case partial
write_user_only_fixtures
cat >> "$HOME_DIR/.codex/config.toml" <<'TOML'

[hooks.state."/tmp/fake-codex/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:hcom-pre"
enabled = true
hcom_codex_cli_version = "0.142.test"
hcom_hook_definition_hash = "sha256:hcom-definition"
TOML
run_setup --hcom-hooks status
assert_contains "status partial: overall" "$RUN_OUT" "hcom hooks: partial"
assert_contains "status partial: Codex" "$RUN_OUT" "Codex:  partial"

# 4. dry-run remove reports intended work and leaves bytes unchanged.
make_case dryrun
write_user_only_fixtures
run_setup --hcom-hooks install >/dev/null
before_sum="$(cksum "$HOME_DIR/.claude/settings.json" "$HOME_DIR/.codex/config.toml" "$HOME_DIR/.codex/hooks.json" | awk '{print $1 ":" $2 ":" $3}' | tr '\n' '|')"
run_setup --dry-run --hcom-hooks remove
assert_eq "dry-run remove: exit 0" "$RUN_RC" "0"
assert_contains "dry-run remove: reports native remove" "$RUN_OUT" "DRY hcom hooks remove claude"
after_sum="$(cksum "$HOME_DIR/.claude/settings.json" "$HOME_DIR/.codex/config.toml" "$HOME_DIR/.codex/hooks.json" | awk '{print $1 ":" $2 ":" $3}' | tr '\n' '|')"
assert_eq "dry-run remove: files unchanged" "$after_sum" "$before_sum"

# 5. default ai-setup does not manage hcom hooks.
make_case default
write_user_only_fixtures
run_setup --dry-run
assert_eq "default setup: exit 0" "$RUN_RC" "0"
assert_file_not_contains_re "default setup: no hcom refs added" "$HOME_DIR/.claude/settings.json" 'HCOM|hcom|Bash\(hcom'
assert_eq "default setup: no native hcom calls" "$(wc -l < "$MOCK_LOG" | tr -d ' ')" "0"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup hcom hook management holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
