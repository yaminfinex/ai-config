---
id: TASK-025
title: 'ai-doctor: detect ~/.claude/.claude.json divergence from ~/.claude.json'
status: Done
assignee:
  - unit-n-keno
created_date: '2026-07-07 08:56'
updated_date: '2026-07-07 12:23'
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
- [x] #1 bin/ai-doctor gains a divergence check: when BOTH $HOME/.claude.json and $HOME/.claude/.claude.json exist and differ (byte compare), it warns naming both paths and prints the two user-decided remedies — re-align (cp ~/.claude.json ~/.claude/.claude.json) and delete (rm ~/.claude/.claude.json; safe post-TASK-011: next pinned launch re-seeds). It NEVER auto-fixes (locked doctrine).
- [x] #2 Quiet cases follow the existing check pattern: either file missing → silent; both identical → info line only when not --quick. The check is pure-local so it also runs under --quick.
- [x] #3 Verification: scratch-HOME matrix smoke (differ→warn+remedies, identical→no warn, missing→silent) PLUS a live run on this box (known-divergent ~/.claude/.claude.json from TASK-001) showing the flag fire. Finding: no test harness exists for bin/* — smoke evidence recorded in notes instead (creating a bin test harness is out of scope, stated not invented).
- [x] #4 Docs hygiene: README.md ai-doctor line + any surface enumerating doctor checks updated or verified unchanged; surfaces named in DONE report.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit c5793cc (branch unit-n-doctor-cull), accepted by hera #2506 (independently verified incl. live run). WHAT: check_claude_config_divergence in bin/ai-doctor, called after the statusline check — both $HOME/.claude.json and $HOME/.claude/.claude.json exist + byte-differ (cmp -s) -> warn naming both paths + print_claude_config_divergence_options (re-align: cp ~/.claude.json ~/.claude/.claude.json; delete: rm ~/.claude/.claude.json, safe post-TASK-011 — next pinned launch re-seeds). NEVER auto-fixes (locked doctrine: user-decided at runtime). Either file missing -> silent; identical -> info line suppressed under --quick; check is pure-local so it runs under --quick too. VERIFICATION: scratch-HOME 5-case matrix (differ -> warn+options; same -> info, suppressed under --quick; missing/onlybase/onlypinned -> silent) + live run on this box flagged the real TASK-001 divergence (hera reproduced). Pinned gate green: go vet/test herder+bottle, battery 17/17 env -u. DOCS: ai-doctor --help, README.md ai-doctor bullet; machine-setup.md + skills verified unchanged (grep clean). NICE-TO-HAVES (not tasks): no test harness exists for bin/* (doctor checks are smoke-verified only); check is claude-only by design (codex/gemini keep state inside pinned dirs).
<!-- SECTION:NOTES:END -->
