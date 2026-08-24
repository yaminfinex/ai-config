---
id: TASK-322
title: 'teardown unit four: doctrine flip — skill and session-context rewrite, backlog reconciliation, binary reinstall'
status: To Do
assignee: []
created_date: '2026-08-24 07:30'
labels:
  - herder
  - teardown
dependencies:
  - TASK-321
ordinal: 318800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Rewrite the orchestrate skill and the hcom agent session context to the
composed lifecycle (spawn via wrapper, bus messaging, compact via inject +
queued send, self-compact helper, cull via wrapper, resume/fork via hcom);
mark herder-spec superseded where verbs died; reconcile the open slim-down
backlog tasks (301-307) against the teardown charter; rebuild and install
the slimmed binary — the compatibility boundary for live seats flips here,
coordinated with the conductor so no running seat is mid-flow on a dead verb.
<!-- SECTION:DESCRIPTION:END -->

Addendum 2026-08-24 (from unit-three review, non-blocking): the old
`herder launch` maintained the ~/.cache/herder/vendorbin pin directory;
nothing maintains or cleans it after the launcher rewire — stale entries
degrade safely, but ai-doctor should flag the orphaned cache. Also
inherited by this unit: mark docs/specs/herder-spec.md superseded where
verbs died; rename/trim the adopted TestReview* reviewer-narration test
comment (unit-two note). NOTE from entry-39 ruling: this unit's repo work
lands on the impl-verb-demolition branch BEFORE the single atomic merge;
"binary reinstall" is obsolete — bin/herder self-builds, so the merge
itself deploys, and the conductor flips machine-side session context
immediately after.
