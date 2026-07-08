---
id: TASK-067
title: hcom bus-snapshot writer for statusline state files (reader shipped in 063)
status: To Do
assignee:
  - hera
created_date: '2026-07-08 09:31'
labels: []
dependencies: []
ordinal: 67000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From TASK-063 (vibe #10333): the claude statusline bus-snapshot segment ships READER-ONLY — it reads $HCOM_DIR/statusline/<instance>.env (override HCOM_STATUSLINE_STATE, integer-whitelisted keys HCOM_UNREAD/HCOM_LAST_AGE_S) and degrades to omission when absent. The WRITER does not exist yet: event-driven, atomic write of the documented .env contract per instance. Likely home is hcom hook or sidecar territory — was deliberately fenced out of 063 while wave A was live in registry/sidecar files. Design constraints: no per-render subprocess (the whole point), atomic replace (tmp+rename), one file per instance, cheap on every bus event. Sequencing: after wave A closes; touches sidecarcmd/hookcmd so respect in-flight fences.
<!-- SECTION:NOTES:END -->
