---
id: TASK-281
title: >-
  grok bridge processes leak past seat death — cull/reap must stop the detached
  supervisors (+ double-start guard)
status: Done
assignee: []
created_date: '2026-07-17 22:12'
updated_date: '2026-07-18 16:40'
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
- [x] #1 Culling/unseating a grok seat stops its bridge supervisor+child within a bounded window (test-pinned)
- [x] #2 Sweep identifies and reaps bridges for non-seated guids with grace + re-verification; never reaps a bridge whose seat is live (negative test)
- [x] #3 Second supervisor for an already-bridged seat refuses or supersedes cleanly; no duplicate pairs (pin)
- [x] #4 Grok spawn reaches a completed seated registry row via bridge-driven canonical completion (or the refusal contract is made family-honest and a supported completion path is documented + pinned)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to main at 41ce8cd (lifecycle commit + edge-hardening fix round). Post-merge gate 63/63 + 4 module passes.

AC#1: cull/manual teardown quiesces then TERM-wait-KILLs the verified supervisor incarnation (start-time+argv+pgid fence defeats pid reuse), explicitly terminates non-PG-leader children by exact pid, and verifies the bridge socket no longer accepts clients as a post-condition — teardown consumes committed liveness verdicts or explicit cull only.

AC#2: observer-adjacent sweep auto-reaps only row-confirmed non-seated grok rows after grace + same-incarnation reverify + zero-client recheck; live-seat and live-client shapes refuse (negative-pinned); ROWLESS bridge pairs are report-only with the exact manual recipe and get a persisted 15s birth grace so healthy launching seats are never reported; age is never evidence.

AC#3: per-seat lifetime flock; concurrent losers reuse or exit with a typed already-running refusal; one-supervisor-one-child pinned under concurrent start.

AC#4: bridge supervisor drives canonical seatcompletion (authoritative BaseName, admitting evidence byte-unchanged, in-lock race convergence pinned via injected projection) for spawn AND fork births (fork carries pane/session/provenance coordinates), then the existing pendingprompt locked-marker handoff exactly once; resume honestly names manual enroll (no armed owner); all former sidecar-promise strings grok-branched and pinned.

Review: dual adversarial (opus + grok calibration — the grok seat reviewing its own seat machinery), one six-item batched round (findings fully complementary: grok seat took contract-honesty + fork-owner P1s; incumbent took teardown post-condition + birth-window hazards), dual delta APPROVE with independently re-executed red-first pins. Builder self-caught a test-discipline violation mid-battery and restarted the full run unprompted.

Field note: the next grok spawn on a rebuilt herder validates bridge-driven completion live — until then the four historical refusal instances stand explained.
<!-- SECTION:NOTES:END -->
