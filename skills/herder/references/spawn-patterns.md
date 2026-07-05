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
WT_JSON="$(herdr worktree create --branch task/foo --base main --label foo --no-focus --json)"
WT_PATH="$(printf '%s' "$WT_JSON" | jq -r .result.worktree.path)"   # or whatever field herdr returns; verify with `herdr worktree create -h`

herder spawn \
  --role implementer \
  --agent claude \
  --cwd "$WT_PATH" \
  --split down \
  --no-focus \
  --prompt-file /tmp/task-brief.md
```

If the worktree response JSON differs, do `herdr worktree create … --json | jq` interactively first to confirm field names — do not guess.

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
3. Closes the root pane — but only after confirming via `herdr pane get` that it still holds the root `terminal_id` (never the agent's). pane ids compact, so a bare-id close could otherwise hit the agent.
4. Re-resolves the agent's `pane_id` by its durable `terminal_id` (the close renumbers panes in the tab).

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

It preflights state (refuses to send into interrupted / modal panes unless `--force`), writes the text, submits Enter, and verifies the prompt buffer cleared. See `references/herder-delta.md` → *Driving peer agents safely* for the rationale.

For raw shells where you don't want submission, drop to the primitives:

```bash
herdr agent send <label> "echo 'still here'"      # literal text, no Enter
herdr pane send-keys <pane_id> Enter              # submit when ready
```

For a **long brief to a codex peer**, do not send it over the wire — see recipe H below.

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

## H. Send a long brief to codex (stage a file, send a one-line pointer)

Codex collapses any paste over ~1k chars into a `[Pasted Content N chars]` blob, and a multi-line brief can trip its "Create a plan?" overlay — in both cases codex parses only the tail and builds the wrong thing. Never push a long brief to codex over the wire. Stage it and point at it.

> **At spawn time this is automatic.** `herder spawn --agent codex` already stages a long or multi-line `--prompt`/`--prompt-file` to `$HERDER_STATE_DIR/briefs/<guid>.md` and sends only a one-line pointer (reported as `brief: staged to …`). The recipe below is for **mid-session** `herder send` to an already-running codex, where staging is still your responsibility.

```bash
# 1. Write the full brief to a gitignored scratch file (napkins/ is gitignored;
#    use /tmp/… outside a repo).
cat > napkins/impl-brief.md <<'EOF'
<the full multi-line brief, however long>
EOF

# 2. Send a SHORT single-line pointer. Single-line sends submit cleanly — no
#    overlay, no [Pasted Content] blob, no doubling.
herder send <guid|label|pane_id> \
  "Read napkins/impl-brief.md in full, then plan before writing any code."
```

`herder send` handles the codex blob case (it treats a fresh `[Pasted Content]` blob as landed instead of re-pasting), but keeping the wire payload to one short line sidesteps the blob and overlay entirely — strictly better.

If a codex composer ends up polluted (e.g. a doubled paste from an earlier attempt), there is **no key that clears it**: `herdr pane send-keys` accepts only `Enter` / `esc` / `C-c`, and `esc` / `C-c` interrupt the agent rather than clearing the line (`BSpace`, `C-u` are rejected as `invalid_key`). Just submit — codex tolerates a doubled idempotent instruction, or expands a `[Pasted Content]` blob on the first Enter and submits on the second.

This is the same file-staging idea as `--prompt-file` for initial prompts (recipe B): for codex, keep the wire payload to a single short line whether it's an initial prompt or a mid-session brief.

## Initial-prompt delivery caveats

`herder spawn` waits up to 15s (override with `--wait-timeout-ms`) for the agent to report `idle` before sending the prompt. If the agent has no herdr integration installed, `wait --status idle` may never resolve and we fall through to the send anyway. If you see prompts landing before the agent prompt is ready, either:

- Install the matching integration (`herdr integration install claude|codex|…`).
- Increase `--wait-timeout-ms`.
- Skip `--prompt` and send manually after the agent is visibly ready.

## Naming convention

`<role>-<short-guid>`, where `short-guid` is the first 8 chars of the GUID. Roles should be short, lowercase, hyphenated: `review`, `impl`, `research`, `qa`, `scratch`. Examples:

- `review-a3f2c91d`
- `impl-7e4a02bb`
- `research-1d9c5f88`

This keeps panes scannable in the herdr sidebar while preserving uniqueness.
