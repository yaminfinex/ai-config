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
for f in tools/herder/tests/check-*.sh; do bash "$f"; done
```

The suites neutralize inherited `HERDER_BIN` / `AI_CONFIG_ROOT` themselves (each pins
`AI_CONFIG_ROOT` to its own checkout and ignores the spawn-exported binary override), so
they are safe to run bare from herder-spawned or worktree sessions. `env -u HERDER_BIN
-u AI_CONFIG_ROOT bash "$f"` still works and is harmless belt-and-braces on checkouts
that predate the sweep.

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
completion reports. `--notify-to` additionally accepts the target's bus name directly: an active
registry row's `hcom_name` matches, and a name the registry doesn't know is accepted if it is
live on the bus the child will join (team-scoped — a global-bus peer for a `--team` child still
refuses, since the child couldn't reach it anyway). Notify is bus-native ONLY: a spawner that
resolves to no bus name is a hard error before any pane is created (the keystroke ring went with
the herdr delivery transport, TASK-003). Pane/terminal notify resolution shares `herder send`'s
reused-pane discipline (TASK-035): a lone active row resolves as before, but when a coordinate
matches several active rows the single bus-live one wins, and an ambiguous coordinate (0 or >1
live) is a warn-and-SKIP — notify is best-effort at spawn time (TASK-017 warn-never-block), so the
worker still spawns rather than the report routing to a guessed session or the spawn hard-failing.

`--worktree BRANCH [--base REF]` is the one-step worktree mode: spawn drives
`herdr worktree create` itself (resolving the source repo from the spawner's cwd, which works
from inside a linked worktree), spawns into the resulting workspace's checkout, and closes the
workspace's seed shell pane under the same identity guard as `--new-tab`. The summary and
`--json` (`worktree` block) surface the created coordinates — `workspace_id`, checkout path,
branch — so an orchestrator can reuse or `herdr worktree remove` the workspace later without
re-querying. If the worktree is created but the spawn then fails, nothing is auto-removed: the
failure report names the workspace and the exact remove command. Worktree/workspace lifecycle
stays herdr-owned; herder only wraps it.

Cleanup has two phases: `herdr worktree remove --workspace <id>` works only while the workspace
is still open. Culling the workspace's last agent auto-closes the workspace (herdr behavior), and
the git worktree + branch stay on disk — from there cleanup is
`git worktree remove <checkout_path> && git branch -D <branch>`. The spawn summary prints this
post-cull breadcrumb with the real coordinates so it survives in the spawn transcript.

## Delivery

hcom is THE transport (TASK-003, locked): `herder send` resolves every target form
(guid | short-guid | label | terminal_id | pane_id) through the spawn registry to the agent's
recorded bus name and delivers over the hcom bus, scoped to the row's recorded `hcom_dir` (team
buses cross correctly), then polls for a `deliver:` receipt — ack ⇒ `delivered`, none in the
window ⇒ `queued` (do NOT resend). A target with no bus-bound registry row is refused with exit 2;
keystrokes are never typed. Exit codes and target forms: `herder send --help`. Contract pinned by
`tests/check-send-contract.sh` (bus-only goldens) + `check-hcom-contract.sh` (scoping/addressing).

Pane/terminal ids are positional and reused across sessions, so one coordinate can match several
active rows (a reused pane accumulates a stale manual-enroll identity per prior session, TASK-035).
A lone candidate resolves as before (bus-less and not-yet-joined rows keep their existing
refuse/queue outcomes); when >1 active row shares the coordinate, resolution delivers to the single
row currently JOINED on the bus and REFUSES (exit 2) with the candidate list on ambiguity (0 or >1
bus-live) rather than guessing — bus liveness is a tiebreaker, never a new gate. `herder enroll`
also retires (closes) prior active rows for a pane on re-enroll, so a reused pane stops carrying a
dead session's forever-`working` row. Pinned by `tests/check-send-resolution.sh` and the
`reenroll_reused_pane` enroll golden.

**Initial prompts ride the bus too (TASK-032).** `herder spawn --prompt` for a bus-capable agent
(claude/codex/gemini) waits for the child to BIND its bus name — positively observable (sidecar
registry enrichment, or the hcom roster correlated by frozen launch pane_id) and
early in boot, well before the TUI is interactive — then sends the FULL prompt (multiline included)
as a verified hcom message and reports the receipt. Verify vocabulary: `delivered` (receipt seen),
`queued` (sent, no receipt in the window — it injects the moment the agent is deliverable; do NOT
resend), `send_failed`/`not_joined` (nothing delivered — a retry via `herder send` is safe),
`bind_timeout` (nothing went on the wire — resend once `herder list` shows the bus name). On
`bind_timeout`/`ready_match_timeout` the summary prints the EXACT verbatim resend command
(`herder send <label> <shell-quoted prompt>`, notify appendix and all) and `--json` carries it as
`resend_command`, so recovery is one paste rather than a retype (TASK-036).
The prompt gate trusts CHILD-SPECIFIC bind signals only (this guid's sidecar enrichment, or the
frozen-launch-pane roster match) — a pre-existing same-tag+cwd bus agent never satisfies it, so a
stale roster match waits out to `bind_timeout` instead of misdelivering the prompt to the old
session. The post-write registry ROW enrichment shares that discipline (TASK-033): it records a bus
name only from the same child-specific signals and never from a tag+cwd guess, so a stale match
leaves the row's name EMPTY for the sidecar to fill from the child's own pane later — a later
`herder send <guid>` can never resolve to the old session. Knobs: `HERDER_SPAWN_BIND_MS` (bind wait,
default 60000) and `HERDER_SPAWN_VERIFY_MS`
(receipt window, default 20000). Family asymmetry (TASK-036): claude/bash publish
`launch_context.pane_id`, so the frozen-launch-pane roster match correlates them in a second or
two; **codex omits `pane_id`** (its hcom `launch_context` carries only `process_id`), so BOTH
pane-id paths — this spawn's roster match and the sidecar's `findRowForPane` — miss it, and it is
reached only via the sidecar's async tag+cwd registry enrichment. That enrichment lands AFTER the
bind window and, under multi-agent load, can lag past any window (measured: a codex joined the bus
~1s after launch yet was still uncorrelated 4+ minutes later), so a codex `bind_timeout` is
EXPECTED, not a fault — the row still enriches eventually and the printed `resend_command` then
delivers. Widening the window is deliberately NOT the fix (the failure is correlation, not boot
speed); the clean fix is upstream — hcom publishing `pane_id` for codex like it does for claude
(TASK-028/029). A slash-command prompt arrives as message TEXT, not a typed
slash command. hcom wakes an idle agent with an EMPTY composer instantly — even a fresh,
never-prompted session; a message sent mid-boot is held until the session can take it (probed
live: send fired 107ms after bind, mid-boot, delivered whole at TUI readiness 2s later).

**The one delivery blocker: unsubmitted composer text.** On BOTH families, text sitting
unsubmitted in the composer starves incoming bus delivery indefinitely and SILENTLY — no receipt,
no error (probed live; it was the root cause of the wave-4 reviewer stranding, TASK-031). Remedy:
read the pane (`herder wait <guid> --read`); if text sits on the input line, submit or clear it —
`herdr pane send-keys <pane> Enter` — and queued messages then inject at the next boundary.
Retiring the boot-paste from bus-capable spawns removed the machinery that used to CREATE that
state; a human draft left in a composer can still do it.

Two deliberate exceptions ride keystrokes, neither reachable as a send transport:

- **Trust-modal auto-accept** during spawn's bind/ready wait (a single Enter; `--safe` opts out).
  The modal blocks boot itself — pre-bind — so both wait paths clear it.
- **Steered self-compaction** (`herder compact '<steer>'`, TASK-022): queues a real
  `/compact <steer>` input line into the CALLER'S OWN pane via the package-private paste engine
  (`internal/spawncmd/bootpaste.go` — its other remaining user is `spawn --prompt` for BASH
  agents, which never get a bus binding). Input automation, not delivery — there is no target
  argument, and identity is proven (HERDER_GUID → session id → terminal+cwd corroboration) before
  anything is typed; unprovable identity refuses, as do a guid/session-id mismatch and a row
  terminal that disagrees with the live pane without session-id corroboration (a stale or
  inherited HERDER_GUID looks exactly like drift). The TASK-024 evidence gating (composer-payload
  check immediately before Enter; cleared composer degrades to not_delivered, never delivered) is
  a locked floor. Pinned by `tests/check-compact-contract.sh` (goldens + grep gates).

  **compact-then-continue** (`herder compact '<steer>' --then '<continuation>'`, TASK-034,
  claude-only): `/compact` normally ends the turn and STOPS. `--then` turns it into
  compact-then-continue. It is NOT a second paste — a plain queued line jumps the `/compact`
  queue and is consumed pre-compaction (claude injects plain messages at a mid-turn tool
  boundary; slash commands hold until turn end — both proven live, task-034 comments). Instead,
  once the `/compact` paste VERIFIES (the TASK-024 floor gates arming — an unverified paste arms
  nothing, so a continuation never fires into an uncompacted session), `herder compact` forks a
  detached, `setsid`-isolated sender (`herder compact-then`, an internal subcommand not in the
  command table). That sender waits for the caller's turn to END, then delivers the continuation
  over the bus through the same receipt-verified engine `herder send` uses (`send.DeliverBus`).
  Turn end is **proven, never assumed from a delay** (a fixed grace window would let a stale
  status read inject mid-turn — experiment #1 over the bus): it fires only on an observed
  `active`→`listening` transition, or — if it armed after the turn already ended — on an hcom
  event-history `listening` record newer than the arm-time watermark. That event-history proof is
  gated on a *trusted* watermark: the arm-time snapshot distinguishes a genuinely empty history
  from an hcom failure (retried a few times), and an unestablished snapshot DISABLES the
  event-history proof rather than trusting a `0` that would accept a pre-arm record (fail-open) —
  the observed transition then remains the only path. If neither proof materializes before
  `--then-timeout` it **fails closed** and drops the continuation loudly (a re-sendable dropped
  message beats a silent mid-turn injection). The target is the caller's OWN
  bus name, captured from the proven self row at compact time and never re-resolved from a pane id
  (task-034 experiment #2 misresolved a reused pane to a stale row). Delivery treats `queued` as
  success (hcom queue-until-deliverable injects it at the next turn — never resent) and retries a
  transient `not_joined`/`send_failed` with a settling backoff over the REMAINING timeout budget.
  Bounded by `--then-timeout` (default 15m; timeout gives up with a loud log line + manual-send
  remedy, never a zombie); one line per phase lands in
  `<herder-state-dir>/compact-then/compact-then-<short>-<pid>.log`. Codex is refused (its
  compaction semantics differ). Covered by `tests/check-compact-contract.sh`
  (armed/aborted/sent/armed-late/timeout goldens + `mock-hcom-then`) and
  `internal/spawncmd/compactthen_test.go` (proof (a)/(b), the naked-listening poison case,
  fail-closed timeout, budget-based retry).

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
operating mode — registry addressing already gives per-agent targeting. Config-dir pin:
`PinConfigDir` (`internal/launchcmd`) pins `CLAUDE_CONFIG_DIR`/`CODEX_HOME`/`GEMINI_CLI_HOME` only
when `HCOM_DIR` is set and ≠ `~/.hcom` (hcom's local-mode condition; pinning on the global bus
would move Claude's JSON state for no reason). Pinning `CLAUDE_CONFIG_DIR=~/.claude` re-roots
claude's top-level config from `~/.claude.json` to `~/.claude/.claude.json`, so the pin also seeds
that file by copying `~/.claude.json` when the target is missing (never overwritten; any failure
silently falls back to fresh state). An onboarded machine therefore skips claude's one-time
onboarding on team buses; only a never-onboarded machine (no `~/.claude.json`) sees it once, and it
persists machine-wide after that.

Without the seed, claude treats a pinned launch as a fresh install and prints an alarming triple to
stderr — `Claude configuration file not found at: ~/.claude/.claude.json` / `A backup file exists
at: ~/.claude/backups/.claude.json.backup.<ts>` / `You can manually restore it by running: cp …` —
which lands in hcom's headless launch logs (`$HCOM_DIR/.tmp/logs/background_*.log`). The emitter is
claude itself (hcom performs no config swap), it is cosmetic (no data is lost, the session works),
and it can still appear when the seed source is absent or unreadable (TASK-011).

**Context ceiling:** an agent nearing its ceiling persists state FIRST (commit WIP + progress
report — compaction loses anything unpersisted), then compacts in place:
`herder compact 'keep: unit, ACs, gate commands, thread; drop tool output'`. Run from the
agent's own tool call, the `/compact` line is queued in its composer and fires at turn end.
The old `herder send "$HERDR_PANE_ID" '/compact …'` recipe died with the keystroke transport;
`herder compact` is its dedicated replacement. To keep going unattended past the ceiling, add
`--then '<continuation>'` (claude-only) — after compaction the detached sender delivers the
continuation to the agent's own bus, so compact-then-STOP becomes compact-then-continue without
a human nudging it back. If the session is too incoherent to steer, the fresh-spawn handoff
still works: HANDOFF report + successor spawn.

## Session Bootstrap

Sessions that route through the shims get a herder-native rewrite of hcom's session bootstrap:

- **claude** — the hook path rewrites hcom's sessionstart additionalContext, reinstating hcom's
  SUBAGENTS block (Task-subagent recipe, `subagent_timeout`) plus herder doctrine. The rewrite is
  degrade-safe: any parse or extract failure emits hcom's original output byte-faithfully.
- **codex** — fresh launches get a `[HERDER SESSION ADDENDUM]` (supersede preamble + the shared
  AGENTS doctrine + a codex-shaped SUBAGENTS block, which fans sub-work out via `herder spawn`
  since codex has no Task tool) threaded as user-level `-c developer_instructions=`; hcom's own
  bootstrap merges first and is superseded by instruction, not removed. On codex **resume/fork**
  hcom strips user developer_instructions (the launch seam cannot deliver there — TASK-014), so
  `herder resume`/`herder fork` re-deliver the addendum **post-boot** (TASK-017): they wait for
  the new session to bind its bus name in the registry (sidecar enrichment, bounded by
  `HERDER_ADDENDUM_SETTLE_MS`, default 60s), then send a resume-worded variant as a verified bus
  message. Delivery is dedup-free (a repeat is a harmless no-op by wording) and never blocks: on
  bind timeout or send failure the command warns with the manual `herder send` remedy and the
  resume/fork still succeeds. The codex `fork --self` fallback is covered too (TASK-027): it rides
  `herder spawn`, reads the child guid back from spawn's `--json` record, and re-delivers the
  addendum over the bus the same way — so fallback-forked codex sessions get the doctrine, not
  hcom's bare stock bootstrap.

The claude and codex doctrine blocks (launch and resume variants) share their doctrine sections
as single constants with byte-identity drift guards.

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
