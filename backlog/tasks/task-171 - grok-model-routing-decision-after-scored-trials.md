---
id: TASK-171
title: 'grok: model-routing decision after scored trials'
status: To Do
assignee: []
created_date: '2026-07-12 21:03'
labels: []
dependencies: []
priority: low
ordinal: 170000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
After first-class delivery is stable, run bounded scored trials across implementation, review, and research/advisor work; compare against GPT-5.6/Opus/Fable lanes on quality, caught defects, latency, cost/quota, recovery burden, context fidelity. Owner decides Grok's standing role (experimental implementer, third-family reviewer, advisor challenger, or explicit-only). Update ONLY the canonical model-doctrine surface; start explicit-only — do not displace existing routing by assumption. Ground truth: docs/design/grok-onboarding-memo.md (routing options section).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Trials bounded, independently reviewed, recording model/version, reasoning mode, outcome, defects, latency, quota/auth interruptions
- [ ] #2 At least one review-direction trial per proposed direction before any standing reviewer role
- [ ] #3 Owner makes explicit routing decision incl. auth/quota-failure fallback; only the canonical doctrine surface edited, with intent review
<!-- AC:END -->
