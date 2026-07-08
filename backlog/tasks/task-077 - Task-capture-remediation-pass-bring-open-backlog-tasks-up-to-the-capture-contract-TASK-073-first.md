---
id: TASK-077
title: >-
  Task-capture remediation pass: bring open backlog tasks up to the capture
  contract (TASK-073 first)
status: In Progress
assignee: []
created_date: '2026-07-08 20:49'
updated_date: '2026-07-08 21:10'
labels: []
dependencies: []
priority: high
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-ratified contract (2026-07-08, in-session): every captured task has THREE readers — the future orchestrator (possibly post-compaction), the dispatched worker, and the eventual reviewers — and each must be able to do a good job from the task text plus its references. Requirements: (1) every reference reachable by the eventual worker, quoted inline, or in backlog docs; (2) acceptance criteria written at capture time, not invented at dispatch; (3) plain language — no run-internal shorthand, no opaque cross-references; (4) the same information standard applies to raw dispatches when no backlog exists.

THE WORK: audit every task in 'To Do' state and rewrite the failing ones. Known-worst first: TASK-073 (node daemon seat observer) — its ground truth is a design doc that exists only on the un-merged branch sessions-missions-design at docs/design/2026-07-08-herder-node-daemon-designs.md, plus a machine-local bus message and a gitignored napkins memo; its text uses run-internal dialect ('D-via-A re-cut', 'cluster E / 3.3(c)'). Remediate by either merging the design doc so the task can cite it durably, or inlining the decided design's operative content (what the daemon observes, what it writes, the four spec-level invariants) directly into the task, then rewriting the description in plain language and adding acceptance criteria. Repeat with lighter touch across the rest of the open tail.

This is orchestrator-lane work (backlog/ has a single writer: the orchestrator) — do not dispatch to a worker; execute directly, one commit per remediated task or one batch commit with per-task summary.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 TASK-073 rewritten: a reader with no run context can state what the unit builds, what its invariants are, and where the decided design lives — verified by the adversarial-review step of its eventual dispatch
- [ ] #2 Every remediated task has acceptance criteria and only reachable-or-inlined references
- [ ] #3 Each open To Do task audited with a pass/rewritten verdict recorded in this task's notes
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 21:10
---
TASK-073 remediated (8f52a6a): retyped as a design task, decision-record content inlined in operative form, all references now reachable (design doc cited at branch+commit+path; napkins memo dependence removed), four ACs added covering the design deliverables and review chain. Remaining: audit the rest of the open To Do tail with pass/rewritten verdicts.
---
<!-- COMMENTS:END -->
