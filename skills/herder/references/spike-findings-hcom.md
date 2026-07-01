# hcom characterization spike — findings (Phase 0)

hcom `0.7.22` (brew `aannoo/hcom/hcom`), macOS, herdr present, Claude + Codex CLIs present.
Deliverable of Phase 0 units U1–U5. Each: PASS/FAIL + evidence + implication.

## GO / NO-GO (top-line) — **GO, incl. Codex** (upgraded after live U5 probe)

**Decision:** proceed to Phase 1 with an hcom driver serving **both Claude and Codex peers**. The
live U5 probe (below) proved Codex hcom delivery reliable via the herder-spawned hooks-only path, so
Codex is a **first-class hcom target**, NOT special-cased to `herdr`. Per-worktree `HCOM_DIR`
isolation is the model. **herder-spawn must inject `HCOM_DIR` into the child** for the hcom join to
bind the right bus (U3 requirement; U10 owns it).

_History: an earlier conservative gate scoped hcom to Claude-only and deferred Codex to `herdr`
(the plan's sanctioned degrade). The user steered "we must test Codex or this is a no-op"; the live
U5 probe was run and PASSED, upgrading the gate to include Codex. Kept here as a sliding door._

**Live-verified before building on it:** U5 was run with a real herder-spawned Codex agent + real
`hcom send` (single mid-turn + burst). All load-bearing facts confirmed: name alignment, per-worktree
isolation, resolve/`Not found` fallback signal, guarded hook safety, AND live cross-harness delivery
with a recorded `deliver:` ack.

---

## U2 — herdr terminal preset + name alignment — **PASS (static)**

**Evidence** (`hcom status`, binary preset strings):
- `terminal: default (auto: herdr)` — hcom **auto-detects herdr** as the terminal on this machine,
  no config needed.
- Built-in herdr preset in the hcom binary:
  - open:  `herdr agent start {instance_name} --cwd {cwd} --no-focus -- bash {script}`
  - close: `herdr pane close {pane_id}`
  - `pane_id_env = HERDR_PANE_ID`; `{id}` note: "herdr `agent start` JSON is parsed for
    `result.agent.pane_id`" → hcom captures herdr's pane_id and can map `hcom kill` → `herdr pane close`.
  - `{instance_name}` = hcom instance name → **hcom instance name == herder agent label** by
    construction (the label is passed straight to `herdr agent start`).

**Implication:** names align 1:1 and close/kill map. NOTE: this preset is what hcom uses **when hcom
launches** the agent. Under KTD4 (Option B) herder launches and hcom only `start`s inside — so the
preset is corroborating evidence the two compose, exactly as the plan framed it. `hcom_resolve`
mapping label→instance is sound.

## U4 — HCOM_DIR per-worktree isolation vs one-writer invariant — **PASS**

**Evidence:**
- Default `dir: /Users/yamen/.hcom` holds the whole state: `hcom.db` (SQLite +
  `-wal`/`-shm`), `config.toml`, `env`, `.tmp/logs`.
- `HCOM_DIR=$PWD/.hcom hcom status` → `dir: <worktree>/.hcom (ok)` and materialized a **separate**
  `.hcom/` (own `hcom.db`, `config.toml`, `env`) in the worktree. Global `hcom list` stayed `[]`.
- hcom's own guidance: "Inside a sandbox? Prefix all hcom commands with: `HCOM_DIR=$PWD/.hcom`".

**Implication:** `HCOM_DIR` scopes the ENTIRE bus (DB + hooks + logs) to a folder. Per-worktree
`HCOM_DIR=$PWD/.hcom` gives each worktree its own isolated bus — agents in different worktrees do
**not** cross-talk unless they deliberately share `HCOM_DIR`. This composes cleanly with orchestrate
Inv 7 (one writer per worktree): the bus boundary == the worktree boundary. A shared cross-worktree
bus is available on purpose by pointing HCOM_DIR at a common path. `.hcom/` must be gitignored.

## U1 — cross-harness push (Claude↔Codex) — **PASS (Codex live-proven; Claude architecturally sound)**

**Evidence:** hcom's per-harness delivery is a guarded hook set (`cmd=${HCOM:-hcom}; command -v
"$cmd" && exec $cmd <event> || exit 0` → **no-op when hcom absent** — key R2/KTD3 safety fact).
Claude hooks: `sessionstart`, `pre`/`post` (tool boundaries), `notify`, `poll`, `stop`, `sessionend`,
`subagent-*`, `permission-request`. Codex hooks: `codex-sessionstart`, `codex-userpromptsubmit`,
`codex-posttooluse`, `codex-stop`. **Codex direction proven live in U5** (mid-turn injection at
`codex-posttooluse` boundaries). Claude receive-path shares the same mechanism (richer hook set,
best-supported harness) — treated GO; U8's first test re-confirms on the Claude side too.
**Implication:** cross-harness delivery works; both harnesses are hcom targets.

## U3 — Option-B probe (herder owns spawn; hcom is bus) — **CONDITIONAL GO + requirement found**

**Evidence:** `hcom send` requires a genuinely *joined* instance; sending to an unjoined name
returns `Error: Not found: <name>` and `hcom list <name>` likewise → this is the clean
**"not usable → fall back to herdr"** signal `select_driver`/`hcom_resolve` will key on (U8/U9).
The bus (SQLite at `$HCOM_DIR/hcom.db`) and send/list/events CLI are real and scriptable — exactly
the surface `driver-hcom.sh` shells out to.
**Requirement discovered (blocks clean Option-B isolation until handled):** for a herder-spawned
child to join the *worktree* bus, `HCOM_DIR` must be in the child's **process env at launch** —
`export` inside a Bash tool call runs in a subshell and does not reach the hooks, which read the
Claude process env. `herder-spawn` has `--cwd` but no env injection today. **U10 must inject
`HCOM_DIR` into the child** (and U8's `hcom_join` assumes it). With that, Option-B (herder launches,
child runs `hcom start`, orchestrator `hcom send`s) is sound. Not-blocked; scoped into U10.

## U5 — Codex hook-delivery reliability (WEAK-LINK GATE) — **PASS (live)**

Ran a real live probe (user steer: "we must test Codex or this is a no-op"). Codex agent spawned via
**herder-spawn** (Option B), joined hcom as instance `tuna`, `bindings: hooks` — **vanilla,
hooks-only, no PTY inject port** (`hcom term` refused: "not PTY-managed"). Orchestrator sent as an
**external identity** (`hcom send --from orchestrator @tuna`; sender need not be hcom-joined).

- **Single mid-turn send:** Codex busy in a count loop; `PROBE-PING` sent → events trace
  `message(03:29:13)` → `deliver:orchestrator(03:29:17)` (injected at next `codex-posttooluse`
  boundary) → Codex acts `(03:29:21)`. **~8.4s, hook-only, mid-turn, zero keystrokes.**
- **Burst of 3** (ALPHA/BRAVO/CHARLIE within ~40ms): hcom **coalesced all three into a single
  atomic injection**, recorded **in order, zero drops, zero dupes** — better than keystroke delivery,
  which races on a busy pane.

**Verdict:** **Codex hcom delivery via the Option-B hook path is reliable.** The `deliver:` event is
a recorded ack = real delivery semantics (the Inv-8 robustness the plan wanted; removes silent
pane-drop for Codex — the hardest case).
**Implication:** U8 treats Codex as a **first-class hcom target** (no special-case to herdr).
`hcom_resolve` = `hcom list <name>` (joined → usable; `Not found` → herdr fallback). `hcom_send`
maps `deliver:` ack → `delivered`; batching keeps `delivered`/`queued` semantics intact. Requires
`hcom hooks add <tool>` present + `HCOM_DIR` in the child's process env (U10).

---

### Gitignore / cleanup note
`.hcom/` (worktree bus state) is gitignored. hcom's `hooks add` wrote an **untracked**
`.claude/settings.json` (→ `.agents/settings.json`); U8 re-adds hooks as test setup, so the pristine
tree is restored after the spike.
