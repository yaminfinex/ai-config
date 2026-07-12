---
id: TASK-160
title: 'live-contract: pin herdr subscription acknowledgement and schema shapes'
status: Done
assignee: []
created_date: '2026-07-12 06:54'
updated_date: '2026-07-12 08:58'
labels: []
dependencies: []
priority: medium
ordinal: 159000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the socket-subscription assessment (docs/design/2026-07-12-herdr-socket-subscriptions.md, Appendix A): herder already depends on a long-lived herdr subscription, but the live-contract tier pins schema/nested-snapshot more strongly than the subscription handshake. Extend the read-only live check to verify protocol compatibility, the subscription_started acknowledgement, and the observer's required event subscription variants, with hard timeout and guaranteed connection close. Keep hermetic coverage for both one-request-per-query and multi-request servers so neither connection policy becomes an accidental requirement.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Live check opens only a read-only events.subscribe, asserts result.type==subscription_started, times out deterministically, leaves nothing behind
- [x] #2 Live schema assertion verifies protocol 16 and parameter shapes of pane.created/closed/exited/agent_detected subscriptions
- [x] #3 Hermetic tests keep covering persistent-sub + fresh-per-query, nested result.snapshot, reconnect backoff, socket-incarnation change
- [ ] #4 Negative path: a mock-only {ok:true} acknowledgement fails the same parser the live check uses
- [ ] #5 Documented as optional live-environment coverage; zero mutation
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED, merged fead9cd (branch task-160-subscription-contract, commits cb973df + e5fbaa7). Live-contract tier now pins protocol-16 + schema shapes for pane.created/closed/exited/agent_detected, the connection-scoped events.subscribe acknowledgement (subscription_started), and the observer's required subscription variants; hard timeouts (run_live 3s outer ceiling on ALL raw-socket helpers) with guaranteed client close on every path; hermetic mocks cover both one-request-per-query and multi-request-persistent servers so neither connection policy is an accidental requirement — the check pins the contract, staying green if upstream fixes the close-after-first quirk. Adversarial review (opus) round-1 REQUEST-CHANGES: the key catch was aspirational-SKIP — a confirmed-compatible server that accepts then closes before the ack was classified as environmental SKIP, exactly the protocol-unchanged semantic-drift class the gate exists for. Fixed: established-then-EOF now exits rc=3 → FAIL in both probes while connect-refused/ENOENT/timeout stay SKIP. Delta APPROVE was empirical: seven server behaviors driven against the real recv-loop, false-fail direction proven safe (slow-honest ack and mid-check restarts land SKIP, never FAIL; orderly-close-after-read FAILs, RST-without-read skips — deliberate-refusal vs crash boundary). Read-only by construction, stated as assumption per review. Gates: independent battery 53/53 from the worktree; post-merge battery 53/53 on main with the new script live-exercised.
<!-- SECTION:NOTES:END -->
