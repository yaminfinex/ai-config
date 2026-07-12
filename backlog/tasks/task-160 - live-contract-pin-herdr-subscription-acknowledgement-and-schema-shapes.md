---
id: TASK-160
title: 'live-contract: pin herdr subscription acknowledgement and schema shapes'
status: In Progress
assignee: []
created_date: '2026-07-12 06:54'
updated_date: '2026-07-12 08:33'
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
- [ ] #1 Live check opens only a read-only events.subscribe, asserts result.type==subscription_started, times out deterministically, leaves nothing behind
- [ ] #2 Live schema assertion verifies protocol 16 and parameter shapes of pane.created/closed/exited/agent_detected subscriptions
- [ ] #3 Hermetic tests keep covering persistent-sub + fresh-per-query, nested result.snapshot, reconnect backoff, socket-incarnation change
- [ ] #4 Negative path: a mock-only {ok:true} acknowledgement fails the same parser the live check uses
- [ ] #5 Documented as optional live-environment coverage; zero mutation
<!-- AC:END -->
