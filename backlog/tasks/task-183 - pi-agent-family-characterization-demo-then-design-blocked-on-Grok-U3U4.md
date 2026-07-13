---
id: TASK-183
title: 'pi agent family: characterization demo then design (blocked on Grok U3+U4)'
status: Done
assignee: []
created_date: '2026-07-13 06:07'
updated_date: '2026-07-13 23:14'
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
- [x] #1 Demo report merged answering the binding fork and behavioral clauses
- [x] #2 Design doc merged with owner rulings recorded
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
UNBLOCKED + DISPATCHED 2026-07-14: grok program complete (surface free). Owner rulings delta: install LATEST pi (delegated to the unit, isolated prefix, exact version recorded); provider env keys ready in ~/.profile; demo scope grew — cross-harness usage comparison (pi vs claude/codex/grok native CLIs for the same model families). Worker worker-luve (codex 5.6 high) in worktree task-183-pi-demo, thread task183pi, brief napkins/run-herder-dx/task-183-pi-demo-brief.md. Also instituted same day: GROK REVIEW CALIBRATION (owner directive) — grok reviewers run alongside incumbent cross-family reviewers on behavior diffs; ledger at napkins/run-herder-dx/grok-review-ledger.md; verdict authority stays with the incumbent during calibration.

AC-1 DONE: demo report merged (docs/design/pi-demo-report-2026-07-13.md, commits 518482b+926283c+8d66d63). Binding fork ANSWERED with probe evidence: native TypeScript extension (sendUserMessage inject probe ran to agent_settled, source=extension; reply content honestly marked uncaptured). Key characterization: pi 0.80.6 (@earendil-works), PI_HOME NOT consumed — managed home rides PI_CODING_AGENT_DIR/SESSION_DIR + isolated HOME; PI_OFFLINE couples update-suppression; one-provider env routing (provider pin per seat); clause table earns/refuses grok-style clauses on pi-specific evidence (/proc ceremony CONDITIONAL pending herder launch-path characterization). Review: FIRST GROK CALIBRATION RUN — incumbent mage (opus) APPROVE-with-findings; calibration mudo (grok) 3 verified grok-only findings incl the inject evidence overclaim; both deltas APPROVE; identical residual P3 found independently by both. AC-2 (design unit) is next — design must treat /proc clause as conditional.

AC-2 CLOSED 2026-07-14: pi first-class design merged 7abce59 (docs/design/pi-first-class-design.md, 1443 lines, 6 commits 5024141..b3ea6cf). Five adversarial fix rounds, dual independent APPROVE (codex incumbent + grok calibration) at b3ea6cf. Review chain highlights: both reviewers independently converged on the no-daemon-shape P1s (r1), the T29 premise-smuggle (r3), and the plaintext-invariant overclaim (r4); calibration lane caught the exec-window orphan-process hole AFTER incumbent approval (r5, orchestrator-verified). Owner rulings recorded in DR-5/owner-decision items 1-7 (incl. new item 7: P7-falsified auth acceptance). /proc clause stays CONDITIONAL, resolved only by activation evidence. Staged implementation units U1-U3+activation defined in the doc; filing follows owner sign-off (fresh-eyes offer outstanding per doctrine).
<!-- SECTION:NOTES:END -->
