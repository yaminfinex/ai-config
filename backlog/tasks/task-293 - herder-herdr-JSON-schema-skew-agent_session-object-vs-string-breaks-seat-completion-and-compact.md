---
id: TASK-293
title: >-
  herder <-> herdr JSON schema skew: agent_session object vs string breaks seat
  completion and compact
status: Done
assignee: []
created_date: '2026-07-20 05:19'
updated_date: '2026-07-21 06:17'
labels: []
dependencies: []
ordinal: 292500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment, STILL LIVE there): newer herdr pane JSON returns agent_session as an OBJECT; herder builds expecting a string fail decoding. Symptoms: seat completion on spawn fails (live_seat_missing — children run but stay unregistered) and compact refuses ('terminal not live in herdr agent list'). Fix: rebuild against current herdr PLUS tolerant decoding (accept string or object; forward-compatible). NOTE: this box's herder/herdr pair currently agree, but any herdr upgrade re-arms the break here — tolerant decoding is the durable fix. Also feeds the ratified per-request version-discipline direction (fail-closed hello) as concrete evidence that silent schema skew is the live failure mode today.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
OWNER UPDATE (2026-07-21): the agent_session fix is being implemented on another box and will land on main there. Local action when it merges: rebase/pull main here, rebuild, THEN run the resident-writer inventory (standing doctrine: long-lived daemons — observer/sidecars — keep the old decoding until their sessions restart; schedule replacement before trusting the fix). Do not implement locally; this task tracks the pull + rollout only.

ROLLOUT DONE (2026-07-21): pulled 36c0ab5 (tolerant decoding verified in diff: both shapes + null/absent, fixture from live 0.7.4 payload). Full battery on main: 63/63 green — sole first-run failure was environmental (a tracked lock file deleted outside git broke the suite's tree copy; restored to HEAD; suite ALL GREEN on re-run; a second red herring was the invoking session's stale GOROOT export from the 1.26.4 era — session-local, the gate template already exports the correct GOROOT). Live-contract suite passed 11/11 against the REAL herdr, which is ALREADY 0.7.4 on this box. Resident-writer inventory: observer was two builds behind — stopped via observer stop and restarted detached on the current build (verified sweeping, protocol_compatible). All pre-upgrade sidecars remain on the pre-fix build with broken pane decoding until their sessions end; accepted deliberately — blast radius is each sidecar's own seat rows, the new observer compensates with correct-decoding sweeps of every seat, and the held seats refresh naturally at cull. No proactive seat restarts (several hold live context, incl. an in-progress owner interview).
<!-- SECTION:NOTES:END -->
