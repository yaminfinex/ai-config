---
name: herder
description: Mechanics for driving herdr surfaces — create workspaces / worktrees / tabs / panes, spawn named GUID-tagged agents (claude, codex, bash, etc.) into panes or tabs with verified prompt delivery, message and wait on running peers, track them via a local registry, and cull them safely. Use when the user says "spawn a <role> agent", "cull that agent", "list spawned agents", "open a worktree pane", or any other request to provision or manage herdr surfaces. Plumbing only — multi-session run protocols (topologies, playbooks, verification) live in the `orchestrate` skill, which builds on this one.
---

# Herder

Mechanics for provisioning and driving agents on herdr surfaces: spawn named GUID-tagged sub-agents into panes/tabs/worktrees, deliver prompts with verification, wait on status, and cull safely. This skill is plumbing only — *which* agents to spawn, who owns handoffs, and how a multi-session run is structured (topologies, playbook/run-log protocol, verification gates) belongs to the `orchestrate` skill; this file is the substrate it runs on.

## Session start

```bash
[ "${HERDR_ENV:-}" = "1" ] || { echo "not in a herdr pane — stop"; exit 0; }
echo "self pane: $HERDR_PANE_ID"
herdr workspace list
herdr agent list
```

Record `$HERDR_PANE_ID`. Never close it, never cull yourself.

## Scripts (in `scripts/`)

| Script | Purpose |
|--------|---------|
| `herder-spawn` | Mint GUID, `herdr agent start` a child, register it, deliver an initial prompt. |
| `herder-send` | Mid-session message to an already-spawned peer, with state preflight + delivery verification. |
| `herder-wait` | Block until a target agent reaches a status. |
| `herder-list` | Reconciled view of registry vs `herdr agent list`. |
| `herder-cull` | Close a pane and mark registry row closed, with `terminal_id` identity check. |

Each script's `--help` is the source of truth for flags. The herder *uses* these; it does not reimplement them.

The `herder-*` scripts are symlinked into `bin/` (on PATH via the managed shell block), so they're callable bare from any pane — **including freshly spawned ones**, which is what makes notify-back work. A spawned agent that never loaded this skill still gets `$HERDER_SEND` (the absolute path to `herder-send`) exported into its shell by `herder-spawn`, so it can ring a peer even if PATH is misconfigured. This closes the gap where agents couldn't find `herder-send` and fell back to raw `herdr agent send` — which writes the text **without** an Enter, so the ring silently never submits.

## Spawning

```bash
herder-spawn --role review --agent codex --split right --no-focus \
  --prompt 'Review the current branch diff vs main and produce a structured report.'
```

Defaults: `--no-focus`, `--split right` for review/research/QA, `--split down` for implementers or long log output. To target a specific parent workspace, use `--from-pane <pane_id>` (resolves to its workspace_id); to target an explicit workspace use `--workspace`. Both are validated against the live workspace list — stale ids fail fast.

**Tab-per-agent: use `--new-tab`, never hand-roll `tab create` + `agent start`.** When the user wants each agent in its own tab, pass `--new-tab`. The naive path leaves a **spare shell** in every tab: `herdr tab create` seeds the tab with a default (root) shell pane, and `herdr agent start --tab` *always* opens a new pane (even without `--split`), so the root shell is left behind. `--new-tab` creates the tab, spawns the agent into it, then closes the root shell — identity-checked by `terminal_id` so it never closes the agent — and re-resolves the agent's `pane_id` after the close compacts ids. The tab is labelled with the agent's label and ends with the agent as its sole pane. Culling the agent later closes its last pane, which auto-closes the tab (no `tab close` needed — that respects the workspace/tab-lifecycle rule below). `--new-tab` and `--tab <id>` are mutually exclusive.

**Permissions are autonomous by default.** `herder-spawn` injects `--dangerously-skip-permissions` (claude) / `--dangerously-bypass-approvals-and-sandbox` (codex) so spawned agents don't stall on tool-approval prompts you can't see in their pane. This is needed because `exec claude` bypasses your shell alias (where skip-permissions usually lives). Pass `--safe` for a default ask-mode agent, or pass your own permission flag via `--extra-arg` (any recognised one suppresses the default). The summary line shows which flag was applied.

**First-run directory-trust modals are handled.** Both claude ("Is this a project you created or one you trust?") and codex ("Do you trust the contents of this directory?") show a trust modal on first run in an untrusted dir — every fresh worktree counts, and the tool-permission flags above do **not** dismiss it. The modal sits at `status=idle` and its selector arrow spoofs the input sigil, so a naive send pastes the prompt *into* the modal and stray characters silently confirm trust. `herder-spawn` detects it and, in autonomous mode, accepts it deliberately (reported as `trust-accepted`). Under `--safe` it refuses and surfaces it instead — you accept it in the pane, then `herder-send` the prompt.

**Initial-prompt delivery is verified, not fire-and-forget.** After the agent settles (output stable, modals cleared), `herder-spawn` delegates the send to `herder-send`, which confirms the text landed (re-pasting if dropped) and submitted. A prompt that can't be confirmed is reported `prompt: NOT confirmed` / `delivery_result` (in `--json`) rather than silently lost — read the pane before assuming it landed.

