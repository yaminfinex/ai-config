---
id: TASK-265
title: >-
  Fleet accretes empty /tmp husks: thousands of zero-byte mktemp leftovers —
  identify the producer and add cleanup
status: To Do
assignee: []
created_date: '2026-07-16 13:40'
labels:
  - hygiene
dependencies: []
priority: low
ordinal: 264500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reviewer observation during an unrelated unit (shared-user box): /tmp holds ~8342 `tmp.*` entries, all empty, with ~41 accreted in a few hours of fleet activity — so something in the toolchain (a wrapper, a gate script, a launcher, or a vendor CLI invoked by seats) is calling mktemp without cleaning up on at least one path. Individually harmless; collectively it is inode litter that will eventually degrade /tmp operations and makes real debris hard to spot.

Scope: (a) identify the producer — sample creation times against fleet activity, instrument or grep the house scripts (bin/, tools/*/tests, gate templates, hcom/herder wrappers) for mktemp calls missing trap-based cleanup; (b) fix the producer(s) to self-clean (mktemp + trap is the house pattern that works — the reviewer's own probes left zero residue with it); (c) one-time sweep of the existing husks (age + empty + pattern-matched only; do not touch non-empty or foreign files).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Producer(s) identified with evidence (not guessed) and fixed to self-clean
- [ ] #2 Existing empty husks swept safely (age+empty+pattern gated); count recorded before/after
- [ ] #3 A fleet-activity window after the fix shows zero new husk accretion
<!-- AC:END -->
