---
id: TASK-085
title: >-
  sesh-shipper: per-user node agent — discovery, cursor tailing, facts, /proc
  correlation, service units
status: To Do
assignee: []
created_date: '2026-07-09 04:11'
updated_date: '2026-07-09 04:22'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 85000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
<!-- SECTION:DESCRIPTION:BEGIN -->
sesh is a team-visibility service for AI coding sessions. Every machine (node) runs a small per-OS-user agent (the SHIPPER) that tails the transcript files Claude Code and Codex CLI already write to disk, and ships their raw bytes plus four identity facts to one central service (the STORE), which keeps a byte-faithful mirror, parses it centrally into a per-message index, and serves one read-only web page (the SURFACE) answering 'what has everyone been working on?'

UNIT TYPE: implement. Designer-authored capture (docs/design/2026-07-09-sesh-task-captures.md @ 6843649 on branch sessions-missions-design) — the settled-decisions list below is DO-NOT-REVERSE; if one seems wrong or blocking, STOP and escalate to @hera (who routes to tomo/owner). Never substitute and disclose later.

PINNED REFS (read in this order, all at commit e58f50a on branch sessions-missions-design; a worker starting from main runs: git fetch origin sessions-missions-design, then git show e58f50a:<path>):
1. docs/specs/session-service-spec.md — THE CONTRACT, read fully. Section refs below (§N); I-n = invariants (§3.3); S-n = acceptance scenarios (§6).
2. docs/design/2026-07-09-session-service-build-brief.md — working mode + verify-early items.
3. docs/design/2026-07-09-session-shipping-prior-art.md — why each mechanism, with upstream bug refs.
(If the branch has merged to main by pickup, paths are the same; e58f50a stays the pinned wording.)

SEQUENCING: lanes 1+2 (shipper/store) freeze the spec §8 wire contract together first, in a short shared doc PR, before parallelizing. Lane 3 (surface) depends only on the index schema. Lane 4 (deploy) is unblocked the moment the store boots anywhere.

BUILD: the per-user node agent at tools/sesh: session-file discovery (fsnotify-as-hint + periodic full rescan), byte-range tailing with per-file cursors (ACK-then-advance), the four facts, /proc SESSION_OWNER correlation on Linux, systemd user unit + launchd agent. Config surface: the store URL (env var or flag), nothing else.

SETTLED DECISIONS (do not reverse; escalate if blocked):
- No parsing on the node — not even 'just to skip blank lines'. Anthropic documents the JSONL format as internal and parse-breaking; parsing lives in the store's one deploy.
- File identity = session uuid + content fingerprint, never path or inode. Inode reuse and /cd moves are documented failure modes of path/inode keying.
- Cursor advances only after the store's durable ACK (at-least-once). The source file is the only buffer — no shipper-side queue.
- One shipper per OS user. /proc/<pid>/environ is mode 0400; the cross-user wall is a kernel fact, not a design preference.
- Codex correlation is fd-exact; Claude is cohort (node, OS user, cwd) unanimous-or-absent. Guessing an owner is ruled worse than showing none.
- Hooks are NOT a dependency for attribution (ruled out on ergonomics). At most a future optional exactness upgrade.
- A correlation once observed is recorded in the cursor registry and never retracted by process death.
- Shipping is file-driven, never process-driven: backfill of dead sessions is the same code path as live tailing.
- Config = store URL by env/flag only. The store's host is a deployment-time value (localhost short-term, likely herd-server co-located later), never baked in.
<!-- SECTION:DESCRIPTION:END -->

ADDENDUM (2026-07-09, designer): docs/design/2026-07-09-sesh-ship-plan.md @ f744ee9 on branch sessions-missions-design is the RATIFIED milestone plan (M0-M4) over the four sesh lanes, including the dispatch mapping table — read it with the other pinned refs. Milestone gates are named spec §6 scenarios passing on a REAL machine, not merges. M2 = first useful ship (browse one node); M4 = done-per-spec. THIS LANE: dispatches first, alongside the store lane; the two workers CO-AUTHOR the M0 wire + index-schema freeze doc PR, and the designer's sign-off gates the M0 merge — no lane code beyond M0 before that gate passes.

ADDITIONAL SETTLED DECISION (owner-confirmed 2026-07-09, spec §7): ONE binary named sesh with subcommands ship/serve/reindex/status. Do not create a separate shipper binary — the shipper is `sesh ship`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 [S1] Install the shipper on a machine with pre-existing Claude/Codex session files (including files whose processes are long dead). Every file ships completely; a byte-compare of store mirror vs source shows zero differences
- [ ] #2 [S3] Truncate a watched file below its cursor while the shipper runs: the shipper resets that cursor to 0 and re-ships once; it does not loop re-ingesting forever
- [ ] #3 [S4] Move a live session file to another directory (Claude /cd does this): the shipper re-identifies it by uuid+fingerprint — bytes keep flowing, nothing re-ships from 0, no second session appears
- [ ] #4 [S5] Delete a source file: the shipper GCs its cursor and does not treat the deletion as truncation (no reset/re-ship of anything)
- [ ] #5 [S6a] Start a codex session in a tree with SESSION_OWNER=alice exported: the shipped facts carry alice, found via the open rollout file descriptor (exact, not inferred)
- [ ] #6 [S6b] Two Claude sessions in the same cwd under different SESSION_OWNER values: facts carry NO owner (honest absence). One alone: facts carry its owner
- [ ] #7 [S7] On a two-user machine, user B's shipper never reads or stamps user A's sessions; A's shipper handles A's — B's shipper must not even attempt the environ read (no access-error logs)
- [ ] #8 [S11] On macOS the same binary ships bytes + hostname + OS user, performs no /proc correlation (compiled out on darwin), and does not error about its absence
- [ ] #9 Store unreachable: shipper holds position (cursor does not advance, no local queue grows) and catches up losslessly when the store returns
- [ ] #10 Kill and restart the shipper mid-file: no bytes lost, no bytes double-indexed (duplicate ranges allowed on the wire — the store absorbs them)
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-09 04:22
---
READ THE RAW FILE, NOT ONLY --plain: this description exceeds the backlog CLI render cap (~3.2k chars) and `backlog task N --plain` SILENTLY TRUNCATES its tail — which here includes the ship-plan addendum, per-lane dispatch timing, and (on 085/086) the one-binary settled decision. Full capture: the task file under backlog/tasks/. Tracked as TASK-090.
---
<!-- COMMENTS:END -->
