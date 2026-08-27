#!/usr/bin/env bash
# check-live-contract.sh - pin herder's assumptions against installed hcom/herdr.
#
# This tier intentionally talks to the live installed binaries, not the hermetic
# mocks used by the rest of the contract battery. Mutating operations are out of
# scope: hcom hook bootstrapping runs under a scratch HCOM_DIR, and herdr checks
# use read/introspection commands or read-only socket requests.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOLDEN_SCHEMA="$TESTS_DIR/goldens/live-contract/herdr-api-schema.json"
LIVE_TIMEOUT_SECONDS=3

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

pass_count=0
fail_count=0
skip_count=0

pass() { printf 'PASS  %s\n' "$1"; pass_count=$((pass_count + 1)); }
fail() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail_count=$((fail_count + 1)); }
skip() { printf 'SKIP  %s - %s\n' "$1" "$2"; skip_count=$((skip_count + 1)); }

have_cmd() { command -v "$1" >/dev/null 2>&1; }
run_live() { timeout --signal=TERM --kill-after=1 "$LIVE_TIMEOUT_SECONDS" "$@"; }

real_hcom() {
  if have_cmd mise; then
    run_live mise which hcom 2>/dev/null && return 0
  fi
  command -v hcom 2>/dev/null
}

real_herdr() {
  command -v herdr 2>/dev/null
}

run_hcom_bootstrap() {
  local hcom_bin="$1" hcom_dir="$2"
  local payload
  payload="$(jq -cn \
    --arg sid "live-contract-$$" \
    --arg transcript "$hcom_dir/transcript.jsonl" \
    --arg cwd "$PWD" \
    '{hook_event_name:"SessionStart",session_id:$sid,transcript_path:$transcript,cwd:$cwd}')"
  printf '%s' "$payload" | env -i \
    HOME="${HOME:-}" \
    PATH="${PATH:-}" \
    HCOM_DIR="$hcom_dir" \
    HCOM_LAUNCHED=1 \
    HCOM_PROCESS_ID="live-contract-$$" \
    timeout --signal=TERM --kill-after=1 "$LIVE_TIMEOUT_SECONDS" "$hcom_bin" sessionstart
}

bootstrap_instance_name() {
  local input="$ROOT/bootstrap-name-envelope.json"
  cat >"$input"
  python3 - "$input" <<'PY'
import json
import re
import sys

with open(sys.argv[1]) as f:
    root = json.load(f)
ac = root["hookSpecificOutput"]["additionalContext"]
marker = re.search(r"\[hcom:([^\]]+)\]", ac)
if not marker:
    raise SystemExit("missing hcom marker")
print(marker.group(1).strip())
PY
}

assert_bootstrap_extracts() {
  local input="$ROOT/bootstrap-envelope.json"
  cat >"$input"
  python3 - "$input" <<'PY'
import json
import re
import sys

with open(sys.argv[1]) as f:
    root = json.load(f)
ac = root["hookSpecificOutput"]["additionalContext"]

def extract(text):
    name = re.search(r"(?m)^\s*-?\s*Your name:\s*(.+?)\s*$", text)
    marker = re.search(r"\[hcom:([^\]]+)\]", text)
    sender = re.search(r"Prioritize @(\S+)", text)
    tag = re.search(r"You are tagged (?:\"([^\"]+)\"|'([^']+)')", text)
    if not (name and marker and sender):
        raise AssertionError("required bootstrap fields did not extract")
    return {
        "display_name": name.group(1).strip(),
        "instance_name": marker.group(1).strip(),
        "sender": sender.group(1).strip(),
        "tag": ((tag.group(1) or tag.group(2)).strip() if tag else ""),
    }

base = extract(ac)
if base["display_name"] == "" or base["instance_name"] == "" or base["sender"] == "":
    raise AssertionError("blank extracted identity")
if base["tag"] != "live-contract":
    raise AssertionError(f"real hcom tag line did not extract: {base!r}")

tag_line = None
for line in ac.splitlines():
    if "You are tagged" in line:
        tag_line = line
        break
if tag_line is None:
    raise AssertionError("real hcom bootstrap did not render a tag line")
if "Message your group:" not in tag_line or "@live-contract-" not in tag_line:
    raise AssertionError(f"real hcom tag line lost group-send guidance: {tag_line!r}")

if '"live-contract"' in tag_line:
    twin_line = tag_line.replace('"live-contract"', "'live-contract'", 1)
elif "'live-contract'" in tag_line:
    twin_line = tag_line.replace("'live-contract'", '"live-contract"', 1)
else:
    raise AssertionError(f"real hcom tag line did not quote the tag: {tag_line!r}")

variant = ac.replace(tag_line, twin_line, 1)
for label, text in (("real", ac), ("quote-style twin", variant)):
    got = extract(text)
    if got["tag"] != "live-contract":
        raise AssertionError(f"{label} tag did not extract: {got!r}")
    for key in ("display_name", "instance_name", "sender"):
        if got[key] != base[key]:
            raise AssertionError(f"{label} changed {key}: {got!r} vs {base!r}")
PY
}

