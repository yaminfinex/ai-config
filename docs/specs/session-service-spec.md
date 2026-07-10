# Session Service Spec — `sesh`

STATUS: **DRAFT — awaiting ratification.** Distilled from the ratified Q18–Q20 rulings
(boundaries v2 §1A/§3), the session-shipping prior-art memo, and the live /proc-correlation
validation run on this node (2026-07-08). The three launch micro-decisions were settled by
the owner 2026-07-09 (§10); task-cutting is unblocked.

Related ground truth:
- `docs/design/2026-07-09-sessions-missions-boundaries-v2.md` — §1A (shape), §3 (identity)
- `docs/design/2026-07-09-session-shipping-prior-art.md` — mechanisms adopted, with citations
- `docs/design/2026-07-08-sessions-missions-boundaries.md` §6b Q18–Q20 — the ruling trail

---

## 1. Purpose & scope

The session service answers one question for a team: **"what has everyone been working
on?"** — by mirroring every harness session transcript (Claude Code, Codex CLI) from every
node to one central, durable, browsable store.

It is the **visibility component** of the three-component boundary (sessions / missions /
herder) and sits at the bottom: it depends on nothing, and nothing about missions, herder,
or hcom appears anywhere in it. Other components may read from it (herder does enrichment
joins against the store); it never reads from them.

In scope: a per-node shipper, a central store (byte mirror + index), and one team web
surface. Out of scope (recorded non-goals, §9): search, node-side parsing, node-side
policy, live relay, per-session ACLs, OTel.

## 2. Ubiquitous language

- **sesh** — the service's name, covering all three parts: the shipper binary, the store,
  and the surface. Code home: `tools/sesh` in this repo for now; expected to move to its
  own repo later — the wire API (§8) is deliberately the *only* contract between shipper
  and store, so the move is a relocation, not a redesign.
- **Session file** — a harness-owned JSONL transcript on disk. Claude Code:
  `~/.claude/projects/<project-slug>/<session-uuid>.jsonl`. Codex CLI:
  `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl`. The formats are
  upstream-internal and version-unstable; the service treats their **bytes** as the
  contract, not their schema.
- **Session** — the logical unit identified by `(tool, logical_session_id)`. The store derives
  that id from parsed content and overlap evidence, falling back to the wire `session_id`
  claim when parsing cannot do better. One session may span multiple files with overlapping
  content; the logical session, not a wire claim or file, is what users see.
- **Shipper** — the per-node, per-OS-user agent that tails session files and ships raw
  byte ranges plus facts to the store. Dumb by doctrine: no parsing, no policy.
- **Facts** — identity observations attached to an ingest: OS user, hostname, and
  `SESSION_OWNER` where visible from the shipper, plus tailnet identity stamped by the store. Facts are
  observations; every interpretation of them happens off-node.
- **Store** — the central service: byte-faithful **mirror** (raw bytes as shipped, the
  durable archive) + parse-on-ingest **index** (per-message rows for rendering) + the
  ingest and read APIs.
- **Surface** — the one team-facing page: people-first recency (person → nodes → sessions),
  with read-only transcript drill-down.
- **Cursor** — per-(session-file) shipper state: file identity + last-ACKed byte offset.
  Lives in the node's **cursor registry**, which also remembers observed SESSION_OWNER
  correlations.
- **File identity** — session UUID (from filename) plus a content fingerprint (hash of the
  first N KB once the file is large enough). Never path, never inode, never device.
- **Fingerprint** — the content hash component of file identity; detects
  same-name-recreated files.
- **`SESSION_OWNER`** — the single cross-surface env var naming the human a work tree is
  operating for. Declared at a work-tree root by whoever provisions it (a human, herder,
  or tooling); inherited through spawns; **read** by the shipper, never set by it.
- **Grant** — the tailnet ACL scope that gates both ingest and read. Access is
  grant-scoped, not whole-tailnet.

## 3. Domain model

### 3.1 Identity spine

