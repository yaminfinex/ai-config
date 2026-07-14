---
id: TASK-212
title: >-
  Fresh-tab placement must not steal focus — pass --no-focus on every pane move
  --new-tab
status: In Progress
assignee: []
created_date: '2026-07-14 22:43'
updated_date: '2026-07-14 22:44'
labels: []
dependencies: []
ordinal: 211000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER UX DEFECT (2026-07-14): every herder spawn/resume/fork yanks terminal focus to the new tab — if the owner is mid-typing to an agent, the keystrokes land in the fresh prompt. ROOT CAUSE: the fresh-tab placement path calls herdr pane move --new-tab WITHOUT a focus flag, inheriting herdr's focusing default — spawncmd/spawn.go:919 (spawn) and lifecyclecmd/lifecycle.go:777 (resume/fork) — while the split path already defaults FocusFlag='--no-focus' (spawn.go:284) and the worktree-create path already passes --no-focus (spawn.go:745). herdr supports 'pane move --new-tab [--focus|--no-focus]' (help-verified). FIX: append the focus flag to both move sites, defaulting --no-focus; honor an explicit '--focus' opt-in where a placement-flag surface already exists (spawn already parses --no-focus at spawn.go:387 — extend to --focus if trivial, otherwise default-only is fine); same default for the new-workspace/new-tab move variants if the code paths share the call. Verify goldens/help text that print placement lines. Tests: assert the constructed moveArgs carry --no-focus by default in both call sites. SCOPE FENCE: spawncmd/spawn.go + lifecyclecmd/lifecycle.go only — do NOT touch grokbridge/launchcmd-grok (an open lane owns them). NOTE: relief reaches the owner only at the next herder rebuild/deploy (already a pending owner item — this raises its priority).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Both pane move --new-tab call sites pass --no-focus by default; tests pin the argv
- [ ] #2 Explicit focus opt-in preserved or added where a flag surface exists; help/goldens updated
- [ ] #3 Full house battery green
<!-- AC:END -->