**Notify-back (`--notify`).** Pass `--notify` so the spawned agent rings *you* when its unit is done, instead of you polling it. `herder-spawn` resolves *your own* pane to its durable `terminal_id` (via `herdr pane get` — `HERDR_PANE_ID` is the `p_<n>` alias form) and exports it as `HERDER_NOTIFY_TO` (or `--notify-to <target>` to point elsewhere) alongside `HERDER_SEND`, then appends a concrete ring command to the prompt — so the worker runs exactly what it was handed, no PATH or skill-load needed, and the ring follows you across pane-id compaction instead of firing into a recycled pane. The ring goes through the same `herder-send` intent (and thus the active delivery driver — hcom when you're a joined instance, herdr otherwise); the worker doesn't pick a transport. The run-log block stays the record; the ring is only a doorbell (`orchestrate` skill, invariant 9). Ring via `$HERDER_SEND`, not a hand-rolled `herdr agent send` — that raw keystroke call writes the text with no Enter so the ring never submits (a herdr-driver artifact the `herder-send` path handles for you).

After spawning, echo `<label>`, short GUID, and pane id back to the user.

Recipes (worktrees, follow-ups, culling): `references/spawn-patterns.md`.

## Sending to a running peer

```bash
herder-send <guid|short-guid|label|terminal_id|pane_id> "message"
```

Refuses to send into interrupted / modal panes unless `--force`. Verifies the prompt buffer cleared before claiming delivery. Use this instead of hand-rolling a transport call. Delivery runs through the **active delivery driver** (see *Delivery drivers* below) — the command expresses intent; the driver (herdr keystrokes, or hcom hooks when the target is a joined hcom instance) does the mechanics. Rationale: `references/herder-delta.md` → *Driving peer agents safely*.

**Sending to a BUSY target reports `verify=queued`, not failure.** If the target is mid-turn when you send (the common case for ringing a working orchestrator), it can't process the message now — it's queued to run after the current turn. `herder-send` detects this (the target was `working` before the send) and reports `verify=queued` with **exit 0** — that is success. So **do not resend on a `queued` (or `not_delivered`) doorbell** — the run-log is the record; resending just piles duplicates into the target's queue. This `queued`-vs-`delivered` contract is driver-agnostic — under the hcom driver the queued case is a message accepted to the bus but not yet injected at a hook boundary; under herdr it is text left on the input line. (herdr-driver detail: the queued text renders where the sigil heuristic can't tell it from an unsubmitted buffer, so herdr deliberately skips the extra-Enter recovery here — on a busy agent each extra Enter stacks another duplicate.)

**Targets resolve by `terminal_id`, not the stored pane number — so sends don't drift** *(herdr-driver resolution)*. A guid/short-guid/label is looked up in the registry, then re-resolved to the agent's *current* pane via its durable `terminal_id` (herdr compacts/reassigns `pane_id`s as panes close, so the spawn-time pane in the registry goes stale and would mis-send to whoever sits there now). A bare `terminal_id` (`term_*`) resolves the same drift-proof way without a registry record — this is the handle `herder-spawn --notify` injects for the orchestrator ring, so the notify-back doorbell follows the orchestrator across pane-id compaction instead of firing into a recycled pane. A raw `pane_id` argument is used verbatim. If the target's terminal isn't live anywhere (agent gone/culled) `herder-send` **refuses** (exit 2) rather than firing into a recycled pane — pass an explicit live `pane_id` to override. Use `herder-send --dry-run <target>` to print where a target resolves (and whether it has drifted) without sending. `herder-wait` and `herder-list` resolve the same way. (The hcom driver instead resolves a label to a joined instance via `hcom list <label>` and has no pane-drift failure mode — the label *is* the address.) Background: `references/herder-delta.md` → *Known sharp edges* (pane-id compaction).

**Long briefs to codex go through a file, not the wire** *(herdr-driver sharp edge)*. Codex collapses any paste over ~1k chars into a `[Pasted Content N chars]` blob (which then needs a fragile, codex-version-specific double-Enter to submit), a multi-line brief trips its "Create a plan?" overlay, and leading characters clip during boot — all three make codex act on only the tail. A short single-line pointer dodges all three at once, so it is the *durable* fix, not patching the Enter dance. **`herder-spawn` now does this automatically for codex**: a long or multi-line initial prompt (incl. anything made multi-line by the `--notify` appendix) is staged to `$HERDER_STATE_DIR/briefs/<guid>.md` and only a one-line `Read <file> …, then plan` pointer is sent (reported as `brief: staged to …` / `brief_file` in `--json`). Claude has none of these pathologies and always gets the inline prompt. For **mid-session** `herder-send` to codex the same discipline is still manual: stage the brief in a file and send a one-line pointer yourself. This paste-collapse dance is a keystroke-transport artifact — the hcom driver delivers a brief as one bus message with a recorded `deliver:` ack, so it does not apply when a codex peer is a joined hcom target. Recipe: `references/spawn-patterns.md` → *Send a long brief to codex*.

