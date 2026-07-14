---
id: TASK-211
title: >-
  Grok margin cleanup — delete inert idle-nudge machinery and retire the
  manual-guest launch path
status: In Progress
assignee: []
created_date: '2026-07-14 22:26'
updated_date: '2026-07-14 22:28'
labels: []
dependencies: []
ordinal: 210000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER RULING 2026-07-14 (gold-plating audit candidates 3+4, ruled IN; one unit — same files): (A) DELETE the idle-aware nudge machinery: nudgeLoop, sessionIdle phase parsing, NudgeCandidates, and the --session-events/--nudge-after/--max-nudges surface plus their tests (grokbridge/binder.go, grokbridge/journal.go, grokbridge/command.go). It is liveness-only, redundant with HCOM_RECOVER + doctrine list_pending + orchestration re-prompting, and PROVEN INERT on the production launch path (launch never passes --session-events so the loop never starts on launched seats — verified binder.go:178-180 + launchcmd/grok.go:489-494); deleting it changes no launched-seat behavior and removes a vendor-coupled events.jsonl phase vocabulary. (B) RETIRE the manual-guest launch path (~200 lines: ambient-GUID corroboration, foreground wrapper, retire-on-stop branch — launchcmd/grok.go manual sections, grokbridge/command.go retire-on-stop surface): replace with a cause+remedy refusal pointing at herder spawn --agent grok. PRE-CHECK REQUIRED before cutting (B): establish whether any check script, smoke, or test depends on the manual path (audit flagged this unverified) — if entangled, STOP-AND-REPORT with the dependency list rather than improvising. KEEP everything else ruled load-bearing: drain contract, spool journal, wake line, generation fencing, session evidence, identity de-latch/refresh, fetch/ack stages (ack stage explicitly ruled kept), project-config MCP registration, launch-failure marker, passthrough refusals. Update docs where nudge/manual-launch are described. Full battery + adversarial review per house rules.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Nudge machinery fully removed (code + flags + tests + docs); no launched-seat behavior change (goldens/battery prove it)
- [ ] #2 Manual-guest path dependency pre-check performed and reported; path retired to cause+remedy refusal (or stop-and-report if entangled)
- [ ] #3 All load-bearing mechanisms untouched; full house battery green
<!-- AC:END -->
