---
id: TASK-011
title: >-
  hcom launch: alarming '.claude.json not found / backup exists' lines in
  headless logs
status: In Progress
assignee:
  - unit-k-zeno
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 08:42'
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
- [ ] #1 Root cause + emitter pinned with live-repro evidence in task notes + investigation napkin: lines are printed by Claude Code at startup (stderr) when CLAUDE_CONFIG_DIR points at a dir lacking .claude.json while its backups/ holds old snapshots; herder trigger = launchcmd.PinConfigDir pinning CLAUDE_CONFIG_DIR=$HOME/.claude under an isolated HCOM_DIR. hcom v0.7.22 source contains NO .claude.json swap — misattribution corrected on the record.
- [ ] #2 Remedy per hera ruling: (a) launchcmd seed-on-pin — when PinConfigDir sets CLAUDE_CONFIG_DIR=$HOME/.claude and $HOME/.claude/.claude.json is missing but $HOME/.claude.json exists, seed it by copy, silent degrade to current behavior on any failure — and/or (b) docs-only known-cosmetic note. Any launchcmd edit is reported on thread unit-k BEFORE editing.
- [ ] #3 Live smoke: reproduce the exact lines against a scratch CLAUDE_CONFIG_DIR (no real-state mutation), and — if (a) lands — show the herder pin path no longer produces them (before/after under scratch HOME + isolated bus).
- [ ] #4 Docs hygiene: troubleshooting note in tools/herder README (or docs/) naming the message, trigger, and cosmetic nature; every touched/verified surface named in DONE report.
- [ ] #5 Upstream decision recorded in notes: nothing filed externally; explicit verdict on whether an hcom or Claude Code upstream writeup is warranted, draft text in report if yes.
- [ ] #6 Pinned gate green (go vet/test herder+bottle, 16/16 check battery with env -u), incl. new test covering the seed path if (a) lands.
<!-- AC:END -->
