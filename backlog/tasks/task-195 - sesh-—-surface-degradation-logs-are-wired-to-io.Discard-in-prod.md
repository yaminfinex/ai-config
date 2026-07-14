---
id: TASK-195
title: sesh — surface degradation logs are wired to io.Discard in prod
status: Done
assignee:
  - mika
created_date: '2026-07-13 20:22'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 194000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Side observation from the read/write-split work: surface.New defaults its logger to io.Discard and newSurfaceHandler passes no WithLogger option, so every degraded-render/lookup-failure line on the live store is dropped — a degraded homepage looks healthy in the journal. Likely a one-line wiring fix (surface.WithLogger over stderr/journal), plus deciding the log shape for session-scoped errors under the identifier-free journal contract (read/write-split design note); keep volume bounded (degradation events, not per-request chatter).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Surface degradation/render-failure events reach the service journal on the live deployment shape
- [x] #2 No session/file/logical identifiers in surface log output (same contract as debug timing); test pins it
- [x] #3 Docs current per decision-001
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 8b3288d (--no-ff, linear 20d03c3 -> 66ebf62 -> 8805dbb,
9 files), pushed; deployed live as sesh-v0.1.10 (store + release +
client). Post-deploy: surface at RTT floor, journal clean of warn/error
(nothing degraded — and degradations now reach it).

Bigger than filed: the previously-discarded lines embedded logical
session ids, file uuids, and raw URL paths — wiring them up verbatim
would have leaked corpus identifiers into journald retention. All call
sites rewritten to a fixed message vocabulary (route class / tool enum /
error class / counts; errors collapse to stable names or Go type names,
never raw strings); per-request aggregation bounds volume. Review then
found a LIVE raw-error attr on the projection rebuild-failure line
(bypassing the injected logger) — killed; all sqlstore journal lines now
flow through the injected logger under the gate. Contract gate: captures
actual emitted records, allowlists message+attr+level, shape-validates
error_class/panic_type values, proves detectors on identifier-carrying
shapes, drives a real rebuild failure and a real template-exec failure
whose error provably contains the seeded identifier (pre-install
self-check pins that premise), plus 16-way concurrent degraded-render
aggregation under -race.

Review: 4 findings (1 HIGH live leak, 2 MEDIUM incl. a closure-test
premise that proved nothing until corrected, 1 LOW race-evidence gap),
all closed with proven detectors. Final verdict APPROVE (reviewer ran an
exotic-driver leak probe and verified the corrected test 20x); merge-gate
battery 58/58 green post-merge.