check_hcom_bootstrap() {
  local hcom_bin="$1" dir="$ROOT/hcom-bootstrap" out name tagged rc
  mkdir -p "$dir"
  out="$(run_hcom_bootstrap "$hcom_bin" "$dir" 2>"$dir/stderr")"
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    fail "hcom bootstrap extraction" "real hcom sessionstart exited $rc: $(cat "$dir/stderr")"
    return
  fi
  if ! name="$(bootstrap_instance_name <<<"$out" 2>"$dir/name.err")"; then
    fail "hcom bootstrap extraction" "could not extract scratch hcom name: $(cat "$dir/name.err")"
    return
  fi
  if ! HCOM_DIR="$dir" run_live "$hcom_bin" config -i "$name" tag live-contract >"$dir/config.out" 2>"$dir/config.err"; then
    fail "hcom bootstrap extraction" "could not configure scratch hcom tag: $(cat "$dir/config.err")"
    return
  fi
  tagged="$(run_hcom_bootstrap "$hcom_bin" "$dir" 2>"$dir/tagged.stderr")"
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    fail "hcom bootstrap extraction" "tagged hcom sessionstart exited $rc: $(cat "$dir/tagged.stderr")"
    return
  fi
  if assert_bootstrap_extracts <<<"$tagged"; then
    pass "hcom bootstrap extraction accepts real tagged output and the alternate quote style"
  else
    fail "hcom bootstrap extraction" "extractor rejected real tagged hcom bootstrap or quote-style twin"
  fi
}

check_hcom_list_shape() {
  local hcom_bin="$1" out rc
  out="$(run_live "$hcom_bin" list self --json 2>"$ROOT/hcom-list-self.err")"
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    if grep -q "Cannot use 'self' without identity" "$ROOT/hcom-list-self.err"; then
      skip "hcom list self --json shape" "no live hcom self identity in this environment"
      return
    fi
    fail "hcom list self --json shape" "hcom list self --json exited $rc: $(cat "$ROOT/hcom-list-self.err")"
    return
  fi
  if jq -e '
      type == "object"
      and (.name | type == "string" and length > 0)
      and (.name | contains("-") | not)
      and (.status | type == "string" and length > 0)
      and (.tool | type == "string" and length > 0)
      and (.session_id | type == "string" and length > 0)
    ' <<<"$out" >/dev/null; then
    pass "hcom list self --json is a single base-name object"
  else
    fail "hcom list self --json shape" "unexpected payload: $out"
  fi
}

check_hcom_roster_launch_context() {
  local hcom_bin="$1" out rc
  out="$(run_live "$hcom_bin" list --json 2>"$ROOT/hcom-list.err")"
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    fail "hcom roster launch_context fields" "hcom list --json exited $rc: $(cat "$ROOT/hcom-list.err")"
    return
  fi
  if jq -e 'type == "array" and ([.[] | select(.launch_context.terminal_preset_effective != null)] | length == 0)' <<<"$out" >/dev/null; then
    skip "hcom roster launch_context fields" "no hcom-launched roster entries with launch_context"
    return
  fi
  if jq -e '
      type == "array"
      and ([.[] | select(.launch_context.terminal_preset_effective != null)] | length > 0)
      and all(.[] | select(.launch_context.terminal_preset_effective != null);
        (.tool | type == "string" and length > 0)
        and (.launch_context | type == "object")
        and (.launch_context.process_id | type == "string" and length > 0)
        and (.launch_context.terminal_preset_effective | type == "string" and length > 0)
        and ((.launch_context.terminal_preset_effective == "fleet"
              and (.launch_context.pane_id | type == "string" and length > 0)
              and (.launch_context.terminal_id | type == "string" and length > 0))
             or (.launch_context.env | type == "object")))
    ' <<<"$out" >/dev/null; then
    pass "hcom roster exposes launch_context for observed agent families"
  else
    fail "hcom roster launch_context fields" "unexpected payload"
  fi
}

