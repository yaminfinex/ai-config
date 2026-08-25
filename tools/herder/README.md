# Herder live fleet view

Herder has one command: `herder list`. It reads a `session.snapshot` directly
from the herdr Unix socket, reads the live hcom roster, and joins rows only by
an exact pane ID. A visible agent pane without a bus row and a bus agent without
a visible pane remain explicit gaps.

Herder owns no ledger, cache, daemon, or lifecycle authority. Spawn, message,
compact, cull, resume, and fork compose through `tools/fleet`, hcom, and herdr.

The self-building launcher at `bin/herder` hashes this module's Go sources and
reuses a checkout-specific last-good binary if a rebuild temporarily fails.
When running Go directly from this module, use `env -u GOROOT go ...`.

## Layout

- `cmd/herder/` — binary entry point.
- `internal/herdrcli/` — herdr socket snapshot and response decoding.
- `internal/hcomidentity/` — hcom roster decoding and identity helpers.
- `internal/listcmd/` — exact-coordinate live join and table rendering.
- `tests/` — hermetic contracts for the surviving surface.

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
