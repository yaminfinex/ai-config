---
id: TASK-089
title: >-
  observer: reconfirm interval default 60m -> 4h (owner cadence ruling at spec
  blessing walkthrough)
status: Done
assignee: []
created_date: '2026-07-09 04:15'
updated_date: '2026-07-10 21:19'
labels: []
dependencies: []
priority: low
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER RULING (2026-07-09, spec blessing walkthrough): the periodic re-confirmation row class stays, but the default interval moves from 60m to 4h — slower registry growth was preferred over per-hour freshness; liveness questions between re-confirmations lean on the observer status file. Mechanism unchanged and blessed: when a sweep finds a live seat whose seat.confirmed_at is older than the interval, it appends a fresh reconciled row.

CHANGE: tools/herder/internal/observercmd/observer.go:26 defaultReconfirmInterval = time.Hour -> 4 * time.Hour. Check reconfirmInterval() for the override path (env/config) and make sure the override is documented wherever observer configuration is described (observer --help and/or the design doc pointer). The spec text does not hard-code the interval (verified at blessing time) — no spec change needed. Docs that mention 60m as default (design doc §5.3) are historical records of the design pass; do not rewrite them, the code + help text are the living truth.

Tiny, bundle-eligible into any observer-adjacent unit.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 defaultReconfirmInterval is 4h; a seat confirmed less than 4h ago produces no reconfirm candidate, one older does (test or golden pins the boundary)
- [ ] #2 the override mechanism is verified working and documented in observer help output
- [ ] #3 gate green: go vet+test both modules, all check suites
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CLOSED by owner re-ruling (2026-07-10, chat): "1h is fine" — the current time.Hour default STAYS; the 60m→4h change is dropped. Basis: the 16h observer bake appended ZERO reconfirm rows to the registry (confirmations ride the status file), so the row-volume concern behind the original 4h ruling proved empty. No code change; ACs moot. (ACs verified manually as moot — task closed without implementation by design.)
<!-- SECTION:NOTES:END -->
