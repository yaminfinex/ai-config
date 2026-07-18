---
id: TASK-281
title: >-
  grok bridge processes leak past seat death — cull/reap must stop the detached
  supervisors (+ double-start guard)
status: To Do
assignee: []
created_date: '2026-07-17 22:12'
updated_date: '2026-07-18 15:01'
labels:
  - herder
  - grok
dependencies: []
priority: medium
ordinal: 280500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Process audit finding: three dead seats (registry state gone; one culled cleanly the same day) each left their detached (ppid=1) grok bridge supervisor+child pair running 13-16h after seat death — 7 leaked processes total, including a DUPLICATE supervisor on one seat (the bridge can apparently double-start; second finding to pin). The bridge pairs were TERM-killed manually after verifying their seats stayed dead; clean exit on TERM.

Cause class: cull/seat-death does not stop the detached bridge supervisors. This is the crashed-agent-residue gap already ruled in doctrine (resource-release protocol: per-resource-class sweeps OUTSIDE herder core, guarded by owner-liveness + grace + no-live-client, never age alone) but no sweep exists for the bridge class. The bridge is herder-OWNED machinery (unlike agent-opened browsers/tunnels), so a stronger fix is also legitimate: cull/unseat of a grok seat should signal its own bridge directly, with the sweep as backstop.

Scope:
1. Teardown path: seat unseat/cull for a grok seat signals its bridge supervisor (TERM, bounded wait, KILL); bridge child follows supervisor.
2. Sweep backstop: a doctor/observer-adjacent sweep finds bridge processes whose --seat guid maps to a non-seated registry row, guarded by grace + re-verify, never age alone.
3. Double-start guard: bridge startup refuses/replaces when a supervisor for the same seat already runs (pin with a test); the observed duplicate suggests a race or retry path spawning a second supervisor.

4. (Added 2026-07-18, four field instances in one day) Completion gap — the birth-side sibling of the teardown gap: grok spawns ALWAYS refuse seat completion (joined_bus_row_missing) because grok seats have no sidecar and the refusal text's promise ('its sidecar will complete the seat automatically') is false for the family; seats then operate registry-rowless as bridge seats indefinitely. The bridge supervisor is the natural completion owner for the grok family: after its child joins the bus, it should drive the same canonical seat-completion path the sidecar uses (or the spawn refusal must stop promising sidecar completion for bridge families and name the real recovery). Family-honest refusal text either way.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Culling/unseating a grok seat stops its bridge supervisor+child within a bounded window (test-pinned)
- [ ] #2 Sweep identifies and reaps bridges for non-seated guids with grace + re-verification; never reaps a bridge whose seat is live (negative test)
- [ ] #3 Second supervisor for an already-bridged seat refuses or supersedes cleanly; no duplicate pairs (pin)
- [ ] #4 Grok spawn reaches a completed seated registry row via bridge-driven canonical completion (or the refusal contract is made family-honest and a supported completion path is documented + pinned)
<!-- AC:END -->
