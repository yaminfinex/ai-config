---
id: TASK-011
title: >-
  hcom launch: alarming '.claude.json not found / backup exists' lines in
  headless logs
status: Done
assignee:
  - unit-k-zeno
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 08:56'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-001 finding: hcom launch's .claude.json swap prints alarming 'config not found / backup exists' lines in headless launch logs. Cosmetic but reads like data loss; consider quieting or clarifying (may belong upstream in hcom — decide whether to patch shim-side or file upstream).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause + emitter pinned with live-repro evidence in task notes + investigation napkin: lines are printed by Claude Code at startup (stderr) when CLAUDE_CONFIG_DIR points at a dir lacking .claude.json while its backups/ holds old snapshots; herder trigger = launchcmd.PinConfigDir pinning CLAUDE_CONFIG_DIR=$HOME/.claude under an isolated HCOM_DIR. hcom v0.7.22 source contains NO .claude.json swap — misattribution corrected on the record.
- [x] #2 Remedy per hera ruling: (a) launchcmd seed-on-pin — when PinConfigDir sets CLAUDE_CONFIG_DIR=$HOME/.claude and $HOME/.claude/.claude.json is missing but $HOME/.claude.json exists, seed it by copy, silent degrade to current behavior on any failure — and/or (b) docs-only known-cosmetic note. Any launchcmd edit is reported on thread unit-k BEFORE editing.
- [x] #3 Live smoke: reproduce the exact lines against a scratch CLAUDE_CONFIG_DIR (no real-state mutation), and — if (a) lands — show the herder pin path no longer produces them (before/after under scratch HOME + isolated bus).
- [x] #4 Docs hygiene: troubleshooting note in tools/herder README (or docs/) naming the message, trigger, and cosmetic nature; every touched/verified surface named in DONE report.
- [x] #5 Upstream decision recorded in notes: nothing filed externally; explicit verdict on whether an hcom or Claude Code upstream writeup is warranted, draft text in report if yes.
- [x] #6 Pinned gate green (go vet/test herder+bottle, 16/16 check battery with env -u), incl. new test covering the seed path if (a) lands.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit a3d42e6 (branch unit-k-launch-noise), accepted by hera #2125. ROOT CAUSE (overturns premise): the lines are printed by Claude Code itself at startup, NOT hcom — hcom v0.7.22 source (cloned, audited) contains no .claude.json swap; misattribution corrected on record. Trigger: launchcmd.PinConfigDir under isolated HCOM_DIR pins CLAUDE_CONFIG_DIR=$HOME/.claude, re-rooting claude top-level config from ~/.claude.json to ~/.claude/.claude.json (missing) while ~/.claude/backups/ holds real snapshots -> claude prints the not-found/backup-exists/restore triple into hcom background_*.log. FIX: seed-on-pin — copy ~/.claude.json to the re-rooted path when our pin fires and target missing; never overwrites, silent degrade, claude-only; setEnvDefault returns whether it set so user presets are never seeded. +5 go tests. VERIFICATION: go vet/test green herder+bottle, battery 16/16 env -u, ZERO golden regens (pin scenarios use empty case-homes). Live smokes: scratch CLAUDE_CONFIG_DIR + stock claude 2.1.202 -p reproduced the exact double-printed triple (exit 0, PONG -> cosmetic); worktree herder launch claude in env -i scratch HOME + isolated bus booted to Welcome-back REPL, no noise, no onboarding, seed byte-identical -> hera addendum (ii) proven, --team caveat wording updated (spawn --help, hera-authorized cross-unit help-text edit; unit-h notified). Docs: README team-bus paragraph + troubleshooting note, launch --help, spawn --help; spawn-patterns.md/herder-delta.md/skills verified unchanged. UPSTREAM: no hcom filing (emits nothing, swaps nothing); optional unfiled Claude Code UX feedback draft in DONE report. USER DECISION pending (hera surfacing): pre-existing divergent ~/.claude/.claude.json (TASK-001 artifact) untouched — keep/re-align/delete options in napkins/task-011-investigation.md. Sliding doors: rejected launch-contract seed probe (would touch all 22 goldens for coverage go tests already give); seed deliberately claude-only (codex/gemini keep state inside pinned dirs). Follow-up spawned: TASK-025 (ai-doctor divergence check).
<!-- SECTION:NOTES:END -->
