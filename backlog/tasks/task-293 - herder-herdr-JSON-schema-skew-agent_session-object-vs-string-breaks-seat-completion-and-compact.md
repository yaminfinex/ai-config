---
id: TASK-293
title: >-
  herder <-> herdr JSON schema skew: agent_session object vs string breaks seat
  completion and compact
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
labels: []
dependencies: []
ordinal: 292500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment, STILL LIVE there): newer herdr pane JSON returns agent_session as an OBJECT; herder builds expecting a string fail decoding. Symptoms: seat completion on spawn fails (live_seat_missing — children run but stay unregistered) and compact refuses ('terminal not live in herdr agent list'). Fix: rebuild against current herdr PLUS tolerant decoding (accept string or object; forward-compatible). NOTE: this box's herder/herdr pair currently agree, but any herdr upgrade re-arms the break here — tolerant decoding is the durable fix. Also feeds the ratified per-request version-discipline direction (fail-closed hello) as concrete evidence that silent schema skew is the live failure mode today.
<!-- SECTION:DESCRIPTION:END -->
