---
id: TASK-244
title: >-
  herder launch boundary passes caller HERDER_*/HERDR_* through — direct launch
  from an agent shell acts as the caller
status: To Do
assignee: []
created_date: '2026-07-15 11:28'
labels: []
dependencies: []
ordinal: 243500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from the launch-env isolation unit (both reviewers converged; explicitly out of that unit's settled scope, which covered HCOM_* only). The launch boundary drops all ambient HCOM_* but deliberately passes HERDER_*/HERDR_* through, relying on the managed spawn path pre-exporting child-minted HERDER_GUID/ROLE/LABEL into the pane. The exposed path is a DIRECT 'herder launch <tool>' from an identity-bearing agent shell: the caller's HERDER_GUID/HERDER_LABEL/HERDR_PANE_ID inherit into the child, which then acts AS the caller for guid-keyed surfaces (mission verb caller identification, lifecycle provenance, enroll). The codebase already treats inherited HERDER_GUID as a hazard elsewhere (grok launcher refuses it; compact refuses on stale/inherited guid shapes). Fix shape (design checkpoint first): the boundary scrubs HERDER_*/HERDR_* unless the launch path explicitly provides child-minted values (spawn does); direct launch either mints fresh identity or refuses with cause+remedy when it detects caller-inherited identity it cannot re-own. Must not break: managed spawn pre-export, sidecar, print bypass, grok identity minting. Note: the isolation unit's tests assert the passthrough with child-guid naming — they are being re-framed in that unit's fix round so this fix will not read as a regression.
<!-- SECTION:DESCRIPTION:END -->
