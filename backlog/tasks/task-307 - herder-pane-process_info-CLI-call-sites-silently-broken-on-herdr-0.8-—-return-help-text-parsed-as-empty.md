---
id: TASK-307
title: >-
  herder: pane process_info CLI call-sites silently broken on herdr 0.8 — return
  help text, parsed as empty
status: Done
assignee: []
created_date: '2026-08-21 11:49'
updated_date: '2026-08-23 10:18'
labels:
  - herder
dependencies: []
priority: high
ordinal: 306500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-verified by hotfix-vemo (TASK-041 round 2, bus #105838, 2026-08-21): herdr 0.8's CLI verb is 'pane process-info --pane <id>' (hyphen, flag-form); the old positional spelling 'pane process_info <id>' returns the pane HELP TEXT on live 0.8, and ParseProcessInfo swallows it as an empty result — silent field breakage. Broken call-sites at HEAD (wayfinder-verified): spawncmd/spawn.go:1446, lifecyclecmd/lifecycle.go:1099, and observercmd/observer.go:467 (third site, found in verification sweep — same CLI positional spelling; NOT socket.go:384, which drives the socket method pane.process_info where the underscore is correct). Fix: switch to the live-verified verb shape (branch task-041-compact-unblock commit f3696fe has the working pattern), and add a live-tier check pinning the verb shape (tests/check-live-contract.sh) so the next herdr CLI reshuffle cannot silently break it again. Also mirror the 0.8 wire reality in mocks: live process_info has NO shell_pid member (foreground_process_group_id + foreground_processes only) — vemo's mock-herdr-compact already mirrors this; the occupant-probe fixtures (probe contract §4) must too. Slim-down interplay: the occupant probe implementation inherits this verb shape + a live smoke (fixture 12 in the probe contract already specifies it).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All three CLI call-sites use the 0.8 verb shape and parse real output (not help text) on live herdr
- [ ] #2 Live-tier check pins the verb shape; mocks drop shell_pid to match 0.8
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-08-21 wayfinder-ruzo: vemo folded the fix onto branch task-041-compact-unblock as commit 28f90b2 (bus #106744) — both ACs met on the branch: three call-sites moved to 'pane process-info --pane' (socket.go:384 underscore untouched), mocks reshaped to 0.8 wire (no shell_pid), live-tier verb pin + rc=0-help-text negative demo in check-live-contract.sh; live-contract 13/13 vs installed herdr 0.8. Separate task-307-process-info-verb branch superseded. Merge is owner-gated with TASK-041; close this at merge.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-08-23 10:18
---
Merged to main 2026-08-23 in ea6e1c0 (single merge with the TASK-041 branch @ 28f90b2, owner-approved via conductor gate package: missions fleet-refit artifacts/conductor/hotfix-merge-gate.md @ daa921f). Post-merge red suite on main: 3 reds / 5 keep-greens as amended. Live-tier verb pin + rc=0 help-text negative demo are in the battery via check-live-contract.sh.
---
<!-- COMMENTS:END -->
