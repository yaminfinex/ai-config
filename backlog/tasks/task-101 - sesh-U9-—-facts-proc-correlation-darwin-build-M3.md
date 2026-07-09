---
id: TASK-101
title: sesh U9 — facts + /proc correlation + darwin build (M3)
status: In Progress
assignee: []
created_date: '2026-07-09 05:29'
updated_date: '2026-07-09 06:54'
labels:
  - sesh
dependencies:
  - TASK-096
  - TASK-097
priority: medium
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: shipper (claude worker). Deliverable: SESSION_OWNER stamped where knowable — codex: walk /proc/<pid>/fd for the leaf process holding the rollout file open, read its environ, exact stamp; claude: cohort by (OS user, cwd of candidate claude processes), unanimous SESSION_OWNER or honest absence; macOS: none — facts-only, correlation behind build tags (correlate_linux.go / correlate_darwin.go no-op). Correlations write into the cursor registry and ship as a header on subsequent PUTs; NEVER retracted by process death (I8). Never read another uid environ — skip silently, no error spam (I9/S7). Reproduces the 2026-07-08 manual validation as code before the registry schema freezes. Requirements R3,R5.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U9 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), spec section 4.2, captures Lane 1 settled decisions (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u9.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Codex fixture process with SESSION_OWNER set stamps exactly (S6a)
- [ ] #2 Two fake claude processes same cwd different owners -> absence; one alone -> stamp (S6b)
- [ ] #3 Owner in registry survives process exit and restarts (I8)
- [ ] #4 Other-uid environ unreadable -> skipped silently (S7)
- [ ] #5 Darwin cross-compile runs facts-only, no /proc references (S11)
<!-- AC:END -->
