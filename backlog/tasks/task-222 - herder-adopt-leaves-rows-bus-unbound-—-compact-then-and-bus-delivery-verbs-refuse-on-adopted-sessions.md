---
id: TASK-222
title: >-
  herder adopt leaves rows bus-unbound — compact --then and bus-delivery verbs
  refuse on adopted sessions
status: Done
assignee: []
created_date: '2026-07-15 01:22'
updated_date: '2026-07-15 03:27'
labels: []
dependencies: []
ordinal: 221500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
URGENT (owner-filed) research-then-fix, HIGH PRIORITY. Live incident, evidence preserved on registry row guid 7ef0b17d:

SYMPTOM CHAIN (verbatim from the affected orchestrator seat): a pane was manually adopted via `herder adopt` after a pane failure; the adopted registry row carries NO bus binding; manual enrollment does not back-fill it; even reclaiming the seat via `hcom start --as <name>` did not bind the ROW (the bus name went live but the registry row still shows no bus coordinates). Consequence: `herder compact --then` refused TWICE (--then requires a bus-bound row to deliver the continuation), leaving only --stop with an embedded steer — the session sat idle until manually nudged. Any other verb that resolves bus delivery from the registry row (herder send fallback paths, spawn-style continuation delivery) is presumably equally broken for adopted rows.

SCOPE:
1. RESEARCH: characterize where adoption (herder adopt) and recognition (hcom start --as reclaim, sidecar enrichment, reconcile) each get/miss bus coordinates on the row; identify the missing back-fill point. Read the live row 7ef0b17d as evidence — READ ONLY, do not mutate live fleet rows during research.
2. FIX: adopted rows must become bus-capable — either bind at adoption when the target is already enrolled, or back-fill bus coordinates at the first recognition event after the bus name goes live (recognition is likely the right seam: adoption can precede enrollment). The fix must cover the demonstrated sequence exactly: adopt -> hcom start --as -> row is bound.
3. REFUSAL HONESTY: compact --then's refusal on an unbound row must state cause + remedy (name the back-fill/recognition step or the repair command), not just refuse.
4. REPAIR PATH: deliver a verified procedure (or automatic heal) for existing unbound adopted rows — applied to 7ef0b17d as the acceptance demo, coordinated with the row's owner before touching it.

Regression tests pinning the adopt->reclaim->bound sequence + the refusal text. Full house battery + adversarial review. No agent names/task numbers in durable strings.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 02971fe at head 1a3a97c. Root cause: adopt enrolled replacements before a hand-resumed process had any identity proof, and reclaim never appended the live bus row to the replacement GUID. Fix: adoption-time binding — source-row durable SID harvested first, operation-scoped hcom start --as as proof, exactly-one-joined-row fail-closed guard, and (review-driven F1 blocker) resumed-SID claims now REQUIRE caller authorization: pane match, or --confirm-resumed-session for pane-less rows, preflight + post-reclaim recheck both mutation-pinned. hcomidentity.Resolve proof classes untouched (additive helper only). Refusal prints cause + exact pinned re-enroll remedy + where to find the transcript SID; compact --then verifies repaired rows from the recorded SID; --dry-run proves zero mutating calls. Upstream husk behavior ledgered in TASK-029 as HYPOTHESIS (no standalone repro). LIVE incident row was never mutated — owner repair runbook is the printed re-enroll remedy. Grok calibration seat ran (ledger row 13). Post-merge battery 60/60 green, pushed in the 485ec9f train.
<!-- SECTION:NOTES:END -->
