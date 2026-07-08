---
id: TASK-065
title: >-
  registry mis-bind: row carries another agent's hcom_name (@viru on lale's
  row); wrong-name binds are sticky post-064
status: To Do
assignee:
  - hera
created_date: '2026-07-08 08:12'
labels: []
dependencies: []
ordinal: 65000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reported by lale (market-sim run, #9332, low priority): registry row lale-orchestrator (guid a9fcee3d) carries hcom_name @viru while the agent's actual bus name is @lale; the @lale row points at long-gone manual session edea1564. Suspected reconcile mis-bind. Observed impact: herder compact --then continuation addressed @viru instead of the orchestrator (harmless that run only because the primary wake was a direct worker report).

Investigation angles: (1) reconcile re-bind path — can assumed-continuity re-binding attach the WRONG live agent's coordinates/name to a row (or vice versa) when multiple candidates exist? TASK-046 reconcile refuses ambiguity all-or-nothing, but name enrichment happens separately via sidecar; (2) post-064 carry semantics can now FAITHFULLY PRESERVE a wrong hcom_name once recorded — carry-forward makes a bad bind sticky, raising the cost of mis-binds (correct wrong names needs an owned-field write from the name owner, i.e. sidecar re-capture; verify that path exists for orchestrator rows without a sidecar); (3) is there a repair verb? rename fixes labels, not hcom_name; sidecar owns hcom_name (TASK-043) but manual/orchestrator sessions may have no sidecar to re-capture. May need herder-level "rebind bus name with verification" or reconcile extension. Related: TASK-060 (F1/F2 reconcile polish). Evidence to collect at fix time: the two rows' full history from lale's registry.
<!-- SECTION:NOTES:END -->
