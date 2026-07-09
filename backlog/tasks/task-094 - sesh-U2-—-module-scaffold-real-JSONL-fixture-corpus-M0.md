---
id: TASK-094
title: sesh U2 — module scaffold + real-JSONL fixture corpus (M0)
status: In Progress
assignee:
  - sesh-scaffold-buro
created_date: '2026-07-09 05:27'
updated_date: '2026-07-09 05:47'
labels:
  - sesh
dependencies:
  - TASK-093
priority: high
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Orchestrator-owned shared ground (no lane). Deliverable: a building, testable skeleton at tools/sesh plus the fixture corpus every later unit cuts goldens from.

Scope: conventional standalone Go module (go.mod independent of repo path, cmd/sesh + internal/, cobra tree with ship/serve/reindex/status/admin-drop-file all stubbed to print not-implemented + exit 1); internal/wire/ types transcribing docs/specs/sesh-wire.md 1:1 (BLOCKED on U1 merge — do fixtures + scaffold first); tests/fixtures/ captured from real machines: normal claude session, resume-into-new-file pair with overlapping history, trailing-partial-line file, interleaved-writers file, codex rollout with meta header. Sanitize secrets BY HAND; provenance + scrub checklist in tests/fixtures/README.md. A leaked secret is a repo incident.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U2 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md). Thread: sesh-u2.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 go build ./... and go vet ./... clean; darwin/arm64 + linux/amd64 cross-compiles pass
- [ ] #2 sesh --help lists the five subcommands; each stub exits 1 not-implemented
- [ ] #3 Fixture-inventory test asserts each named churn case present and parses as line-JSONL
- [ ] #4 Module-isolation test: no imports from elsewhere in the repo
- [ ] #5 fixtures README records provenance + scrub checklist for every fixture
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixture rulings (thread sesh-u2, #25146): trailing-partial case = byte-prefix-truncated COPY of the real normal session (real bytes, chosen read boundary; provenance must state source + offset), untruncated original kept alongside. Interleaved-writers case = real forked-parentUuid-chain file as honest stand-in; OPEN GAP recorded: no genuine two-writer file exists on this claude version (two-terminal resume writes per-terminal files — contradicts prior-art interleave claim; carried to design record). Resume-pair verified with 141 overlapping message uuids.
<!-- SECTION:NOTES:END -->
