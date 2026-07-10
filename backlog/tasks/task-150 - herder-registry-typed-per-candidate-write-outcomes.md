---
id: TASK-150
title: 'herder registry: typed per-candidate write outcomes'
status: To Do
assignee: []
created_date: '2026-07-10 10:15'
labels: []
dependencies: []
references:
  - docs/specs/herder-spec.md
modified_files:
  - tools/herder/internal/registry/write.go
priority: high
ordinal: 149000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the confirmed-write contract already stated in the herder spec. Registry multi-row writes currently return only encoded rows plus a batch error, forcing observer and callers to reconstruct applied/noop and collapsing refusal to the whole batch. Replace that ambiguity with typed per-candidate applied/noop/refused outcomes and migrate every writer to surface or handle them explicitly. This is the unharvested residue from the retired systemic review; it is distinct from the existing discarded-error gate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Registry write API returns one typed outcome per candidate: applied, noop, or refused with reason
- [ ] #2 Observer and all production writers consume outcomes directly; no encoded-row matching reconstructs status
- [ ] #3 Multi-row mixed outcome behavior is atomic where required and pinned by tests
- [ ] #4 No caller discards write outcomes or errors; repository gate proves it
<!-- AC:END -->
