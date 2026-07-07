---
id: TASK-003
title: 'herder: kill the herdr keystroke delivery transport — hcom is THE transport'
status: In Progress
assignee: []
created_date: '2026-07-07 05:37'
updated_date: '2026-07-07 07:42'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
DECISION (user, locked): herder message delivery has exactly one transport, the hcom bus. The herdr keystroke fallback is removed — "why be half pregnant". A target that cannot resolve to a bus-bound agent gets a clear hard error, not typed keystrokes. Ported from the parked napkin brief (napkins/herder-go-port/parked-keystroke-kill-brief.md on the origin machine — essentials embedded here since napkins do not travel).

PHASE A — map first, no code: every path reaching TransportHerdr (driver/selection.go, driver/herdr.go, send/send.go; callers: spawncmd, lifecyclecmd, waitcmd). What resolves herdr-only today (terminal ids, pane ids, bash agents, unregistered panes) and what each becomes (resolve-to-bus vs refuse). Boot-time initial prompt delivery rides the spawn paste path, NOT the delivery driver — out of scope, verify and state so.

PHASE B — the cut: remove TransportHerdr and keystroke delivery; if Selection collapses to a one-transport shell, delete the abstraction and call hcom directly (keep herdr-side RESOLUTION helpers mapping guid/label/terminal -> registry row -> hcom name). send: bus-only, hard error naming what was tried. Notify: ALREADY PARTLY DONE since the brief was written — notify is bus-native via resolveSpawnerBus, keystroke ring survives only for bus-less spawners; remaining work is removing that ring. Regenerate affected goldens (expect check-hcom-contract bus-less cases -> refusals, send/spawn/wait goldens, help text) reviewing EVERY diff.

STALENESS corrections vs the original brief: skills/herder is DELETED — the two-transport doc now lives at tools/herder/docs/delivery-drivers.md (rewrite single-transport or delete, deletion preferred if nothing unique survives); suites number 16 not 15; send --help HERDER_BUS override wording (auto|herdr|hcom) must lose its herdr arm.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 git grep TransportHerdr returns zero; no keystroke-delivery code remains in tools/herder (resolution helpers exempt)
- [ ] #2 herder send at a non-bus target refuses cleanly with correct exit code — live negative smoke, no keystrokes typed
- [ ] #3 Notify fully bus-native, no terminal-id ring remnants (grep-gate HERDER_NOTIFY_TO semantics)
- [ ] #4 delivery-drivers.md rewritten or deleted; every regenerated golden diffed and justified; 16 suites + go gates green
<!-- AC:END -->
