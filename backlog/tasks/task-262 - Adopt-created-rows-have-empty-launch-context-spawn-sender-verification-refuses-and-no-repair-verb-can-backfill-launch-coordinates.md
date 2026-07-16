---
id: TASK-262
title: >-
  Adopt-created rows have empty launch_context: spawn sender verification
  refuses, and no repair verb can backfill launch coordinates
status: To Do
assignee: []
created_date: '2026-07-16 09:20'
labels:
  - herder
dependencies: []
priority: high
ordinal: 261500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER DIRECTIVE (2026-07-16): HIGHEST PRIORITY BY FAR — a peer orchestrator is spawn-dead in the field on this defect; operational seat repair executed same night, this task fixes the class.

Live outage, peer orchestrator, blocking their dispatch. A session recovered via `herder adopt <old-guid> --confirm-dead` (after a restart left it with an unresolvable HERDR_PANE_ID — see the self-location task's stale-env variant) ended with a healthy bus row (hooks_bound, process_bound, correct sid and directory) whose launch_context is EMPTY {} — the row was created at boot under a different bus name and renamed by the adopt, so it never received launch coordinates.

Consequences, all verified live:
1. `herder spawn` refuses: "initial prompt sender identity is not verified: no joined bus row matches the calling session, process, or pane" — sender verification has no launch coordinates to match against.
2. Repair enroll refuses: "stored bus name <name> cannot be corroborated because live bus identity proof is unavailable."
3. `herder reconcile --apply` re-confirms the registry row (terminal live) but does NOT backfill launch coordinates; spawn still refuses after it.

So an adopt-recovered orchestrator is permanently spawn-dead with no healing verb. Workaround (proven): explicit env prefix on spawn (HERDR_PANE_ID=<real pane> HERDER_GUID=<guid>, promptless, then herder send after bind) — or proxy-spawn by another orchestrator.

Corroborating class evidence, same day: a different long-lived session's bare spawn refused on a STALE `--from-pane` derived from ancient launch context (pane long gone). The spawn-side pane/identity derivation trusts stale or absent launch-context sources without validating them against resolvable panes.

Fix directions: (a) adopt's final bind should record launch coordinates for the surviving row (it knows the live pane/terminal it just verified); (b) reconcile (or a repair verb) should be able to backfill launch_context from a live-verified pane; (c) spawn sender verification should fall back to the live-verified registry row (terminal+pane+bus) when launch_context is empty, and validate derived pane ids as resolvable before refusing on them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Adopt-recovered rows can spawn without env-prefix workarounds (launch coordinates recorded at adopt final-bind, or spawn verification falls back to the live-verified row)
- [ ] #2 A repair path exists that backfills empty launch_context from a live-verified pane, with a red-first fixture reproducing the empty-context spawn refusal
- [ ] #3 Spawn-side pane derivation validates candidate pane ids as resolvable and names the refusal cause + the recovery in its output
<!-- AC:END -->
