---
id: TASK-145
title: >-
  herder instructions reach only herder-launched sessions — raw/resumed codex
  gets stock hcom bootstrap advertising the wrong spawn recipe
status: To Do
assignee: []
created_date: '2026-07-10 01:41'
labels: []
dependencies: []
priority: high
ordinal: 145000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FOUND LIVE (2026-07-10, owner-hit): a raw `codex resume` of session laze had NO herder operating instructions in context — only the stock hcom [HCOM SESSION] bootstrap, which actively advertises `Spawn agents: hcom <n> <tool>` and an `hcom start` recovery hint. laze followed it and launched unmanaged reviewer agents (no registry rows, wrong panes/permissions/bootstrap); owner had to kill them.

MECHANICS (verified): the herder-aware block ("## AGENTS (herder lifecycle)" + an explicit SUPERSEDED paragraph overriding hcom spawn/kill/resume recipes) is injected ONLY by herder spawn generated launch scripts (~/.hcom/.tmp/launch/codex_*.sh). hcom hooks in ~/.codex/config.toml fire for ANY codex session, so raw launches/resumes get the stock template with the competing recipe. The unmanaged path is the only one advertising itself to unmanaged sessions.

DIRECTIONS TO EVALUATE: (a) machine-wide hcom template override so the stock bootstrap on this box points at herder (does hcom config support template customization?); (b) herder observer/sidecar adoption injects or delivers the overlay when it recognizes an unmanaged-but-seated session; (c) at minimum, herder resume re-injects the overlay (managed resume may already — verify) and docs tell operators to prefer herder resume over raw codex resume. Coordinate with the spawn/resume placement work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Verified statement of which launch/resume paths carry the herder overlay today (spawn, herder resume, fork, raw claude, raw codex, raw codex resume)
- [ ] #2 Chosen mechanism implemented so raw-launched/resumed sessions on this machine no longer see the bare hcom spawn recipe without the herder supersede
- [ ] #3 Regression check covering the injection (script-level test or documented manual verification)
<!-- AC:END -->
