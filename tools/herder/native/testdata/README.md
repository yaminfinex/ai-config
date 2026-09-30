# Fixtures

Recorded from the live serve with `record.sh` (read-only GETs, then `scrub.py`), last on 2026-10-01.
`cargo test` reads them; nothing here needs the network.

| File | Endpoint |
|---|---|
| `fleet.json` | `GET /api/fleet` |
| `events.sse` | the first two `/api/events` frames: `hello`, then the `fleet` board |
| `viewer.json` | `GET /api/viewer` |
| `state-spaces.json`, `state-spaces.members.json`, `state-notes.json` | `GET /api/state/<ns>?since=0` |
| `agents/<name>/detail.json` | `GET /api/agents/<name>` (`launch_context.env` removed) |
| `agents/<name>/tail.json` | `GET …/entries?limit=N` (N per agent in `record.sh`) |
| `agents/<name>/before.json` | `GET …/entries?before=<tail from>&limit=…` |

Agents: `mupu` (claude, every kind except `task_notification`), `conductor-line` (claude, with
`task_notification` and `compact_divider`), `grill-confirm-lubo` (codex), and a small slice of `riko`, the
largest transcript on the server (~90 MB). For perf runs `just fetch-big riko` pages the whole transcript
backward into `big/riko.pages.json` (gitignored, a few minutes over the tailnet).

`scrub.py` replaces secret-looking values (`sk-…`, `ghp_…`, `xox…`, `AKIA…`, JWTs, private keys and
`key/token/secret/password = value` shapes) with `[scrubbed]`. It is deliberately broad, so a few
harmless identifiers get hit too.
