---
id: TASK-128
title: >-
  sesh README cleanup: remove milestone/unit/requirement identifiers, make it
  standalone
status: Done
assignee: []
created_date: '2026-07-09 21:05'
updated_date: '2026-07-10 10:30'
labels: []
dependencies: []
priority: medium
ordinal: 128000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (owner, 2026-07-09)

tools/sesh/README.md shipped saturated with run-scoped delivery identifiers — M0/M2/M4 milestone tags (including SECTION HEADINGS "M2 Exposure Runbook", "M4 tsnet Grant Runbook"), U7 unit letters, R23 requirement IDs — 13+ occurrences. Owner: "insane; we need a standing order to NEVER use opaque/delivery identifiers anywhere." The standing order now exists (orchestrate SKILL.md invariant 11); this task is the cleanup of the artifact that triggered it.

## Scope

1. Rewrite tools/sesh/README.md so every M*/U*/R* reference becomes self-describing prose: "M2 Exposure Runbook" → "Interim exposure runbook (Tailscale Serve, read-only surface)"; "M4 tsnet Grant Runbook" → "Tailnet-native mode runbook (embedded tsnet + app-capability grants)"; "R23" → name the behavior (stale-binary-vs-newer-registry refusal); "(U7)" → drop. The ordering/phasing information the milestones encoded must survive as plain language (e.g. "until tailnet-native auth ships, keep ingest loopback-only").
2. Check the rest of the merged sesh/mish trees for the same disease: docs/specs/sesh-wire.md, tools/mish/README.md, skills/mish/SKILL.md, help text, error messages, code comments. Fix what's durable; leave docs/plans/* alone (plans are run-scoped by nature).
3. Do not change any behavior, commands, file paths, or the semantics of runbooks — wording only. The "M2 exposure owner sign-off: PENDING (@bigboss)" line is a live gate marker — reword to describe the gate (owner sign-off before surface exposure) without the milestone tag, keep the pending status.

## Acceptance criteria

1. grep -E 'M[0-9]\b|U[0-9]+\b|R[0-9]+\b|TASK-[0-9]+' over tools/sesh, tools/mish, docs/specs/sesh-wire.md, skills/mish returns zero durable-doc hits (test fixtures/goldens exempt if load-bearing).
2. A reader with no knowledge of the build run can follow both runbooks end to end.
3. sesh + mish suites and house gate green (docs-only diff expected — gate proves nothing broke).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Done 2026-07-10 via the docs-consolidation remediation unit (merged): sesh README rewritten standalone — verified zero milestone/unit/requirement/task identifiers by grep on merged main. Note: ACs lived in the description body, not CLI AC fields (known capture slip), so no check-ac; both criteria verified manually.
<!-- SECTION:NOTES:END -->
