---
id: TASK-247
title: >-
  herder send-window lockfiles never unlink — 11k+ accumulated in /tmp
  fleet-wide
status: To Do
assignee: []
created_date: '2026-07-15 12:11'
labels: []
dependencies: []
ordinal: 246500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Noticed during a review wind-down (not tied to any current diff): lockSendWindow creates /tmp/herder-send-<sha>.lock per (busdir,sender,target) key and never unlinks — over 11,000 accumulated on one machine. Harmless individually (empty flock files) but unbounded growth in shared /tmp, and the keyspace makes manual cleanup risky while sends are in flight (an unlink during a held flock silently splits the mutex — two senders can then hold 'the' lock simultaneously). Fix shape needs care for exactly that race: either O_TMPFILE/flock-on-open patterns, a post-unlock unlink guarded by re-stat (inode match), or a doctor/host sweep that only removes locks older than a bound with no holder (fuser/flock probe). The naive unlink-after-unlock is racy — design checkpoint required. Note there are now TWO lock implementations sharing the key formula (the send engine's unbounded flock and the cull path's deadline-bounded NB variant) — whatever ships must keep them mutually correct.
<!-- SECTION:DESCRIPTION:END -->
