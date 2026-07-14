---
id: TASK-195
title: sesh — surface degradation logs are wired to io.Discard in prod
status: In Progress
assignee:
  - mika
created_date: '2026-07-13 20:22'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 194000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Side observation from the read/write-split work: surface.New defaults its logger to io.Discard and newSurfaceHandler passes no WithLogger option, so every degraded-render/lookup-failure line on the live store is dropped — a degraded homepage looks healthy in the journal. Likely a one-line wiring fix (surface.WithLogger over stderr/journal), plus deciding the log shape for session-scoped errors under the identifier-free journal contract (read/write-split design note); keep volume bounded (degradation events, not per-request chatter).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Surface degradation/render-failure events reach the service journal on the live deployment shape
- [ ] #2 No session/file/logical identifiers in surface log output (same contract as debug timing); test pins it
- [ ] #3 Docs current per decision-001
<!-- AC:END -->
