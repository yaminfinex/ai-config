---
id: TASK-293
title: >-
  herder <-> herdr JSON schema skew: agent_session object vs string breaks seat
  completion and compact
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
updated_date: '2026-07-21 05:21'
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
<!-- SECTION:NOTES:END -->
