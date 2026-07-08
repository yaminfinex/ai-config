---
id: TASK-051
title: >-
  herder fork: native path fails under herdr 0.7.3 — 'launch failed before
  lifecycle bind', pane dies instantly (spawn path works)
status: To Do
assignee: []
created_date: '2026-07-08 05:08'
updated_date: '2026-07-08 09:14'
labels: []
dependencies: []
priority: medium
ordinal: 51000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera, 2026-07-08, herdr 0.7.3): herder fork --self --label spec-hera --split down failed twice with 'herder-lifecycle: launch failed before lifecycle bind'; registry rows were created (c2c0821b, c0f9f401) and self-closed correctly, but the pane died before any bind — herder wait found the terminal not live anywhere. Same session, same epoch: herder spawn works (bash probe AND a claude spawn with --extra-arg --resume <sid> --extra-arg --fork-session, which bound, delivered, and verified — that is the documented WORKAROUND for forking until this is fixed). So the native fork launch path (hcom-fork-based) broke under herdr 0.7.3 while the spawn/launch path survived. x-ref TASK-046 (protocol v14 changes); suspect the fork-specific pane/launch call uses a request shape or seed-pane dance that 0.7.x rejects. Fix after or alongside TASK-046; add a fork acceptance check to the herdr-upgrade runbook gate.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:20
---
Second independent repro (spec-ravu #5816, same session-fork exercise). CORRECTION to their read: the orphan rows c0f9f401/c2c0821b are NOT stuck — registry shows both status=closed with close_reason='pane exited before lifecycle bind' (self-cleanup worked). The real nit: 'herder cull --guid <closed>' refuses with 'no matching active records', which reads like a failure and misled a second orchestrator — the refusal should say 'already closed at <ts> (<reason>)'. Fold that message fix into this ticket.
---

created: 2026-07-08 08:30
---
[hera 2026-07-08] +message-polish item from kato #9547 (A3 residual LOW): node.go malformed-marker refusal with len(nodes)==1 suggests 'rerun with --new' but --new re-refuses in that exact state — drop the suggestion there or make --new restore-from-single-row. Joins fork-fix + cull-message items.
---

created: 2026-07-08 09:14
---
[hera 2026-07-08] +3 polish items from bozo #10145: (1) LOW latent: new-tab re-fetch queries the PRE-move pane id with no terminal_id fallback — fine while new-tab moves are same-workspace, add the fallback if that doctrine ever shifts; (2) NIT: compactMessage (spawn.go:1533) strips only whitespace, control/ANSI bytes survive into the human stderr summary; (3) NIT: dead write opts.Tab (spawn.go:809).
---
<!-- COMMENTS:END -->
