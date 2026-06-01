---
name: herder-fork
description: Fork the current claude or codex session into a new herdr pane via the herder. Use when the user says "fork this", "fork right", "fork below", "fork the session", "branch this session", or wants to spawn a forked copy of the active agent next to the current pane. Companion to the `herder` skill.
---

# herder-fork

Forks the *current* claude/codex session into a sibling herdr pane via `herder-spawn`. The original conversation keeps running where it is; the fork starts as a copy that diverges from this point on.

Companion to the `herder` skill. Same hard requirement (`HERDR_ENV=1`), same registry, same spawn primitive — different intent. `herder` provisions arbitrary sub-agents; `herder-fork` duplicates the *self* session.

## Session start

```bash
[ "${HERDR_ENV:-}" = "1" ] || { echo "not in a herdr pane — herder-fork needs herdr"; exit 0; }
```

Requires `herder-spawn` (resolved from `$PATH` or the sibling `herder` skill's `scripts/` dir).

## Detection

| Agent | Detected via | Session id source |
|-------|--------------|-------------------|
| claude | `$CLAUDECODE=1` or `$CLAUDE_CODE_SESSION_ID` set | `$CLAUDE_CODE_SESSION_ID` |
| codex  | `$CODEX_HOME` set, or `--agent codex` | `--last` (codex doesn't expose a session-id env var; fall back to most recent) |

Override either with `--agent <name>` and `--session-id <uuid>`.

## Invocation

```bash
herder-fork                        # right split, auto-detect agent + session
herder-fork --right                # explicit right (default)
herder-fork --below                # split down instead
herder-fork --prompt 'continue with the auth changes only'
herder-fork --agent codex --session-id <uuid>
```

Behind the scenes:

- **claude** → `claude --resume <id> --fork-session [--effort $CLAUDE_EFFORT]`
- **codex**  → `codex fork <id>` (or `codex fork --last` if no id)

The fork is spawned by `herder-spawn` with `--from-pane $HERDR_PANE_ID` so it lands in the current pane's workspace. Role label is `fork-claude-<short>` or `fork-codex-<short>` so the registry stays scannable alongside other herder-spawned agents.

## Safety / pane lifecycle

- The forked session id is persisted by claude/codex's own session store. If the pane is closed, the fork can still be resumed later with `claude --resume <new-id>` / `codex resume <new-id>`.
- Default is `--no-focus`; the user keeps the current pane focused.
- This skill never closes panes or culls. Use `herder-cull` for that.

## Caveats

- `claude --fork-session` requires a current session id. If you're not actually in a claude session (e.g. running herder-fork from a bash pane), pass `--agent codex` or `--session-id` explicitly.
- codex has no env-exposed session id today, so the default codex fork picks the most recent session in this cwd. If multiple sessions share the cwd, pass `--session-id` to disambiguate.
- `CLAUDE_EFFORT` is propagated when set, since claude reads it from env at start. Other args (--model, --agent) are not auto-forwarded — pass them via `--`:

  ```bash
  herder-fork -- --model claude-sonnet-4-6
  ```
