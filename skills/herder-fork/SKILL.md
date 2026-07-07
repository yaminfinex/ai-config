---
name: herder-fork
description: Fork the current claude or codex session into a new herdr pane via the herder. Use when the user says "fork this", "fork right", "fork below", "fork the session", "branch this session", or wants to spawn a forked copy of the active agent next to the current pane. Wraps the `herder fork` CLI — see `herder fork --help`.
---

# herder-fork

Forks the *current* Claude/Codex session into a sibling herdr pane. The original
conversation keeps running where it is; the fork starts as a copy that diverges from this
point on.

Same hard requirements as the underlying `herder fork` (`HERDR_ENV=1` and `herder` on PATH),
same registry, same placement defaults — see `herder fork --help` for the contract. Prefer this
helper when the user says "fork this"; prefer `herder fork <guid>` directly when you already know
the target guid/label. See `tools/herder/README.md` for the implementation map and
`docs/machine-setup.md` for machine activation.

## Session start

```bash
[ "${HERDR_ENV:-}" = "1" ] || { echo "not in a herdr pane — herder-fork needs herdr"; exit 0; }
```

Requires `herder` on PATH.

## Paths

| Case | Path | Result |
|------|------|--------|
| Registered Claude | `herder fork <guid>` | New guid, bus-bound from birth, `provenance.forked_from`, sidecar enrichment. |
| Codex self-fork | `herder spawn --agent codex --extra-arg fork --extra-arg --last` (or explicit id) | Raw `codex fork`; codex exposes no reliable session id to the helper on this machine. |
| Unregistered Claude | `herder spawn --agent claude --extra-arg --resume <id> --extra-arg --fork-session` | Raw Claude fork; bus/provenance adoption requires enrolling first or later manual cleanup. |

Registered Claude means either `$HERDER_GUID` is present in the pane env, or the helper can
correlate the current pane to an hcom row and resolve that row's `session_id` / `hcom_name`
back to a herder registry row. If no registry identity is found, the helper intentionally
keeps the raw Claude fallback rather than minting a fake `forked_from` row.

## Invocation

```bash
herder-fork                        # right split, auto-detect agent + session
herder-fork --right                # explicit right (default)
herder-fork --below                # split down instead
herder-fork --skip-perms           # raw Claude fallback only: --dangerously-skip-permissions
herder-fork --yolo                 # alias for --skip-perms
herder-fork --prompt 'continue with the auth changes only'
herder-fork --agent codex --session-id <uuid>
herder-fork --cwd /path/to/repo    # override auto-resolved cwd
```

Placement is preserved on both paths: registered Claude passes cwd/split/focus through to
the native lifecycle; raw fallbacks pass `--from-pane`, `--cwd`, `--split`, and focus flags
to `herder spawn`.

## Cwd resolution

The helper resolves cwd from the **current pane**, not the workspace checkout:

1. `herdr pane get "$HERDR_PANE_ID"` → `.result.pane.foreground_cwd`
2. `.result.pane.cwd`
3. matching workspace's `worktree.checkout_path`, then `worktree.repo_root`, then workspace `cwd`
4. `$PWD`

This is mandatory for session stores keyed by project directory. A pane can be running in a
different checkout than its original workspace metadata; using the workspace checkout first can
make `claude --resume ... --fork-session` miss the session and exit immediately. Pass
`--cwd PATH` only for deliberate cross-directory forks.

## Safety / pane lifecycle

- This skill never closes panes or culls. Use `herder cull` for cleanup.
- Default is `--no-focus`; the user keeps the current pane focused.
- Native Claude forks inherit `herder fork`'s lifecycle behavior: label uniqueness, launch-failure
  closure rows, hcom bus binding, and provenance are handled by the Go substrate.
- Raw fallback forks are best-effort compatibility. For Claude, enroll first when provenance and
  bus routing matter. For Codex, the raw `codex fork --last` path remains the working self-fork
  path until Codex exposes a reliable current session id here.

## Caveats

- `claude --fork-session` requires a session id on the raw fallback path. If you are not actually
  in a Claude session, pass `--agent codex`, `--session-id`, or enroll the pane first.
- `CLAUDE_EFFORT` is propagated only on the raw Claude fallback, since native `herder fork` owns
  its launch arguments.
- Extra args after `--` are raw-fallback only and land after `--resume <id> --fork-session` or
  after `codex fork <id|--last>`:

  ```bash
  herder-fork -- --model claude-sonnet-4-6
  herder-fork --yolo -- --model claude-sonnet-4-6
  ```
