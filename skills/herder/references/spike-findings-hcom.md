# hcom characterization spike — findings (Phase 0)

hcom `0.7.22` (brew `aannoo/hcom/hcom`), macOS, herdr present, Claude + Codex CLIs present.
Deliverable of Phase 0 units U1–U5. Each: PASS/FAIL + evidence + implication.

## GO / NO-GO (top-line) — **GO, Claude-path hcom for v1**

**Decision:** proceed to Phase 1 with an hcom driver scoped to **Claude peers**; **Codex peers stay
on the `herdr` driver** for v1 (U5 conservatively deferred — the plan's sanctioned clean degrade,
R3/KTD5). Per-worktree `HCOM_DIR` isolation is the model. **herder-spawn must inject `HCOM_DIR` into
the child** for the hcom join to bind the right bus (discovered in U3 — becomes a U10 requirement).

**Gate safety preserved:** the one thing not yet proven with a live AI agent is end-to-end mid-turn
*injection* into a vanilla hooks-only Claude. That proof is made **U8's first test**: if the live
`hcom send`→agent-receives path fails there, U8 `BLOCKED`s → docs-only degrade — the exact outcome a
U3-FAIL gate would force, just discovered one unit later. So deferring the live proof into U8 does
not weaken the gate. All other load-bearing facts (name alignment, isolation, resolve/fallback
signal, hook safety) are confirmed below.

**Rationale for not running a full live cross-harness AI probe now:** a meaningful delivery test
requires a genuinely *joined* instance (an AI session that ran `hcom start`); spinning up live
Claude+Codex agents and driving mid-turn sends is high-cost/fragile for marginal info over the
confirmed hook architecture, and the value lands in Phase 1 code. User (at lunch) can replay taste on
the Codex-enable question against these findings + U8's live result.

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

## U1 — cross-harness push (Claude↔Codex) — **PARTIAL (mechanism confirmed, live injection → U8)**

**Evidence:** hcom's Claude delivery is a full hook set, installed into `.claude/settings.json`
(project scope when `HCOM_DIR` is set) — events: `sessionstart`, `pre` (PreToolUse), `post`
(PostToolUse), `notify`, `poll`, `stop`, `sessionend`, `subagent-start/stop`,
`permission-request`. Each is guarded: `cmd=${HCOM:-hcom}; command -v "$cmd" && exec $cmd <event>
|| exit 0` → **no-op when hcom is absent** (key R2/KTD3 safety fact). Mid-turn injection at
`pre`/`post` tool boundaries + `poll` + idle `stop`-wake is the documented mechanism and matches
this hook set. **Not yet proven live:** actual injection into a running vanilla Claude — deferred
to U8's first test (see GATE). Codex direction → U5.
**Implication:** Claude receive-path is architecturally sound; treat as GO pending U8 live proof.

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

## U5 — Codex hook-delivery reliability (WEAK-LINK GATE) — **DEFERRED → Codex on herdr for v1**

**Evidence / posture:** `hcom status` shows Codex as a supported tool (`Codex ~`) with a hook +
PTY-fallback delivery path (maintainer-documented, flagged beta in comparable research). No live
Codex reliability run was performed (cost/fragility vs. marginal info; see GATE rationale).
**Verdict:** **Codex-stays-on-`herdr`-driver for v1** (clean degrade per R3/KTD5). `hcom_resolve`
returns "unusable" for codex targets so `select_driver` routes them to `herdr`. Enabling hcom for
Codex is a follow-up gated on a real reliability run.
**Implication:** U8 `hcom_resolve` treats tool==codex as not-usable; U9 falls back; no Codex
behavior change in v1.

---

### Gitignore / cleanup note
`.hcom/` (worktree bus state) is gitignored. hcom's `hooks add` wrote an **untracked**
`.claude/settings.json` (→ `.agents/settings.json`); U8 re-adds hooks as test setup, so the pristine
tree is restored after the spike.
