---
id: TASK-280
title: >-
  spawn bind window: codex joins now exceed the 60s default; undelivered prompt
  should ride the sidecar late-completion
status: To Do
assignee: []
created_date: '2026-07-17 22:06'
updated_date: '2026-07-17 22:16'
labels:
  - herder
  - dx
dependencies: []
priority: high
ordinal: 279500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field class, 10+ instances over two days, 100% of codex spawns affected under current machine load: codex children join the hcom bus at ~75-120s (measured from launch to ready event), past the 60s default bind window (HERDER_SPAWN_BIND_MS). The seat now self-heals via sidecar late-completion (merged), but the INITIAL PROMPT rides bus-first delivery inside the bind window — on timeout it is silently stranded and every operator must manually re-deliver the task over the bus after the child joins. This is the remaining pain of the class; the seat half is solved.

Two-part fix:
1. Prompt hand-off: when spawn refuses on bind-timeout with a running child pane, the pending initial prompt is persisted for the sidecar, which delivers it (verified, receipt-checked) immediately after it completes the seat — making the late-join path fully self-healing end to end. Refusal text updates to promise both ("sidecar will complete the seat AND deliver the prompt"). Idempotence: if the spawn caller re-delivers manually first, the sidecar delivery must not double-submit (dedupe on content or a delivery marker; blind double-submit is the known hazard).
2. Window default: raise the non-claude default bind window based on measured join latency (claude joins <5s; codex 75-120s under load). Family-aware default or a single higher default — decide at design checkpoint with the measurement data; the env knob stays as override.

Operational mitigation until this lands (broadcast to fleets): HERDER_SPAWN_BIND_MS=300000 on codex spawns.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Measured repro fixture: child joins after the window; spawn refuses; sidecar completes seat AND delivers the stranded prompt verified; refusal text names both behaviors
- [ ] #2 Manual-redelivery-then-sidecar-delivery does not double-submit (dedupe pinned by test)
- [ ] #3 Non-claude bind window default raised per measurement or made family-aware; env override still wins; claude path unchanged
- [ ] #4 Sidecar late-completion failure mode diagnosed and fixed: live repro observed where the child env carried guid+instance-name+process-id, the bus row was joined (7+ min), the sidecar ran, and NO completion occurred — window extension alone is insufficient for this shape
- [ ] #5 Sidecar gains minimal decision-point observability (stderr log lines: scan results, correlate outcomes, completion attempts/refusals) — every sidecar log observed in the field was 0 bytes
<!-- AC:END -->
