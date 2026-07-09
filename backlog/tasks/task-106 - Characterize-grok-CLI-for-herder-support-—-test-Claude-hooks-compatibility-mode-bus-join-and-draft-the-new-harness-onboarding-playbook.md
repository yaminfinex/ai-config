---
id: TASK-106
title: >-
  Characterize grok CLI for herder support — test Claude-hooks compatibility
  mode, bus join, and draft the new-harness onboarding playbook
status: To Do
assignee: []
created_date: '2026-07-09 06:04'
labels: []
dependencies: []
priority: medium
ordinal: 106000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: investigate. Deliverable is a written characterization report plus a proven yes/no on bus integration — NOT merged herder code. If the findings call for code changes, capture them as a separate implement task with this report as its reference.

GOAL: determine what it takes for herder to spawn and manage grok (xAI's grok CLI, the command-line coding agent) as a first-class agent type, the way it manages claude and codex today. grok ships a Claude compatibility mode for hooks, so the working hypothesis (owner's) is that it may Just Work if herder can launch it and have it join the hcom bus — but that is a hypothesis to confirm empirically, not an assumption to build on.

BACKGROUND — how herder integrates a harness today (starting pointers, all under tools/herder/internal/):
- launchcmd/launch.go:21 — the tool switch currently admits claude|codex|gemini; per-tool launch argument construction below it.
- spawncmd/spawn.go (~:1457) — per-agent spawn handling; spawncmd/bootpaste.go — per-tool boot-paste/prompt-delivery differences.
- hookcmd/template.go — hook config generation (this is what binds a spawned agent to the hcom bus from birth: hooks fire on session start / turn events and register the agent).
- lifecyclecmd/lifecycle.go — fork/resume paths have per-agent branches.
- hcom side: agents bind via hooks + pty (see hcom docs/help); verified delivery ('queued' injects at the target's next turn) depends on turn-boundary semantics.

WHAT TO CHARACTERIZE (evidence = actual runs with logs/transcripts, not docs reading alone):
1. Launch mechanics: how grok starts in interactive terminal mode, what flags exist for config/hooks/permissions, whether it can be pointed at a project dir like claude/codex.
2. Hooks compatibility mode: how it is enabled; WHICH Claude hook events it implements (SessionStart, UserPromptSubmit, Stop/turn-end, PreToolUse/PostToolUse, ...) and which are missing or behave differently; whether hook stdout/context-injection semantics match Claude's.
3. Session/transcript behavior: where grok writes session files, their format, whether a session id exists that herder's registry and the observer could key on.
4. Prompt injection: whether text can be delivered into a running grok session and submitted (the pty paste path), and whether turn-end detection works for verified delivery.
5. The gap list: exactly which herder touchpoints (files above) need a grok case, and whether hcom needs anything new.

THE PLAYBOOK (second deliverable): generalize the findings into a draft 'new harness onboarding playbook' — the checklist any future CLI agent (grok, gemini, whatever comes next) must satisfy to join herder/hcom, e.g.: hook surface for bus binding, turn-end signal, prompt-injection path, session identity + transcript location, permissions/danger mode, fork/resume support or explicit non-support. Keep it as a doc (suggested home: docs/design/, dated), written so a future implement task can execute against it.

SAFETY RAILS: run all grok experiments in a scratch directory, never against this repo's live registry/bus state; if grok requires an account/API key that is not already configured on this machine, STOP and report that as a finding rather than signing up for anything. Do not modify herder code in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A characterization report exists (napkin or docs/design/, dated) covering all five numbered areas, each backed by at-least-one actual grok run (command + observed output quoted), or an explicit documented blocker (e.g. no API key on this machine) for areas that could not be exercised
- [ ] #2 The Claude-hooks-compatibility claim is tested empirically: a minimal hook config in grok's compatibility mode, with a table of which Claude hook events fired vs did not, and whether hook-injected context reached the model
- [ ] #3 A definitive answer to the owner's hypothesis: can a grok session be launched and join the hcom bus (register + receive a delivered message)? YES with a reproduced transcript, or NO with the specific missing capability named
- [ ] #4 A gap list names each herder file/function needing a grok case, each item marked trivial-switch-arm / needs-design / blocked-upstream
- [ ] #5 A draft new-harness onboarding playbook exists as a standalone doc, written harness-agnostically, with grok as its first worked example
- [ ] #6 No herder/hcom production code or live registry/bus state was modified; all experiments ran in scratch dirs
<!-- AC:END -->
