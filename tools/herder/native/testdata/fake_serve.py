#!/usr/bin/env python3
"""A fake herder serve over the recorded fixtures, for harness runs that press cmd-enter: nothing a
scenario sends may reach a real agent. Reads answer from testdata/; POST …/message never delivers.

    testdata/fake_serve.py PORT [--message ok|slow|409|502|hold] [--retired AGENT] [--queued AGENT] [--notes]
                                [--turn AGENT[,AGENT…] | --block AGENT[,AGENT…] | --read AGENT[,AGENT…]]…
                                [--turn-at SECONDS] [--share AGENT]

`slow` answers ok after a second, `hold` keeps the POST open (the composer stays "sending"); `409` is a
sender collision. `POST /api/state/<ns>` keeps the rows in memory, last write wins, and later reads of
that namespace return them (U5); `--notes` starts the notes namespace with web's two notes on mupu
(`notes-web.json`). Each `--turn` is one more fleet frame on the stream, `--turn-at` seconds after it
opens and 0.3 s apart, in which those agents have finished another turn (U6); a `--block` frame, in
the same order, shows them blocked; a `--read` frame is web reading them (RM): a `read.markers` row at
their current turn, then a `state-changed` nudge. A namespace with no fixture (`read.markers`) answers
404 until something is posted to it. `--queued` gives the agent two queued messages in its detail. `--share` adds the agent to the first space's members too (an agent
in two spaces). Every request is logged on stderr.
"""

import argparse
import json
import pathlib
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

DATA = pathlib.Path(__file__).resolve().parent
ARGS = None
STATE = {}  # namespace -> {key: row}, what was posted (and --notes, --read)
NUDGES = 0  # --read frames sent: the revision a namespace with no fixture stands at, past its rows


def ago(seconds):
    """A time `seconds` back in hcom's wire form (`hcomevents` keeps its `ts`): microseconds, +00:00."""
    at = time.time() - seconds
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(at)) + f".{int(at % 1 * 1e6):06d}+00:00"


# --queued: an operator's request and another agent's inform, waiting for the agent's next turn.
QUEUED = [
    {"id": 371204, "sender": "web-yamen-core-infinex-gg", "intent": "request", "operator": True,
     "preview": "When you're back, check the riko walk numbers\nand say if RSS moved.", "sent_at": ago(95)},
    {"id": 371210, "sender": "chief-mihe", "intent": "inform", "preview": "A2 merged; A3 is next.",
     "sent_at": ago(12)},
]


def fixture(name):
    return (DATA / name).read_text()


