---
id: TASK-288
title: 'daemon self-refresh: long-lived herder daemons re-exec on binary drift'
status: To Do
assignee: []
created_date: '2026-07-18 20:45'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 287500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Systemic fix for the stale-resident-writer class (companion to the enable-preflight scan task): per-seat sidecars and other long-lived herder daemons should detect that the installed/cached herder build has changed since they started and re-exec themselves onto the current binary at a safe boundary (never mid-write, never in a way that emits false holder-exit death evidence for their seat). The observer already has restart/nudge machinery — extend the concept to sidecars, or unify. DESIGN-FIRST (load-bearing daemon lifecycle): design checkpoint must cover drift detection mechanism, safe re-exec boundaries relative to liveness evidence and the holder-exit apply path, crash-during-reexec behavior, and interplay with the double-start guard. Candidate extension (NOT yet owner-agreed, do not build without ruling): a registry-declared min-writer-contract fence generalizing the existing v1/v2 migration fence — writers check the floor at open and fail closed with remedy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint approved before code: drift detection, safe re-exec boundary, liveness/holder-exit interaction, double-start guard interplay
- [ ] #2 A sidecar started on build A observably re-execs onto build B after B lands, without unseating its seat or emitting death evidence
- [ ] #3 Re-exec never interrupts an in-flight registry write; failure to re-exec degrades to current behavior (keep running old build) with a report
<!-- AC:END -->
