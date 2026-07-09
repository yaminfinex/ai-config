---
id: TASK-088
title: >-
  sesh-deploy: store hosting, tailnet join + grant policy, shipper packaging +
  node-by-node rollout
status: To Do
assignee: []
created_date: '2026-07-09 04:11'
labels: []
dependencies: []
priority: medium
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
sesh is a team-visibility service for AI coding sessions. Every machine (node) runs a small per-OS-user agent (the SHIPPER) that tails the transcript files Claude Code and Codex CLI already write to disk, and ships their raw bytes plus four identity facts to one central service (the STORE), which keeps a byte-faithful mirror, parses it centrally into a per-message index, and serves one read-only web page (the SURFACE) answering 'what has everyone been working on?'

UNIT TYPE: implement. Designer-authored capture (docs/design/2026-07-09-sesh-task-captures.md @ 6843649 on branch sessions-missions-design) — the settled-decisions list below is DO-NOT-REVERSE; if one seems wrong or blocking, STOP and escalate to @hera (who routes to tomo/owner). Never substitute and disclose later.

PINNED REFS (read in this order, all at commit e58f50a on branch sessions-missions-design; a worker starting from main runs: git fetch origin sessions-missions-design, then git show e58f50a:<path>):
1. docs/specs/session-service-spec.md — THE CONTRACT, read fully. Section refs below (§N); I-n = invariants (§3.3); S-n = acceptance scenarios (§6).
2. docs/design/2026-07-09-session-service-build-brief.md — working mode + verify-early items.
3. docs/design/2026-07-09-session-shipping-prior-art.md — why each mechanism, with upstream bug refs.
(If the branch has merged to main by pickup, paths are the same; e58f50a stays the pinned wording.)

SEQUENCING: lanes 1+2 (shipper/store) freeze the spec §8 wire contract together first, in a short shared doc PR, before parallelizing. Lane 3 (surface) depends only on the index schema. Lane 4 (deploy) is unblocked the moment the store boots anywhere.

BUILD: stand the store up (short-term host per owner: wherever is convenient — localhost-class; the URL is the only coupling), tailnet join + grant policy for it, shipper packaging + rollout node-by-node (per-user systemd units on Linux, launchd on macOS), including at least one macOS laptop and one shared multi-user node. Unblocked the moment the store boots anywhere.

SETTLED DECISIONS (do not reverse; escalate if blocked):
- Store host is a deployment-time value by owner ruling — do not build host assumptions into anything; medium-term expectation is herd-server co-location, and that must remain a URL change.
- Grant scope before content: the deny path is verified before transcripts flow.
- No Windows in v1.
- Code lives at tools/sesh now and moves to its own repo later — nothing in deploy may depend on the repo location (no repo-path assumptions in units or scripts).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 [S8] Before any real transcripts flow: a second tailnet identity outside the grant is verified DENIED at connection level (the brief's verify-early item 3)
- [ ] #2 Rollout is order-free: a node onboarded a week late backfills its full local history (30-day window) with no special handling
- [ ] #3 Shipper units survive reboot and user re-login on both platforms; store survives restart with mirror and index intact
- [ ] #4 A store host migration (change the URL the shippers read, restart units) loses no data and requires no other node change — proving the URL-only coupling
- [ ] #5 The shared-node deployment runs one shipper per OS user, each under its own uid
<!-- AC:END -->
