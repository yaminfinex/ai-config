---
id: TASK-091
title: >-
  registry: finish the legacy-view retirement — re-express consumers against the
  four-state machine, delete the two-state mapping
status: Done
assignee:
  - hera-run
created_date: '2026-07-09 05:09'
updated_date: '2026-07-12 11:10'
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
- [x] #1 Every legacy-view consumer is re-expressed against the four-state machine with an explicit seated-vs-non-retired choice recorded per call site (list in the DONE report)
- [x] #2 The two-state mapping (legacyRecordFromV2Object or its successor) is deleted; no production code path consumes the legacy view
- [x] #3 Behavior pins: cull/spawn/send/sidecar goldens still pass or are updated with the semantic change named
- [x] #4 gate green: go vet+test both modules, all check suites
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 1f28306 (8af91d2 refactor + d1ad72e fix). Legacy two-state view DELETED: legacyRecordFromV2Object + LegacyFromV2 gone; Status-free DecodeLegacyV1Raw (LegacyV1+Raw guarded) is the only v1 compat read; no production symbol maps v2 State to active/closed. Every consumer re-expressed with per-call-site seated-vs-non-retired classification (full table in worker DONE report, thread task091): seated = pane/terminal routing (send/spawn notify/compact self/fork live-parent/resume already-running/enroll v2 cleanup), non-retired = label lease + visibility (label owners, cull explicit selection, list default, reconcile eligibility, sidecar inverse guard). NAMED semantic changes, all pinned by goldens: (1) send refuses dormant coordinates with cause+remedy wording (refuse_legacy_active_term, refuse_v2_unseated_term); (2) fork/resume liveness narrowed to seated — dormant raw-v1-active row with coincidentally-live terminal no longer blocks (fork/resume dormant_live_terminal goldens; ratified-dormancy correct). enroll LegacyV1 protective raw-pane cleanup preserved (prevents carrySeatFields re-seat conflict). Fence extensions granted mid-unit after worker stop-and-report: listcmd, reconcilecmd, one-line write.go swap, enroll legacy re-plumb, renamecmd/adoptcmd coordinate reads. Opus adversarial review: APPROVE, 2 P3s fixed in delta round, delta APPROVE empirical (reviewer ran the printed remedies end-to-end). Gates: independent 53/53 both rounds, post-merge 53/53.
<!-- SECTION:NOTES:END -->
