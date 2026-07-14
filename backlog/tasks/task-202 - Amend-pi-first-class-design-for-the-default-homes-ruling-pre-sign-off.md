---
id: TASK-202
title: Amend pi first-class design for the default-homes ruling (pre-sign-off)
status: Done
assignee: []
created_date: '2026-07-14 01:47'
updated_date: '2026-07-14 02:52'
labels: []
dependencies: []
ordinal: 201000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TYPE: design (docs-only). OWNER RULING 2026-07-14 (standing-orders 20.8): pi seats use the DEFAULT pi home and default install location, vendor-updated; the managed-home/isolated-prefix provisioning in docs/design/pi-first-class-design.md (currently at f1af746) must be amended BEFORE owner sign-off. Settled decisions (do not relitigate): default home ruled; DR-2 keep-custom delivery decision stands (docs/design/2026-07-14-hcom-native-pi-characterization.md, merged a1e8c49 — delivery is orthogonal to home location); credential scoping in launch-time env construction is RETAINED (ruling does not revoke it); same-UID concession framework (owner item 8) unchanged. SCOPE: (1) rewrite the provisioning/managed-home surface (DR-3 territory) to default-home + recorded vendor version; (2) sweep the FULL document for load-bearing managed-home references (launch contract, threat model, T34 branches, A-register, operator-capability file location under the operator real home — likely unchanged but verify wording) and reconcile; (3) HONESTY DUTY: anywhere default-home WEAKENS a previously stated property, state the delta explicitly in the owner-decision section so sign-off covers it — do not silently relabel; (4) update the status header (amendment round, date). OUTPUT: docs-only commit(s) on the unit branch; adversarial review orchestrator-dispatched (cross-family + calibration); owner signs off on the amended head.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
MERGED 73da70e (docs-only, one file, 523+/224-). Round 10 (0fae1a6) then FOUR fix rounds under incumbent codex re-cert + grok calibration:
- R1 (6 items): default-home fidelity gaps.
- R2 (3 items): activation predicate broke the memory-lost reload branch.
- R3 (2 items): reload branch re-opened a tokenless mutation forgeable by a model-tool child — closed by deleting the branch (provenance-indistinguishable from a model-tool child, so no process-identity proof authorizes a write).
- R4 (1 item, ganu P1 orchestrator-confirmed against the artifact): the round-3 control-degraded-from-authenticated-silence derivation contradicted T29 (hung driver = never-resolving await, NO failure-path writer), so no record/threshold could distinguish lost-token from hung-driver. CLOSED BY DELETION: control-degraded retired from the vocabulary entirely; both stale-lease causes collapse to lease-derived driver-degraded with one controlled-relaunch recovery; indistinguishability stated plainly for owner sign-off.
Final APPROVE at 02e5dc5 (ganu re-cert; orchestrator grep-verified retirement). Owner item 9 a-e honesty register intact. Reviews: review-202-brief.md. NEXT: owner SIGN-OFF of the amended design (owner-desk item 1b) — pi U1 gates on it.
<!-- SECTION:NOTES:END -->
