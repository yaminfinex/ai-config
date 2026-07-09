---
id: TASK-100
title: 'sesh U8 — ops: sesh status + admin drop-file + M2 exposure (M2)'
status: In Progress
assignee:
  - sesh-store-soho
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 06:48'
labels:
  - sesh
dependencies:
  - TASK-098
  - TASK-099
priority: medium
ordinal: 100000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Split: store half (codex) + CLI/runbook. Deliverable: sesh status node-side (cursor summary, poisoned files, store reachability, last-ACK age; nonzero exit on unreachable/poisoned — scriptable); store nodes view (last-PUT age per hostname+OS-user, stale >48h flagged, R11); sesh admin drop-file <identity> deletes one file mirror bytes + index rows, requires --yes, logs the drop in the store DB (R20). M2 exposure: README runbook documents the tailscale-serve interim (read-only port only; ingest stays loopback) and records the owner sign-off. Requirements R11,R19,R20,R21(status).

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U8 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md). Thread: sesh-u8. Owner (@bigboss) ratifies M2 exposure — orchestrator routes it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 status exit codes correct for healthy / unreachable / poisoned
- [ ] #2 drop-file removes exactly one file bytes+rows, leaves session siblings, refuses without --yes; drop + reindex leaves no orphans
- [ ] #3 Nodes view flags an aged last-PUT (injected old timestamp)
- [ ] #4 Ingest handler rejects non-loopback source pre-M4; only read port exposed via serve config
- [ ] #5 Runbook section reviewed at M2 sign-off
<!-- AC:END -->
