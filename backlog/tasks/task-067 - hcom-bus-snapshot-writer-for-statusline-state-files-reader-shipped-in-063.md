---
id: TASK-067
title: hcom bus-snapshot writer for statusline state files (reader shipped in 063)
status: Done
assignee:
  - hera
created_date: '2026-07-08 09:31'
updated_date: '2026-07-08 10:54'
labels: []
dependencies: []
ordinal: 67000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From TASK-063 (vibe #10333): the claude statusline bus-snapshot segment ships READER-ONLY — it reads $HCOM_DIR/statusline/<instance>.env (override HCOM_STATUSLINE_STATE, integer-whitelisted keys HCOM_UNREAD/HCOM_LAST_AGE_S) and degrades to omission when absent. The WRITER does not exist yet: event-driven, atomic write of the documented .env contract per instance. Likely home is hcom hook or sidecar territory — was deliberately fenced out of 063 while wave A was live in registry/sidecar files. Design constraints: no per-render subprocess (the whole point), atomic replace (tmp+rename), one file per instance, cheap on every bus event. Sequencing: after wave A closes; touches sidecarcmd/hookcmd so respect in-flight fences.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 10:24
---
Dispatched: codex worker task067-ba2a7445 (bus @task067-sumo), worktree /home/grace/.herdr/worktrees/ai-config/task-067-statusline-writer, branch task-067-statusline-writer off main 22297c1. Brief: napkins/run-herder-dx/brief-067.md — mandatory design-note checkpoint to @hera before implementation (home choice sidecarcmd vs hookcmd; HCOM_LAST_AGE_S staleness tension: timestamp-key contract extension vs bounded staleness). Adversarial opus review to follow DONE per doctrine.
---

created: 2026-07-08 10:44
---
Round 1: worker DONE 84a60b1; orchestrator gate independently GREEN from worker worktree (vet+test herder+bottle OK, 27/27 suites incl new check-statusline-snapshot.sh). Opus adversarial review (review067-naso, #11129): REQUEST-CHANGES soft — P2-A base_name collision (two rows sharing base_name -> per-tick write churn + readers shown each other's data; data half inherited from 063 reader contract; fix = per-tick collision dedupe: remove owned file + skip while collision persists + doc), P2-B design-note release-time self-cleanup unimplemented, LOW-1 doc the unbounded AGE_S staleness for non-EPOCHSECONDS readers, LOW-2 writer->reader integration test + diff>2s rewrite boundary with unread unchanged, LOW-3 optional boot double-list. Clean angles: multi-writer ping-pong, filename traversal, override-path safety, nil-rows no-wipe, reader regression. Fix round dispatched (#11141); reviewer standing by for delta re-verdict.
---

created: 2026-07-08 10:54
---
Round 2: fix commit 3849e6e — orchestrator regate green from worker worktree; opus delta re-verdict APPROVE (#11274: P2-A collision dedupe cannot flap, remove-once verified; P2-B release self-cleanup correct, foreign files untouched; LOW-1 doc accurate; LOW-2a genuine writer->reader integration with cleanup verified; LOW-2b >2s boundary proven; LOW-3 skipped as optional). Merged main 3a92aaa (no-ff); post-merge gate on main FROM REPO ROOT: vet+test herder+bottle OK, 27/27 suites. Residuals recorded by reviewer as non-blocking: transient roster-disagreement window (self-heals next tick), double-release ENOENT no-op, temp-test orphan on hard kill (loud-fail). Worker task067-ba2a7445 + reviewer review067-4554fe5d culled; worktree/branch task-067-statusline-writer removed.
---
<!-- COMMENTS:END -->
