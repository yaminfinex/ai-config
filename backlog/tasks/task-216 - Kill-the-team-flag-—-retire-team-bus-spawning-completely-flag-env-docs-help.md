---
id: TASK-216
title: >-
  Kill the --team flag — retire team-bus spawning completely (flag, env, docs,
  help)
status: To Do
assignee: []
created_date: '2026-07-15 00:02'
labels: []
dependencies: []
ordinal: 214000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER DIRECTIVE (2026-07-15, HIGH PRIORITY): 'kill the --team flag and retire it from docs etc COMPLETELY, it's a massive footgun and a dangling unused requirement.' The hazard is long-established (spawn --team silos the agent onto a separate hcom bus at $HOME/.hcom/teams/<team>; the spawner stays on the global bus so reports never route home — silent starvation both directions), doctrine has been global-bus-only for the whole run, and the pi design just had to add a refusal gate for it. SCOPE: (1) remove --team from herder spawn parsing OR hard-refuse with one cause+remedy line (prefer full removal; if any internal plumbing makes removal risky, refusal is acceptable — state which and why in the DONE report); (2) remove the HERDER_TEAM env plumbing and any team-derived HCOM_DIR construction in spawn/launch (spawn.go team-bus path ~:662-670); (3) scrub --team/team-bus from help text, README, docs, goldens; (4) sweep the whole repo for team-bus references (docs/design mentions of the hazard may stay as history — mark superseded rather than erase where they are provenance); (5) the pi design's global-bus-only decision simplifies from 'refusal gate L2' to 'the flag no longer exists' — leave the design text to the pi lane but note the interaction in your DONE report; (6) upstream note: hcom's own team feature is not ours to remove — this retires HERDER's surface only. Tests: refusal-or-absence pinned (spawning with --team must fail loud with remedy, or be an unknown-flag error); goldens updated; full house battery (59). Adversarial review per house rules.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 --team surface removed or hard-refused (stated which + why); HERDER_TEAM plumbing gone; unknown-flag/refusal behavior pinned by tests
- [ ] #2 Docs/help/README/goldens scrubbed; repo-wide sweep done (historical hazard records marked superseded, not erased)
- [ ] #3 Full house battery green (59); pi-design interaction noted in DONE report
<!-- AC:END -->
