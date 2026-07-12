---
id: TASK-111
title: >-
  herder rename --take-from: atomic label transfer between sessions
  (systemic-review AC-19)
status: In Progress
assignee:
  - hera-run
created_date: '2026-07-09 07:05'
updated_date: '2026-07-12 09:22'
labels: []
dependencies: []
priority: medium
ordinal: 111000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: implement.

From the 2026-07-08 systemic review (docs/design/2026-07-08-herder-systemic-review-memo.md, AC-19, cluster F verb gaps): when a label should move from one session to another (successor takes over a role; a stale seat still holds the good name), today's route is a multi-step manual dance (rename the old, rename the new) with a window where the label is free for anyone to claim, or held by neither. Proposed verb: `herder rename <target> --take-from <other>` — atomically release the label from <other> and assign it to <target> in one registry transaction, refusing if <other> is seated-and-live unless the caller confirms.

SCOPE: implement in tools/herder (renamecmd + registry v2 UpdateLocked path; both rows written in ONE locked update so no observer sweep or concurrent writer can see the intermediate state). Respect four-state lifecycle rules (cannot take from a lost session; taking from a retired session is a plain claim since retirement already released the label — refuse with guidance instead of no-op transfer). Check overlap with open TASK-042 before starting: if 042 has since shipped a conflicting rename surface, stop and report to @hera rather than building a parallel path.

Adversarial review: mandatory (behavior-carrying registry write path).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder rename <target> --take-from <other> transfers the label in one atomic locked registry update; no intermediate state (label free or doubly-held) is ever observable in the registry file
- [ ] #2 Refusal paths: <other> seated-and-live refuses without explicit confirmation flag; <other> lost refuses; <other> retired refuses with guidance to use plain rename
- [ ] #3 Both affected rows carry correct four-state status after transfer; herder list shows the label on <target> only
- [ ] #4 Tests cover the transfer, each refusal path, and a concurrent-writer race (transfer holds the lock for the full two-row update)
- [ ] #5 Full check suite ALL GREEN bare from repo root
<!-- AC:END -->
