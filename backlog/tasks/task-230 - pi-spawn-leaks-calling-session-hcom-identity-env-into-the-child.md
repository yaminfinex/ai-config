---
id: TASK-230
title: pi spawn leaks calling-session hcom identity env into the child
status: To Do
assignee: []
created_date: '2026-07-15 05:23'
updated_date: '2026-07-15 05:36'
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
- [ ] #1 pi child env contains no caller hcom identity vars; child connects as a fresh identity
- [ ] #2 Red-first regression test on the pi launch env construction
- [ ] #3 Claude/codex/grok launch paths pinned clean for the same class
- [ ] #4 A pi spawn that dies pre-bind leaves the caller's bus row untouched
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Full forensic evidence in run-log 2026-07-15 incident entry + pi-review-ledger row 1. Backup at ~/.hcom/archive/hcom-backup-pre-hera-repair-20260715.db. No fleet messages leaked into the pi session (position never advanced); two inbound messages bounced during the outage window and were re-delivered.
<!-- SECTION:NOTES:END -->