class Fake(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def reply(self, status, body):
        data = body.encode() if isinstance(body, str) else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        url = urlparse(self.path)
        q = {k: v[0] for k, v in parse_qs(url.query).items()}
        parts = url.path.strip("/").split("/")
        if url.path == "/api/events":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            self.wfile.write(fixture("events.sse").encode())
            self.wfile.flush()
            board = json.loads(fixture("fleet.json"))
            for i, (kind, agents) in enumerate(ARGS.frames):
                time.sleep(ARGS.turn_at if i == 0 else 0.3)
                if kind == "read":
                    self.log_message("%s: %s", kind, agents)
                    self.wfile.write(read(board, agents.split(",")).encode())
                    self.wfile.flush()
                    continue
                for pane in panes(board):
                    if pane.get("agent") not in agents.split(","):
                        continue
                    if kind == "block":
                        pane["bus_status"] = "blocked"
                    elif pane.get("turn_end_id"):
                        pane["turn_end_id"] += 1
                self.log_message("%s: %s", kind, agents)
                self.wfile.write(f"event: fleet\ndata: {json.dumps(board)}\n\n".encode())
                self.wfile.flush()
            while True:
                time.sleep(15)
                self.wfile.write(b"event: ping\ndata: {}\n\n")
                self.wfile.flush()
        elif url.path == "/api/fleet":
            self.reply(200, fixture("fleet.json"))
        elif url.path == "/api/viewer":
            self.reply(200, fixture("viewer.json"))
        elif parts[:2] == ["api", "state"] and not (DATA / f"state-{parts[2]}.json").exists():
            held = STATE.get(parts[2])
            if held is None:
                return self.reply(404, {"error": "unknown state namespace", "detail": parts[2]})
            self.reply(200, {"rows": list(held.values()), "rev": len(held) + NUDGES})
        elif parts[:2] == ["api", "state"]:
            got = json.loads(fixture(f"state-{parts[2]}.json"))
            if parts[2] == "spaces.members" and ARGS.share:
                members = got["rows"][0]["value"]["members"]
                members.append({"kind": "agent", "name": ARGS.share})
            rows = {r["key"]: r for r in got["rows"]} | STATE.get(parts[2], {})
            self.reply(200, {"rows": list(rows.values()), "rev": got["rev"] + len(STATE.get(parts[2], {}))})
        elif parts[:2] == ["api", "agents"] and len(parts) == 3:
            path = DATA / "agents" / parts[2] / "detail.json"
            detail = json.loads(path.read_text()) if path.exists() else {"name": parts[2]}
            if parts[2] == ARGS.retired:
                detail["bus_status"] = "retired"
            if parts[2] == ARGS.queued:
                detail["queued"] = QUEUED
            self.reply(200, detail)
        elif parts[:2] == ["api", "agents"] and parts[3:] == ["entries"]:
            page = "before" if "before" in q else "tail"
            path = DATA / "agents" / parts[2] / f"{page}.json"
            if "from" in q or not path.exists():
                at = int(q.get("from", 0))
                window = {"mode": "from", "from": at, "limit": 100}
                self.reply(200, {"sessionId": q.get("sessionId", "s"), "window": window,
                                 "entries": [], "nextOffset": at})
            else:
                self.reply(200, path.read_text())
        else:
            self.reply(404, {"error": "not found", "detail": url.path})

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0))).decode()
        parts = urlparse(self.path).path.strip("/").split("/")
        if parts[:2] == ["api", "state"] and len(parts) == 3:
            rows = json.loads(body)["rows"]
            self.log_message("state %s: %s", parts[2], body)
            merge(parts[2], rows)
            return self.reply(200, {"accepted": [r["key"] for r in rows], "rev": 1})
        if parts[:2] != ["api", "agents"] or parts[3:] != ["message"]:
            return self.reply(404, {"error": "not found", "detail": self.path})
        self.log_message("message to %s: %s (%s)", parts[2], body, ARGS.message)
        if ARGS.message in ("hold", "slow"):
            time.sleep(600 if ARGS.message == "hold" else 1)
        if ARGS.message == "409":
            return self.reply(409, {"error": "sender refused",
                                    "detail": "the fake serve refuses every message"})
        if ARGS.message == "502":
            return self.reply(502, {"error": "substrate unreachable", "detail": "hcom timed out"})
        self.reply(200, {"sent": True, "to": parts[2], "from": "web-fake", "intent": "request"})


def panes(board):
    for ws in board["workspaces"]:
        for tab in ws["tabs"]:
            yield from tab["panes"]


def read(board, agents):
    """Web reads `agents` now: their markers at the board's turns, newer than anything posted; the nudge."""
    global NUDGES
    now = int(time.time() * 1000)
    rows = [{"key": p["agent"], "value": {"turn": p["turn_end_id"], "pos": None, "at": now, "unread": False,
                                          "updated": now}, "updated": now, "writeID": f"web-fake-{p['agent']}",
             "deleted": False} for p in panes(board) if p.get("agent") in agents]
    merge("read.markers", rows)
    NUDGES += 1
    rev = len(STATE["read.markers"]) + NUDGES
    return f'event: state-changed\ndata: {json.dumps({"namespace": "read.markers", "rev": rev})}\n\n'


def merge(ns, rows):
    held = STATE.setdefault(ns, {})
    for r in rows:
        old = held.get(r["key"])
        if old is None or (r["updated"], r["writeID"]) > (old["updated"], old["writeID"]):
            held[r["key"]] = r


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("port", type=int)
    p.add_argument("--message", default="ok", choices=["ok", "slow", "409", "502", "hold"])
    p.add_argument("--retired")
    p.add_argument("--queued")
    p.add_argument("--notes", action="store_true")
    frame = lambda kind: lambda agents: (kind, agents)
    p.add_argument("--turn", action="append", dest="frames", type=frame("turn"), default=[])
    p.add_argument("--block", action="append", dest="frames", type=frame("block"), default=[])
    p.add_argument("--read", action="append", dest="frames", type=frame("read"), default=[])
    p.add_argument("--share")
    p.add_argument("--turn-at", type=float, default=3.0)
    ARGS = p.parse_args()
    if ARGS.notes:
        merge("notes", json.loads(fixture("notes-web.json"))["rows"][:2])
    ThreadingHTTPServer(("127.0.0.1", ARGS.port), Fake).serve_forever()
