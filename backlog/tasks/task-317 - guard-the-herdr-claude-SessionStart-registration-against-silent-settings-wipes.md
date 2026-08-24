---
id: task-317
title: Guard the herdr claude SessionStart registration against silent settings wipes
status: To Do
assignee: []
created_date: '2026-08-23'
labels: [herder, plumbing]
dependencies: []
---

## Teardown reconciliation — 2026-08-24

Remains open and becomes more important: herdr's SessionStart binding is a
surviving placement/session signal, and the live-contract tripwire still does
not verify the settings registration.

## Description

On 2026-08-21 a wholesale rewrite of ~/.claude/settings.json (writer
unidentified) silently dropped the herdr SessionStart hook registration.
Every claude started afterwards never reported agent_session to herdr, so
herdr showed those panes as session-less (detection-lost) fleet-wide.
`herdr integration status` did NOT catch it — it verifies the hook script
file exists, not that the settings registration survives. Root-cause
writeup: missions fleet-refit, probe-contract Field addendum 3 (c4e748b).
Restored 2026-08-23 with `herdr integration install claude` (idempotent).

## Acceptance criteria

- [ ] check-live-contract.sh (or a sibling check) asserts the herdr
      SessionStart registration is present in ~/.claude/settings.json, so
      the wipe class fails a gate instead of silently degrading resolution
      evidence.
- [ ] The assertion distinguishes "hook script missing" from "settings
      registration missing" in its failure message.
- [ ] Note filed upstream (herdr): `integration status` should verify the
      live registration, not just the script file.
- [ ] Optional follow-up recorded if the settings-rewrite writer is
      identified.
