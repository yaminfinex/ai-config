# Agent Status Lines

## Claude

`claude/statusline.sh` is installed through `claude/settings.shared.json` and
renders from Claude's JSON statusline input plus process environment. It never
calls `herder`, `herdr`, `hcom`, or SQLite during render.

The herder segment uses these environment variables when present:

- `HERDR_ENV`, `HERDR_PANE_ID`
- `HERDER_LABEL`, `HERDER_ROLE`
- `HCOM_INSTANCE_NAME` or `HCOM_NAME`

## Statusline Snapshot Contract

Statusline renderers and the herder sidecar share a tiny optional state file
per hcom instance. Renderers must omit unavailable segments when the file is
absent or a key is malformed.

Default path:

```text
$HCOM_DIR/statusline/${HCOM_INSTANCE_NAME:-${HCOM_NAME:-self}}.env
```

Override path:

```text
$HCOM_STATUSLINE_STATE
```

Bus activity keys:

```sh
HCOM_UNREAD=3
HCOM_LAST_TS=1783506400
HCOM_LAST_AGE_S=42
```

`HCOM_LAST_TS` is the preferred last-activity Unix timestamp. Readers with
`EPOCHSECONDS` compute age from it during render without subprocesses, so the
displayed age stays fresh without rewriting the file every second.
`HCOM_LAST_AGE_S` remains a fallback for old files and for shells without
`EPOCHSECONDS`; that fallback is a write-time age and can become unboundedly
stale while `HCOM_UNREAD` stays unchanged.

Claude context keys:

```sh
CTX_PCT=24
CTX_TOKENS=61768
CTX_SIZE=258400
CTX_TS=1783506400
```

`claude/statusline.sh` writes these keys on each render when Claude supplies
context-window metrics. `CTX_PCT` is the rounded percentage used by operators,
`CTX_TOKENS` is the current total input token count, `CTX_SIZE` is the model
context-window size, and `CTX_TS` is the Unix timestamp of the render that wrote
the values. The write is an atomic temp-file plus rename in the snapshot
directory, and it preserves valid `HCOM_*` values already present in the file.
`herder list` reads `$HCOM_DIR/statusline/<hcom-name>.env` from each registry
row's recorded bus directory/name and renders `unknown` when no context
snapshot exists. It marks stale values from `CTX_TS` instead of reporting them
as fresh.

The writer runs from the herder sidecar host loop. On each hcom roster pass it
maintains one atomically replaced file per safe bus instance key under
`$HCOM_DIR/statusline/`. The key is hcom's `base_name` when present, matching
`HCOM_INSTANCE_NAME`; otherwise it falls back to hcom's `name`. It skips writes
when the rendered values are unchanged, and tolerates timestamp drift between
multiple sidecars by not rewriting when `HCOM_UNREAD` is unchanged and the
existing `HCOM_LAST_TS` is within one sidecar tick. Unsafe names are skipped,
and if multiple live rows map to the same safe key in one roster pass, the
writer removes that `<safe-name>.env` once and writes nothing for the key until
the collision clears. Readers then omit the bus segment instead of showing
another agent's data. Best-effort cleanup only removes `<safe-name>.env` files
inside the `statusline/` directory. The writer never writes or deletes the
`HCOM_STATUSLINE_STATE` override path. Sidecar writes preserve valid `CTX_*`
values already present in the file.

## Codex

Codex CLI `0.142.5` exposes native TUI footer/title configuration, not a
Claude-style custom command hook or custom statusline input schema.
`codex/config.shared.toml` therefore manages the native subset:

```toml
[tui]
status_line = ["model-with-reasoning", "context-remaining", "git-branch", "current-dir"]
terminal_title = ["spinner", "project", "git-branch", "model", "status"]
```

This covers model, context remaining, branch, and project/current directory
where Codex supports them. It cannot render a custom herder/hcom segment or read
the bus snapshot until Codex adds a custom footer item or command hook. Since it
cannot publish `CTX_*` through the statusline snapshot today, `herder list`
shows `unknown` for Codex rows unless another supported writer creates the
snapshot in the future.
