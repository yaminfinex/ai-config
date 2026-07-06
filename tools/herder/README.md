# Herder

Herder is the Go-backed command substrate for driving herdr panes from ai-config. The interface is
`herder <subcommand>` on PATH, exposed by the self-building launcher at `bin/herder`.

The launcher hashes the Go sources, builds into a local cache when needed, and re-execs the cached
binary. It also self-heals common Go environment issues; when running Go directly from this module,
use `env -u GOROOT go ...`.

## Layout

- `cmd/herder/` - binary entry point.
- `internal/` - subcommands, registry handling, hcom/herdr drivers, launch wrappers, and sidecars.
- `shims/` - `claude` and `codex` PATH shims that route interactive launches through
  `herder launch`.
- `tests/` - hermetic contract suites, fixtures, mocks, and goldens.

## Gates

From the repository root:

```bash
for f in tools/herder/tests/check-*.sh; do bash "$f"; done
```

From this directory:

```bash
env -u GOROOT go clean -testcache
env -u GOROOT go test ./...
env -u GOROOT go vet ./...
```

## Activation And Usage

Run `bin/ai-setup` from the ai-config checkout to put `bin/` and `tools/herder/shims/` on PATH via
mise. Restart the shell, then verify with `ai-doctor`.

Usage lives in the `herder` skill. Machine setup details live in `docs/machine-setup.md`.
