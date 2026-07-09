---
id: TASK-096
title: 'sesh U4 — shipper: discovery, cursors, tailing (M1)'
status: To Do
assignee: []
created_date: '2026-07-09 05:28'
labels:
  - sesh
dependencies:
  - TASK-093
  - TASK-094
priority: high
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: shipper (claude worker). Deliverable: sesh ship moving bytes for its OS user — discovery (fsnotify hint + 60s authoritative rescan over the globs: <uuid>.jsonl under ~/.claude/projects/** and rollout-*-<uuid>.jsonl under ~/.codex/sessions/**; watched root may be a symlink, symlinks below not followed), file identity (uuid + SHA-256 fingerprint per wire doc), cursor registry, backfill from 0, churn handling per the plan file-identity state diagram — implement it literally. Requirements R1,R2,R4,R23.

Cursor registry (R23): single per-user JSON file under ${SESH_STATE_DIR:-$XDG_STATE_HOME/sesh}, atomic temp+fsync+rename under exclusive flock (flock held for daemon lifetime = single-instance lock), schema_generation field; an older writer refuses with a typed error naming cause and remedy — error text must NEVER advise deleting the registry (herder-incident lesson: read docs/specs/herder-spec.md sections 5.2/5.4 and backlog tasks 083/084 on main). Unreadable registry -> rebuild via rescan + recovery GET. Store down -> hold cursor, jittered backoff, no local queue.

Execution note: characterization-first — encode each fixture churn case as a failing test BEFORE writing the state machine.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U4 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), docs/specs/sesh-wire.md on sesh-build, captures Lane 1 settled decisions (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u4.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Cold-start backfill ships a pre-seeded fixture tree fully (S1)
- [ ] #2 Truncate below cursor -> single reset + re-ship, no loop (S3); move mid-tail -> no re-ship (S4); delete -> cursor GC only (S5)
- [ ] #3 Same-path recreate >=1KiB -> fingerprint mismatch -> reset; <1KiB -> size-regression rule catches it first
- [ ] #4 kill -9 mid-file + restart -> no loss, replay absorbed; store unreachable -> cursor holds, memory flat
- [ ] #5 Higher schema_generation -> typed refusal, non-destructive message; corrupt registry -> rebuild via rescan + recovery GET
<!-- AC:END -->
