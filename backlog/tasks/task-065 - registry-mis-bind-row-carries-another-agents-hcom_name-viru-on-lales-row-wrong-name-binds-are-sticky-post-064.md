---
id: TASK-065
title: >-
  registry mis-bind: row carries another agent's hcom_name (@viru on lale's
  row); wrong-name binds are sticky post-064
status: To Do
assignee:
  - hera
created_date: '2026-07-08 08:12'
updated_date: '2026-07-12 06:52'
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
- [ ] #1 Root cause of the original wrong bind identified from the preserved rows history (named path, not speculation)
- [ ] #2 The succession carry-forward path that preserves wrong names is identified and its fix decided (re-resolve live identity at seat time, or refuse to carry unverified hcom_name)
- [ ] #3 Repair affordance decided (existing verb / new verb / observer lane) with filed-ready follow-up task text if build work results
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reported by lale (market-sim run, #9332, low priority): registry row lale-orchestrator (guid a9fcee3d) carries hcom_name @viru while the agent's actual bus name is @lale; the @lale row points at long-gone manual session edea1564. Suspected reconcile mis-bind. Observed impact: herder compact --then continuation addressed @viru instead of the orchestrator (harmless that run only because the primary wake was a direct worker report).

Investigation angles: (1) reconcile re-bind path — can assumed-continuity re-binding attach the WRONG live agent's coordinates/name to a row (or vice versa) when multiple candidates exist? TASK-046 reconcile refuses ambiguity all-or-nothing, but name enrichment happens separately via sidecar; (2) post-064 carry semantics can now FAITHFULLY PRESERVE a wrong hcom_name once recorded — carry-forward makes a bad bind sticky, raising the cost of mis-binds (correct wrong names needs an owned-field write from the name owner, i.e. sidecar re-capture; verify that path exists for orchestrator rows without a sidecar); (3) is there a repair verb? rename fixes labels, not hcom_name; sidecar owns hcom_name (TASK-043) but manual/orchestrator sessions may have no sidecar to re-capture. May need herder-level "rebind bus name with verification" or reconcile extension. Related: TASK-060 (F1/F2 reconcile polish). Evidence to collect at fix time: the two rows' full history from lale's registry.

CARRY-FORWARD CONFIRMED (2026-07-12 investigation): the successor row (8f1d10a3) carried hcom_name=viru through HOURLY RECONCILE EVENTS even after the original row unseated — carry semantics faithfully preserve the poison and nothing re-consults live bus identity. Minting site is enroll (TASK-043); this task owns the carry/reconcile re-verification + repair affordance. Dispatching in identity-integrity unit A1.
<!-- SECTION:NOTES:END -->
