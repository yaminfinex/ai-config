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

`herder show <name>` (or `herder show --session <current-session-id>`) adds a
live `vitals` block with model, context used/window/percent, transcript mtime,
and session file. Its JSON output carries the same facts under `vitals`, with
non-fatal transcript read failures in `vitals_error`. `herder list` adds MODEL
and CONTEXT (`82k/258k 32%`) columns. These changing values are reverse-scanned
from current transcripts on demand and are never persisted in the agent store.

## Files

| file | writer | contents |
|---|---|---|
| `events.jsonl` | `Store.Append` only (CLI and serve) | append-only, one JSON object per line |
| `snapshot.json` | nothing today | `{version, events_offset, agents, requests}` — rebuildable, never authoritative |

`launch-edges.jsonl` (the serve's old web-launch record, one directory up)
is imported once on first open when `events.jsonl` is absent: each edge
becomes a `launch-ready` with `by=<web identity>`, `launcher_kind: web` and
an id derived from the edge line (`sha256`, version nibble 8), so a re-import
yields the same ids. The import is atomic: under `agents/init.lock` (a
separate inode from the journal, same 2 s budget) the whole set is built in
a temp file, fsynced, renamed to `events.jsonl`, and the directory fsynced.
A crash mid-import leaves no journal, and the next open imports everything.
Malformed edge lines are skipped with one warning each.

There is no rotation yet; the journal grows until measurement earns a retention policy.

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

`spawn.sh` records requested and ready (or failed) around the existing launch.
When a serve launches it, `FLEET_LAUNCHER` carries the server-derived web
identity and `FLEET_LAUNCHER_KIND=web`. Outside the serve, the fleet wrappers
pass `--by` and `--by-kind=agent` from `hcom list self --json` when the process
is an hcom seat. Requested placement also accepts `worktree_branch` with its
required `repo`.

Register's best-effort `by` fallback precedence is `$HCOM_NAME`, then the seat
environment as
`${HCOM_TAG:+$HCOM_TAG-}$HCOM_INSTANCE_NAME`, then `$USER`, then `unknown`.
The fleet wrappers prefer hcom self because the environment fallback can be
stale on a renamed or resumed seat. The hcom choices are agents; `$USER` is a
user. Empty values are skipped.

The serve mirrors hcom life events (`created`, `ready`, `stopped`, and
`batch_launched`) through the same append API with `by_kind=mirror`. It catches
up the latest 500 life events at startup, then subscribes until the serve
context ends. `hcom_event` preserves the bus id and the event id is derived from
`hcom-life:<id>`, so replay appends nothing. Other life actions are ignored.
Refused life events are audited once and skipped; an unavailable store is retried.

Before append, the mirror resolves `instance`, `by`, and every `instances`
entry through the last roster already polled by the serve: exactly one matching
`base_name` becomes that row's full `name`; zero or multiple matches stay raw.

Mission assignment (the `assign` kind, `fleetview.Row.Mission`) is recorded
but not displayed in `herder list` until the mission model is specced
(owner ruling 2026-09-09); `show` prints it when present.

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

Reader tolerance: a malformed *complete* line is skipped with one warning
and its bytes are left in place; unknown fields on a well-formed line are
accepted (a newer serve and an older CLI share this journal across a
`--watch` re-exec). Validation is strict only at the write boundary.

## Idempotency

`id` is the idempotency key. Replaying an id (a retry with `--id`) with the
same payload returns the receipt of the existing line, fsyncs it (the first
writer may have died before its fsync) and appends nothing; the CLI prints
`replayed=true`. The same id with a *different* payload (after the `by`
default; `at` is excluded because the id already fixes the time) is an
error, not a replay: "id X already has a different payload", register exit
2, no line. A corrected fact needs a new id. The check runs under the lock so
a concurrent retry cannot double-append.

Cost: the id check scans the journal from byte 0 on every append, O(n) in
journal length (about 40 ms at 20k lines). Rare life events use this same path;
an id set or offset cache waits for a measurement showing the scan matters.

## Snapshot

`snapshot.json` is the projection at `events_offset`. One-shot `list` and
`show` never rewrite it, and nothing refreshes it today. Refreshing belongs to
the long-lived serve (follow-up TASK-125). The snapshot is trusted only when its
offset lands on a record boundary of the current file; otherwise full replay.
Replay from a snapshot plus tail equals full replay byte-for-byte (tested).

## Identity key: (name, incarnation)

hcom reuses names. Each name holds an ordered list of incarnations; `culled`,
`launch-failed` and `mirror.stopped` close the current one, and the next
event that is not an end-of-life attachment (`cull-requested`, `session.*`
end) opens a new one. The fold with the roster picks the first incarnation
not closed before hcom's `created_at` (decoded in `hcomidentity`; `created`
accepted as an alias), then rejects it when its first event predates
`created_at` unless its open session is the roster's session: a raw `hcom
kill`, a crash or a missed wrapper records no close, and roster creation is
the newer evidence. With no roster time it takes the latest. A new
incarnation inherits nothing: no manager, no assignment, no launcher.

`mirror.batch_launched` fans out to its `instances` (name optional).
`session.observed` without a name is a pane-only session kept under
`unnamed_sessions` keyed tool/session. `session.ended`/`superseded` close
the session the event names, never "whichever is current". Reparents are
ordered by event time, so an older one arriving late never overwrites a
newer manager. A registered `launch-ready` supersedes weaker `mirror.*`
attribution for launcher/launcher_kind (and the default manager, unless a
reparent was explicit).

On full replay, an old base-name mirror record is folded into a registered
full-name twin only when exactly one tagged launch incarnation contains the
base record's `mirror.ready`; base-only and ambiguous records remain separate.
At display time, `list` and `show` may read a remaining mirror-only base record
for a full roster name, but only when exactly one roster row owns that
`base_name`; this fallback never re-keys or writes the store.

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

The web serve appends `annotate` events for display-name writes with the
server-derived sender in `by` and `by_kind: web`. Those writes use the same
store validation and fold as the register CLI. After trimming, an annotation
title is at most 80 runes and contains no control characters; the CLI exits 2
when either rule is violated.

## One validator

`agentstore.SpecFor(kind)` is the single per-kind contract (required, one-of,
optional fields, keyed by CLI flag name). `Store.Append` validates every
event through it, so an in-process writer (the serve) cannot record
what the CLI would refuse: unsupported tool, missing launch tag, two
placement targets, `culled` without a pane, `annotate` with a manager,
invalid `by_kind`, negative `steer_chars`. The register CLI only parses
flags into an event.

## Exit codes (`herder register`)

0 appended or identical replay · 2 usage or invalid event (including a
replayed id with a different payload) · 3 store unavailable (unwritable dir,
lock timeout, failed first-open import). `list`/`show` exit 0 with rows
shown as `unregistered` and one stderr warning when the store is unavailable.
