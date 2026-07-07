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
- `internal/` - subcommands, registry handling, the hcom bus delivery engine, launch wrappers,
  and sidecars.
- `shims/` - `claude` and `codex` PATH shims that route interactive launches through
  `herder launch`. Print one-shots (`claude -p/--print`) bypass the bus and exec the real
  binary — see "Print one-shot bypass" under Delivery below.
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
completion reports. Notify is bus-native ONLY: a spawner that resolves to no bus name is a hard
error before any pane is created (the keystroke ring went with the herdr delivery transport,
TASK-003).

## Delivery

hcom is THE transport (TASK-003, locked): `herder send` resolves every target form
(guid | short-guid | label | terminal_id | pane_id) through the spawn registry to the agent's
recorded bus name and delivers over the hcom bus, scoped to the row's recorded `hcom_dir` (team
buses cross correctly), then polls for a `deliver:` receipt — ack ⇒ `delivered`, none in the
window ⇒ `queued` (do NOT resend). A target with no bus-bound registry row is refused with exit 2;
keystrokes are never typed. Exit codes and target forms: `herder send --help`. Contract pinned by
`tests/check-send-contract.sh` (bus-only goldens) + `check-hcom-contract.sh` (scoping/addressing).

Three deliberate exceptions ride keystrokes, none reachable as a send transport:

- **Boot-time initial prompt** (`herder spawn --prompt`): typed into the freshly booted pane by
  the spawn-private paste engine (`internal/spawncmd/bootpaste.go`) — at that moment the agent has
  no bus binding yet (hcom name capture happens after delivery; bash agents never get one).
- **Trust-modal auto-accept** at spawn readiness (a single Enter; `--safe` opts out).
- **Steered self-compaction** (`herder compact '<steer>'`, TASK-022): queues a real
  `/compact <steer>` input line into the CALLER'S OWN pane via the same package-private engine.
  Input automation, not delivery — there is no target argument, and identity is proven
  (HERDER_GUID → session id → terminal+cwd corroboration) before anything is typed; unprovable
  identity refuses, as do a guid/session-id mismatch and a row terminal that disagrees with the
  live pane without session-id corroboration (a stale or inherited HERDER_GUID looks exactly
  like drift). Pinned by `tests/check-compact-contract.sh` (goldens + grep gates).

**Print one-shot bypass (TASK-010):** `claude -p/--print ...` hand-run through the shims skips the
bus entirely — hcom hard-codes print mode as a persistent background agent (stdin nulled, stdout to
`~/.hcom/logs`, Stop hook polling the bus), so a routed one-shot would never return its answer.
`herder launch` detects the flag before building hcom args, sets `HCOM_LAUNCH_INFLIGHT=1`, and execs
the PATH-resolved tool; the shim's recursion guard resolves the real binary. `--tag` is ignored on
this path and hcom need not be installed. Claude-only: codex `-p` is `--profile`, and codex
one-shots (`codex exec`) still ride the hcom path. Applies to fresh launches only — `--resume`/
`--fork` stay on hcom.

**Team buses (opt-in ringfence):** the bus is scoped by `HCOM_DIR`, pinned into the child's env at
spawn: `--team <name>` (else `$HERDER_TEAM`, else the global `~/.hcom` bus) →
`HCOM_DIR=$HERDER_TEAMS_ROOT/<name>` (default root `~/.hcom/teams`). The global bus is the normal
operating mode — registry addressing already gives per-agent targeting. Config-dir pin caveat:
`PinConfigDir` (`internal/launchcmd`) pins `CLAUDE_CONFIG_DIR`/`CODEX_HOME`/`GEMINI_CLI_HOME` only
when `HCOM_DIR` is set and ≠ `~/.hcom` (hcom's local-mode condition; pinning on the global bus
moved Claude's JSON state and caused first-run onboarding). First team-bus claude launch per
machine therefore hits one-time onboarding; it persists machine-wide once completed.

**Context ceiling:** an agent nearing its ceiling persists state FIRST (commit WIP + progress
report — compaction loses anything unpersisted), then compacts in place:
`herder compact 'keep: unit, ACs, gate commands, thread; drop tool output'`. Run from the
agent's own tool call, the `/compact` line is queued in its composer and fires at turn end.
The old `herder send "$HERDR_PANE_ID" '/compact …'` recipe died with the keystroke transport;
`herder compact` is its dedicated replacement. If the session is too incoherent to steer, the
fresh-spawn handoff still works: HANDOFF report + successor spawn.

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
under `docs/` here (`herder-delta.md`, `spawn-patterns.md`). Multi-session
run protocols live in the `orchestrate` skill. Machine setup details live in `docs/machine-setup.md`.
