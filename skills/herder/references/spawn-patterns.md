# Spawn patterns

Concrete recipes for common herder requests. All examples assume `scripts/herder-spawn` is on $PATH (or invoked by absolute path from the skill).

## A. Spawn a Codex review agent in a split

User: "spawn a review agent (codex) to review the current diff"

```bash
herder-spawn \
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

herder-spawn \
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
herder-spawn --role scratch --agent bash --split right --no-focus
```

`HERDER_GUID` and `HERDER_LABEL` are still injected into the shell env so the user can interact with the pane and the herder still owns the registry record.

## D. Cull a spawned agent

```bash
# By short guid (preferred, displayed in herder-list output)
herder-cull --guid a3f2c91d

# By label
herder-cull --label review-a3f2c91d

# By pane id
herder-cull --pane w…-3

# Sweep records whose pane is gone (user closed it manually):
herder-cull --gone
```

Always `--dry-run` first when sweeping in unfamiliar state.

## E. Peek a spawned agent's screen

```bash
herdr pane read <pane_id> --lines 80
herdr agent read <label> --source recent --lines 200
```

Use this before sending a follow-up so you don't interrupt working state.

## F. Send a follow-up to a spawned agent

```bash
# Literal text, no submit
herdr agent send <label> "Quick clarification: focus only on auth.ts changes."

# Submit
herdr pane send-keys <pane_id> Enter
```

For most LLM agents, the user expects submit. For raw shells, you may prefer to leave the text un-submitted.

## Initial-prompt delivery caveats

`herder-spawn` waits up to 10s (override with `--wait-timeout-ms`) for the agent to report `idle` before sending the prompt. If the agent has no herdr integration installed, `wait --status idle` may never resolve and we fall through to the send anyway. If you see prompts landing before the agent prompt is ready, either:

- Install the matching integration (`herdr integration install claude|codex|…`).
- Increase `--wait-timeout-ms`.
- Skip `--prompt` and send manually after the agent is visibly ready.

## Naming convention

`<role>-<short-guid>`, where `short-guid` is the first 8 chars of the GUID. Roles should be short, lowercase, hyphenated: `review`, `impl`, `research`, `qa`, `scratch`. Examples:

- `review-a3f2c91d`
- `impl-7e4a02bb`
- `research-1d9c5f88`

This keeps panes scannable in the herdr sidebar while preserving uniqueness.
