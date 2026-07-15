---
id: TASK-245
title: >-
  Bare --reply-to acks broadcast to the whole bus — the owner desk still
  receives them even with a verified live sender
status: To Do
assignee: []
created_date: '2026-07-15 11:46'
labels: []
dependencies: []
ordinal: 244500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Incumbent-review discovery on the sender-identity unit (wire-proven on a real scratch bus, pre- AND post-fix): a reply sent with --reply-to <id> but NO @target is a BROADCAST (hcom 'send -- msg' = send-to-all), so a polite worker ack to a prompt still lands on the owner seat regardless of the sender-identity fix. That fix genuinely narrows the incident — the stamped sender is now an addressable live name, so well-behaved repliers CAN @ it and route correctly; pre-fix the stamp was unaddressable, replies errored, and agents fell back to broadcast. But the broadcast path itself remains open.

Fix directions (evaluate at design checkpoint): (a) doctrine/bootstrap text — replies always @ the sender (cheap, immediate, rides the spawn-context surface); (b) herder-side: prompt text instructs the exact reply form; (c) upstream: hcom reply-to should default-route to the replied message's sender when no @target is given (candidate ledgered). Desk-side intent gating (acks never promote) exists as defense in depth. AC sketch: red-first wire test on a scratch bus proving a bare --reply-to worker ack no longer reaches an owner-side peer through whichever layers ship; doctrine text pinned by bootstrap goldens.
<!-- SECTION:DESCRIPTION:END -->