check_herdr_schema() {
  local herdr_bin="$1" current="$ROOT/herdr-api-schema.json"
  if [[ ! -f "$GOLDEN_SCHEMA" ]]; then
    fail "herdr api schema golden" "missing $GOLDEN_SCHEMA"
    return
  fi
  if ! run_live "$herdr_bin" api schema --json >"$current" 2>"$ROOT/herdr-schema.err"; then
    fail "herdr api schema --json drift check" "herdr api schema --json failed: $(cat "$ROOT/herdr-schema.err")"
    return
  fi
  if diff -u "$GOLDEN_SCHEMA" "$current" >"$ROOT/herdr-schema.diff"; then
    pass "herdr api schema --json matches committed snapshot"
  else
    printf '%s\n' '--- herdr api schema diff (first 200 lines) ---'
    sed -n '1,200p' "$ROOT/herdr-schema.diff"
    fail "herdr api schema --json drift check" "schema differs from committed golden"
  fi
}

socket_path_from_status() {
  local herdr_bin="$1"
  python3 - "$herdr_bin" <<'PY'
import json
import subprocess
import sys

try:
    out = subprocess.check_output(
        [sys.argv[1], "status", "server"],
        stderr=subprocess.STDOUT,
        text=True,
        timeout=2.0,
    )
except Exception as exc:
    print(str(exc), file=sys.stderr)
    sys.exit(1)

try:
    obj = json.loads(out)
    result = obj.get("result", obj)
    sock = result.get("socket", "")
    protocol = result.get("protocol")
    compatible = result.get("compatible")
except json.JSONDecodeError:
    sock = ""
    protocol = None
    compatible = None
    for line in out.splitlines():
        key, separator, value = line.partition(":")
        if not separator:
            continue
        if key.strip() == "socket":
            sock = value.strip()
        elif key.strip() == "protocol":
            try:
                protocol = int(value.strip())
            except ValueError:
                pass
        elif key.strip() == "compatible":
            compatible = value.strip().lower()
if not isinstance(protocol, int) or protocol < 19:
    print(f"herdr status server reported protocol {protocol!r}, older than supported protocol 19", file=sys.stderr)
    sys.exit(2)
if compatible not in (True, "yes"):
    print(f"herdr status server reported incompatible status {compatible!r}", file=sys.stderr)
    sys.exit(2)
if not sock:
    print("herdr status server did not report socket", file=sys.stderr)
    sys.exit(1)
print(sock)
PY
}

assert_snapshot_response() {
  local input="$ROOT/snapshot-response.json"
  cat >"$input"
  python3 - "$input" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    obj = json.load(f)
if not isinstance(obj.get("id"), (str, int)):
    raise SystemExit("missing response id")
result = obj.get("result")
if not isinstance(result, dict):
    raise SystemExit("missing result object")
if result.get("type") != "session_snapshot":
    raise SystemExit("result.type is not session_snapshot")
snapshot = result.get("snapshot")
if not isinstance(snapshot, dict):
    raise SystemExit("missing nested result.snapshot object")
if not isinstance(snapshot.get("protocol"), int):
    raise SystemExit("snapshot.protocol is not an integer")
if not isinstance(snapshot.get("panes"), list):
    raise SystemExit("snapshot.panes is not an array")
if not isinstance(snapshot.get("agents"), list):
    raise SystemExit("snapshot.agents is not an array")
PY
}

