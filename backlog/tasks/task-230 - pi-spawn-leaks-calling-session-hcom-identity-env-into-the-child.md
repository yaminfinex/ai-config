---
id: TASK-230
title: pi spawn leaks calling-session hcom identity env into the child
status: To Do
assignee: []
created_date: '2026-07-15 05:23'
labels:
  - herder
dependencies: []
priority: high
ordinal: 229500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident (first pi review-calibration spawn): 'herder spawn --agent pi --provider anthropic --model claude-opus-4-8:high' launched a pi child that inherited the CALLING session's hcom identity environment (HCOM_PROCESS_ID / instance-name class vars). The pi child connected to the bus AS THE CALLER (no created/started life event for a new identity — the caller's existing session row simply flipped tool to pi), then exited pre-bind; its exit recorded 'stopped, reason exit:quit' on the CALLER's row, archiving it. Aftermath: the caller (the orchestrator seat) lost its bus identity; 'hcom start --as <name>' refuses with a latest-identity-tool-mismatch guard (latest says pi, current session is claude), and no supported hcom verb recovers the row. Bind-predicate failure and pane cleanup on the herder side worked as designed — the leak is in child env construction on the pi launch path (claude/codex launchers do not exhibit this; suspect the pi launcher passes the caller's environ through where other families scrub or replace hcom identity vars).

Fix scope: (1) pi child env must be scrubbed of caller hcom identity vars (process id, instance name, launch markers) so the child mints its OWN identity exactly like claude/codex children; (2) regression test: spawn-path env construction proves no caller-identity var reaches the pi child (red-first against the current builder); (3) audit claude/codex/grok launchers for the same class while there (they appear clean in the field — pin it with tests). Separately ledgered upstream (not this task): hcom's reclaim guard blocks the rightful owner after a cross-tool hijack with no recovery verb, and the refusal exits rc=0.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 pi child env contains no caller hcom identity vars; child connects as a fresh identity
- [ ] #2 Red-first regression test on the pi launch env construction
- [ ] #3 Claude/codex/grok launch paths pinned clean for the same class
- [ ] #4 A pi spawn that dies pre-bind leaves the caller's bus row untouched
<!-- AC:END -->
