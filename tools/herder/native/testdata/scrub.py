"""Scrub secret-looking values from recorded fixtures, in place. Run by record.sh on the staging copy.

    scrub.py <dir>     scrub every .json/.sse under <dir>
    scrub.py --check   run the synthetic checks and exit non-zero if any secret shape survives

Works on decoded string values, not on the raw file text, so escaped quotes and punctuation inside a
secret can't split it. JSON files are walked; the JSON on each `data:` line of an .sse file is walked too.
"""

import json
import pathlib
import re
import sys

# A whole string value under one of these keys is a secret.
SECRET_KEY = re.compile(r"(?i)^(.*_)?(api[_-]?key|secret|token|password|passwd|authorization|bearer|private[_-]?key)$")

# Inside free text (including code and JSON quoted inside a transcript string): replace the value part.
# The key may be bare or quoted; the value may be bare or quoted, may contain punctuation, and ends at
# whitespace, a quote or a backtick.
Q = r"[\"'`]?"
VALUE = r"[^\s\"'`]{8,}"
IN_TEXT = [
    re.compile(r"(?i)(authorization\s*:\s*bearer\s+)\S+"),
    re.compile(r"(?i)(" + Q + r"(?:api[_-]?key|secret|token|password|passwd)" + Q + r"\s*[:=]\s*" + Q + r")" + VALUE),
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


# (input, what must be gone, what must survive)
CHECKS = [
    ('password="abcd!1234secret"', "abcd!1234secret", 'password="[scrubbed]"'),
    ('{"api_key":"abcdefgh12345678!rest"}', "abcdefgh12345678", '{"api_key":"[scrubbed]"}'),
    ("export TOKEN=abcd!efgh?ijkl", "abcd!efgh", "TOKEN=[scrubbed]"),
    ("Authorization: Bearer eyJabc.def.ghi-jkl", "eyJabc", "Authorization: Bearer [scrubbed]"),
    ("secret: 'p@ss/word=12'", "p@ss/word", "secret: '[scrubbed]'"),
    ("key sk-abcdefghijklmnopqrstuvwxyz1234", "sk-abcdefgh", "key [scrubbed]"),
    ({"nested": {"Api-Key": "12345678abc"}}, "12345678abc", "[scrubbed]"),
    ("used_tokens: 102997 and tokens=12", "NONE", "used_tokens: 102997 and tokens=12"),
]


def check() -> int:
    bad = 0
    for raw, gone, kept in CHECKS:
        out = scrub_value(raw)
        text = json.dumps(out) if not isinstance(out, str) else out
        if (gone != "NONE" and gone in text) or kept not in text:
            bad += 1
            print(f"scrub check FAILED: {raw!r} -> {text!r}")
    print(f"scrub check: {len(CHECKS) - bad}/{len(CHECKS)} ok")
    return 1 if bad else 0


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--check":
        sys.exit(check())
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).parent)
    for path in sorted(root.rglob("*")):
        if "big" in path.parts:
            continue
        if path.suffix == ".json":
            scrub_json_file(path)
        elif path.suffix == ".sse":
            scrub_sse_file(path)
    print(f"scrub: {hits} replacements")