fetch_socket_snapshot() {
  local sock="$1"
  run_live python3 - "$sock" <<'PY'
import json
import socket
import sys

sock_path = sys.argv[1]
req = {"id": "live-contract-session-snapshot", "method": "session.snapshot", "params": {}}
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
try:
    s.settimeout(2.0)
    s.connect(sock_path)
    s.sendall((json.dumps(req, separators=(",", ":")) + "\n").encode())
    chunks = []
    while True:
        data = s.recv(65536)
        if not data:
            print("connection closed before session.snapshot response", file=sys.stderr)
            sys.exit(3)
        chunks.append(data)
        joined = b"".join(chunks)
        if b"\n" in joined:
            print(joined.splitlines()[0].decode())
            break
finally:
    s.close()
PY
}

check_herdr_socket_snapshot() {
  local herdr_bin="$1" sock out rc
  sock="$(socket_path_from_status "$herdr_bin" 2>"$ROOT/herdr-status.err")"
  rc=$?
  if [[ "$rc" -eq 2 ]]; then
    fail "herdr socket session.snapshot protocol compatibility" "$(cat "$ROOT/herdr-status.err")"
    check_snapshot_negative_demo
    return
  fi
  if [[ "$rc" -ne 0 ]]; then
    skip "herdr socket session.snapshot nested shape" "server status unavailable: $(cat "$ROOT/herdr-status.err")"
    check_snapshot_negative_demo
    return
  fi
  out="$(fetch_socket_snapshot "$sock" 2>"$ROOT/herdr-socket.err")"
  rc=$?
  if [[ "$rc" -eq 3 ]]; then
    fail "herdr socket session.snapshot nested shape" "$(cat "$ROOT/herdr-socket.err")"
    check_snapshot_negative_demo
    return
  fi
  if [[ "$rc" -ne 0 ]]; then
    skip "herdr socket session.snapshot nested shape" "socket request unavailable: $(cat "$ROOT/herdr-socket.err")"
    check_snapshot_negative_demo
    return
  fi
  if assert_snapshot_response <<<"$out"; then
    pass "herdr socket session.snapshot returns nested result.snapshot"
  else
    fail "herdr socket session.snapshot nested shape" "unexpected payload"
  fi

  check_snapshot_negative_demo
}

check_snapshot_negative_demo() {
  local flat='{"id":"negative-demo","result":{"type":"session_snapshot","protocol":19,"panes":[],"agents":[]}}'
  if assert_snapshot_response <<<"$flat" >/dev/null 2>&1; then
    fail "negative demo: flat session.snapshot is rejected" "flat-serving response passed nested-shape assertion"
  else
    pass "negative demo: flat session.snapshot is rejected by the live assertion path"
  fi
}

# TASK-317: on 2026-08-21 a wholesale rewrite of ~/.claude/settings.json
# silently dropped the herdr SessionStart hook registration — every claude
# started afterwards looked session-less fleet-wide, and `herdr integration
# status` did not catch it because it verifies the hook script FILE, not the
# settings registration. Post-teardown the same wipe class would also strip
# hcom's hooks, the only remaining self-registration path onto the bus. These
# checks pin the live registrations so the wipe class fails a gate.

claude_sessionstart_commands() {
  jq -r '.hooks.SessionStart[]?.hooks[]?.command // empty' "$1" 2>/dev/null
}

check_claude_settings_registrations() {
  local settings="$1"
  local label_herdr="claude settings herdr SessionStart registration"
  local label_hcom="claude settings hcom hook registration"
  if [[ ! -f "$settings" ]]; then
    fail "$label_herdr" "$settings missing entirely (settings-wipe class); reinstall: herdr integration install claude"
    fail "$label_hcom" "$settings missing entirely (settings-wipe class); reinstall: bin/ai-setup --hcom-hooks install"
    return
  fi
  local cmds herdr_cmd script
  cmds="$(claude_sessionstart_commands "$settings")"
  herdr_cmd="$(grep -m1 'herdr-agent-state' <<<"$cmds" || true)"
  if [[ -z "$herdr_cmd" ]]; then
    fail "$label_herdr" "settings REGISTRATION missing (the hook script may still exist on disk — the silent-wipe class herdr integration status does not catch); reinstall: herdr integration install claude"
  else
    script="$(sed -n "s/.*'\([^']*herdr-agent-state[^']*\)'.*/\1/p" <<<"$herdr_cmd")"
    if [[ -n "$script" && ! -f "$script" ]]; then
      fail "$label_herdr" "registration present but hook SCRIPT missing at $script (a different failure than a settings wipe); reinstall: herdr integration install claude"
    else
      pass "claude settings herdr SessionStart registration present, hook script on disk"
    fi
  fi
  if grep -q 'sessionstart' <<<"$cmds"; then
    pass "claude settings hcom sessionstart hook registration present"
  else
    fail "$label_hcom" "hcom sessionstart hook not registered — new claudes will not self-register on the bus; reinstall: bin/ai-setup --hcom-hooks install"
  fi
}

