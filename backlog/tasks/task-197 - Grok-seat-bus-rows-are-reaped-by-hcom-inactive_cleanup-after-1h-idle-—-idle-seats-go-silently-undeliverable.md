---
id: TASK-197
title: >-
  Grok seat bus rows are reaped by hcom inactive_cleanup after 1h idle — idle
  seats go silently undeliverable
status: To Do
assignee: []
created_date: '2026-07-13 22:37'
labels: []
dependencies: []
ordinal: 196000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
LIVE EVIDENCE 2026-07-14: a grok reviewer seat idle ~1h after delivering its report had its hcom bus row stopped by the system reaper (life event: action=stopped, by=system, reason=inactive_cleanup, exactly +1h from last activity). The pane and herder registry row stayed up, the seat TUI later showed a restarted/banner state, and a queued verified-delivery message died with the row — the seat was silently unreachable until culled. Grok bridge-bound identities appear to hcom as ad-hoc rows, which the reaper treats as expired one-shots. Fix space (herder-side): bridge keepalive or periodic row refresh while the seat lives; re-bind on reap detection; and/or the grok status op should read back ROW PRESENCE so status flips unhealthy when the row disappears (status-op-authoritative liveness — activation-proven pattern, extend to steady-state). Also decide the queued-delivery contract when a row dies with messages pending (this event retired 3 pending messages as undeliverable at cull). Related: TASK-191 (deterministic hcom resolution) hardened bind-time; this is steady-state.
<!-- SECTION:DESCRIPTION:END -->
