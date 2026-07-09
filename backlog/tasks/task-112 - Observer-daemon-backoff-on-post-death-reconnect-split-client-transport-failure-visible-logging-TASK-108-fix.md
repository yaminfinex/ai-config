---
id: TASK-112
title: >-
  Observer daemon: backoff on post-death reconnect + split-client transport +
  failure-visible logging (TASK-108 fix)
status: To Do
assignee: []
created_date: '2026-07-09 07:14'
labels: []
dependencies: []
priority: high
ordinal: 112000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: implement.

Fix the observer daemon busy-loop diagnosed in TASK-108 (full diagnosis: napkins/run-herder-dx/2026-07-09-task-108-observer-busy-loop-diagnosis.md; summary below is self-sufficient if the napkin is gone).

DEFECT: tools/herder/internal/observercmd/observer.go runDaemon (~:848-905): the connection-died-after-successful-connect path has NO backoff — after ticker.Stop()/client.Close() at the loop bottom, control returns straight to connectHerdrSocket(). Backoff (waitOrSignal) guards only the connect-failure and subscribe-failure paths. When the herdr server closes every connection after its first request (observed live: herdr 0.7.3 post-update-handoff enforces one-request-per-connection while still self-reporting protocol 16), the daemon reconnects at ~5.5 cycles/s, parsing the full registry twice per degraded sweep = sustained ~34% CPU with a 0-byte log, while hd.available=false means all pane-seat observation silently never runs (inert witness).

BUILD (three parts, all from the diagnosis fix-shape):
1. Backoff after the post-death client.Close() — same waitOrSignal(interval, signals) treatment the other two paths get. This alone drops CPU to ~0%.
2. Split-client connection model: one subscribe-only connection for the event stream + a FRESH connection per RPC (session.snapshot etc.). Diagnosis verified this works against the live one-request-per-connection server today — it makes the daemon functional (not just quiet) under the new herdr semantics, and remains correct against servers that allow multi-request connections.
3. Observability of the failure class: log scanner errors in readLoop (currently scanner.Err() is never checked, socket.go ~:201), log reconnect transitions with cause, and STOP touching the heartbeat file on failed sweeps (a heartbeat that advances while every sweep fails is what made this daemon look healthy).

SETTLED DECISIONS (do not reverse; escalate if blocked):
- Do not add any write authority or repair behavior — daemon stays observation-only per the ratified spec and the memo invariants.
- The epoch rule and positive-evidence-only write discipline are untouched; this is a transport/loop fix.
- Do not work around by reverting herdr: the daemon must be robust to a server that closes per-request AND to one that does not.

ALSO RECORD (not this task's code): herdr upstream changed connection semantics without a protocol bump — candidate for the owner-gated upstream-filing batch at closeout.

ADVERSARIAL REVIEW: mandatory (behavior-carrying daemon loop).

VALIDATION BEYOND TESTS: after merge, restart the daemon on the fixed build and verify against the LIVE herdr: CPU <2% sustained over 10+ min, hd.available=true on sweeps, protocol_compatible=true, log shows subscribe stream alive. That restart also restarts the observer BAKE clock from zero (the prior bake is invalidated — daemon was an inert witness throughout).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Post-death reconnect path waits interval (signal-aware) before reconnecting; a test proves no tight reconnect loop when the server closes connections immediately
- [ ] #2 Daemon works against a one-request-per-connection server: subscribe stream on its own connection stays alive, RPCs use fresh connections, sweeps succeed (test with a mock herdr enforcing close-after-first-request)
- [ ] #3 readLoop scanner errors and reconnect transitions are logged with cause; failed sweeps do NOT advance the heartbeat file (test asserts heartbeat stalls while sweeps fail)
- [ ] #4 Existing observer contract suite still ALL GREEN (incl. T-11d socket-generation semantics preserved across the split-client change)
- [ ] #5 go vet + go test clean in tools/herder and tools/bottle; full check suite ALL GREEN bare from repo root
- [ ] #6 Live validation after merge: daemon restarted on fixed build against live herdr shows CPU <2% over 10+ min, hd.available=true, nonempty log; bake clock restart recorded in the run-log
<!-- AC:END -->
