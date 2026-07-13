---
id: TASK-191
title: Harden grok binder identity env against launching-pane tool signals
status: To Do
assignee: []
created_date: '2026-07-13 13:01'
labels: []
dependencies: []
ordinal: 190000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from the activation unit's review chain (reviewer direction (b), deferred to keep activation lean). The grok bridge binder's identity invocation (runHcomSeatIdentity) builds its env from os.Environ(), scrubbing only HCOM_PROCESS_ID/CODEX_THREAD_ID/HCOM_TAG — it inherits the launching pane's tool signals (CLAUDE*/CLAUDECODE). hcom start keys claude-hook-install-and-exit-1 off those vars (suppressed only by HCOM_LAUNCHED/adhoc signals), so a binder started from a Claude-pane context without a launched signal can hit hcom's hook-install path instead of binding. Reachability today is narrow (herder-spawned panes carry HCOM_LAUNCHED; bare terminals carry no CLAUDE*), but the binder should present a DETERMINISTIC identity env regardless of who launched it: allowlist-build the identity-invocation env (house rule: allowlists on security boundaries) rather than scrub-listing os.Environ(). Includes: pin that a binder launched with hostile/foreign tool signals still binds adhoc and never triggers hook installation. Evidence and bisection: activation review thread, reviewer round-2/round-3 findings.
<!-- SECTION:DESCRIPTION:END -->
