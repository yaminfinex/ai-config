---
id: TASK-268
title: >-
  Adopt enroll-leg refuses on the adoptee's own held label — circular refusal
  whose remedy is the refusing command
status: Done
assignee: []
created_date: '2026-07-17 02:31'
updated_date: '2026-08-21 05:00'
labels:
  - herder
dependencies:
  - TASK-303
priority: medium
ordinal: 267500
---

## Teardown reconciliation — 2026-08-24

Closed by deletion as previously ruled. Adopt/enroll retired and continuity is
`hcom r <name-or-uuid>`; the composition suite records the incident 268
successor.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-proven 2026-07-17 (fleet escalation, verbatim refusal on record): running the exact adopt command that adopt/enroll refusals prescribe, from the replacement pane, fails in the enroll leg with 'label X is held by guid <adoptee> in state unseated (dead/unseated); from the replacement pane run herder adopt <adoptee>...' — the label holder IS the adopt target, and the refusal's first suggested remedy is the very command that just refused. This bites every degraded row whose session still carries its spawn-time HERDER_LABEL (the common case: env label == stored label), i.e. precisely the rows adopt exists to replace. Second defect in the same refusal: its alternate remedy (retire-then-rename) is a trap — adopt refuses retired targets, and a bare enroll after retire attempts guid reuse of the retired guid and refuses too, so following the printed remedy forecloses the designed recovery permanently.

Fix shape: the enroll leg's label-conflict check must treat a label held by the ADOPT TARGET as transferable (the composite's take-label leg atomically moves it two legs later); negatives stay refusing (label held by any OTHER row, seated or not). Refusal-text hygiene: remedies must be executable from the refusing state (no self-prescription; drop or fix the retire-then-rename branch for adopt-reachable rows). Field workaround validated by contract reading (confirm in fixture): HERDER_LABEL=<temp> override on the adopt invocation sidesteps the conflict and the take-label leg restores the real label.

This is a live instance of the repair-circularity class the identity design lane (task-267) is chartered on; the fix here is tactical and must stay inside the keep-list fences (no label theft from live rows, atomic take unchanged).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Red-first fixture reproduces the field refusal (adopt target holds the env label, row unseated) and heals under the fix; label held by any other row still refuses
- [ ] #2 Adopt refusal remedies are executable from the refusing state: no remedy prescribes the refusing command; retire-then-rename no longer suggested where it forecloses adopt
- [ ] #3 HERDER_LABEL temp-override workaround pinned by a fixture as a supported path (take-label leg restores the stored label)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Re-scoped 2026-08-21 by the slim-down charter (napkins/herder-slimdown-charter.md decision 4): resolves BY DELETION, not by the fix-shape in this description. The enroll-leg repair fences that create the circularity (enroll.go:199-201, 225, 518-526 at HEAD ada6bf7 — see deletion map napkins/herder-slimdown-deletion-map.md §2/§4) are deleted outright; adopt's composition survives with occupant-probe evidence. This task's field refusal transcript becomes a pass-3 red fixture (TASK-303) that must pass as silent self-heal; close this task when that fixture is green under the deletion. Do NOT implement the transferable-label fix described here.
<!-- SECTION:NOTES:END -->
