---
id: TASK-181
title: >-
  sesh — self-contained docs: move all sesh docs under tools/sesh and update
  every reference
status: Done
assignee: []
created_date: '2026-07-13 06:03'
updated_date: '2026-07-13 06:41'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 180000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ruling 2026-07-13: sesh must be self-contained — all its docs live under tools/sesh. Move docs/design/2026-07-12-sesh-store-served-distribution.md, docs/specs/sesh-wire.md, docs/specs/session-service-spec.md into tools/sesh/docs/ (design/ + specs/), update every live reference (tools/sesh README + ops/README + code comments + tests + justfile comments + backlog docs doc-001/doc-002 pointers; historical backlog task bodies stay untouched). Ride-along doc top-ups from execution: tagged fleet machines need their tag in the grant src (tag:superset lesson); first ssh sesh-host needs host-key accept-new.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 git mv preserves history; zero dangling references to old paths (grep gate)
- [x] #2 tools/sesh is fully self-contained: a checkout of tools/sesh alone carries spec, design, ops and field docs
- [x] #3 Execution-lesson doc top-ups landed (grant src for tagged fleet, ssh accept-new)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Done commit 976f7be. All three docs R100 byte-identical (blob-verified), zero-dangling gate with scan() error discrimination + existence assertions for the three spec-cited shared-corpus files. SCOPE RULING recorded: sesh operational docs in-module; spec citations of shared sessions/missions corpus stay repo-root as provenance (mission-spec + herder docs cite them). Review closures #56603/#56607.
<!-- SECTION:NOTES:END -->
