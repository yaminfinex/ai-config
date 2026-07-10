---
id: TASK-146
title: 'observer: bake review + autostart decision (closure task)'
status: To Do
assignee: []
created_date: '2026-07-10 01:50'
updated_date: '2026-07-10 01:51'
labels: []
dependencies: []
priority: high
ordinal: 146000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The observer daemon has been baking since 2026-07-09T09:42Z (manual instance, pid 2876552) on the post-backoff-fix build; the autostart default stays OFF until the owner reviews the bake. This task is the closure: assemble bake evidence for the watch items (busCorroboratesDead breadth, reconnect/generation behavior across herdr restarts, reconfirmation row volume vs interval, false dormant-live / turnover rates), owner reviews, and the autostart default flips ON or the daemon is parked with a reason. Related: the reconfirm-interval cadence ruling is its own open task; the spec erratum fold-in is separate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Bake evidence assembled for all four watch items with numbers from the live state dir
- [ ] #2 Owner ruling recorded: autostart ON (with chosen cadence) or parked with reason
- [ ] #3 If ON: autostart default flipped + docs updated; if parked: standing orders updated
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Bake evidence (assembled 2026-07-10, ~16h into bake, pid 2876552, same pid throughout):
(1) Health: heartbeat current, sweeps clean (applied=0 noop=11 refused=0), protocol compatible, no connection gap. New spawns confirmed within seconds of seat (two live examples). CPU steady ~0.4% (measured earlier at 10m32s: 3 CPU-sec).
(2) Fail-closed proven LIVE: during the registry poison window the observer refused 10-11 candidates per sweep for hours (archive byte verification) rather than writing through corruption, then recovered IN PLACE after the poison row was excised — no restart needed, same pid. This is the strongest single piece of bake evidence.
(3) busCorroboratesDead breadth: NOT exercised — zero corroboration rows during bake (no un-culled deaths occurred; every seat left via explicit cull). No overreach observed, but no positive firing either.
(4) Reconfirmation row volume: zero observer rows appended to the registry during the entire bake; confirmations ride the status file. Volume concern currently empty at the 60m cadence.
(5) False dormant-live/turnover: zero flags across ~11 sessions cycling through the fleet in 16h; no live session misflagged.
Honest gap: the interesting write paths (corroborate-dead, dormant flagging, reconnect/generation across a herdr restart) were never triggered because the fleet stayed healthy — the bake proves safety and fail-closed posture, not positive-path breadth. Ruling options: flip autostart ON on safety evidence, or extend bake with a synthetic exercise (kill an agent process without cull; restart herdr) to see the positive paths fire once.
<!-- SECTION:NOTES:END -->
