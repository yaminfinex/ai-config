---
id: TASK-179
title: >-
  sesh — T3: store deployment on quick-host (unit, user, backup, deploy-store,
  release ergonomics)
status: In Progress
assignee: []
created_date: '2026-07-13 02:22'
updated_date: '2026-07-13 02:53'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 178000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
doc-002 T3 as refined by the ratified design (docs/design/2026-07-12-sesh-store-served-distribution.md §2b hosting shape, §7 publishing, §8 exposure + admin ask). Two phases.

BUILD PHASE (agent, repo artifacts only, NO VM access): idempotent ops bootstrap script (create sesh OS user, /var/lib/sesh incl. tsnet dir, install system unit, auth-key handoff via TS_AUTHKEY once at first start); etc system unit sesh-serve.service (dedicated user, --tsnet, hostname sesh, data dir /var/lib/sesh); just deploy-store recipe mirroring quick's deploy-server shape (CGO_ENABLED=0 linux/amd64 build, gcloud IAP scp, sudo install, restart, print running version) — quickd/Caddy/VM tailscaled untouched by construction; sesh-owned backup script + system timer to the GCS bucket pattern under a separate sesh prefix (sqlite snapshot-API copy never live-file, mirror copied in recoverable ordering, tsnet dir included) — intentional refinement of doc-002's 'rider on quick's timer': same machinery, zero quick-owned files; release ergonomics: just tag helper (monorepo-prefixed sesh-vX.Y.Z) and default dest for just release; escape triggers (mirror size / quickd incident / team growth) + skew policy paragraph (store-first, wire-v2-is-fleet-event, schema forward-only, current+previous window) recorded in ops docs; owner execution runbook (key arrives → bootstrap → deploy-store → deny-verify pair → first tagged release → Slack announcement) in README/ops docs per decision-001.

EXECUTION PHASE (owner-driven, after tailnet admin delivers the tag:sesh auth key): run bootstrap, deploy, record deny-verification (403/refusal outside grant, 404 inside) BEFORE real transcript flow, publish first release, restore-drill the backup once.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Bootstrap script idempotent and unit-tested where testable; second run is a no-op; no repo-path assumptions
- [x] #2 just deploy-store builds, ships via gcloud IAP, restarts, prints the running store version; recipe never overwrites the only known-good binary without the versioned path/atomicity the design requires
- [x] #3 Backup script + timer cover mirror + store.sqlite (snapshot API) + tsnet dir in recoverable ordering; restore drill documented step-by-step
- [x] #4 just tag + default release dest land; tagging documented in README release section
- [x] #5 Escape triggers + skew policy recorded in ops docs; README runbook updated end-to-end (admin key → announce); decision-001 honored
- [ ] #6 EXECUTION: store reachable at http://sesh.<tailnet>.ts.net:8765, deny-verify pair recorded before real transcript flow, first tagged release published, backup restore drill performed once
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
BUILD PHASE complete on branch task-179-store-deployment (95938f8 ops/justfile/docs, d94a52e gate) — substance review by reviewer-sune: original report bus #54891 (1 P1), F1 closure + CLEAN/MERGE-READY verdict bus #54981; three recorded deviations explicitly ACCEPTED (ops/ placement, no auto-restore on empty data dir, sesh-owned backup timer). Explicit coverage gaps (all owner-runbook items, untestable without VM/tailnet/root): tsnet join + key handoff end-to-end; gcloud IAP scp/ssh + GCS rsync stub-asserted only; useradd/chown/setgid effects skipped under non-root test seam; sqlite3 CLI shimmed via python3 locally; ssh alias + IAP ProxyCommand documented not executed. AC6 = EXECUTION PHASE, owner-driven, blocked on tailnet admin delivering the tag:sesh auth key.
<!-- SECTION:NOTES:END -->
