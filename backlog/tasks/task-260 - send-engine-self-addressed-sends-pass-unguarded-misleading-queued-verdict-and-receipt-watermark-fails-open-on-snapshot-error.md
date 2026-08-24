---
id: TASK-260
title: >-
  send engine: self-addressed sends pass unguarded (misleading queued verdict)
  and receipt watermark fails open on snapshot error
status: Done
assignee: []
created_date: '2026-07-16 06:35'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 259500
---

## Teardown reconciliation — 2026-08-24

Closed by deletion. Herder's send engine retired. Peer delivery is hcom-native;
self-compaction deliberately uses two composer injections because a
self-addressed bus send reroutes to the owner.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two residuals from the compact-continuation review, both in the shared bus delivery engine (internal/send/hcom.go), both reviewer-proven by mutation probes; the compact fix closed its own callers but the engine chokepoint stays exposed.

1. DeliverBus(sender==busName) still posts and returns queued with zero injection — hcom never delivers self-sends. herder send to one's own verified name reaches this unguarded and reproduces the identical misleading queued/do-not-resend verdict (agent waits forever). Fix: refuse sender==busName at the engine guard alongside the existing empty-sender check (sender_unverified class), closing the whole class at one site; regression-pin it.

2. The pre-send receipt watermark FAILS OPEN: if the newest-event-id snapshot query exits non-zero, the watermark stays zero and ANY in-window receipt satisfies acknowledgement — a receipt from a PRIOR send by the same (deterministic, per-session-stable) derived sender inside the backdated window could satisfy a later send, claiming delivered for an undelivered continuation. Narrow (needs snapshot failure + same-sender receipt within ~1s + lock bypass via lockErr) and pre-existing, but the distinct-sender fix newly populates receipt history for these senders — first-time exposure. Fix: snapshot failure sets a reject-all sentinel (fail closed toward queued/do-not-resend); pin it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Engine refuses self-addressed sends with the unverified-sender verdict class; regression-pinned; callers with legitimate self-notify needs surveyed before the guard lands (none known)
- [ ] #2 Watermark snapshot failure fails closed (reject-all sentinel), pinned by a test forcing the snapshot error
<!-- AC:END -->
