---
id: TASK-033
title: >-
  herder spawn: post-write capture loop still tag+cwd-enriches the registry row
  (addressing metadata can mislabel)
status: In Progress
assignee:
  - unit-u-fila
created_date: '2026-07-07 21:46'
updated_date: '2026-07-08 01:22'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Residual flagged by Unit R during the TASK-032 P1 fix (out of that fix scope by design): the prompt-delivery bind gate now trusts child-specific signals only (222b1bb), but the POST-WRITE capture loop that enriches the registry ROW with the hcom name still uses the tag+cwd unique-match fallback (pre-existing since wave 1, visible in the bind_stale_tagcwd golden). Consequence: with a stale same-tag+cwd agent on the bus, a new row can be enriched with the OLD agent bus name — no prompt misdelivery (the gate is fixed), but later manual `herder send <guid>` resolves the row and messages the wrong session. Fix direction: apply the same child-specific discipline (this-guid sidecar enrichment / frozen-launch-pane roster match) to row enrichment, or leave the name empty for sidecar enrichment to fill and never guess. Check whether sidecarcmd already corrects a wrong guess later (it enriches by pane launch context — may self-heal; establish and pin either way).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Row enrichment never records a bus name from a tag+cwd-unique guess; regression scenario with a stale same-tag+cwd agent pins the row left empty (or child-verified) — extend bind_stale_tagcwd golden
- [ ] #2 Established + pinned whether sidecar enrichment self-heals a wrong row name; herder send to a stale-enriched row either resolves correctly or refuses
- [ ] #3 Pinned gate green (go vet/test + full battery)
<!-- AC:END -->
