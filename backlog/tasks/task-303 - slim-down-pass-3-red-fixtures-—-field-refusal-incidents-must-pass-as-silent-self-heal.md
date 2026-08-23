---
id: TASK-303
title: >-
  slim-down pass 3: red fixtures — field refusal incidents must pass as silent
  self-heal
status: Done
assignee: []
created_date: '2026-08-21 04:40'
updated_date: '2026-08-21 05:25'
labels:
  - herder
  - slimdown
dependencies:
  - TASK-301
  - TASK-302
references:
  - napkins/herder-slimdown-charter.md
priority: high
ordinal: 302500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Wayfinding pass 3 (charter: napkins/herder-slimdown-charter.md). Build red fixtures from the field incidents that must pass as *silent self-heal* under ground-truth resolution — the old refusal transcripts become the test names: TASK-268's adopt circle (adopt refuses on the adoptee's own held label), TASK-041's compact self-location dead-end after pane renumbering, TASK-262's uncorroborated stored bus name (spawn-dead but fully alive). Each fixture reproduces the recorded refusal state and asserts: occupant probe matches → rebind stale coordinates from observation, append evidence to registry, proceed silently. Negatives stay refusing (positive mismatch, no occupant, multi-match). TASK-268 and TASK-041 resolve by deletion via these fixtures.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Red fixture per incident (268 adopt circle, 041 compact self-location, 262 uncorroborated bus name) reproducing the recorded refusal shape
- [x] #2 Keep-list negatives pinned: positive mismatch and no-occupant still refuse; multi-match fails closed
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Delivered 2026-08-21: tools/herder/tests/red-check-slimdown-fixtures.sh + napkins/red-fixtures-notes.md. Wayfinder re-ran the suite end-to-end: 3 reds reproduce the recorded refusals (268 circular label refusal byte-shape; 041 'terminal term_ME is not live…cannot locate your own pane'; 262 'sender identity is not verified…Nothing was launched'), 5 keep-list greens hold (foreign-label refusal, temp-label adopt workaround incl. full adopt composite rc=0, compact positive-mismatch, compact pane-gone, spawn multi-match fail-closed), exit 1 RED AS DESIGNED. Suite carries red- prefix (battery discovery is the check-*.sh glob per tests README; no runner script exists) — renaming to check-*.sh is the single act that wires it in once green; that rename belongs to the final pass-4 task. Flip mechanics pre-verified per fixture (notes file). Probe substrate pre-seeded: cohort transcripts under fake HOME, synthetic proc root via proposed hook HERDER_PROBE_PROC_ROOT (hook name needs confirming in the probe implementation task), process_info mock answers per observer-suite precedent. NOTE for pass 4: fixtures 268-red currently shows an extra nonfatal enroll warning prefix vs the recorded transcript; 262/041 wording drifted post-TASK-046/294 — drift documented in the notes file.
<!-- SECTION:NOTES:END -->
