---
id: TASK-285
title: >-
  credential sweep conflates issuance with cutover enable — auto-creates the
  marker at 100%, violating the two-step owner-gated flip
status: In Progress
assignee: []
created_date: '2026-07-18 13:51'
updated_date: '2026-07-18 14:07'
labels:
  - herder
  - bug
dependencies: []
priority: high
ordinal: 284500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident 2026-07-18 (twice in one day): credentialcmd sweep, after reaching 100% coverage, unconditionally calls seatcred.EnableCutover — so ANY successful sweep flips the fleet to credential-authenticated verbs. The ratified design and rollout docs describe two separate steps: sweep = issuance only (behavior-neutral, safe to run operationally), explicit enable = the owner-gated flip. Consequence today: the orchestrator ran the sweep twice as an operational unblock (documented as behavior-neutral) and unknowingly flipped the cutover both times while a registry carry defect was live-stripping generations, forcing two emergency marker rollbacks; the operator-facing confusion (verbs suddenly demanding --credential-file) was attributed to the owner having flipped it, which never happened. Fix: sweep stops at the coverage report (exit 0 at 100%, naming the enable command as the next step); a separate explicit subcommand (e.g. herder credential enable) creates the marker, refusing below 100% coverage. Contract suite pins: sweep at 100% does NOT create the marker; enable refuses below 100%; enable creates it at 100%. Docs updated to match. NOTE: the credential-DX design (approved, pending owner sign-off) already assumes the two-step contract — this fix restores the implementation to the ratified design; coordinate merge order with the carry fix so re-enable happens only after both.
<!-- SECTION:DESCRIPTION:END -->
