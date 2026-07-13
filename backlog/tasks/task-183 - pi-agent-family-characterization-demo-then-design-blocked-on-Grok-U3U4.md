---
id: TASK-183
title: 'pi agent family: characterization demo then design (blocked on Grok U3+U4)'
status: To Do
assignee: []
created_date: '2026-07-13 06:07'
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
