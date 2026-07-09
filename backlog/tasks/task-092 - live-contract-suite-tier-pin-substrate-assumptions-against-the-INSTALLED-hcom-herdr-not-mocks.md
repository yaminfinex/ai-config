---
id: TASK-092
title: >-
  live-contract suite tier: pin substrate assumptions against the INSTALLED
  hcom/herdr, not mocks
status: Done
assignee: []
created_date: '2026-07-09 05:09'
updated_date: '2026-07-09 12:58'
labels: []
dependencies: []
priority: high
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the systemic review (owner-blessed HIGH 2026-07-09 — priority upgraded by events): our suites test against mocks and canned fixtures that mirror hcom/herdr behavior, so mock-vs-live drift produces "battery green, live pairing broken". This failure mode has now occurred repeatedly, most recently the observer session.snapshot bug: the mock served a flat payload, the live server a nested one, and 30 suites stayed green while the shipped component was blind in production (adversarial review of that fix explicitly flagged the missing live-shape enforcement).

WORK: a new test tier, tools/herder/tests/check-live-contract.sh, that runs its assertions against the INSTALLED hcom and herdr binaries and SKIPS CLEANLY (with a visible skip count, not silent green) when they are absent. Predicates to pin (re-verify the list at dispatch): (1) bootstrap tag-line extraction against real hcom output, both quote styles; (2) hcom list --json shape (single object keyed by base name); (3) roster launch_context fields per agent family; (4) herdr agent-list envelope; (5) herdr api schema --json snapshot diff as mechanical drift detection; (6) herdr socket session.snapshot result shape (the nested result.snapshot wrapper) against the live server — this is the one that would have caught the observer incident. Wire the tier into the herdr/hcom upgrade runbook AND run it periodically, not only at upgrades.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 check-live-contract.sh exists, asserts all pinned predicates against installed binaries, and skips visibly (never silently passes) when a binary is absent
- [ ] #2 The session.snapshot nested-shape predicate demonstrably fails against a hypothetical flat-serving server (negative demo through the real assertion path, not a hand grep)
- [ ] #3 Upgrade runbook references the tier as a required step; a periodic-run mechanism or documented cadence exists
- [ ] #4 gate green: go vet+test both modules, all check suites (including the new tier skipping or passing per environment)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 8f32694 (b790d19+bb9a6ae+376b2be). Three adversarial review rounds by cross-family reviewer garo, all findings demonstrated+fixed+re-verified (aspirational-green, skip-abuse, mixed-binary, false-red roster, vanishing negative demo, deleted diff artifact, env coupling). Independent gate green all rounds; post-merge gate on main running.
<!-- SECTION:NOTES:END -->
