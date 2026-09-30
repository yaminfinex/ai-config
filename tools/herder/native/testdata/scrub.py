"""Scrub secret-looking strings from every recorded fixture, in place. Run by record.sh."""

import json
import pathlib
import re

SECRET = [
    # key = value / "key": "value" shapes
    r'(?i)(api[_-]?key|secret|token|password|passwd|authorization|bearer)(\\?["\']?\s*[:=]\s*\\?["\']?)([A-Za-z0-9_\-./+=]{8,})',
    # well-known prefixes
    r"\b(sk|rk)-[A-Za-z0-9_\-]{16,}",
    r"\bgh[pousr]_[A-Za-z0-9]{20,}",
    r"\bgithub_pat_[A-Za-z0-9_]{20,}",
    r"\bxox[abpors]-[A-Za-z0-9\-]{10,}",
    r"\bAKIA[0-9A-Z]{16}\b",
    r"\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}",
    r"-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----",
]

root = pathlib.Path(__file__).parent
hits = 0
for path in sorted(root.rglob("*")):
    if path.suffix not in (".json", ".sse") or "big" in path.parts:
        continue
    text = path.read_text()
    for pat in SECRET:
        text, n = re.subn(pat, lambda m: (m.group(1) + m.group(2) if m.lastindex and m.lastindex >= 2 else "") + "[scrubbed]", text, flags=re.S)
        hits += n
    if path.name == "detail.json":
        # launch_context.env carries SSH_CONNECTION addresses; nothing in it is client-visible state.
        d = json.loads(text)
        d.get("launch_context", {}).pop("env", None)
        text = json.dumps(d, separators=(",", ":")) + "\n"
    path.write_text(text)
print(f"scrub: {hits} replacements")
