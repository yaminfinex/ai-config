---
id: TASK-094
title: sesh U2 — module scaffold + real-JSONL fixture corpus (M0)
status: Done
assignee:
  - sesh-scaffold-buro
created_date: '2026-07-09 05:27'
updated_date: '2026-07-09 05:56'
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
- [x] #1 go build ./... and go vet ./... clean; darwin/arm64 + linux/amd64 cross-compiles pass
- [x] #2 sesh --help lists the five subcommands; each stub exits 1 not-implemented
- [x] #3 Fixture-inventory test asserts each named churn case present and parses as line-JSONL
- [x] #4 Module-isolation test: no imports from elsewhere in the repo
- [x] #5 fixtures README records provenance + scrub checklist for every fixture
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to sesh-build @ dd0e847 (ff). Orchestrator re-ran all gates fresh (-count=1): test/vet/gofmt clean, darwin-arm64 + linux-amd64 cross-compiles pass, stubs exit 1, fixture inventory + isolation tests green; secret spot-scan clean (only hits = scrub checklist text in README). Corpus: 5 real churn cases + resume pair that produced the U1 empirical finding. Wire types carry a two-way drift guard against sesh-wire.md verbatim JSON examples. Sliding doors (accepted): ErrorResponse.Code for Go name clash; db-tagged single IndexMessage struct. Worker: sesh-scaffold-buro, kept alive for U5. Trail: thread sesh-u2.
<!-- SECTION:NOTES:END -->
