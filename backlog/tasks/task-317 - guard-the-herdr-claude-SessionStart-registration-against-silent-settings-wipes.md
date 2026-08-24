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

- [x] check-live-contract.sh (or a sibling check) asserts the herdr
      SessionStart registration is present in ~/.claude/settings.json, so
      the wipe class fails a gate instead of silently degrading resolution
      evidence.
- [x] The assertion distinguishes "hook script missing" from "settings
      registration missing" in its failure message.
- [ ] Note filed upstream (herdr): `integration status` should verify the
      live registration, not just the script file.
- [ ] Optional follow-up recorded if the settings-rewrite writer is
      identified.

## Gate shipped — 2026-08-24

check-live-contract.sh now pins both registrations in the real
~/.claude/settings.json: the herdr SessionStart hook (registration
presence and hook-script-on-disk are distinct failures with distinct
remedies) AND the hcom sessionstart hook, which post-teardown is the
only self-registration path onto the bus — the same wipe class would
sever both. A negative demo runs the same assertion against a
wipe-signature fixture (valid JSON, user hooks intact, registrations
gone) and must reject it. Live run: PASS=16 FAIL=0 SKIP=0; full
battery + fleet suite 18/18 green. Remaining: the upstream note to the
herdr maintainer (no tracker known from this box) — owner to route.
