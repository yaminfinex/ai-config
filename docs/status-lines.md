# Agent Status Lines

## Claude

`claude/statusline.sh` is installed through `claude/settings.shared.json` and
renders from Claude's JSON statusline input plus process environment. It never
calls `herder`, `herdr`, `hcom`, or SQLite during render.

The herder segment uses these environment variables when present:

- `HERDR_ENV`, `HERDR_PANE_ID`
- `HERDER_LABEL`, `HERDER_ROLE`
- `HCOM_INSTANCE_NAME` or `HCOM_NAME`

## Context Warning Hook

Claude runs `$HOME/.claude/hooks/context-nudge.sh` after every user prompt and
successful tool use. The hook asks `herder show --session <id> --json` for the
current roster row's `vitals.context_usage.used_tokens`, with a hard two-second
timeout; it never parses a transcript. The default warning bands are the
ordered, unique, absolute token counts `200000,250000`. A personal setting can
override them:

```json
{
  "env": {
    "AI_CONTEXT_NUDGE_BANDS": "150000,200000"
  }
}
```

The marker for a Claude session is
`${XDG_STATE_HOME:-$HOME/.local/state}/ai-config/context-nudge/claude-<session-id>.bands`.
It contains one warned band per line. A band is warned at most once in a normal
compaction cycle, even if usage later dips below it. Claude's `SessionStart`
event with `source: "compact"` removes that session's marker; other starts do
not. The hook never compacts, blocks, or contacts an orchestrator itself.

The count describes the last completed request known to herder. The current
prompt or tool result may not be reflected yet because the transcript-backed
roster observation lags the current turn. Claude transcripts do not carry a
context-window size, so bands are absolute by design, not percentages. A first
observation above multiple bands produces one combined reminder.

The hook is fail-open: missing or slow herder data, malformed input or output,
invalid preferences, and state errors produce no output and never block work.
Marker persistence and advisory delivery cannot be atomic, so a process failure
between them can lose a reminder. Concurrent hook processes are not serialized,
so at-most-once is a normal-operation rather than transactional guarantee.
Codex context nudges are not managed by ai-config today; this hook is
Claude-only.

## Statusline Snapshot Contract

> **Superseded 2026-08-24 for herder production.** The per-seat sidecar was
> deleted, so it no longer creates or refreshes these snapshots. The contract
> remains as history for existing files; live status comes from hcom and herdr,
> while herder list/observer expose display cache only.

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

Claude reads the override path when present. Its context writer updates that
path only when the file already exists; sidecar-owned creation/removal remains
authoritative so collision-removed snapshots do not get recreated by a render.

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

`claude/statusline.sh` writes these keys into an existing snapshot file on each
render when Claude supplies context-window metrics. `CTX_PCT` is the rounded
percentage used by operators, `CTX_TOKENS` is the current total input token
count, `CTX_SIZE` is the model context-window size, and `CTX_TS` is the Unix
timestamp of the render that wrote the values. The write is an atomic temp-file
plus rename in the snapshot directory, and it preserves valid `HCOM_*` values
already present in the file.
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
writer removes that `<safe-name>.env` on each collided pass and writes nothing
for the key until the collision clears. Readers then omit the bus segment
instead of showing another agent's data. The collision guard is name-keyed only:
a renderer with a stale duplicated `HCOM_INSTANCE_NAME` can still write context
into the shared name's existing file until a future ownership token is added.
Best-effort cleanup only removes `<safe-name>.env` files inside the
`statusline/` directory. The writer never writes or deletes the
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
