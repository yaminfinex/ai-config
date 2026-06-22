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

After spawning, echo `<label>`, short GUID, and pane id back to the user.

Recipes (worktrees, follow-ups, culling): `references/spawn-patterns.md`.

## Sending to a running peer

```bash
herder-send <guid|short-guid|label|pane_id> "message"
```

Refuses to send into interrupted / modal panes unless `--force`. Verifies the prompt buffer cleared before claiming delivery. Use this instead of hand-rolling `herdr agent send` + `pane send-keys Enter`. Rationale: `references/herder-delta.md` → *Driving peer agents safely*.

**Long briefs to codex go through a file, not the wire.** Codex collapses any paste over ~1k chars into a `[Pasted Content N chars]` blob, and a multi-line brief can trip its "Create a plan?" overlay — both make codex act on only the tail. Keep codex sends **short and single-line**: stage the full brief in a file (e.g. `napkins/<task>-brief.md`, gitignored) and `herder-send` a one-line pointer that tells codex to read the file and plan. Recipe: `references/spawn-patterns.md` → *Send a long brief to codex*.

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
