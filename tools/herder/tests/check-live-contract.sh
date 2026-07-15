#!/usr/bin/env bash
# check-live-contract.sh - pin herder's assumptions against installed hcom/herdr.
#
# This tier intentionally talks to the live installed binaries, not the hermetic
# mocks used by the rest of the contract battery. Mutating operations are out of
# scope: hcom hook bootstrapping runs under a scratch HCOM_DIR, and herdr checks
# use read/introspection commands or read-only socket requests. Herdr event
# The check assumes Herdr subscriptions are connection-scoped. The subscription
# probe guarantees that its client connection closes immediately after the
# acknowledgement; it does not inspect server-side subscription state.

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
        and (.launch_context.pane_id | type == "string" and length > 0)
        and (.launch_context.process_id | type == "string" and length > 0)
        and (.launch_context.terminal_preset_effective | type == "string" and length > 0)
        and (.launch_context.env | type == "object"))
    ' <<<"$out" >/dev/null; then
    pass "hcom roster exposes launch_context for observed agent families"
  else
    fail "hcom roster launch_context fields" "unexpected payload"
  fi
}

check_herdr_agent_list() {
  local herdr_bin="$1" out rc
  out="$(run_live "$herdr_bin" agent list 2>"$ROOT/herdr-agent-list.err")"
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    fail "herdr agent list envelope" "herdr agent list exited $rc: $(cat "$ROOT/herdr-agent-list.err")"
    return
  fi
  if jq -e '
      (.id | type == "string" and length > 0)
      and (.result.type == "agent_list")
      and (.result.agents | type == "array")
      and all(.result.agents[];
        (.pane_id | type == "string" and length > 0)
        and (.terminal_id | type == "string" and length > 0)
        and (.agent_status | type == "string" and length > 0))
    ' <<<"$out" >/dev/null; then
    pass "herdr agent list returns the pinned envelope"
  else
    fail "herdr agent list envelope" "unexpected payload"
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

  if assert_subscription_schema "$current"; then
    pass "herdr schema pins protocol and observer subscription parameter shapes"
  else
    fail "herdr subscription schema contract" "protocol, request, acknowledgement, or observer subscription variants drifted"
  fi
}

