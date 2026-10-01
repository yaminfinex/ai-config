#!/usr/bin/env python3
"""A fake herder serve over the recorded fixtures, for harness runs that press cmd-enter: nothing a
scenario sends may reach a real agent. Reads answer from testdata/; POST …/message never delivers.

    testdata/fake_serve.py PORT [--message ok|409|502|hold] [--retired AGENT]

`hold` keeps the POST open (the composer stays "sending"). Every request is logged on stderr.
"""

import argparse
import json
import pathlib
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

DATA = pathlib.Path(__file__).resolve().parent
ARGS = None


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
            while True:
                time.sleep(15)
                self.wfile.write(b"event: ping\ndata: {}\n\n")
                self.wfile.flush()
        elif url.path == "/api/fleet":
            self.reply(200, fixture("fleet.json"))
        elif url.path == "/api/viewer":
            self.reply(200, fixture("viewer.json"))
        elif parts[:2] == ["api", "state"]:
            self.reply(200, fixture(f"state-{parts[2]}.json"))
        elif parts[:2] == ["api", "agents"] and len(parts) == 3:
            path = DATA / "agents" / parts[2] / "detail.json"
            detail = json.loads(path.read_text()) if path.exists() else {"name": parts[2]}
            if parts[2] == ARGS.retired:
                detail["bus_status"] = "retired"
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
        if parts[:2] != ["api", "agents"] or parts[3:] != ["message"]:
            return self.reply(404, {"error": "not found", "detail": self.path})
        self.log_message("message to %s: %s (%s)", parts[2], body, ARGS.message)
        if ARGS.message == "hold":
            time.sleep(600)
        if ARGS.message == "409":
            return self.reply(409, {"error": "sender refused",
                                    "detail": "the fake serve refuses every message"})
        if ARGS.message == "502":
            return self.reply(502, {"error": "substrate unreachable", "detail": "hcom timed out"})
        self.reply(200, {"sent": True, "to": parts[2], "from": "web-fake", "intent": "request"})


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("port", type=int)
    p.add_argument("--message", default="ok", choices=["ok", "409", "502", "hold"])
    p.add_argument("--retired")
    ARGS = p.parse_args()
    ThreadingHTTPServer(("127.0.0.1", ARGS.port), Fake).serve_forever()
