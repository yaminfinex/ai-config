---
id: TASK-087
title: >-
  sesh-surface: read-only web page — people-first recency, transcript render,
  raw fallback, view-time owner precedence
status: To Do
assignee: []
created_date: '2026-07-09 04:11'
labels: []
dependencies: []
priority: medium
ordinal: 87000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
sesh is a team-visibility service for AI coding sessions. Every machine (node) runs a small per-OS-user agent (the SHIPPER) that tails the transcript files Claude Code and Codex CLI already write to disk, and ships their raw bytes plus four identity facts to one central service (the STORE), which keeps a byte-faithful mirror, parses it centrally into a per-message index, and serves one read-only web page (the SURFACE) answering 'what has everyone been working on?'

UNIT TYPE: implement. Designer-authored capture (docs/design/2026-07-09-sesh-task-captures.md @ 6843649 on branch sessions-missions-design) — the settled-decisions list below is DO-NOT-REVERSE; if one seems wrong or blocking, STOP and escalate to @hera (who routes to tomo/owner). Never substitute and disclose later.

PINNED REFS (read in this order, all at commit e58f50a on branch sessions-missions-design; a worker starting from main runs: git fetch origin sessions-missions-design, then git show e58f50a:<path>):
1. docs/specs/session-service-spec.md — THE CONTRACT, read fully. Section refs below (§N); I-n = invariants (§3.3); S-n = acceptance scenarios (§6).
2. docs/design/2026-07-09-session-service-build-brief.md — working mode + verify-early items.
3. docs/design/2026-07-09-session-shipping-prior-art.md — why each mechanism, with upstream bug refs.
(If the branch has merged to main by pickup, paths are the same; e58f50a stays the pinned wording.)

SEQUENCING: lanes 1+2 (shipper/store) freeze the spec §8 wire contract together first, in a short shared doc PR, before parallelizing. Lane 3 (surface) depends only on the index schema. Lane 4 (deploy) is unblocked the moment the store boots anywhere.

BUILD: one read-only web page served by the store process: people-first recency (person -> nodes -> sessions, most-recently-active first), transcript drill-down rendered from the index, raw-JSONL-lines fallback from the mirror. Display-owner precedence computed here, at view time: SESSION_OWNER fact > tailnet identity > OS user > hostname, with the winning fact's source shown. Depends only on the index schema (frozen by lanes 1+2 wire PR).

SETTLED DECISIONS (do not reverse; escalate if blocked):
- No search (explicit kill). Recency + drill-down is the whole v1 surface.
- Display precedence is view-time store/surface logic — revisable without touching any node. No precedence logic may migrate into the shipper.
- Honest absence is a feature: absence of SESSION_OWNER means 'nobody claimed this work tree' and must render as absence.
- Raw fallback is mandatory, not nice-to-have — it is the format-churn escape hatch.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 [S2] A session spanning multiple overlapping files renders as ONE clean transcript (no duplicated history), ordered correctly
- [ ] #2 [S6/S11] A codex session with SESSION_OWNER shows that owner labeled with its source; a macOS session with no owner fact falls through to tailnet identity; a session with no owner claim at all groups honestly under node/OS-user — never a guessed name
- [ ] #3 [S10] A session whose index entries are quarantined still opens: the raw-lines fallback renders from the mirror. The surface is never fully blind to a mirrored session
- [ ] #4 Recency ordering reflects last shipped activity; a session active seconds ago on any node appears at the top of its person's group within the rescan-interval bound
- [ ] #5 The page exposes zero write actions and no search box
<!-- AC:END -->
