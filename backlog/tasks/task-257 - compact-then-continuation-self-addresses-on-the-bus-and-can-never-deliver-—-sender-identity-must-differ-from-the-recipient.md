---
id: TASK-257
title: >-
  compact --then continuation self-addresses on the bus and can never deliver —
  sender identity must differ from the recipient
status: In Progress
assignee: []
created_date: '2026-07-16 01:48'
updated_date: '2026-07-16 02:04'
labels: []
dependencies: []
priority: high
ordinal: 256500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The compact --then detached sender delivers the continuation over the bus using the compacting session's OWN bus name as BOTH sender and recipient (the arm call passes the row's bus name for both). hcom accepts the post (it lands in event history from an ext_-prefixed external instance) but never injects an agent's own messages back into its own session — so the continuation is visible on the bus, the sender log reports 'queued — @<name> was busy; the bus will inject at its next turn', and the recipient idles indefinitely with no delivery. The queued verdict is a misread: the receipt window sees no pickup because self-sends are filtered, not because the target is busy. Live evidence: recipient idle-listening for 9 minutes post-compact, message visible in events, zero deliver:<name> status event, while every other message in the same window delivered in seconds.

Fix shape (design checkpoint first — this is a delivery-contract surface): the continuation must be sent FROM a distinct sender identity that hcom will deliver to the target (e.g. a reserved continuation sender name, or an ext identity that is not the recipient's own name), while keeping the fail-closed turn-end proof and receipt verification intact. The 'queued' verdict text should also stop asserting 'was busy' when the receipt window simply expired — state what was observed, not an inferred cause. Add a red test that pins the sender!=recipient invariant (a self-addressed continuation must be refused at arm time or sent from a distinct identity — never posted un-deliverable).

Interim operational practice (already doctrine): before ending the compact turn, schedule a harness wakeup carrying the full continuation prompt as the fallback; recover a missed continuation by reading the posted message off the bus event history.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause pinned with a failing test: a continuation armed for a session whose bus name equals the sender identity is proven undeliverable (or refused) in a hermetic fixture — the test is red on current code
- [ ] #2 Continuation delivery uses a sender identity hcom actually delivers to the recipient, receipt-verified end-to-end; a live (or live-contract-tier) proof shows the continuation injecting into the compacted session's next turn
- [ ] #3 Delivery verdict text no longer asserts an inferred cause ('was busy') for an expired receipt window — it states the observation and the consequence
- [ ] #4 Turn-end fail-closed proof and NOT-resending discipline unchanged (regression-pinned)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-16: codex builder in worktree task-257-compact-then, design checkpoint mandated before code (delivery-contract surface).

Design checkpoint APPROVED 2026-07-16 with riders: derived sender = fixed prefix + verified recipient bus name (fixture-proven against real hcom: self-send filtered with no receipt; never-joined external sender injects with sender-keyed receipt); equality refusals on both arm path and internal parser, no self-send fallback; verdict tokens frozen (prose-only honesty fix, goldens swept); stale-receipt guard coverage for the reused derived sender to be stated in DONE; wire-test placement/battery-count bookkeeping required.
<!-- SECTION:NOTES:END -->
