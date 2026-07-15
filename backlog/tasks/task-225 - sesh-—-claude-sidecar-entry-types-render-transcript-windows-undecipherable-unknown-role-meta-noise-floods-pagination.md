---
id: TASK-225
title: >-
  sesh — claude sidecar entry types render transcript windows undecipherable:
  unknown-role meta noise floods pagination
status: To Do
assignee: []
created_date: '2026-07-15 14:55'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 224500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-reported (2026-07-15): a live claude session page is
"undecipherable" — verified at
/s/claude/b0e97a40-fe04-45c7-836c-c232f8434ff9 on the live surface.
The latest window (messages 2763-2962 of 2962) renders as a wall of
entries shaped:

  role=unknown, etype in {ai-title, mode, permission-mode, last-prompt},
  body = the bare etype string, no content.

These are Claude Code sidecar/state lines (newer Claude Code versions
append them to the session JSONL alongside chat messages). The claude
index parser predates them: they land as unknown-role rows with no
renderable content, they COUNT as messages for windowing/pagination,
and at high frequency (state lines per turn) they flood the tail
window — the never-500 floor holds (page renders, 200 in ~0.4-0.8s)
but the transcript is unreadable, and real messages are pushed out of
the latest window.

Fix has two sides; both must land coherently:
1. INDEX semantics for these entry types — classify known claude
   sidecar/state types deliberately (meta rows, excluded from the
   message stream the way non-message rows are handled for other
   tools), decided against the real current claude JSONL format (this
   box has live corpora — and mind TASK-208: fixtures follow the
   documented private-repo precedent until that ruling). NO DDL; if
   classification truly needs schema, STOP and surface it. Unknown
   FUTURE types must still degrade safely (render floor), not vanish
   silently — the distinction is "known meta, excluded" vs "unknown,
   degraded-visible".
2. SURFACE windowing/pagination — windows and message counts should
   reflect renderable conversation, not meta noise; decide and
   document whether meta rows are collapsed, hidden behind a details
   toggle, or excluded with a count badge. Bounded-window and
   RTT-floor discipline unchanged; raw page stays byte-faithful.

Corpus note: rows already ingested as unknown will re-classify only on
reindex — state the operational story (reindex on deploy or accept
gradual). Frozen surfaces as always: wire v1 (Amendments 3+4), ACK
durability, R23, I1-I11, write discipline, fact_observations
INSERT-only, identifier-free journal contract, TASK-136/149/220
equivalence properties, empty-uuid non-participation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Known claude sidecar/state entry types classified deliberately in the index (decided against real current claude JSONL); unknown future types still degrade visibly, never silently dropped
- [ ] #2 Transcript windows/pagination reflect renderable conversation; the reported session's latest window shows real messages (live verification post-deploy); raw page byte-faithful
- [ ] #3 Reindex/equivalence properties green (incremental == Reindex incl. the new classification); reindex-vs-gradual reclassification story recorded
- [ ] #4 Full pinned gate green; never-500 floor unchanged
<!-- AC:END -->
