---
id: TASK-064
title: >-
  post-A2 projection: spawn registration masks sidecar hcom_name enrichment
  (TASK-045 symptom returns)
status: In Progress
assignee:
  - vibe
created_date: '2026-07-08 07:38'
updated_date: '2026-07-08 07:46'
labels: []
dependencies: []
ordinal: 64000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
LIVE on main, found by vibe on first post-A2-merge codex spawn (#8318, guid a27c2465, deterministic repro). Evidence: row1 event=recognised 07:33:39 carries seat.hcom_name task049-deli (045 sidecar correlation WORKED, enriched through the A2 locked writer); row2 event=registered 07:33:53 (spawn final registration) has NO seat.hcom_name; latest-row-per-guid projection takes row2 -> herder list BUS=-, herder send cannot resolve, spawn reported hcom_capture:not_found. Original TASK-045 symptom returns, new cause: later partial-seat append masks earlier enrichment in the collapse.

Two defects, both A2-path: (1) spawn final registration does not merge/carry forward seat fields already recorded for the guid — violates the owned-fields spirit (a registration should not clobber enrichment it does not own; note A2 reviewer audit assumed spawn builds a COMPLETE fresh seat, untrue when capture misses). (2) spawn capture check concluded not_found although the enrichment was in the registry 14s earlier — field-shape mismatch post-v2, or check-once-no-re-read after the registration append.

Fix direction pending spec-ravu ruling (requested): EITHER projection resolves seat sub-fields event-sourced (most recent row that HAS the field) OR writers must read-modify-write carrying forward seat fields inside the flock (extend carrySeatFields discipline to registered events). Capture check re-reads after final append either way.

NOT a reopen of TASK-056: A2 shipped through its gate; this is an integration defect between 045-enrichment and A2-projection semantics that no existing test seeds (sidecar-enriches-BEFORE-spawn-registers ordering). Regression test must pin exactly that ordering.

Sequencing: dispatch via vibe (their find, their worker). Conflict fence: wave A3 (in flight, wave-a3-node-gate) also touches registry/write.go — second lander takes conflict-check + regate, same rule as F1/A2. Interim workaround: hcom send to bus name from hcom list (unblocks any run today).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 07:41
---
[hera 2026-07-08] SPEC RULING (spec-ravu #8407, normative): fix is B-centralised; A (field-level event-sourced projection) is spec-ILLEGAL — §5.1 declares snapshot-per-event, and rotation reseed (latest row per non-retired guid) is only legal because rows are self-contained; event-sourced fields would be destroyed by reseed. §5.2 correct reading: writers may only CHANGE owned fields but every appended row is a FULL snapshot; non-owned fields carry forward from the projection read under the same flock. The bug is a straight §5.2 violation (mirror image of stale-enrichment-cannot-revert-rename). FIX FENCE: (1) merge lives ONCE in the shared locked append helper — (guid, event, owned-field patch); helper overlays patch on projected row under flock, appends full snapshot; (2) absence-in-patch = carry forward, NEVER clear; clearing is an owned op (unseat clears seat{} explicitly); (3) hcom_name stays sidecar-owned (TASK-043); spawn omitting it is correct, helper carries it; (4) tests: recognised+name then registered-without keeps name/bus-reachable; rename-revert mirror stays green; rotate-reseed after mixed sequence keeps name. Second Q: recognised-before-registered is a legitimate race; spawn final registration is legal IFF its patch changes projected state, else §5.2 mandates idempotent NO-OP (no append); never in-place update; no third mode. Spec erratum staged on herder-spec branch c3dbc5e — fold into next spec-touching merge with owner blessing. Defect 2 (capture check never re-reads) rides in the same unit per the ticket.
---

created: 2026-07-08 07:46
---
[hera, from vibe #8663] Dispatched: codex worker task064-tori, worktree task-064-seat-merge, brief spec-ruling-faithful (merge ONCE in normalizeSessionAppend as event+owned-field-patch overlay; absence=carry-forward; per-sub-field seat merge; no-op registration; 4 ruling tests; fenced to internal/registry; A3 conflict warning included). Defect 2 SHARPENED by vibe recon: spawn already re-reads after final append (registryCapturedName, 6x700ms loop, spawn.go:976) — it read the masked projection, so the carry-forward fix resolves defect 2 with NO spawncmd change; brief requires a test on exactly the read path spawn uses. Pipeline: vibe review -> hera gate -> adversarial review (engine diff, mandatory) -> merge; second lander vs A3 takes conflict regate.
---
<!-- COMMENTS:END -->
