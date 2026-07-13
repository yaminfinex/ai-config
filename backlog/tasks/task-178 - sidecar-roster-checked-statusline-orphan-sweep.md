---
id: TASK-178
title: 'sidecar: roster-checked statusline orphan sweep'
status: To Do
assignee: []
created_date: '2026-07-13 01:40'
labels: []
dependencies: []
priority: low
ordinal: 177000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from the statusline rename fix's final review (recommended by the reviewer as its own task): the sidecar cannot clean a renamed session's PRE-RENAME frozen-name snapshot file (frozen name absent from its env; renderer-side deletion would be an unroster-checked hot-path delete — rejected). A roster-checked orphan sweep in the sidecar — remove statusline/<name>.env whose HCOM_LIVE_NAME matches no live row's base name — cleans that class, the pre-fix-format files (no HCOM_LIVE_NAME marker, currently refused+retried until rewritten), AND the pre-existing dead-session orphans main already leaks (3 stale files observed live). Constraints: sweep must be roster-guarded (never delete a file any live row owns), rate-limited (not per-tick), and covered by live-env-shaped tests (no HCOM_INSTANCE_NAME/HCOM_PROCESS_ID injection into sidecar env).
<!-- SECTION:DESCRIPTION:END -->
