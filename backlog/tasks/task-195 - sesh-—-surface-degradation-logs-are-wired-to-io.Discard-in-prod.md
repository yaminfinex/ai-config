---
id: TASK-195
title: sesh — surface degradation logs are wired to io.Discard in prod
status: To Do
assignee: []
created_date: '2026-07-13 20:22'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 194000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Side observation from the read/write-split work: serve wires the surface logger to io.Discard, so writeDegraded paths and render failures are invisible on the live node — a degraded homepage looks healthy in the journal. Route surface logs to the service journal (identifier-free, consistent with the debug-timing no-identity contract in the read/write-split design note), keep volume bounded (degradation events, not per-request chatter).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Surface degradation/render-failure events reach the service journal on the live deployment shape
- [ ] #2 No session/file/logical identifiers in surface log output (same contract as debug timing); test pins it
- [ ] #3 Docs current per decision-001
<!-- AC:END -->
