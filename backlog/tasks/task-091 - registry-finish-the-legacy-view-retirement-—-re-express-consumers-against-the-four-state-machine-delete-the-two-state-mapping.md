---
id: TASK-091
title: >-
  registry: finish the legacy-view retirement — re-express consumers against the
  four-state machine, delete the two-state mapping
status: To Do
assignee: []
created_date: '2026-07-09 05:09'
labels: []
dependencies: []
priority: medium
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the systemic review (owner-blessed 2026-07-09): the registry still exposes a legacy two-state view (Status active/not) alongside the v2 four-state machine (seated/unseated/retired/lost), and every legacy-view consumer conflates two different questions — "is this session seated" (liveness) vs "is this label non-retired" (lease). That conflation is the proven bug class behind the cull-writes-nothing and label-entombment incidents; any NEW consumer written against the legacy view re-rolls those dice.

WORK: enumerate the legacy consumers (at memo time: registry.go ActiveLabelOwner, ActiveByPaneOrTerminal, ActiveCandidates; cullcmd selectTargets; sidecarcmd latest.Status; spawncmd and send resolvers — re-enumerate at dispatch, the list may have moved) and re-express EACH against the four-state machine, choosing per call site which question it actually asks (seated vs non-retired). Then delete the legacyRecordFromV2Object two-state mapping so the legacy view cannot be consumed by new code. Pairs naturally with TASK-066 (namespace_id consumer resolution) — same consumer-sweep motion, bundle if convenient.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every legacy-view consumer is re-expressed against the four-state machine with an explicit seated-vs-non-retired choice recorded per call site (list in the DONE report)
- [ ] #2 The two-state mapping (legacyRecordFromV2Object or its successor) is deleted; no production code path consumes the legacy view
- [ ] #3 Behavior pins: cull/spawn/send/sidecar goldens still pass or are updated with the semantic change named
- [ ] #4 gate green: go vet+test both modules, all check suites
<!-- AC:END -->
