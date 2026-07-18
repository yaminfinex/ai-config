---
id: TASK-286
title: >-
  credential DX: implement verb-default credential self-resolution per
  signed-off design
status: To Do
assignee: []
created_date: '2026-07-18 20:45'
labels:
  - herder
  - credentials
dependencies: []
priority: high
ordinal: 285500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the owner-signed design-of-record docs/design/2026-07-18-credential-self-resolution.md (TASK-282, rev 9, 8 review rounds). Core: credential-authenticated verbs default to self-resolution anchored on the calling-process kernel ppid chain intersected with live herdr process_info; environment variables are veto/provenance only, never authentication authority; explicit --credential-file remains the override; raw API stays explicit. Scope = deltas D1-D6 in the design, including D5 applier-composition adopt recovery and D6 bash attribution verification. All authority-changing deltas are cutover-marker-gated. SEQUENCING DEPENDENCY: D3 requires a herdr surface addition first (process_info gains PID-namespace inode + start-time fields; herder hard-refuses self-resolution when absent) — confirm herdr surface availability or split D3 behind it. Design decisions are settled through 8 adversarial rounds + owner sign-off: implementers do not relitigate; deviations are stop-and-report.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All six credential-authenticated verbs resolve credentials by default via ppid-chain-x-process_info anchor with env veto-only, matching the design refusal matrix
- [ ] #2 Explicit --credential-file override and raw API behavior unchanged; escape hatches per design
- [ ] #3 D3 hard-refuses (typed, cause+remedy) when herdr lacks ns-inode/start-time fields
- [ ] #4 Authority-changing deltas gated on the cutover marker; behavior with marker off is unchanged
- [ ] #5 Full house battery green; adversarial review passed
<!-- AC:END -->
