---
id: TASK-291
title: >-
  harness /fork of a bus-bound session strips identity env and re-registers a
  fresh bus name, stranding the standing identity
status: To Do
assignee: []
created_date: '2026-07-20 04:25'
labels: []
dependencies: []
ordinal: 290500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed live: a harness-level conversation fork (/fork) of a long-lived orchestrator session stripped HCOM_SESSION_ID/HERDER_GUID from the environment; the session-start hook then treated the continuation as a NEW session and minted a fresh bus identity, leaving the standing bus name as an idle ghost row. Every peer/crew report addressed to the standing name would have queued against the ghost indefinitely — silent starvation of the orchestration seat. The herder registry seat was UNAFFECTED (guid/label/pane intact); only the bus binding drifted.

Recovery (proven, clean): back up the bus db, then `hcom start --as <standing-name>` from the continuation session — documented reclaim path ("after compaction/resume/clear"); the ghost row rebinds to the live session and the freshly minted name is retired. Explicit-prefix verb practice (passing pane id + guid literally on herder verbs) kept lifecycle verbs working throughout; only inbound bus delivery was at risk.

Unit shape: (1) detection — the session-start hook (or hcom) should notice a registry seat whose pane matches the current session but whose bus name differs from the newly minted one, and at minimum WARN loudly (better: offer/auto reclaim); (2) consider whether fork-shaped continuations can carry identity env through; (3) doc: add this class to the identity-hazards doc (it is adjacent to the vendor-CLI identity-hijack class but triggered by the harness fork command, with inverse symptom — abandonment, not takeover).
<!-- SECTION:DESCRIPTION:END -->
