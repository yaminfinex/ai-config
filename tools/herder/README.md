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
  `herder launch`. Print one-shots (`claude -p/--print`) bypass the bus and exec the real
  binary — see the print-bypass note in `docs/delivery-drivers.md`.
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

## Spawn Environment

`herder spawn` shapes the child pane's environment deliberately; three behaviors are worth
knowing when working across checkouts and worktrees:

- **Shims come from the SPAWNING checkout.** Spawn prepends `<spawning checkout>/tools/herder/shims`
  to an hcom-capable child's PATH, so spawning from a worktree injects *that worktree's* shims, not
  main's. This is by design — the shim rewrites the hcom bootstrap with the code you are actually
  running — and it is safe: shims carry a `herder-path-shim` marker, recognize sibling copies from
  other checkouts by content, and never exec each other into a loop.
- **mise ordering is re-pinned.** rc-file `mise activate` is prompt-hook driven and stays inert in
  a spawned pane's `-lic` wrapper (stale `__MISE_*` state, no prompt), which can leave `/usr/bin`
  ahead of mise's toolchains — e.g. the OS go shadowing the pinned one. The login-shell wrapper
  therefore pins `${MISE_DATA_DIR:-~/.local/share/mise}/shims` to the front of the child's PATH;
  shims re-resolve per-directory at call time, so this is position-proof. No mise → no-op.
  (`--no-login-shell` skips this fix; it needs runtime shell expansion.)
- **Checkout-scoped env is re-pointed.** A child spawned `--cwd` into a *different* ai-config
  checkout (typically a worktree) gets `AI_CONFIG_ROOT` and `HERDER_BIN` re-pointed at that
  checkout — `bin/herder` and `lib/common.sh` let the inherited env var beat their own location, so
  without this the child silently builds and tests the spawner's tree. The spawn-time launch itself
  still rides the spawner's `bin/herder` (the proven-buildable tree). Outside any checkout, the
  inherited values are left untouched.

`--notify` resolves the spawner's bus name from the registry by guid *and* by pane/terminal
coordinates, so enrolled sessions (no `$HERDER_GUID` in their environment) get bus-native
completion reports; the keystroke ring remains the fallback for genuinely bus-less spawners.

## Activation And Usage

Run `bin/ai-setup` from the ai-config checkout to put `bin/` and `tools/herder/shims/` on PATH via
mise. Restart the shell, then verify with `ai-doctor`.

Usage lives in `herder --help` (and each subcommand's `--help`); low-level notes and recipes are
under `docs/` here (`herder-delta.md`, `spawn-patterns.md`, `delivery-drivers.md`). Multi-session
run protocols live in the `orchestrate` skill. Machine setup details live in `docs/machine-setup.md`.