## Delivery drivers

`herder-send` (and the `--notify` ring) express **delivery intent** — "get this message to that agent" — without naming a transport. A pluggable **delivery driver** does the mechanics, selected at runtime:

- **`herdr` — the always-present keystroke fallback.** Writes text into the target's pane and submits it, verifying delivery by sigil heuristic + status. Never requires anything beyond herdr, so a machine with no other transport behaves exactly as it always has. All the resolution/queued/paste-collapse sharp edges above are *herdr-driver* behavior.
- **`hcom` — a bus driver, auto-selected when the target is a joined + usable hcom instance.** Delivers over hcom's hooks + SQLite bus instead of keystrokes: a message becomes a bus event injected at the peer's next hook boundary, with a recorded `deliver:` ack (real delivery semantics, no silent pane-drop). Both Claude and Codex are first-class hcom targets (Codex proven live: mid-turn injection, bursts coalesced/ordered/no-loss). Selection is capability-detected — `auto` picks hcom only when `hcom list <label>` shows the target joined, else falls back to herdr.

**`HERDER_BUS` override** (env; values `auto` | `herdr` | `hcom`): `auto` (default) capability-detects as above; `herdr` forces keystrokes regardless of hcom; `hcom` forces the bus (for testing — errors if the target isn't joined). Selection is transport-neutral by design: **no command or flag ever names a transport** — you never pass `--hcom`.

**Transport-neutral join on spawn (`--bus auto|off`).** `herder-spawn --bus auto` (the default) attaches the child to the active bus at launch: it injects `HCOM_DIR=<worktree>/.hcom` (per-worktree bus isolation — the bus boundary == the worktree boundary) and pins the child's hcom instance name to its herder **label** via `HCOM_INSTANCE_NAME` (a bare `hcom start` would auto-assign a random name that `herder-send <label>` could never resolve), then registers the join. Join is **non-fatal** — if it fails or hcom is absent, the agent is still fully reachable via herdr (hard fallback). `--bus off` skips it entirely. `--bus` never takes a transport name (`--bus hcom` is a usage error) — the driver, not the flag, picks the transport.

Full contract, selection logic, and the guide for adding a third driver: `references/delivery-drivers.md`.

## Waiting

```bash
herder-wait <target> [--status idle|working|blocked] [--timeout MS] [--read]
```

Default status `idle`. The claude/codex integration hooks never emit `done`, so don't wait for it. If `herder-wait` returns sooner than expected, read the pane and call again.

Prefer being *rung* over blocking here: a spawned agent that finishes can `herder-send` its orchestrator a one-line doorbell, so the orchestrator idles and wakes on the message instead of burning a turn in `herder-wait`. The `orchestrate` skill owns that protocol (invariant 9); `herder-wait` is then the **backstop** for a dropped ring — a busy orchestrator only queues a send and one at a modal refuses it (`herder-send` exit 2) — not the primary signal. Keep backstop waits bounded so an incoming ring isn't blocked behind a long `herder-wait` loop.

## Culling

```bash
herder-cull --guid <short>      # or --label / --pane
herder-cull --gone [--dry-run]  # records whose terminal_id is no longer live
```

`herder-cull` verifies `terminal_id` before closing — herdr `pane_id`s can compact and reassign, so a stale id may point to someone else's work. Refuses on mismatch; `--force` bypasses. Confirm before culling unless the user gave explicit consent for *this* cull.

## Safety rules

- Never close `$HERDR_PANE_ID` (your own pane).
- Never close panes outside the registry without explicit user confirmation.
- **Never call `herdr workspace close` or `herdr tab close`.** Workspace/tab lifecycle is the user's. Closing the last tab implicitly closes the workspace with no `api.request.start` log line — no clean post-mortem.
- **Never send `esc` to a running peer agent.** It's the only input-shaped key `pane send-keys` accepts, but it doubles as **interrupt** for codex/claude. Use `herder-send` instead of hand-rolling.
- Never `herdr session stop` / `session delete` without explicit confirmation.
- Default `--no-focus` so the user keeps their context.
- `herdr pane read` / `agent read` before sending follow-ups; `herdr agent send` writes literal text without Enter.
- When unsure about a herdr flag, run `herdr <cmd> -h` or `--json` interactively — do not guess.

## References

- `orchestrate` skill — multi-session run protocols built on these mechanics (sequential phases, relay, fan-out, adversarial structures, state-file contracts).
- `references/herder-delta.md` — `agent start` / `agent send` / `pane read` source modes / `worktree` / `integration` / known sharp edges / driving peer agents safely / why we mint our own GUID.
- `references/spawn-patterns.md` — concrete spawn/worktree/cull/follow-up recipes.
- Base herdr usage (concepts) lives upstream at https://github.com/ogulcancelik/herdr/blob/master/SKILL.md — fetch on demand. `herdr <cmd> -h` is the source of truth for current syntax.
