#!/bin/sh
# Re-record the fixtures under testdata/ from the live serve. Read-only GETs; never POSTs.
#
#   testdata/record.sh                 # the committed fixture set
#   testdata/record.sh --full riko     # page one whole transcript into testdata/big/ (gitignored)
#
# Everything is recorded into a staging directory and scrubbed there; it is published into testdata/
# only when every GET succeeded and the scrub ran, so a failure can't leave raw recordings behind.
# HERDER_URL overrides the server (tailnet address only; loopback refuses /api/state reads).
set -eu
cd "$(dirname "$0")"
URL="${HERDER_URL:-http://yamen-superset-f4-med-syd-1:4400}"
# agent:tail-limit — mupu (claude, most kinds; the rare ones sit in its last 250) · conductor-line
# (task_notification, compact_divider) · grill-confirm-lubo (codex). Limits keep the repo small.
AGENTS="mupu:250 conductor-line:400 grill-confirm-lubo:150"
BIG="riko"                                         # the largest transcript on the server (~90 MB)

if [ "${1:-}" = "--full" ]; then
  name="${2:?agent name}"; mkdir -p big; out="big/$name.pages.json"
  echo "paging $name backward into $out"
  python3 - "$URL" "$name" "$out" <<'PY'
import json, sys, urllib.request, urllib.parse
url, name, out = sys.argv[1:4]
def fetch(q): return json.load(urllib.request.urlopen(f"{url}/api/agents/{urllib.parse.quote(name)}/entries?{q}", timeout=120))
pages = [fetch("limit=500")]
while True:
    prev = pages[-1]["window"]["from"]
    if prev == 0: break
    p = fetch(f"before={prev}&limit=500&sessionId={pages[-1]['sessionId']}")
    if not p.get("entries"): break
    pages.append(p)
json.dump(pages, open(out, "w"))
print(f"  {len(pages)} pages, {sum(len(p['entries']) for p in pages)} entries")
PY
  exit 0
fi

STAGE=$(mktemp -d "${TMPDIR:-/tmp}/herder-fixtures.XXXXXX")
trap 'rm -rf "$STAGE"' EXIT INT TERM

# get PATH FILE — a non-2xx or a transport failure aborts the whole recording (set -e).
get() {
  mkdir -p "$STAGE/$(dirname "$2")"
  curl -sS -f -m 60 "$URL$1" -o "$STAGE/$2"
  echo "  $2  $(wc -c < "$STAGE/$2" | tr -d ' ') bytes"
}
field() { python3 -c "import json,sys;print(json.load(open(sys.argv[1]))$2)" "$STAGE/$1"; }

echo "recording from $URL into $STAGE"
get /api/fleet fleet.json
get /api/viewer viewer.json
get '/api/state/spaces?since=0' state-spaces.json
get '/api/state/notes?since=0' state-notes.json
get '/api/state/spaces.members?since=0' state-spaces.members.json

# The first two SSE frames: hello, then the full board.
curl -sS -f -N -m 20 "$URL/api/events" 2>/dev/null | awk '/^event: fleet/{f=1} {print} f && /^$/{exit}' > "$STAGE/events.sse"
grep -q '^event: fleet' "$STAGE/events.sse"
echo "  events.sse  $(wc -c < "$STAGE/events.sse" | tr -d ' ') bytes"

for spec in $AGENTS; do
  a=${spec%%:*}; n=${spec##*:}
  get "/api/agents/$a" "agents/$a/detail.json"
  get "/api/agents/$a/entries?limit=$n" "agents/$a/tail.json"
  from=$(field "agents/$a/tail.json" "['window']['from']")
  sid=$(field "agents/$a/tail.json" "['sessionId']")
  get "/api/agents/$a/entries?before=$from&limit=50&sessionId=$sid" "agents/$a/before.json"
done

# A slice from deep inside the big transcript: a tail, then one before= page.
get "/api/agents/$BIG/entries?limit=100" "agents/$BIG/tail.json"
from=$(field "agents/$BIG/tail.json" "['window']['from']")
get "/api/agents/$BIG/entries?before=$from&limit=100" "agents/$BIG/before.json"

python3 scrub.py "$STAGE"
cp -R "$STAGE"/. .
echo "published; total: $(du -sh . | cut -f1)"
