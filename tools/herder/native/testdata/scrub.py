"""Scrub secret-looking values from recorded fixtures, in place. Run by record.sh on the staging copy.

Works on decoded string values, not on the raw file text, so escaped quotes and punctuation inside a
secret can't split it. JSON files are walked; the JSON on each `data:` line of an .sse file is walked too.
"""

import json
import pathlib
import re
import sys

# A whole string value under one of these keys is a secret.
SECRET_KEY = re.compile(r"(?i)^(.*_)?(api[_-]?key|secret|token|password|passwd|authorization|bearer|private[_-]?key)$")

# Inside free text: replace the value part only. Values may contain punctuation; they end at whitespace
# or a quote.
VALUE = r"[^\s\"'`]{8,}"
IN_TEXT = [
    re.compile(r"(?i)(authorization\s*:\s*bearer\s+)\S+"),
    re.compile(r"(?i)\b((?:api[_-]?key|secret|token|password|passwd)\s*[:=]\s*)" + VALUE),
    re.compile(r"()\b(?:sk|rk)-[A-Za-z0-9_\-]{16,}"),
    re.compile(r"()\bgh[pousr]_[A-Za-z0-9]{20,}"),
    re.compile(r"()\bgithub_pat_[A-Za-z0-9_]{20,}"),
    re.compile(r"()\bxox[abpors]-[A-Za-z0-9\-]{10,}"),
    re.compile(r"()\bAKIA[0-9A-Z]{16}\b"),
    re.compile(r"()\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}"),
    re.compile(r"()-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----", re.S),
]

hits = 0


def scrub_text(s: str) -> str:
    global hits
    for pat in IN_TEXT:
        s, n = pat.subn(lambda m: m.group(1) + "[scrubbed]", s)
        hits += n
    return s


def scrub_value(v, key=None):
    global hits
    if isinstance(v, str):
        if key is not None and SECRET_KEY.match(key) and len(v) >= 8:
            hits += 1
            return "[scrubbed]"
        return scrub_text(v)
    if isinstance(v, list):
        return [scrub_value(x) for x in v]
    if isinstance(v, dict):
        return {k: scrub_value(x, k) for k, x in v.items()}
    return v


def scrub_json_file(path: pathlib.Path):
    d = json.loads(path.read_text())
    if path.name == "detail.json":
        # launch_context.env carries SSH_CONNECTION addresses; nothing in it is client-visible state.
        d.get("launch_context", {}).pop("env", None)
    path.write_text(json.dumps(scrub_value(d), separators=(",", ":")) + "\n")


def scrub_sse_file(path: pathlib.Path):
    out = []
    for line in path.read_text().split("\n"):
        if line.startswith("data:"):
            body = line[5:].lstrip(" ")
            line = "data: " + json.dumps(scrub_value(json.loads(body)), separators=(",", ":"))
        out.append(line)
    path.write_text("\n".join(out))


root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).parent)
for path in sorted(root.rglob("*")):
    if "big" in path.parts:
        continue
    if path.suffix == ".json":
        scrub_json_file(path)
    elif path.suffix == ".sse":
        scrub_sse_file(path)
print(f"scrub: {hits} replacements")
