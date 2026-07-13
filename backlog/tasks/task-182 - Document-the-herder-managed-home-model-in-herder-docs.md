---
id: TASK-182
title: Document the herder-managed home model in herder docs
status: Done
assignee: []
created_date: '2026-07-13 06:07'
updated_date: '2026-07-13 10:38'
labels: []
dependencies: []
priority: medium
ordinal: 181000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ask (2026-07-13): the managed-home model must be clearly documented in herder's user-facing docs. Content: (1) the model — a 'fully herder-managed' agent family gets a dedicated home under the herder state root (grok: <state>/grok-home), with the contract config atomically REWRITTEN ON EVERY LAUNCH (auto-update off, hooks off, MCP registered) — it is the launch contract rendered as config, not user config; (2) the three deliberate drifts vs running the CLI manually (home, pinned binary version, auth source) and why; (3) the manual-verification path: 'herder launch <agent>' runs an interactive session under exactly the harness herder drives — this is how the owner verifies the real harness; (4) the contrast model: claude/codex share the user's live home deliberately (config/skills delivery is load-bearing); grok/pi are fully herder-managed; 'herder launch claude/codex' could adopt the managed model later if multi-account isolation is wanted.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder docs clearly explain the managed-home model, the drift trade-offs, and the herder-launch verification path
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Shipped folded into TASK-170 U5 (merged e258b87): tools/herder/README.md managed-home section — dedicated <state>/grok-home with contract config atomically rewritten EVERY launch under the seed lock; the three deliberate drifts vs manual grok (home, pinned binary version, auth source) and rationale; herder launch grok as the owner's manual-verification path (bounded guest); claude/codex live-home contrast; future managed-model option for multi-account isolation. Docs fidelity adversarially verified (reviewer mupo P2-1/P2-2 fidelity findings fixed in round 2: removed false mint/contract-parity clauses, removed invented Pi family reference).
<!-- SECTION:NOTES:END -->