```
(tool, logical_session_id)              ← store-derived session key
  └─ wire session claims (1..n)         ← fallback identity when parsing cannot unify
       └─ session files (1..n)           ← identity = uuid + fingerprint, not path
            └─ shipped byte ranges       ← mirror rows, idempotent by (file identity, offset)
                 └─ parsed messages      ← composite-key dedup; empty UUIDs never dedup
```

A session's transcript as rendered is the **union of its files' parsed messages**. Rows with
a message UUID dedupe by `(tool, logical_session_id, entry_type, message_uuid)`; rows without
a UUID remain distinct. Claude Code verifiably violates one-file-per-session (resume
can create a new file; stream-json resume has duplicated entire history; concurrent
resumes interleave into one file; `/cd` relocates the file between project dirs).

### 3.2 Attribution model

Each session carries facts, stamped at ingest, and a **display owner computed at view
time** in the store:

1. `SESSION_OWNER` — when the shipper observed one (Linux-only, §4.2)
2. Tailnet identity — the store-stamped WhoIs user of the shipping node; authoritative on
   personal devices, generic on shared nodes
3. OS user — the shipper's uid name
4. Hostname — always present; the floor is "this node had this session"

The precedence is **store logic, revisable without touching any node**. Absence of
`SESSION_OWNER` is meaningful (nobody claimed this work tree) and must render as honest
absence, never be guessed. **Attribution is never authentication**: facts affect display
and grouping only, never access.

Set-time policy lives elsewhere by doctrine: herder stamps `SESSION_OWNER` from a
mission's owner or a node delegation lease; humans export it in a work tree's env. The
session service defines only the read side.

### 3.3 Invariants

- **I1 — Facts, never verdicts.** Nodes ship observations; all interpretation is view-time
  in the store or set-time in the component with context.
- **I2 — Byte-faithful mirror.** What the shipper sends is the file's raw bytes; the
  mirror stores them unmodified. Any harness-format knowledge lives in exactly one place:
  the store's ingest parser.
- **I3 — File-driven, never process-driven.** A session file ships because it exists and
  has unshipped bytes — whether its process is alive, dead, or predates the shipper's
  install. Backfill from offset 0 is the same code path as tailing.
- **I4 — At-least-once, idempotent ingest.** The cursor advances only after the store's
  durable ACK. Matching replay ranges compare overlap and append only matching excess;
  divergent history enters the frozen confirm-then-open conflict-generation protocol.
- **I5 — Dedup is correctness, not polish.** The index dedupes non-empty message UUIDs by
  `(tool, logical_session_id, entry_type, message_uuid)` across all files of a session;
  empty UUIDs never dedupe.
- **I6 — Identity survives churn.** Cursors key on file identity, so renames/moves
  (`/cd`) don't re-ship; size regression below the cursor means truncation → reset to 0
  and re-ship; deletion is not truncation → GC the cursor, keep the mirror.
- **I7 — The mirror outlives the source.** Clients delete transcripts (~30-day default
  cleanup); the store retains. The mirror is the team's durable session archive.
- **I8 — Correlations are remembered.** Once a `SESSION_OWNER` is observed for a session,
  it is recorded in the cursor registry and shipped; process death never retracts it.
- **I9 — One shipper per OS user.** `/proc/<pid>/environ` is 0400; cross-user reads are a
  hard wall, not a permission nicety. Multi-user nodes run one shipper per user.
- **I10 — Attribution ≠ authentication** (restated as invariant: no fact ever gates
  access).
- **I11 — Zero upward dependencies.** No herder, hcom, or mission concept in code, wire,
  or storage.

## 4. Components — from model to machinery

### 4.1 Shipper

One binary, cross-platform (Linux servers + macOS laptops), running per OS user
(user-level systemd unit / launchd agent).

- **Discovery**: watches the Claude and Codex session roots. fsnotify events are a hint;
  a periodic full rescan is the guarantee (catches queue overflows, moves, files created
  while the shipper was down). New file → new cursor at offset 0.