check_settings_registration_negative_demo() {
  # The exact 2026-08-21 wipe signature: valid settings JSON, user content
  # intact, registration rows gone. Run in a subshell so the fixture's FAIL
  # lines do not count against the live summary.
  local wiped="$ROOT/wiped-settings.json" out
  printf '{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/keep/user"}]}]}}\n' > "$wiped"
  out="$(check_claude_settings_registrations "$wiped")"
  if grep -q 'FAIL.*herdr SessionStart' <<<"$out" && grep -q 'FAIL.*hcom hook' <<<"$out"; then
    pass "negative demo: wipe-class settings (registrations gone) is rejected by the live assertion path"
  else
    fail "negative demo: wipe-class settings is rejected" "wipe fixture passed the registration checks: $out"
  fi
}

check_hcom_pi_launch_line() {
  local hcom_bin="$1" out
  if ! out="$(HOME="$ROOT/pi-home" HCOM_DIR="$ROOT/pi-home/.hcom" run_live "$hcom_bin" pi --help 2>"$ROOT/hcom-pi-help.err")"; then
    fail "hcom Pi launch line" "hcom pi --help failed: $(cat "$ROOT/hcom-pi-help.err")"
    return
  fi
  if grep -qF 'hcom [N] pi [args...]' <<<"$out" &&
     grep -qF 'HCOM_NOTES' <<<"$out" &&
     grep -qF 'hcom r <target>' <<<"$out" &&
     grep -qF 'hcom f <target>' <<<"$out"; then
    pass "hcom Pi launch line pins launch, lifecycle, and notes surfaces"
  else
    fail "hcom Pi launch line" "installed hcom no longer advertises the pinned Pi launch/lifecycle/notes surface"
  fi
}

if have_cmd jq && have_cmd python3 && have_cmd timeout; then
  :
else
  printf 'SKIP  live-contract prerequisites - jq, python3, and timeout are required\n'
  printf '\nSUMMARY live-contract: PASS=0 FAIL=0 SKIP=10\n'
  exit 0
fi

if [[ -d "$HOME/.claude" ]]; then
  check_claude_settings_registrations "$HOME/.claude/settings.json"
else
  skip "claude settings herdr SessionStart registration" "no ~/.claude on this machine"
  skip "claude settings hcom hook registration" "no ~/.claude on this machine"
fi
check_settings_registration_negative_demo

if hcom_bin="$(real_hcom)" && [[ -n "$hcom_bin" && -x "$hcom_bin" ]]; then
  check_hcom_bootstrap "$hcom_bin"
  check_hcom_list_shape "$hcom_bin"
  check_hcom_roster_launch_context "$hcom_bin"
  check_hcom_pi_launch_line "$hcom_bin"
else
  skip "hcom bootstrap extraction" "installed hcom not found"
  skip "hcom list self --json shape" "installed hcom not found"
  skip "hcom roster launch_context fields" "installed hcom not found"
  skip "hcom Pi launch line" "installed hcom not found"
fi

if herdr_bin="$(real_herdr)" && [[ -n "$herdr_bin" && -x "$herdr_bin" ]]; then
  check_herdr_schema "$herdr_bin"
  check_herdr_socket_snapshot "$herdr_bin"
else
  skip "herdr api schema --json drift check" "installed herdr not found"
  skip "herdr socket session.snapshot nested shape" "installed herdr not found"
  skip "negative demo: flat session.snapshot is rejected" "installed herdr not found"
fi

printf '\nSUMMARY live-contract: PASS=%d FAIL=%d SKIP=%d\n' "$pass_count" "$fail_count" "$skip_count"
if [[ "$skip_count" -gt 0 ]]; then
  printf 'VISIBLE SKIP COUNT: %d\n' "$skip_count"
fi

if [[ "$fail_count" -eq 0 ]]; then
  exit 0
fi
exit 1
