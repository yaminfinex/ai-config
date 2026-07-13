---
id: TASK-068
title: 'herder send: resolve seat.hcom_name (bus name) as a send key'
status: To Do
assignee: []
created_date: '2026-07-08 10:03'
updated_date: '2026-07-13 01:05'
labels:
  - herder
  - dx
dependencies: []
priority: low
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herder send resolves guid / label / terminal / pane — but NOT seat.hcom_name (the bus name). Live hit during TASK-063 round 3: `herder send task063-taro` refused because the registry label is task063-6cf471f0 while the bus name is task063-taro; guid worked. Operators think in bus names (that is what hcom list shows and what @-mentions use), so send should accept hcom_name as a resolution key, with the same ambiguity refusal discipline as label resolution (duplicate hcom_names across live rows must refuse, not pick).

Reported by vibe during TASK-063 round-3 hand-back (bus #10730).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder send <bus-name> resolves via seat.hcom_name (repro from the description passes: send by bus name while the label differs)
- [ ] #2 duplicate hcom_name across live rows refuses with candidates named (same discipline as label ambiguity)
- [ ] #3 resolver order documented in send --help; suite covers resolution and refusal
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13 staleness audit: KEEP, capture real. Resolve matches guid/short-guid/label only (registry.go:319-328); send help/refusal omits hcom_name (send.go:139,303-318). The HERDER_BUS=hcom debug literal path is NOT hcom_name resolution (no namespace routing, no duplicate-name refusal).
<!-- SECTION:NOTES:END -->
