# Herder

Herder is the Go-backed command substrate for driving herdr panes from ai-config. The interface is
`herder <subcommand>` on PATH, exposed by the self-building launcher at `bin/herder`.

The launcher hashes the Go sources (locale-pinned), reuses a per-hash cached binary when one
exists (checking `$XDG_CACHE_HOME/herder`, `~/.cache/herder`, and a UID-scoped shared tmp cache),
and rebuilds only on a miss. Builds pick a Go toolchain that satisfies `go.mod` — PATH `go` if its
version is new enough, else mise-installed toolchains probed directly — and pin
`GOTOOLCHAIN=local`, so a build never stalls on a toolchain download; no satisfying toolchain is a
fast, explicit error. Stale cached binaries are pruned only after a successful build, only by age,
so parallel checkouts never wipe each other's builds. It also self-heals common Go environment
issues; when running Go directly from this module, use `env -u GOROOT go ...`.

## Layout

- `cmd/herder/` - binary entry point.
- `internal/` - subcommands, registry handling, hcom/herdr drivers, launch wrappers, and sidecars.
- `shims/` - `claude` and `codex` PATH shims that route interactive launches through
  `herder launch`.
- `tests/` - hermetic contract suites, fixtures, mocks, and goldens.

## Gates

From the repository root:

```bash
for f in tools/herder/tests/check-*.sh; do env -u HERDER_BIN -u AI_CONFIG_ROOT bash "$f"; done
```

The `env -u` matters in herder-spawned or worktree sessions: inherited `HERDER_BIN` /
`AI_CONFIG_ROOT` beat the scripts' own locations and silently point the suites at another
checkout's tree (the suites will neutralize these themselves under TASK-019; until then, unset
them at the call site).

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

## Session Bootstrap

Sessions that route through the shims get a herder-native rewrite of hcom's session bootstrap:

- **claude** — the hook path rewrites hcom's sessionstart additionalContext, reinstating hcom's
  SUBAGENTS block (Task-subagent recipe, `subagent_timeout`) plus herder doctrine. The rewrite is
  degrade-safe: any parse or extract failure emits hcom's original output byte-faithfully.
- **codex** — fresh launches get a `[HERDER SESSION ADDENDUM]` (supersede preamble + the shared
  AGENTS doctrine + a codex-shaped SUBAGENTS block, which fans sub-work out via `herder spawn`
  since codex has no Task tool) threaded as user-level `-c developer_instructions=`; hcom's own
  bootstrap merges first and is superseded by instruction, not removed. Known gap: codex
  **resume/fork** strips user developer_instructions, so those sessions carry only hcom's stock
  bootstrap until TASK-017 lands.

The claude and codex doctrine blocks are a shared constant with a byte-identity drift guard.

## Activation And Usage

Run `bin/ai-setup` from the ai-config checkout to put `bin/` and `tools/herder/shims/` on PATH via
mise. Restart the shell, then verify with `ai-doctor`. This is a machine-wide takeover: once the
shims are on PATH, *every* interactive `claude`/`codex` launch in a mise-activated shell — hand-
launched ones included, not just herder-spawned panes — routes through `herder launch` and gets
the herder-native bootstrap. `HCOM=/abs/path` bypasses the hcom PATH shim when you need stock
behavior; non-mise contexts (GUI editors, launchd) simply never see the shims.

Usage lives in `herder --help` (and each subcommand's `--help`); low-level notes and recipes are
under `docs/` here (`herder-delta.md`, `spawn-patterns.md`, `delivery-drivers.md`). Multi-session
run protocols live in the `orchestrate` skill. Machine setup details live in `docs/machine-setup.md`.