- **Tailing**: reads from the cursor offset, ships raw byte ranges. Partial trailing
  lines ship as-is (the mirror doesn't care; the ingest parser holds back the incomplete
  tail). The source file is the buffer — when the store is unreachable, the shipper just
  stops advancing; no local queue.
- **File identity**: session UUID claim from the filename immediately; fingerprint recorded once
  the file exceeds the fingerprint window (identity must work at size ~0 — freshly created
  session files are tiny). Same UUID + different fingerprint = recreated file → reset.
- **Facts**: hostname and OS user attached to every ship; `SESSION_OWNER` per §4.2.
  Tailnet identity is **not** client-supplied (§4.3).
- **Cursor registry**: a single local state file per user; offsets ACK-then-advance;
  cursor GC on file deletion; observed owner correlations recorded per session id.

### 4.2 SESSION_OWNER correlation (Linux-only enrichment)

Validated live on Linux before implementation. The shipper correlates session files to
running processes via `/proc`, reads `SESSION_OWNER` from `/proc/<pid>/environ`, and
stamps the session:

- **Codex — exact.** The leaf codex process holds its rollout file open; `pid → open fd →
  file` is an exact join.
- **Claude — cohort-scoped.** No open fd, no session id in the env. Correlate by
  (node, OS user, cwd): if every candidate claude process in that cohort agrees on one
  `SESSION_OWNER` value, stamp it; any disagreement → honest absence. Same-cwd collisions
  are real; guessing is worse than absence.
- **macOS — none.** Facts-only; no correlation attempted. Personal devices are covered by
  tailnet identity, which is the better signal there anyway.
- Hooks are **not** a dependency (ruled out on ergonomics); at most a future optional
  exactness upgrade for the Claude cohort case.
- Upstream ask on file (not load-bearing): Claude Code exposing its session id in process
  env would delete the cwd-ambiguity class.

### 4.3 Store

- **Ingest**: authenticated byte-range writes keyed by (tool, session_id, file identity,
  offset). Idempotent per I4. On receipt the store stamps the **tailnet identity** of the
  connecting node via WhoIs — identity is injected by the tailnet layer, never trusted
  from request content.
- **Mirror**: raw bytes per file identity, retained past client deletion (I7). Retention
  policy is a store setting (default: keep indefinitely until a policy exists).
- **Index (parse-on-ingest)**: parses mirrored bytes into per-message rows, derives logical
  session identity from content/overlap evidence, and applies I5's composite-key dedup while
  holding back trailing partial lines. When an upstream format change breaks
  parsing, the mirror is unaffected and the index is **re-derivable from the mirror** after
  a parser fix: one deploy, no node touched. Parse failures quarantine the file's index
  entries, never block the mirror.
- **Auth**: tsnet-embedded (or equivalent) listener; WhoIs on every connection; a tailnet
  grant scopes which identities may ship and which may read. Not whole-tailnet —
  transcripts contain pasted secrets, and tailnets contain phones and CI boxes.

### 4.4 Surface

One page, people-first recency:

- **Rows**: person (display owner per §3.2, with its source visible) → their nodes → their
  sessions, most-recently-active first. Sessions with no owner claim group under
  node/OS-user honestly.
- **Drill-down**: read-only transcript render from the index (roles, text, tool calls
  collapsed sensibly). A render-failure fallback shows raw JSONL lines from the mirror —
  the surface must never be fully blind to a session the mirror holds.
- **No search** (killed, S9). No write actions of any kind.

## 5. Expected behaviour

- New session starts on a node → file appears → cursor at 0 → bytes flow within the rescan
  interval at worst; the session is on the surface with whatever facts were observable.
- Node offline for a week → on reconnect, shippers resume from cursors; anything the
  client deleted meanwhile is already mirrored up to its last shipped offset.
- Shipper installed on a box with 30 days of existing history → I3: everything ships,
  attributed by whatever facts remain observable (dead sessions get no SESSION_OWNER
  unless previously recorded — honest absence).
