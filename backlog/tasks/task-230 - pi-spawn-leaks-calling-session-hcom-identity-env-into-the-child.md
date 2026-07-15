---
id: TASK-230
title: pi spawn leaks calling-session hcom identity env into the child
status: Done
assignee: []
created_date: '2026-07-15 05:23'
updated_date: '2026-07-15 11:56'
labels:
  - herder
dependencies: []
priority: high
ordinal: 229500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident, forensics COMPLETE (db-verified). CONFIRMED VECTOR: any DIRECT pi invocation from an identity-bearing shell. A diagnostic 'pi -p' run from the orchestrator's shell inherited the caller's hcom identity env (HCOM_PROCESS_ID / HCOM_INSTANCE_NAME class); the hcom pi extension (default pi home) honored the inherited identity WITHOUT tool/session continuity checks and took over the caller's LIVE bus row in place (row's tool flipped claude->pi, session_id/directory/transcript_path rewritten to the pi session; no created/started life event — same instance, new squatter). On pi exit (SIGTERM after 60s) hcom recorded stopped/exit:quit and DELETED the instances row, archiving a snapshot with tool=pi. The caller was then locked out: 'hcom start --as' refuses on latest-identity tool/directory mismatch (guard blocks the victim because the thief wrote last), and no supported verb recovers. Repair executed (owner-approved): consistent db backup, one-field snapshot edits (tool, directory) in the stop event, reclaim clean.

RULED OUT: the herder spawn path did NOT hijack — db shows the spawned pi child never connected to hcom at all (zero life events in its window, no pi instance rows). Its failure is a SEPARATE defect: spawned pi dies pre-bind (pane gone before bind-timeout) — root cause unknown.

Fix scope: (1) herder pi launch: scrub caller hcom identity vars from the child env so a pi child can only mint its own identity (red-first test on env construction); audit claude/codex/grok launchers for the same class and pin clean. (2) Diagnose + fix the spawned-pi instant-death (why the TUI exits pre-bind in a herdr pane; the direct 'pi -p' runs fine for 60s+). (3) Doctrine/guard for the direct-invocation footgun where herder can help (e.g. herder doctor check or wrapper warning); the extension-side fix is upstream (candidates already ledgered: extension honors inherited env cross-tool; reclaim guard strands the victim; refusal exits rc=0).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 pi child env contains no caller hcom identity vars; child connects as a fresh identity
- [x] #2 Red-first regression test on the pi launch env construction
- [x] #3 Claude/codex/grok launch paths pinned clean for the same class
- [x] #4 A pi spawn that dies pre-bind leaves the caller's bus row untouched
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged bd31fee (--no-ff, head 4b670a3, three commits), post-merge battery 61/61 green on main, pushed. BATTERY GROWTH: this unit adds check-identity-doctor.sh — house count 60 → 61 as of this merge. AC1: allowlist boundary drops every ambient HCOM_* (fabricated-future-key pinned at the real boundary through bin/herder) and re-adds only child-owned values; children mint fresh identity and bind (positive half pinned per family: claude/codex/pi/grok scrub-before-seat-overlay/print bypass). AC2: red-first env-construction regression was the first commit. AC3: all launch families pinned both directions; passthrough fixtures honestly re-framed (spawn pre-export establishes HERDER_* ownership, not the boundary — follow-up task filed for the direct-launch class). AC4: pre-bind pi death evidence rebuilt against a REAL disposable hcom.db with byte-compare + PATH-recorded lifecycle interception; both injected hazards red-discriminated; WAL escape disproven by reviewer. Pi pre-bind root cause: hcom wrapper synchronous pre-child update check (no supported skip knob) — ruled typed diagnosis/remedy only, no private-state workaround; upstream candidate ledgered. Bonus surface: ai-doctor identity-bearing-shell warning, hermetically pinned incl. strict-exempt discrimination both directions (ruled: situational nudge never trips --strict; ordinary warnings still do). Reviews: incumbent opus NOT-APPROVE(2)→CONDITIONAL APPROVE→final fixes landed with killer mutations shown red; grok calibration APPROVE with the same doctor gap found independently.
<!-- SECTION:NOTES:END -->
