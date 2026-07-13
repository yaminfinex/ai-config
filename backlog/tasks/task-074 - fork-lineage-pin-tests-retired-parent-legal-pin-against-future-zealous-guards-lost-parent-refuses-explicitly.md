---
id: TASK-074
title: >-
  fork lineage pin tests: retired parent legal (pin against future zealous
  guards); lost parent refuses explicitly
status: To Do
assignee: []
created_date: '2026-07-08 11:53'
updated_date: '2026-07-13 01:05'
labels: []
dependencies: []
priority: low
ordinal: 74000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From spec-ravu ruling #12237 (TASK-071 dosa LOW-4): fork from a RETIRED parent is spec-LEGAL — transcript is the substrate (§7), retirement closes occupancy/label not history (§3.1-3), fork mints a new guid with parent undisturbed (no §3.2 edge). Resume/fork asymmetry is principled and the §7 drafting omission was load-bearing. Erratum staged (6b59162, pending blessing batch). Do NOT add a fork-of-retired guard.

Scope (tiny, bundle-eligible into any lifecyclecmd-adjacent unit):
1. Pin test: fork retired parent -> child registered with forked_from=<retired guid>, child seats fine, parent row count unchanged and state still retired.
2. The ONE illegal parent is LOST (transcript verified gone = no substrate): fork of a lost parent must refuse EXPLICITLY with a clean message naming the lost verification, rather than failing wherever the missing transcript bites; test asserts refusal + zero rows appended anywhere.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Pin test: fork of a RETIRED parent succeeds — child registered with forked_from=<retired guid>, child seats, parent row count unchanged and state still retired
- [ ] #2 Fork of a LOST parent (transcript verified gone) refuses explicitly, naming the lost verification; zero rows appended anywhere
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13 staleness audit: KEEP — capture re-verified accurate against main (evidence in run journal / audit report #52404).
<!-- SECTION:NOTES:END -->
