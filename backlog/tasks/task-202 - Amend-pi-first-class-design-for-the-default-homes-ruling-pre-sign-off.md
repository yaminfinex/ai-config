---
id: TASK-202
title: Amend pi first-class design for the default-homes ruling (pre-sign-off)
status: To Do
assignee: []
created_date: '2026-07-14 01:47'
labels: []
dependencies: []
ordinal: 201000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TYPE: design (docs-only). OWNER RULING 2026-07-14 (standing-orders 20.8): pi seats use the DEFAULT pi home and default install location, vendor-updated; the managed-home/isolated-prefix provisioning in docs/design/pi-first-class-design.md (currently at f1af746) must be amended BEFORE owner sign-off. Settled decisions (do not relitigate): default home ruled; DR-2 keep-custom delivery decision stands (docs/design/2026-07-14-hcom-native-pi-characterization.md, merged a1e8c49 — delivery is orthogonal to home location); credential scoping in launch-time env construction is RETAINED (ruling does not revoke it); same-UID concession framework (owner item 8) unchanged. SCOPE: (1) rewrite the provisioning/managed-home surface (DR-3 territory) to default-home + recorded vendor version; (2) sweep the FULL document for load-bearing managed-home references (launch contract, threat model, T34 branches, A-register, operator-capability file location under the operator real home — likely unchanged but verify wording) and reconcile; (3) HONESTY DUTY: anywhere default-home WEAKENS a previously stated property, state the delta explicitly in the owner-decision section so sign-off covers it — do not silently relabel; (4) update the status header (amendment round, date). OUTPUT: docs-only commit(s) on the unit branch; adversarial review orchestrator-dispatched (cross-family + calibration); owner signs off on the amended head.
<!-- SECTION:DESCRIPTION:END -->
