# Herder live fleet view

`herder list` reads a `session.snapshot` directly from the herdr Unix socket,
reads the live hcom roster, and joins rows only by an exact pane ID. A visible
agent pane without a bus row and a bus agent without a visible pane remain
explicit gaps. It also reverse-scans current transcripts for MODEL and CONTEXT.
`herder show <name>` and `herder show --session <id>` print the corresponding
live vitals alongside the agent-store view.

Herder owns no ledger, daemon, or lifecycle authority. A running `herder
serve` keeps an in-memory, never-persisted cache of session vitals (the
observer) and answers the CLI over a local socket; without a serve the CLI
reads transcripts directly, so nothing depends on the serve being up. Spawn, message,
compact, cull, resume, and fork compose through `tools/fleet`, hcom, and herdr.

The self-building launcher at `bin/herder` hashes this module's Go sources and
reuses a checkout-specific last-good binary if a rebuild temporarily fails.
When running Go directly from this module, use `env -u GOROOT go ...`.

## Layout

- `cmd/herder/` — binary entry point.
- `internal/herdrcli/` — herdr socket snapshots and the `herdr status server --json` discovery contract.
- `internal/hcomidentity/` — hcom roster decoding and identity helpers.
- `internal/listcmd/` — exact-coordinate live join and table rendering.
- `internal/claudesession/`, `internal/codexsession/` — per-tool transcript parsing; `ObserveVitals` is each tool's ONE vitals envelope parse.
- `internal/sessionvitals/` — the ONE vitals reader (`ReadDirect`, `Seed`, `Advance`) and the ONE cache-then-direct lookup body (`ReadWith`; `Read` = socket cache for the CLI).
- `internal/observer/` — serve-scoped in-memory session vitals: roster discovery, seed, fsnotify tail, phases, 24 h TTL. Never persisted.
- `internal/herdersock/` — the local socket protocol, client and server (`<state dir>/herder.sock`, one JSON line each way, op `vitals`).
- `internal/servecmd/` — HTTP serve; `observe.go` wires observer, socket and the shared agent-store projection.
- `internal/showcmd/` — one-agent store and live-vitals rendering (`--json` carries `source: cache|direct`).
- `tests/` — hermetic contracts for the surviving surface.

## How a `herder show` call flows

`herder show <name>` resolves the agent's roster row, then calls
`sessionvitals.Read`. Read asks the serve first: `herdersock.Ask` stats
`<state dir>/herder.sock`; if the file is absent there is no serve (the serve
unlinks its socket on every exit) and Read falls straight to the direct
transcript read. If the file exists it dials once; a refused connection is a
stale socket from a dead serve, so the client unlinks it and reads directly. A
live serve gets one JSON line `{"op":"vitals","tool":…,"session":…}` and must
answer within 150 ms; the answer carries the observer's vitals, its
`observed_at` stamp and phase, and show prints `source: cache`. Any miss,
timeout or malformed reply falls to the direct read, `source: direct`. Inside
the serve the socket server hands each request to `observer.Lookup`; the
observer's entries were seeded by the same `sessionvitals` reader the direct
path uses and are kept current by tailing the transcript directory with
fsnotify. So there is one reader with two transports: the socket is a cache in
front of the direct read, never a second calculation. `herder list` does the
same per row. `GET /api/agents/{name}` inside the serve skips the socket and
calls `observer.Lookup` directly through the same `sessionvitals.ReadWith`
body, falling back to the direct read for a session the observer does not
know.

Signposted, not built: more socket ops (board, list), a `vitals` SSE event,
the CLI as a pure client, the observer as a separate daemon process. The
per-connection transcript push (`servecmd/transcriptwatch.go`) still has its
own watcher; `observer/watch.go` says why.

## Gates

From the repository root:

```bash
for f in tools/herder/tests/check-*.sh; do bash "$f"; done
```

`check-live-contract.sh` is the read-only substrate tier. It may inspect hcom
and herdr state, but it never creates, moves, closes, or modifies panes,
workspaces, agents, or the installed binary.

From `tools/herder`:

```bash
env -u GOROOT go clean -testcache
env -u GOROOT go test -count=1 ./...
env -u GOROOT go vet ./...
env -u GOROOT go build ./...
```
