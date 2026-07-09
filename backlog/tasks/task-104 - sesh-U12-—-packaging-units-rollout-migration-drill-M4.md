---
id: TASK-104
title: 'sesh U12 — packaging, units, rollout, migration drill (M4)'
status: In Progress
assignee: []
created_date: '2026-07-09 05:29'
updated_date: '2026-07-09 07:59'
labels:
  - sesh
dependencies:
  - TASK-100
  - TASK-102
  - TASK-103
priority: medium
ordinal: 104000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: deploy. Deliverable: etc/systemd/sesh-ship.service (per-user unit, ExecStart pinned absolute binary path, Restart=on-failure, store URL via Environment= or drop-in), etc/launchd/dev.sesh.ship.plist.tmpl (follow the repo existing template-token pattern), install script, rollout runbook in tools/sesh/README.md. Rollout: store first (tsnet up, grant applied, deny verified), then nodes in any order — at least one macOS laptop and one shared multi-user node (two shippers, two uids). Migration drill: move store host keeping tsnet identity — zero shipper changes, zero loss. No repo-path assumptions in units or scripts (the module moves repos later). Requirements R22 + deploy half of R18/R19.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U12 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), captures Lane 4 (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u12. Owner (@bigboss) ratifies M4 done-per-spec.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Units survive reboot + user re-login on both platforms (runbook checklist executed)
- [ ] #2 Late-onboarded node backfills full 30-day history unaided
- [ ] #3 Shared node runs two isolated shippers (S7 fleet half)
- [ ] #4 Store host migration loses nothing, changes nothing on nodes
- [ ] #5 Stale binary vs newer registry refuses cleanly in the field (R23)
<!-- AC:END -->
