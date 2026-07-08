# Agent Status Lines

## Claude

`claude/statusline.sh` is installed through `claude/settings.shared.json` and
renders from Claude's JSON statusline input plus process environment. It never
calls `herder`, `herdr`, `hcom`, or SQLite during render.

The herder segment uses these environment variables when present:

- `HERDR_ENV`, `HERDR_PANE_ID`
- `HERDER_LABEL`, `HERDER_ROLE`
- `HCOM_INSTANCE_NAME` or `HCOM_NAME`

## hcom Bus Snapshot Contract

Statusline renderers may read a tiny optional state file and must omit the bus
activity segment when the file is absent.

Default path:

```text
$HCOM_DIR/statusline/${HCOM_INSTANCE_NAME:-${HCOM_NAME:-self}}.env
```

Override path:

```text
$HCOM_STATUSLINE_STATE
```

Current reader keys:

```sh
HCOM_UNREAD=3
HCOM_LAST_AGE_S=42
```

The writer is intentionally not implemented here because the event-driven hcom
or herder sidecar/internals that should maintain this file are outside this
unit. A future writer should update the file atomically and avoid per-render
database or CLI work.

## Codex

Codex CLI `0.142.5` exposes native TUI footer/title configuration, not a
Claude-style custom command hook. `codex/config.shared.toml` therefore manages
the native subset:

```toml
[tui]
status_line = ["model-with-reasoning", "context-remaining", "git-branch", "current-dir"]
terminal_title = ["spinner", "project", "git-branch", "model", "status"]
```

This covers model, context remaining, branch, and project/current directory
where Codex supports them. It cannot render a custom herder/hcom segment or read
the bus snapshot until Codex adds a custom footer item or command hook.
