---
id: TASK-048
title: >-
  herdr-0.7.3 audit: re-test composer-clear via pane.send_keys key-combos (0.7.0
  #613) — 'no key clears a polluted composer' doctrine may be stale
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
labels: []
dependencies: []
priority: medium
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

Upstream pane.send_keys/pane.send_input.keys now accept Herdr key-combo strings (ctrl+h/j/k/l cited; parser likely general). Our doctrine (docs/spawn-patterns.md:155) says only Enter/esc/C-c are accepted and BSpace/C-u are rejected invalid_key, hence 'just submit through pollution'. Re-test ctrl+u / ctrl+backspace on a scratch pane on 0.7.3; if a clear key exists, update spawn-patterns doctrine and give bootpaste/send a real polluted-composer recovery path instead of submit-through (codex double-submit tolerance no longer load-bearing). Small, self-contained, no blockers.
<!-- SECTION:DESCRIPTION:END -->
