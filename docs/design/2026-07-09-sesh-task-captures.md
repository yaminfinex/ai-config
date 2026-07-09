---
title: "sesh build — per-lane task captures (designer-authored)"
date: 2026-07-09
status: CAPTURE INPUT for board filing (hera). Authored by the designer per the
  unit-type doctrine. Written for three readers with zero run context: a future
  orchestrator, the worker who picks the task up, and reviewers.
---

# sesh — per-lane captures

**What sesh is, in one paragraph.** A team-visibility service for AI coding sessions.
Every machine ("node") runs a small per-OS-user agent (the **shipper**) that tails the
transcript files Claude Code and Codex CLI already write to disk, and ships their raw
bytes — plus four identity facts — to one central service (the **store**), which keeps a
byte-faithful mirror, parses it centrally into a per-message index, and serves one
read-only web page (the **surface**) answering "what has everyone been working on?"

## Pinned refs (read in this order; all at commit `e58f50a` on branch `sessions-missions-design`)

A worker starting from `main` runs:

```
git fetch origin sessions-missions-design
git show e58f50a:docs/specs/session-service-spec.md          # 1. THE CONTRACT — read fully
git show e58f50a:docs/design/2026-07-09-session-service-build-brief.md   # 2. working mode + verify-early
git show e58f50a:docs/design/2026-07-09-session-shipping-prior-art.md    # 3. why each mechanism, w/ upstream bug refs
```

(If the branch has merged to main by pickup time, the paths are the same; `e58f50a`
remains the pinned wording this capture was written against.) Spec section numbers below
(§N) refer to the spec; "I-n" are its invariants (§3.3); "S-n" are its acceptance
scenarios (§6).

## Sequencing (verbatim into every capture)

Lanes 1+2 freeze the spec §8 wire contract together first, in a short shared doc PR,
before parallelizing. Lane 3 depends only on the index schema. Lane 4 is unblocked the
moment the store boots anywhere.

---

## Lane 1 — sesh-shipper

**Build:** the per-user node agent at `tools/sesh`: session-file discovery
(fsnotify-as-hint + periodic full rescan), byte-range tailing with per-file cursors
(ACK-then-advance), the four facts, `/proc` SESSION_OWNER correlation on Linux, systemd
user unit + launchd agent. Config surface: the store URL (env var or flag), nothing else.

**Acceptance criteria (pass/fail a reviewer can run):**

- [S1] Install the shipper on a machine with pre-existing Claude/Codex session files
  (including files whose processes are long dead). Every file ships completely; a
  byte-compare of store mirror vs source shows zero differences.
- [S3] Truncate a watched file below its cursor while the shipper runs. The shipper
  resets that cursor to 0 and re-ships once; it does not loop re-ingesting forever.
- [S4] Move a live session file to another directory (Claude `/cd` does this). The
  shipper re-identifies it by uuid+fingerprint: bytes keep flowing, nothing re-ships from
  0, no second session appears.
- [S5] Delete a source file. The shipper GCs its cursor and does not treat the deletion
  as truncation (no reset/re-ship of anything).
- [S6a] Start a codex session in a tree with `SESSION_OWNER=alice` exported. The shipped
  facts carry `alice`, found via the open rollout file descriptor (exact, not inferred).
- [S6b] Run two Claude sessions in the same cwd under different SESSION_OWNER values:
  facts carry NO owner (honest absence). Run one alone: facts carry its owner.
- [S7] On a two-user machine, user B's shipper never reads or stamps user A's sessions;
  A's shipper handles A's. (Verify: B's shipper logs no environ access errors for A —
  it must not even attempt the read.)
- [S11] On macOS the same binary ships bytes + hostname + OS user and performs no /proc
  correlation (compiled out on darwin), and does not error about its absence.
- Store unreachable: shipper holds position (cursor does not advance, no local queue
  grows), and catches up losslessly when the store returns.
- Kill and restart the shipper mid-file: no bytes lost, no bytes double-indexed
  (duplicate ranges allowed on the wire — the store absorbs them).

**Settled decisions (do not reverse; escalate to tomo/owner if blocked):**

- No parsing on the node — not even "just to skip blank lines." Anthropic documents the
  JSONL format as internal and parse-breaking; parsing lives in the store's one deploy.
- File identity = session uuid + content fingerprint, never path or inode. Inode reuse
  and `/cd` moves are documented failure modes of path/inode keying.
- Cursor advances only after the store's durable ACK (at-least-once). The source file is
  the only buffer — no shipper-side queue.
- One shipper per OS user. `/proc/<pid>/environ` is mode 0400; the cross-user wall is a
  kernel fact, not a design preference.
- Codex correlation is fd-exact; Claude is cohort (node, OS user, cwd) unanimous-or-
  absent. Guessing an owner is ruled worse than showing none.
- Hooks are NOT a dependency for attribution (ruled out on ergonomics). At most a future
  optional exactness upgrade.
- A correlation once observed is recorded in the cursor registry and never retracted by
  process death.
- Shipping is file-driven, never process-driven: backfill of dead sessions is the same
  code path as live tailing.
- Config = store URL by env/flag only. The store's host is a deployment-time value
  (localhost short-term, likely herd-server co-located later), never baked in.

## Lane 2 — sesh-store

