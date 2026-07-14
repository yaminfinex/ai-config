---
id: TASK-213
title: >-
  Grok transport T2 handshake race — HCOM_RECOVER can precede the expected
  per-id wake (pre-existing flake)
status: To Do
assignee: []
created_date: '2026-07-14 23:04'
labels: []
dependencies: []
ordinal: 212000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Surfaced during the nudge-deletion review by the codex incumbent: broad package runs of the grok transport tests intermittently fail TestT2 because the HCOM_RECOVER line can arrive before the expected per-id wake line in the tap handshake — a test-side ordering assumption, reproduced ~1/100 at BOTH merge-base and the nudge-deletion head (100-run probe each), so pre-existing and not change-introduced; the fail-closed transport gate passes because it runs the declared list serially. RESEARCH-THEN-FIX: establish whether the race is test-only (assertion assumes wake-before-recover ordering the binder never promises) or a real tap-protocol ordering gap; fix the test to accept both orders if test-only, or file the protocol finding if real. Small unit; grokbridge tests territory.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Race root-caused (test-only vs protocol) with evidence
- [ ] #2 Flake eliminated (100-run probe green) or protocol finding filed
<!-- AC:END -->
