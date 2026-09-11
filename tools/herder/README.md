# Herder live fleet view

`herder list` reads a `session.snapshot` directly from the herdr Unix socket,
reads the live hcom roster, and joins rows only by an exact pane ID. A visible
agent pane without a bus row and a bus agent without a visible pane remain
explicit gaps. MODEL and CONTEXT come from a running serve's in-memory cache
over its local socket when one answers, else from a direct reverse scan of the
current transcript (cache-then-direct, one reader).
`herder show <name>` and `herder show --session <id>` print the corresponding
live vitals alongside the agent-store view.

Herder owns no ledger, daemon, or lifecycle authority. A running `herder
serve` keeps an in-memory, never-persisted cache of session vitals (the
observer) and answers the CLI over a local socket; without a serve the CLI
reads transcripts directly, so nothing depends on the serve being up. Spawn, message,
compact, cull, resume, and fork compose through `tools/fleet`, hcom, and herdr.
The web board reads that observer in-process for per-row used-token counts and
never scans transcripts on the board path.

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
- `internal/servecmd/` — HTTP serve; `observe.go` wires the observer and the socket; `projection.go` owns the shared agent-store projection (its refresh triggers and the broker).
- `internal/showcmd/` — one-agent store and live-vitals rendering (`--json` carries `source: cache|direct`).
- `web/src/features/sidebar/` — the fleet rail's three tree builders (placement, supervision, groups) and their pure sidecars (`sidebarView.ts` expansion, `renameModel.ts`, `groupDropModel.ts` drag-to-group and open-as-space plans).
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
`observed_at` stamp, and show prints `source: cache`. Any miss,
timeout or malformed reply falls to the direct read, `source: direct`. Inside
the serve the socket server hands each request to `observer.Lookup`; the
observer's entries were seeded by the same `sessionvitals` reader the direct
path uses and are kept current by tailing the transcript directory with
fsnotify. So there is one reader with two transports: the socket is a cache in
front of the direct read, never a second calculation. `herder list` does the
same per row, sequentially: with no serve each eligible row costs one failed
stat; with a hung serve each row can wait up to 150 ms before its direct
read, so N eligible rows can cost N × 150 ms. `GET /api/agents/{name}` inside the serve skips the socket and
calls `observer.Lookup` directly through the same `sessionvitals.ReadWith`
body, falling back to the direct read for a session the observer does not
know.

Future directions for the socket and the observer are listed once, in
`internal/herdersock/protocol.go`; the observer's own two signposts sit at
`servecmd/observe.go` (`startObserver`: one roster poll for the whole serve)
and `observer/watch.go` (why two watchers exist).

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
