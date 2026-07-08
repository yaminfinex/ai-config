---
id: TASK-048
title: >-
  adopt ctrl+u composer-clear in recovery paths (bootpaste/spawn recovery,
  cull/send help text) — clear-key doctrine falsified live on 0.7.3
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 05:08'
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:08
---
vibe (herdr-0.7.3 audit, bus #5689, applied by hera): Verification half DONE — doctrine FALSIFIED live on 0.7.3: a composer-clear key exists and works end-to-end. Evidence (2026-07-08, disposable probes keyprobe-b4a557db/clprobe-e7ebebb4/cxprobe-4e5b86b5, all culled): (1) `herdr pane send-keys` accepts herdr-native combo strings — ctrl+u and backspace accepted+delivered; tmux-style C-u/BSpace still rejected invalid_key (capability is new; docs must not suggest tmux syntax). (2) Claude composer: ctrl+u clears cleanly (native kill-line, 'Ctrl+Y to paste deleted text' shown). (3) Starvation re-confirmed on 0.7.3: queued hcom message did NOT inject while composer polluted. (4) Clear->unblock proven: after ctrl+u the starved message injected immediately and the probe replied on the bus (#5661 UNBLOCKED). (5) Codex composer: ctrl+u clears too (placeholder restored). New doctrine for spawn-patterns.md:155: polluted composer -> `herdr pane send-keys <pane> ctrl+u`; queued bus delivery resumes at next boundary. Submit-through and codex double-submit tolerance no longer load-bearing. BOARD DECISION (hera): task re-scoped to the remaining CODE half — adopt ctrl+u in bootpaste/spawn recovery + cull/send help text; the doc rewrite folds into TASK-049's sweep.
---
<!-- COMMENTS:END -->
