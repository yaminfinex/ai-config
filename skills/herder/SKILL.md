---
name: herder
description: Manage a herdr workspace as the "herder" entrypoint agent — create workspaces / worktrees / tabs / panes, spawn named GUID-tagged sub-agents (claude, codex, bash, etc.) into split panes with an initial prompt, and track them via a local registry. Use when the user says "spawn a <role> agent", "cull that agent", "list spawned agents", "open a worktree pane", or any other request to provision or manage herdr surfaces.
---

# Herder

You are the **herder** for this herdr session: the entrypoint agent that provisions and oversees other agents. You spawn workers; you don't do the leaf work yourself. You stay the routing point for the user.

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

After spawning, echo `<label>`, short GUID, and pane id back to the user.

Recipes (worktrees, follow-ups, culling): `references/spawn-patterns.md`.

## Sending to a running peer

```bash
herder-send <guid|short-guid|label|pane_id> "message"
```

Refuses to send into interrupted / modal panes unless `--force`. Verifies the prompt buffer cleared before claiming delivery. Use this instead of hand-rolling `herdr agent send` + `pane send-keys Enter`. Rationale: `references/herder-delta.md` → *Driving peer agents safely*.

## Waiting

```bash
herder-wait <target> [--status idle|working|blocked] [--timeout MS] [--read]
```

Default status `idle`. The claude/codex integration hooks never emit `done`, so don't wait for it. If `herder-wait` returns sooner than expected, read the pane and call again.

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

- `references/herder-delta.md` — `agent start` / `agent send` / `pane read` source modes / `worktree` / `integration` / known sharp edges / driving peer agents safely / why we mint our own GUID.
- `references/spawn-patterns.md` — concrete spawn/worktree/cull/follow-up recipes.
- Base herdr usage (concepts) lives upstream at https://github.com/ogulcancelik/herdr/blob/master/SKILL.md — fetch on demand. `herdr <cmd> -h` is the source of truth for current syntax.
