---
id: TASK-168
title: >-
  sesh — design pass: store-served distribution (/install.sh + /releases + sesh
  update)
status: Done
assignee: []
created_date: '2026-07-12 20:54'
updated_date: '2026-07-12 21:14'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 167000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner sanctioned option 1c from doc-002 (TASK-155): the store serves its own release channel, so the distribution URL IS the store URL and onboarding is one curl. This task is the full design pass; implementation follows in separate tasks.

Owner riders binding the design:
1. Day-1 must work WITHOUT tsnet: the design carries a staged exposure ladder, and URL changes between stages are an accepted migration tool (announce + reinstall).
2. Tailnet-admin asks must be minimized, sized, and scripted: the design states exactly what to request from the tailnet admin at each stage (paste-ready), and which stages need no ask at all.
3. Standing ask: ALL relevant docs (README runbooks, wire-doc note, specs, install-ship.sh deprecation, doc-002 cross-refs) are enumerated in the design's docs plan and kept current by every implementation task that follows.

Deliverable: design doc in docs/design/ covering endpoints + listener placement, installer script semantics (incl. re-install/URL-change vs drop-in preservation), sesh setup interplay, sesh update flow, release publishing recipe, staged exposure ladder, admin-ask scripts, data/backup classification of releases/, docs plan, and explicit decision points for owner ratification. Frozen constraints untouched: wire v1, ACK durability, R23, drop-in preservation (refinements flagged as decision points, not exercised), I1-I11, URL-only coupling.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Design doc in docs/design/ covers all listed areas; every open choice is a numbered decision point with a recommendation, none exercised
- [x] #2 Staged exposure ladder gives a zero-admin-ask day-1 path with honest risk statement, and sized paste-ready admin asks for later stages
- [x] #3 URL-change/reinstall flow is fully specified end-to-end and reconciled with the frozen drop-in preservation rule
- [x] #4 Docs plan enumerates every document each follow-up implementation task must update, as ACs it hands them
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design delivered: docs/design/2026-07-12-sesh-store-served-distribution.md, revised after independent codex design review (11 findings, all folded: DP-4 pristine-detection rejected as unsound in favor of provenance digest / always-refuse; admin ask corrected — tag owners cannot mint auth keys, ask includes one reusable tagged key + expiry-disable (DP-7); crash-safe update replacement ordering; VERSION-once immutable fetch paths; staged publish with atomic mv; route-scoped any-of-verbs auth; equality-only version semantics; per-OS config parsing rules; URL-migration baseline inventory + retention deadline). Seven DPs await owner ratification; follow-up implementation tasks T-0/T-A/T-B are filed-ready in the design doc §11. decision-001 records the standing docs-currency rule; doc-002 carries the owner decision note.
<!-- SECTION:NOTES:END -->