assert_subscription_schema() {
  python3 - "$1" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    schema = json.load(f)

if schema.get("protocol") != 16:
    raise SystemExit("schema protocol is not 16")

request = schema.get("schemas", {}).get("request", {})
defs = request.get("$defs", {})
params = defs.get("EventsSubscribeParams")
if not isinstance(params, dict):
    raise SystemExit("missing EventsSubscribeParams")
if params.get("type") != "object" or params.get("required") != ["subscriptions"]:
    raise SystemExit("events.subscribe params shape drifted")
subscriptions = params.get("properties", {}).get("subscriptions", {})
if subscriptions.get("type") != "array" or subscriptions.get("items", {}).get("$ref") != "#/schemas/request/$defs/Subscription":
    raise SystemExit("events.subscribe subscriptions array shape drifted")

request_variants = request.get("oneOf", [])
subscribe_requests = [
    item for item in request_variants
    if item.get("properties", {}).get("method", {}).get("const") == "events.subscribe"
]
if len(subscribe_requests) != 1:
    raise SystemExit("events.subscribe request variant missing or duplicated")
subscribe_request = subscribe_requests[0]
if subscribe_request.get("required") != ["method", "params"]:
    raise SystemExit("events.subscribe request required fields drifted")
if subscribe_request.get("properties", {}).get("params", {}).get("$ref") != "#/schemas/request/$defs/EventsSubscribeParams":
    raise SystemExit("events.subscribe params reference drifted")

subscription = defs.get("Subscription", {})
variants = {}
for item in subscription.get("oneOf", []):
    event_type = item.get("properties", {}).get("type", {}).get("const")
    if event_type:
        variants[event_type] = item
for event_type in ("pane.created", "pane.closed", "pane.exited", "pane.agent_detected"):
    item = variants.get(event_type)
    if not isinstance(item, dict):
        raise SystemExit(f"missing subscription variant {event_type}")
    if item.get("type") != "object" or item.get("required") != ["type"]:
        raise SystemExit(f"subscription parameter shape drifted for {event_type}")
    if set(item.get("properties", {})) != {"type"}:
        raise SystemExit(f"unexpected parameters for {event_type}")

success = schema.get("schemas", {}).get("success_response", {}).get("$defs", {}).get("ResponseResult", {})
acks = [
    item for item in success.get("oneOf", [])
    if item.get("properties", {}).get("type", {}).get("const") == "subscription_started"
]
if len(acks) != 1 or acks[0].get("required") != ["type"]:
    raise SystemExit("subscription_started acknowledgement shape drifted")
PY
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
if protocol != 16:
    print(f"herdr status server reported protocol {protocol!r}, expected 16", file=sys.stderr)
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

assert_subscription_response() {
  local input="$ROOT/subscription-response.json"
  cat >"$input"
  python3 - "$input" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    obj = json.load(f)
if obj.get("id") != "live-contract-events-subscribe":
    raise SystemExit("subscription acknowledgement id does not match request")
result = obj.get("result")
if not isinstance(result, dict):
    raise SystemExit("missing subscription result object")
if result.get("type") != "subscription_started":
    raise SystemExit("result.type is not subscription_started")
PY
}

fetch_socket_subscription_ack() {
  local sock="$1"
  run_live python3 - "$sock" <<'PY'
import json
import socket
import sys

sock_path = sys.argv[1]
req = {
    "id": "live-contract-events-subscribe",
    "method": "events.subscribe",
    "params": {"subscriptions": [
        {"type": "pane.created"},
        {"type": "pane.closed"},
        {"type": "pane.exited"},
        {"type": "pane.agent_detected"},
    ]},
}
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
try:
    s.settimeout(2.0)
    s.connect(sock_path)
    s.sendall((json.dumps(req, separators=(",", ":")) + "\n").encode())
    chunks = []
    while True:
        data = s.recv(65536)
        if not data:
            print("connection closed before subscription acknowledgement", file=sys.stderr)
            sys.exit(3)
        chunks.append(data)
        joined = b"".join(chunks)
        if b"\n" in joined:
            print(joined.splitlines()[0].decode())
            break
finally:
    # Assuming Herdr scopes subscriptions to their connection, this close ends
    # the read-only probe on success, mismatch, timeout, and socket errors.
    s.close()
PY
}

check_herdr_socket_subscription() {
  local herdr_bin="$1" sock out rc
  sock="$(socket_path_from_status "$herdr_bin" 2>"$ROOT/herdr-subscription-status.err")"
  rc=$?
  if [[ "$rc" -eq 2 ]]; then
    fail "herdr socket subscription protocol compatibility" "$(cat "$ROOT/herdr-subscription-status.err")"
    check_subscription_negative_demo
    return
  fi
  if [[ "$rc" -ne 0 ]]; then
    skip "herdr socket subscription acknowledgement" "live server unavailable: $(cat "$ROOT/herdr-subscription-status.err")"
    check_subscription_negative_demo
    return
  fi
  out="$(fetch_socket_subscription_ack "$sock" 2>"$ROOT/herdr-subscription.err")"
  rc=$?
  if [[ "$rc" -eq 3 ]]; then
    fail "herdr socket subscription acknowledgement" "$(cat "$ROOT/herdr-subscription.err")"
    check_subscription_negative_demo
    return
  fi
  if [[ "$rc" -ne 0 ]]; then
    skip "herdr socket subscription acknowledgement" "live socket unavailable: $(cat "$ROOT/herdr-subscription.err")"
    check_subscription_negative_demo
    return
  fi
  if assert_subscription_response <<<"$out"; then
    pass "herdr socket subscription returns subscription_started; client closes immediately"
  else
    fail "herdr socket subscription acknowledgement" "unexpected payload"
  fi
  check_subscription_negative_demo
}

check_subscription_negative_demo() {
  local malformed='{"id":"live-contract-events-subscribe","result":{"ok":true}}'
  if assert_subscription_response <<<"$malformed" >/dev/null 2>&1; then
    fail "negative demo: malformed subscription acknowledgement is rejected" "mock-only acknowledgement passed the live assertion path"
  else
    pass "negative demo: malformed subscription acknowledgement is rejected by the live assertion path"
  fi
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
  local flat='{"id":"negative-demo","result":{"type":"session_snapshot","protocol":16,"panes":[],"agents":[]}}'
  if assert_snapshot_response <<<"$flat" >/dev/null 2>&1; then
    fail "negative demo: flat session.snapshot is rejected" "flat-serving response passed nested-shape assertion"
  else
    pass "negative demo: flat session.snapshot is rejected by the live assertion path"
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
  check_herdr_agent_list "$herdr_bin"
  check_herdr_schema "$herdr_bin"
  check_herdr_socket_snapshot "$herdr_bin"
  check_herdr_socket_subscription "$herdr_bin"
else
  skip "herdr agent list envelope" "installed herdr not found"
  skip "herdr api schema --json drift check" "installed herdr not found"
  skip "herdr subscription schema contract" "installed herdr not found"
  skip "herdr socket session.snapshot nested shape" "installed herdr not found"
  skip "negative demo: flat session.snapshot is rejected" "installed herdr not found"
  skip "herdr socket subscription acknowledgement" "installed herdr not found"
  check_subscription_negative_demo
fi

printf '\nSUMMARY live-contract: PASS=%d FAIL=%d SKIP=%d\n' "$pass_count" "$fail_count" "$skip_count"
if [[ "$skip_count" -gt 0 ]]; then
  printf 'VISIBLE SKIP COUNT: %d\n' "$skip_count"
fi

if [[ "$fail_count" -eq 0 ]]; then
  exit 0
fi
exit 1
