---
id: TASK-183
title: 'pi agent family: characterization demo then design (blocked on Grok U3+U4)'
status: In Progress
assignee: []
created_date: '2026-07-13 06:07'
updated_date: '2026-07-13 19:21'
labels: []
dependencies: []
priority: medium
ordinal: 182000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner direction (2026-07-13): add the pi coding agent (multi-provider harness) as a herder agent family. OWNER RULINGS RECORDED: (1) pi is FULLY HERDER-MANAGED — dedicated PI_HOME under the herder state root, same model as grok; (2) other launch-contract clauses (version gate, update suppression, /proc ceremony) are EARNED BY CHARACTERIZATION, not inherited — the demo decides; (3) credentials: provider keys reach panes via shell config (verified: pane shells carry bashrc-exported keys); least-privilege per-seat filtering (only the routed provider's key in the child env) is the design direction; (4) big architectural fork for the demo to answer FIRST: native hcom binding via pi's TypeScript extension API / RPC mode (claude-hooks-like, no bridge) vs grokbridge-style binder. Pipeline: owner provisions pinned pi install -> characterization demo unit (throwaway state, grok-demo-report is the template) -> design unit w/ design gate + fresh-eyes ledger candidate -> staged impl. SEQUENCING: blocked on Grok U3+U4 merge (same integration surface).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Demo report merged answering the binding fork and behavioral clauses
- [ ] #2 Design doc merged with owner rulings recorded
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
UNBLOCKED + DISPATCHED 2026-07-14: grok program complete (surface free). Owner rulings delta: install LATEST pi (delegated to the unit, isolated prefix, exact version recorded); provider env keys ready in ~/.profile; demo scope grew — cross-harness usage comparison (pi vs claude/codex/grok native CLIs for the same model families). Worker worker-luve (codex 5.6 high) in worktree task-183-pi-demo, thread task183pi, brief napkins/run-herder-dx/task-183-pi-demo-brief.md. Also instituted same day: GROK REVIEW CALIBRATION (owner directive) — grok reviewers run alongside incumbent cross-family reviewers on behavior diffs; ledger at napkins/run-herder-dx/grok-review-ledger.md; verdict authority stays with the incumbent during calibration.
<!-- SECTION:NOTES:END -->
