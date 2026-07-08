# Spawn patterns

Concrete recipes for common herder requests. All examples assume `herder` is on `$PATH`.

## Permissions & trust (read first)

`herder spawn` spawns agents in **autonomous mode by default** — it injects `--dangerously-skip-permissions` (claude) / `--dangerously-bypass-approvals-and-sandbox` (codex) and auto-accepts the first-run directory-trust modal (reported as `trust-accepted`). You therefore do **not** pass permission flags in the examples below. To override:

- `--safe` — spawn a default ask-mode agent and refuse to auto-accept the trust modal (it's surfaced for you to accept manually). Use when spawning into a directory you don't fully trust.
- `--extra-arg <flag>` — pass your own permission flag; any recognised one suppresses the injected default.

Check the summary line / `--json` `delivery_result` to confirm the initial prompt was actually delivered (`delivered`), not dropped (`not_landed`) or blocked (`blocked_trust_modal`).

## A. Spawn a Codex review agent in a split

User: "spawn a review agent (codex) to review the current diff"

```bash
herder spawn \
  --role review \
  --agent codex \
  --split right \
  --no-focus \
  --prompt 'You are a code reviewer. Run `git diff` against the merge-base with main and produce a structured review.'
```

Report back to user:

> Spawned `review-a3f2c91d` (codex) in pane `w…-3`. GUID `a3f2c91d-…`. Initial prompt sent.

## B. Spawn a Claude implementer in a new worktree

User: "spawn a claude to implement <task> on a branch off main"

```bash
herder spawn \
  --role implementer \
  --agent claude \
  --worktree task/foo \
  --base main \
  --no-focus \
  --prompt-file /tmp/task-brief.md
```

`--worktree` does the whole dance in one verified step: it drives `herdr worktree create`
(resolving the source repo from your cwd — works from inside a linked worktree too), spawns the
agent into the new workspace's checkout, and closes the workspace's seed shell pane under the
same identity guard as `--new-tab`, so the agent ends up the sole pane of its own workspace.
The summary and `--json` (`worktree` block) report the created coordinates — `workspace_id`,
checkout path, branch — keep the `workspace_id` if you plan to `herdr worktree remove` later.
If the worktree gets created but the spawn then fails, nothing is auto-removed; the failure
report names the workspace and the exact remove command.

`herdr worktree remove --workspace <id>` only applies while the workspace is open. Culling the
workspace's last agent auto-closes it, leaving the git worktree + branch on disk — clean those
up with `git worktree remove <checkout_path> && git branch -D <branch>` (the spawn summary
prints this breadcrumb with the real coordinates; it's in your spawn transcript).

Do **not** hand-roll `herdr worktree create --json` + `jq` + `herder spawn --cwd` for this —
that two-CLI dance predates `--worktree` and leaves a spare seed shell pane in the new workspace.

## C. Spawn a bare terminal pane (no agent)

```bash
herder spawn --role scratch --agent bash --split right --no-focus
```

`HERDER_GUID` and `HERDER_LABEL` are still injected into the shell env so the user can interact with the pane and the herder still owns the registry record.

## C2. Give each agent its own tab (no spare shell) — `--new-tab`

User: "spawn these in separate tabs" / "one tab per agent".

```bash
herder spawn --role impl --agent claude --new-tab --no-focus \
  --prompt 'Implement <task> …'
```

Do **not** hand-roll `herdr tab create` then `herder spawn --tab <id>`: `tab create` seeds the tab with a default (root) shell pane, and `herdr agent start --tab` *always* opens a new pane (even with no `--split`), so you end up with **agent + spare shell** in every tab. `--new-tab` does the whole dance and closes the seed shell:

1. `herdr tab create --label <agent-label> [--workspace …] [--cwd …]` → captures the root pane's `pane_id` + `terminal_id`.
2. `herdr agent start --tab <new-tab>` → the agent lands as a second pane.
3. Closes the root pane — but only after confirming via `herdr pane get` that it still holds the root `terminal_id` (never the agent's). Within a run, a stale pane id hits nothing; after restart, ids reshuffle, so a bare-id close must still be guarded.
4. Re-resolves the agent's `pane_id` by its move-stable `terminal_id`.

The summary prints `tab: <id> (new, root shell closed; agent is sole pane)`; `--json` adds `new_tab` / `root_pane_closed`. If the close is skipped (identity check fails), the summary warns `root shell NOT closed` so you can clean it up by hand. Culling the agent later closes its last pane, which auto-closes the tab — no `tab close` call needed.

## D. Cull a spawned agent

```bash
# By short guid (preferred, displayed in herder list output)
herder cull --guid a3f2c91d

# By label
herder cull --label review-a3f2c91d

# By pane id
herder cull --pane w…-3

# Sweep records whose pane is gone (user closed it manually):
herder cull --gone
```

Always `--dry-run` first when sweeping in unfamiliar state.

## E. Peek a spawned agent's screen

```bash
herdr pane read <pane_id> --lines 80
herdr agent read <label> --source recent --lines 200
```

Use this before sending a follow-up so you don't interrupt working state.

## F. Send a follow-up to a spawned agent

For mid-session messages to a running peer, prefer the wrapper:

```bash
herder send <guid|short-guid|label|pane_id> "Quick clarification: focus only on auth.ts changes."
```

Delivery is bus-only (TASK-003): every target form resolves through the registry to the peer's
recorded hcom name and the message rides the bus with a delivery receipt (`verify=delivered`,
or `queued` when the peer is mid-turn — do NOT resend). A target with no bus-bound registry row
(bash panes, sidecar rows) is refused with exit 2; keystrokes are never typed.

For raw shells (which `herder send` refuses), drive the pane with the primitives:

```bash
herdr agent send <label> "echo 'still here'"      # literal text, no Enter
herdr pane send-keys <pane_id> Enter              # submit when ready
```

## G. Spawn off a specific parent pane (not the focused one)

When the herder is running in one workspace but the user wants the new agent to join a *different* pane's workspace (e.g. spawn a reviewer next to a long-running implementer), use `--from-pane` to bind to that parent's workspace:

```bash
herder spawn \
  --role review \
  --agent codex \
  --from-pane w652d833fd5cdcd-1 \
  --split right \
  --no-focus \
  --prompt 'Review the diff.'
```

`--from-pane` and `--workspace` are mutually exclusive. `herder spawn` resolves `--from-pane` to its `workspace_id` and validates it against the live workspace list before calling `agent start`, so a stale id fails fast with a clear error instead of the upstream `agent_placement_not_found` JSON.

## H. Long briefs to codex (everything rides the bus now)

Codex's composer collapses any *paste* over ~1k chars into a `[Pasted Content N chars]` blob, and a multi-line paste can trip its "Create a plan?" overlay — in both cases codex parses only the tail. These are KEYSTROKE pathologies, and since TASK-032 no codex-bound prompt travels by keystroke: `herder spawn --prompt`/`--prompt-file` delivers the FULL brief (any length, multiline) as a verified hcom message once the child binds its bus name, and mid-session `herder send` always rode the bus. No brief-file staging, no one-line pointer — those existed only to dodge the paste pathologies. A big file pointer is still often kinder to the peer's context than a wall of text, but that is a context choice, not a transport constraint.

If a composer ends up polluted anyway (e.g. a human pasted into the pane), **unsubmitted composer text starves incoming bus delivery** — on both families, nothing injects until it is submitted or cleared (silent: no receipt, no error). For stray or garbage text, clear the composer with the herdr-native combo string: `herdr pane send-keys <pane_id> ctrl+u`; queued bus messages inject at the next boundary. Use `Enter` only when the visible text is a legitimate message that should submit. `ctrl+u` and `backspace` are herdr-native key names; tmux-style names such as `C-u`, `Ctrl-u`, `^U`, `BSpace`, and capital-`Escape` are still rejected as `invalid_key`.

## Initial-prompt delivery caveats

For bus-capable agents (claude/codex/gemini), `herder spawn` waits up to 60s (`HERDER_SPAWN_BIND_MS`) for the child to bind its bus name, then sends the prompt as a verified hcom message and polls up to 20s (`HERDER_SPAWN_VERIFY_MS`) for the delivery receipt. `verify: delivered` = receipt seen; `verify: queued` = sent, injects when the agent is deliverable — do NOT resend either way. On `bind_timeout` nothing was sent; deliver manually once `herder list` shows the bus name. bash agents keep the typed-into-the-pane path and the `--wait-timeout-ms` boot ready-wait (default 15s).

## Naming convention

`<role>-<short-guid>`, where `short-guid` is the first 8 chars of the GUID. Roles should be short, lowercase, hyphenated: `review`, `impl`, `research`, `qa`, `scratch`. Examples:

- `review-a3f2c91d`
- `impl-7e4a02bb`
- `research-1d9c5f88`

This keeps panes scannable in the herdr sidebar while preserving uniqueness.
