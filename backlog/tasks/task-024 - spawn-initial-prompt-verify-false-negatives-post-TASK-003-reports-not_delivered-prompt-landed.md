---
id: TASK-024
title: >-
  spawn initial-prompt verify: false negatives post-TASK-003 (reports
  not_delivered, prompt landed)
status: Done
assignee: []
created_date: '2026-07-07 08:35'
updated_date: '2026-07-07 09:29'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found live dispatching wave 3: 2 of 3 herder spawn calls reported 'prompt: NOT confirmed (verify: not_delivered, ready: status=done,stable)' yet hcom transcript shows both workers received the prompt and began executing (guids 2cfa1f6c unit-i, df6e5375 unit-k; the third, 11d5c38b, verified 'delivered'). The sigil/paste verify in the now in-process boot-paste engine (spawncmd/bootpaste.go) appears to race the agent's TUI start — in-process delivery is faster than the old shell-out, so the verify read may fire after claude clears/redraws. Orchestrator followed doctrine (read pane/transcript before resend), so no double-submit — but a false 'NOT confirmed' invites exactly that mistake. Investigate the verify race and make it reliable (or degrade its wording to 'unverified — check transcript').
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 root cause identified with evidence; verify no longer false-negatives on claude/codex spawn (or reports honestly-unverifiable wording)
- [x] #2 spawn suite covers the race (mock scenario or timing hook); 16 suites + go gates green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commits 72d6b9b + 19acb48 on unit-i-compact (fixed alongside TASK-022, same file ownership). ROOT CAUSE (live-evidenced with a 200ms sampler on a real spawn): (1) claude collapses the pasted prompt into a "[Pasted text]" blob — the raw text NEVER appears in recent-unwrapped reads, killing msgPresent/verifyDelivered; (2) the SUBMITTED echo re-raises the window-wide blob count, defeating waitBlobSubmitted and driving pointless extra Enters; (3) the herdr done->working status flip (~0.9s good case) can lag past the 3s verify window. All three signals miss -> false not_delivered despite clean submission (matches wave-3 guids 2cfa1f6c/df6e5375). FIX in bootpaste.go: composer-line-empty accepted as submission evidence, armed ONLY when (a) landing was POSITIVELY confirmed and (b) a read IMMEDIATELY before the single Enter still shows the payload in the composer (composerHoldsPayload — text trailing sigil or blob on sigil line; codex P2 hardening). Cleared-composer degrades to not_delivered, never delivered. Spawn suite: additive claude_echoloss scenario (mock: post-Enter echo loss + no status flip) — pre-fix binary FAILS it, fixed binary verifies delivered; all pre-existing spawn goldens byte-identical. Compact suite clear_before_enter pins the P2 degrade path. Gates 17/17 + go green; live: two probe spawns verified delivered through the fixed engine.
<!-- SECTION:NOTES:END -->
