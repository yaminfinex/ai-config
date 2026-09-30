#!/bin/sh
# Re-record the fixtures under testdata/ from the live serve. Read-only GETs; never POSTs.
#
#   testdata/record.sh                 # the committed fixture set
#   testdata/record.sh --full riko     # page one whole transcript into testdata/big/ (gitignored)
#
# HERDER_URL overrides the server (tailnet address only; loopback refuses /api/state reads).
set -eu
cd "$(dirname "$0")"
URL="${HERDER_URL:-http://yamen-superset-f4-med-syd-1:4400}"
# agent:tail-limit — mupu (claude, most kinds; the rare ones sit in its last 250) · conductor-line
# (task_notification, compact_divider) · grill-confirm-lubo (codex). Limits keep the repo small.
AGENTS="mupu:250 conductor-line:400 grill-confirm-lubo:150"
BIG="riko"                                         # the largest transcript on the server (~90 MB)

get() { curl -sS -m 60 -w '\n%{http_code}' "$URL$1"; }
# get_json PATH FILE — writes the body; on a non-2xx writes the refusal body as-is (the api tests want it)
get_json() {
  out=$(get "$1"); code=${out##*
}; body=${out%
*}
  printf '%s\n' "$body" > "$2"
  echo "  $2  [$code]  $(wc -c < "$2" | tr -d ' ') bytes"
}

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

echo "recording from $URL"
get_json /api/fleet fleet.json
get_json /api/viewer viewer.json
get_json '/api/state/spaces?since=0' state-spaces.json
get_json '/api/state/notes?since=0' state-notes.json
get_json '/api/state/spaces.members?since=0' state-spaces.members.json

# The first two SSE frames: hello, then the full board.
curl -sS -N -m 20 "$URL/api/events" 2>/dev/null | awk '/^event: fleet/{f=1} {print} f && /^$/{exit}' > events.sse || true
echo "  events.sse  $(wc -c < events.sse | tr -d ' ') bytes"

for spec in $AGENTS; do
  a=${spec%%:*}; n=${spec##*:}
  mkdir -p "agents/$a"
  get_json "/api/agents/$a" "agents/$a/detail.json"
  get_json "/api/agents/$a/entries?limit=$n" "agents/$a/tail.json"
  from=$(python3 -c "import json;print(json.load(open('agents/$a/tail.json'))['window']['from'])")
  sid=$(python3 -c "import json;print(json.load(open('agents/$a/tail.json'))['sessionId'])")
  get_json "/api/agents/$a/entries?before=$from&limit=50&sessionId=$sid" "agents/$a/before.json"
done

# A slice from deep inside the big transcript: a tail, then one before= page.
mkdir -p "agents/$BIG"
get_json "/api/agents/$BIG/entries?limit=100" "agents/$BIG/tail.json"
from=$(python3 -c "import json;print(json.load(open('agents/$BIG/tail.json'))['window']['from'])")
get_json "/api/agents/$BIG/entries?before=$from&limit=100" "agents/$BIG/before.json"

python3 scrub.py
echo "total: $(du -sh . | cut -f1)"
