---
id: TASK-261
title: >-
  Docs lift after the enroll-repair trial: repair-proof rationale comment,
  compact re-enroll help case, compact-then derived-sender help naming
status: To Do
assignee: []
created_date: '2026-07-16 08:05'
labels:
  - herder
  - docs
dependencies: []
priority: low
ordinal: 260500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Three small documentation improvements identified during review cycles, all docs/comment-only, none behavior-carrying:

1. Enroll call-site rationale comment: the unmerged trial branch for the enroll-repair unit carries a call-site comment explaining WHY feeding the stored label to the pinned ownership proof collapses the proof to terminal-only — the reviewer called it the best explanation either arm produced and the thing most likely to stop a future reader reintroducing the bug. Lift its substance onto main next to the pinned-proof-label call site (source: branch task-255-grok @ 0201fc1, enroll.go around the proof-label selection; rewrite in-place style, no branch references).

2. Enroll help, compact re-enroll case: the same branch's help text documents the standard compact repair flow (spawned agent, new sid, identity claim carried by ambient label env) — the merged arm's help does not. Lift the substance.

3. compact --then help: still describes the continuation as delivering to "your own bus" without naming the derived distinct sender; update to state the actual mechanism (fixed prefix + recipient-derived external sender).

Docs-only diff (stakes-gated review per house rules). Constraint: no agent names, task numbers, run identifiers, or SHAs in the durable text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Pinned-proof call site carries the collapse rationale in a comment; enroll help documents the compact re-enroll case; goldens regenerated
- [ ] #2 compact --then help names the derived-sender delivery mechanism accurately
- [ ] #3 Identifier sweep clean on the diff (no run-scoped identifiers in durable text)
<!-- AC:END -->
