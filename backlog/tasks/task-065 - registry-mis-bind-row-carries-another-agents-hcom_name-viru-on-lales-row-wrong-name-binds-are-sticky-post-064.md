---
id: TASK-065
title: >-
  registry mis-bind: row carries another agent's hcom_name (@viru on lale's
  row); wrong-name binds are sticky post-064
status: Done
assignee:
  - hera
created_date: '2026-07-08 08:12'
updated_date: '2026-07-12 07:49'
labels: []
dependencies: []
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
INVESTIGATION: a registry row can carry another agents hcom_name, and the wrong bind is STICKY. Field evidence (lale, market-sim run, rows preserved as repro): row lale-orchestrator (a9fcee3d) carried hcom_name=viru while the agent answered as @lale; after that row fully unseated, a fresh successor row (8f1d10a3) seated on the same terminal STILL carried hcom_name=viru — the mis-bind survives row succession, so whatever seats successor rows carries the poisoned name forward instead of re-resolving live bus identity. Observed impact: compact --then continuation addressed the wrong agent.

Investigation angles: (1) which path minted the original wrong bind (reconcile re-bind with multiple candidates? sidecar name enrichment racing?); (2) the carry-forward path that preserves it across succession — post-064 carry semantics faithfully preserve a wrong hcom_name, and nothing consults live bus identity (same disease as TASK-043 enroll-trusts-env); (3) repair affordance — rename fixes labels not hcom_name, sidecar owns hcom_name but orchestrator/manual rows may have no sidecar; decide between an owned-field re-capture path, a herder rebind-with-verification verb, or an observer-lane fix (the TASK-080 observer already witnesses bus identity; a hcom_name mismatch between live bus and row is squarely an observation). Evidence to pull at fix time: both rows full history from lales registry.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 11:31
---
lale field data (#11888): wrong-name carry-forward confirmed STICKY across a fresh seat — original row a9fcee3d fully unseated (seat null, live gone); a fresh row lale-orchestrator-2 (8f1d10a3) seated on lale's exact terminal at 11:29:21 (not orchestrator-initiated — presumed reconcile/sidecar re-recognition) and the NEW row still carries hcom_name=viru. So the mis-bind survives row succession, not just row lifetime; whatever seats a successor row carries the poisoned hcom_name forward instead of re-resolving live identity. Old rows a9fcee3d/edea1564 left untouched as repro evidence. Related mechanism note: TASK-043 (enroll trusts stale env) and the systemic-review observer-blind-spot finding — the carry-forward path likewise never consults live bus identity.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause of the original wrong bind identified from the preserved rows history (named path, not speculation)
- [x] #2 The succession carry-forward path that preserves wrong names is identified and its fix decided (re-resolve live identity at seat time, or refuse to carry unverified hcom_name)
- [x] #3 Repair affordance decided (existing verb / new verb / observer lane) with filed-ready follow-up task text if build work results
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED in identity-integrity unit A1, merged a1c5acd (with TASK-043). AC1 root cause: minting site is enrollcmd.Run copying launch-time HCOM_INSTANCE_NAME (named by the compact-then investigation memo docs/design/2026-07-12-compact-then-proof-failure-investigation.md from the preserved lale/viru rows — launch wrapper exported the stale name, enroll copied it, hourly reconciles carried it). AC2 carry paths fixed: registered-row carry, observer SID-turnover/hourly reconfirm, reconcile --apply, and pane-correlated sidecar enrichment now re-verify hcom_name against the live bus or explicitly mark hcom_verified:false; new additive registry field hcom_verified (*bool, omitempty — old rows validate, nil-safe). Poison no longer survives succession. Reconcile distinguishes roster UNAVAILABLE (hcom list error → no writes, no mass downgrade) from unresolvable (genuine downgrade). AC3 repair affordance: re-enroll on the existing guid with live verification + SID corroboration (decided verb shape: no new verb, existing enroll is the honest surface). Defense in depth: compact --then arm-time preflight proves the stored name belongs to the CALLING session; stopped-wrong-name and live-neighbor both refuse with cause + herder enroll remedy (misdelivery-worse-than-drop honored). Adversarial review round-1 F1-F5 fixed, delta APPROVE; both batteries green.
<!-- SECTION:NOTES:END -->
