# Agent store (`$HERDER_STATE_DIR/agents/`)

The agent store holds herder's *augmenting* data about fleet agents: launch
provenance, assignment, annotation, the mutable manager pointer, and session
history. It masters none of hcom's or herdr's facts. `internal/agentstore` is
the only reader and writer; the CLI surfaces are `herder register`, `herder
show` and the LAUNCHER / MANAGER / MISSION columns of `herder list`.

## What herder never does with this store

No lifecycle verb consults it before acting. `register` records what a
caller reports; it performs nothing, asks hcom/herdr nothing, and a register
failure never fails the lifecycle action (the wrapper logs one line and
continues). Reads never gate anything either: `list` and `show` print with
degraded columns when the store cannot be written or read.

## Files

| file | writer | contents |
|---|---|---|
| `events.jsonl` | `Store.Append` only (CLI now, serve later) | append-only, one JSON object per line |
| `snapshot.json` | whoever replays (`list`/`show` now, serve later) | `{version, events_offset, agents, requests}` — rebuildable, never authoritative |

`launch-edges.jsonl` (the serve's old web-launch record, one directory up)
is imported once on first open when `events.jsonl` is absent: each edge
becomes a `launch-ready` with `by=<web identity>`, `launcher_kind: web` and
an id derived from the edge line (`sha256`, version nibble 8), so a re-import
yields the same ids and idempotency absorbs it.

## Event schema

Common fields: `id` (UUID; v7 generated, `--id` accepted for retries),
`at` (RFC3339 UTC), `kind`, `by`, `by_kind` (`agent|web|user|unknown|serve|
observer|mirror`), `name` (absent on `launch-requested`), `request`.

| kind | extra fields |
|---|---|
| `launch-requested` | tool, model, effort, tag, placement{workspace\|pane\|split_from}, prompt_ref, launcher_kind |
| `launch-ready` | batch, pane, cwd, session (plus tool/model/effort/tag/placement when no request precedes it) |
| `launch-failed` | reason, batch, pane |
| `cull-requested`, `culled` | pane, close (`managed\|label-fallback`) |
| `resume` | pane, from_session |
| `fork` | from_name, pane |
| `compact-requested` | steer_chars |
| `assign` | mission, brief, thread, task |
| `annotate` | title, note |
| `reparent` | manager |
| `mirror.created/ready/stopped/batch_launched` | hcom_event, reason, batch, instances, parent_name, is_hcom_launched |
| `session.observed/ended/superseded` | session, tool, path, reason |

## Locking and atomicity

Append: open `O_WRONLY|O_APPEND|O_CREATE`, `flock(LOCK_EX)` on that file
(polled non-blocking, bounded 2 s, then "store unavailable" exit 3), repair a
torn tail, check the id, one `write(2)` of the whole line (refused above
16 KiB), `fsync`, unlock. Two `herder register` processes therefore never
interleave; the test spawns 50 real processes × 20 events and expects 1000
well-formed lines.

Torn tail: a final line without its newline never had a receipt (the writer
died mid-write or the disk filled). The next writer, under the lock,
truncates to the last complete newline, prints one stderr line and proceeds.
Readers ignore a trailing partial line without repairing it.

## Idempotency

`id` is the idempotency key. Replaying an id (a retry with `--id`) returns
the receipt of the existing line and appends nothing; the CLI prints
`replayed=true`. The check runs under the lock so a concurrent retry cannot
double-append.

## Snapshot

`snapshot.json` is the projection at `events_offset`. A reader loads it,
replays the tail, and rewrites it (temp + rename). The snapshot is trusted
only when its offset lands on a record boundary of the current file;
otherwise full replay. Replay from a snapshot plus tail equals full replay
byte-for-byte (tested). A snapshot write failure is silent for reads.

## Identity key: (name, incarnation)

hcom reuses names. Each name holds an ordered list of incarnations; `culled`,
`launch-failed` and `mirror.stopped` close the current one, and the next
event that is not an end-of-life attachment (`cull-requested`, `session.*`
end) opens a new one. The fold with the roster picks the first incarnation
not closed before hcom's `created_at` (decoded in `hcomidentity`; `created`
accepted as an alias); with no roster time it takes the latest. A new
incarnation inherits nothing: no manager, no assignment, no launcher.

Binding: when a register event claimed session S and the roster says S′,
the view records `binding: conflict {claimed: S, roster: S′}` and keeps S′
current. The store is never re-keyed.

## Launcher vs manager

`launcher` (from the `by` of `launch-requested`/`launch-ready`, `fork`, or a
`mirror.*`) is immutable provenance: who launched me. `manager` is the
mutable hierarchy pointer: who manages me. It defaults to the launcher and
changes only through `reparent {name, manager, by}` (`herder register
reparent --name X --manager Y`). Hierarchy views hang off `manager`;
`launcher` stays for audit.

## Exit codes (`herder register`)

0 appended or identical replay · 2 usage · 3 store unavailable (unwritable
dir, lock timeout, failed first-open import). `list`/`show` exit 0 with rows
shown as `unregistered` and one stderr warning when the store is unavailable.