**Build:** the central service at `tools/sesh`: idempotent byte-range ingest (spec §8
API), byte-faithful mirror storage, parse-on-ingest index (per-message rows, message-uuid
dedup across a session's files, trailing-partial-line holdback, quarantine on parse
failure, a re-derive-index-from-mirror command), tailnet identity stamping via WhoIs,
grant check on every connection.

**Acceptance criteria:**

- [S1] Mirror contents byte-compare identical to shipped sources for every file.
- [S2] Ingest two files belonging to one Claude session with overlapping message history
  (resume-into-new-file). The index holds each message uuid once; both files remain
  intact in the mirror.
- [S5] After a client deletes its source file, the mirror and index retain the full
  transcript indefinitely.
- [S8] A tailnet device outside the grant can neither PUT nor read — connection-level
  deny. An in-grant shipper's uploads carry a store-stamped WhoIs identity; any identity
  claim inside request content is ignored.
- [S9] Re-send an already-ACKed byte range: the store overwrite-compares; no mirror
  corruption, no duplicate index rows. Restart the store mid-ingest and repeat: same.
- [S10] Feed valid-JSONL-but-unparseable lines: the mirror stores them, the affected
  index entries quarantine (visible as such), ingest of other files continues. After a
  parser fix, the re-derive command rebuilds the index from the mirror alone and the
  transcript renders.
- Trailing partial line (no newline yet): mirrored as-is; the index excludes it until
  the line completes.
- PUT response returns the durable high-water mark; a cursor-recovery GET tells a
  shipper with a lost registry what the store already has for a file identity.

**Settled decisions:**

- HTTP PUT byte ranges under `/v1` is the wire (owner-confirmed 2026-07-09):
  `PUT /v1/files/{tool}/{session_id}/{file_uuid}/bytes?offset=N`, facts in headers.
  Curl-debuggable; idempotency falls out of addressing bytes. This API is the ONLY
  contract between shipper and store — do not add a second channel.
- Message-uuid dedup is core correctness, not an optimization: Claude verifiably writes
  one session across multiple files with duplicated history.
- The mirror is the durable record; the index is disposable/re-derivable. Parse failures
  quarantine — they never block or mutate the mirror.
- Identity is stamped from tailscaled/tsnet WhoIs at the connection, never trusted from
  the client. Attribution is never authentication (facts never gate access).
- Access is grant-scoped, not whole-tailnet: transcripts contain pasted secrets, and
  tailnets contain phones and CI boxes.
- The store joins the tailnet under its own node identity so it can change hosts without
  any shipper change beyond the URL.

## Lane 3 — sesh-surface

**Build:** one read-only web page served by the store process: people-first recency
(person → nodes → sessions, most-recently-active first), transcript drill-down rendered
from the index, raw-JSONL-lines fallback from the mirror. Display-owner precedence
computed here, at view time: SESSION_OWNER fact > tailnet identity > OS user > hostname,
with the winning fact's source shown.

**Acceptance criteria:**

- [S2] A session spanning multiple overlapping files renders as ONE clean transcript
  (no duplicated history), ordered correctly.
- [S6/S11] A codex session with SESSION_OWNER shows that owner labeled with its source;
  a macOS session with no owner fact falls through to tailnet identity; a session with
  no owner claim at all groups honestly under node/OS-user — never a guessed name.
- [S10] A session whose index entries are quarantined still opens: the raw-lines
  fallback renders from the mirror. The surface is never fully blind to a mirrored
  session.
- Recency ordering reflects last shipped activity; a session active seconds ago on any
  node appears at the top of its person's group within the rescan-interval bound.
- The page exposes zero write actions and no search box.

**Settled decisions:**

- No search (explicit kill). Recency + drill-down is the whole v1 surface.
- Display precedence is view-time store/surface logic — revisable without touching any
  node. No precedence logic may migrate into the shipper.
- Honest absence is a feature: absence of SESSION_OWNER means "nobody claimed this work
  tree" and must render as absence.
- Raw fallback is mandatory, not nice-to-have — it is the format-churn escape hatch.

## Lane 4 — sesh-deploy

**Build:** stand the store up (short-term host per owner: wherever is convenient —
localhost-class; the URL is the only coupling), tailnet join + grant policy for it,
shipper packaging + rollout node-by-node (per-user systemd units on Linux, launchd on
macOS), including at least one macOS laptop and one shared multi-user node.

**Acceptance criteria:**

- [S8] Before any real transcripts flow: a second tailnet identity outside the grant is
  verified DENIED at connection level (the brief's verify-early item 3).
- Rollout is order-free: a node onboarded a week late backfills its full local history
  (30-day window) with no special handling.
- Shipper units survive reboot and user re-login on both platforms; store survives
  restart with mirror and index intact.
- A store host migration (change the URL the shippers read, restart units) loses no
  data and requires no other node change — proving the URL-only coupling.
- The shared-node deployment runs one shipper per OS user, each under its own uid.

**Settled decisions:**

- Store host is a deployment-time value by owner ruling — do not build host assumptions
  into anything; medium-term expectation is herd-server co-location, and that must remain
  a URL change.
- Grant scope before content: the deny path is verified before transcripts flow.
- No Windows in v1.
- Code lives at `tools/sesh` now and moves to its own repo later — nothing in deploy may
  depend on the repo location (no repo-path assumptions in units or scripts).