- Claude resume creates a second file for the session → both files mirror; index dedup
  renders one clean transcript.
- `/cd` moves a session file across project dirs → rescan re-finds it by identity; no
  re-ship, no duplicate.
- Session file truncated/recreated → size regression resets the shipper cursor; matching
  history replays idempotently, while divergent bytes follow confirm-then-open generation
  handling so prior mirror evidence is never overwritten.
- Store down → shippers hold position; no data loss (source files are the buffer); catch
  up on return.
- Two users on one shared node → two shippers; each reads only its own environ (I9);
  transcripts attribute per-user by OS-user fact even before SESSION_OWNER.

## 6. Acceptance scenarios

1. **Backfill parity**: install shipper on a node with pre-existing sessions; byte-compare
   mirror vs source for every file → identical; surface lists them.
2. **Resume churn**: force a Claude resume-into-new-file; transcript renders without
   duplicated history; mirror holds both files.
3. **Truncation**: truncate a watched file mid-ship; shipper resets and re-ships; no
   infinite re-ingest loop (the filebeat #38070 failure).
4. **Move**: `/cd` a live session; no duplicate session appears; bytes keep flowing.
5. **Deletion vs retention**: delete a source file; cursor GCs; mirror and surface retain
   the transcript.
6. **Owner stamping**: codex session under `SESSION_OWNER=alice` → stamped alice (exact);
   two claude sessions same cwd, different owners → honest absence; single claude
   cohort → stamped.
7. **Cross-user wall**: user B's shipper never stamps user A's sessions; A's sessions
   still ship via A's shipper.
8. **Auth scope**: a tailnet device outside the grant can neither ship nor read; an
   in-grant device's WhoIs identity appears store-stamped on what it ships.
9. **Store restart / duplicate range**: re-send an already-ACKed range → no index
   duplication, no mirror corruption.
10. **Parser-break drill**: feed the index an unparseable-but-valid-JSONL variant → mirror
    intact, quarantined index entries, raw fallback renders; parser fix + re-derive
    restores the transcript.
11. **macOS**: laptop shipper ships facts-only sessions; display owner falls through to
    tailnet identity.

## 7. Deployment shape

- **One binary, `sesh`, subcommands**: `sesh ship` (the
  long-running per-user node agent), `sesh serve` (store + surface, one process),
  `sesh reindex` (re-derive index from mirror), `sesh status` (read-only: cursors, store
  reachability, last-ship times), and `sesh admin` for explicit operator repair. It is a
  plain Go build with no repository launcher shim. The darwin build compiles out /proc correlation; same binary otherwise. A later
  repo split may produce separate binaries from the same command tree — nothing depends
  on single-binary-ness except build simplicity.
- **Shipper**: `sesh ship` under a per-user systemd unit (Linux) / launchd agent
  (macOS); config = the store URL (env var or flag) + nothing else worth deciding on a
  node. The store's location is therefore a deployment-time value, not a spec concern:
  localhost in the short term, likely co-located with the herd server in the medium term —
  either way no shipper changes beyond the URL.
- **Store + surface**: `sesh serve`, one deployable process (mirror storage + index DB +
  HTTP), joined to the tailnet under its own node identity, so it can migrate hosts
  without shippers noticing.
- Rollout order: store first, then shippers node-by-node (I3 makes onboarding
  order-free — each node backfills whenever its shipper lands).

## 8. Wire protocol (owner-confirmed 2026-07-09)

This HTTP API is the **only** protocol between services. Shipper→store is the one
cross-service boundary; the server-rendered HTML surface reads store state in-process and
has no separate read protocol. Versioned under
`/v1` so the later repo split and store relocation are non-events.

HTTP over the tailnet (plain localhost allowed for the dev/short-term same-host case).
One write verb dominates:

- `PUT /v1/files/{tool}/{session_id}/{file_uuid}/bytes?offset=N` — raw byte range in the
  body; headers carry fingerprint, hostname, OS user, optional SESSION_OWNER; response is
  the durable-ACK high-water mark (the shipper's new cursor).
- A small `GET` set for cursors-recovery (ask the store "what do you have for this file
  identity?") so a shipper with a lost registry can resume without re-shipping the world.
- Read side: no public JSON protocol; HTML routes read the store and mirror in-process.

## 9. Non-goals (recorded decisions, not omissions)

- **No search** (S9 kill). Recency + drill-down only.
- **No node-side parsing** — upstream formats are internal and parse-breaking; parsing lives
  in the store's one deploy.
- **No node-side policy** — no attribution ladders, no display precedence on nodes (Q20).
- **No process supervision semantics** — the service never starts, stops, signals, or
  even requires session processes; files are the interface (I3).
- **No live relay guarantees** — the surface shows recency, not a realtime tail; latency
  is rescan-interval class, best-effort better via fsnotify.
- **No per-session ACLs in v1** — grant-scoped team visibility; the honest threat model
  is "the team can read each other's shells already." Revisit with team growth.
- **No OTel transport** — disqualified by lack of faithful backfill, transcript-shape
  mismatch, truncation, and per-tool divergence. Optional telemetry may be added later,
  never as the transcript spine.
- **No hcom/herder/mission awareness** (I11).
- **No authentication derived from attribution** (I10).
- **No Windows** in v1.

## 10. Launch micro-decisions (settled by owner, 2026-07-09)

1. **Store host** — ruled a non-decision: the shipper takes the store URL by env/flag;
   localhost short-term, probably herd-server co-located later. Spec §7 encodes this.
2. **Tool name & repo home** — **`sesh`**, living at `tools/sesh` for now with a planned
   later move; the §8 API is the only cross-service contract, keeping the move cheap.
3. **Wire detail** — HTTP PUT byte ranges confirmed as drafted (§8).

## 11. Transport rationale and precedents

The service deliberately combines mechanisms that mature log shippers already proved:

- Content fingerprints avoid inode/path identity failures. Filebeat documents fingerprint
  identity and inode-reuse data loss; Vector independently uses content-derived checkpoints.
  See [Filebeat file identity](https://www.elastic.co/docs/reference/beats/filebeat/file-identity),
  [Filebeat inode reuse](https://www.elastic.co/guide/en/beats/filebeat/current/inode-reuse-issue.html),
  and [Vector checkpointing](https://vector.dev/highlights/2021-01-31-file-source-checkpointing/).
- Persistent cursors advance only after durable acknowledgement, making delivery at-least-once
  and requiring idempotent store writes. Fluent Bit's tail input is representative:
  [tail input documentation](https://docs.fluentbit.io/manual/data-pipeline/inputs/tail).
- Size regression is truncation, and filesystem notifications are hints rather than a complete
  history. Periodic rescans catch missed events, moves, and downtime gaps.
- Raw-file shipping is necessary because harness telemetry is incomplete and divergent, while
  Claude resume behavior can create overlapping files. The mirror therefore remains evidence;
  parsing and unification stay centrally re-runnable.
- Tailnet-native mode uses connection-bound identity and app-capability grants rather than
  client-supplied identity: [Tailscale identity](https://tailscale.com/docs/concepts/tailscale-identity)
  and [application capabilities](https://tailscale.com/blog/app-capabilities).

## 12. Decisions embedded in this spec

- Store-derived logical-session spine; file identity = uuid + fingerprint
- Dumb shipper / byte mirror / parse-on-ingest split
- Facts-not-verdicts, view-time display owner, set-time policy elsewhere
- SESSION_OWNER as sole cross-surface var; /proc correlation tiers (codex exact, claude
  cohort, macOS none); hooks non-dependency (Q19–Q20 + live validation)
- One shipper per OS user (environ wall)
- Tailnet grant auth, WhoIs-stamped identity, never client-supplied
- File-driven shipping; mirror outlives client cleanup
- Kill list honored: no search, no OTel spine, no events relay
