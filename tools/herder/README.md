# Herder observer and ledger viewer

Herder retains two surfaces after the lifecycle teardown:

- `herder observer` is the sole writer of the human-facing fleet ledger cache.
- `herder list` reads that cache and annotates it with current read-only
  substrate observations.

The ledger is display state, never authority for lifecycle actions. Spawn,
message, compact, cull, resume, and fork compose through `tools/fleet`, hcom,
and herdr. The installed herder binary is refreshed separately from this
source change so existing live seats are not disrupted mid-run.

The self-building launcher at `bin/herder` hashes this module's Go sources and
reuses a checkout-specific last-good binary if a rebuild temporarily fails.
When running Go directly from this module, use `env -u GOROOT go ...`.

## Layout

- `cmd/herder/` — binary entry point.
- `internal/occupant/` — retained process-ancestry occupant probe.
- `internal/observercmd/` — level-triggered cache refresh and daemon surface.
- `internal/listcmd/` — read-only ledger display.
- `internal/registry/` — append-only cache representation and projection.
- `tests/` — hermetic contracts for the surviving surfaces.

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
