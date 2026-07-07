---
id: TASK-025
title: 'ai-doctor: detect ~/.claude/.claude.json divergence from ~/.claude.json'
status: In Progress
assignee:
  - unit-n-keno
created_date: '2026-07-07 08:56'
updated_date: '2026-07-07 09:37'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-011 nice-to-have (Unit K): when both ~/.claude.json and ~/.claude/.claude.json exist and differ, pinned (team-bus) claude sessions and plain claude sessions run with different identity/config state — silent drift. Add an ai-doctor check that flags the divergence and prints the re-align/delete options (documented in the TASK-011 notes + napkins/task-011-investigation.md on the unit-k branch). Post-TASK-011, deleting the pinned copy is safe: next pinned launch re-seeds from ~/.claude.json.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 bin/ai-doctor gains a divergence check: when BOTH $HOME/.claude.json and $HOME/.claude/.claude.json exist and differ (byte compare), it warns naming both paths and prints the two user-decided remedies — re-align (cp ~/.claude.json ~/.claude/.claude.json) and delete (rm ~/.claude/.claude.json; safe post-TASK-011: next pinned launch re-seeds). It NEVER auto-fixes (locked doctrine).
- [ ] #2 Quiet cases follow the existing check pattern: either file missing → silent; both identical → info line only when not --quick. The check is pure-local so it also runs under --quick.
- [ ] #3 Verification: scratch-HOME matrix smoke (differ→warn+remedies, identical→no warn, missing→silent) PLUS a live run on this box (known-divergent ~/.claude/.claude.json from TASK-001) showing the flag fire. Finding: no test harness exists for bin/* — smoke evidence recorded in notes instead (creating a bin test harness is out of scope, stated not invented).
- [ ] #4 Docs hygiene: README.md ai-doctor line + any surface enumerating doctor checks updated or verified unchanged; surfaces named in DONE report.
<!-- AC:END -->
