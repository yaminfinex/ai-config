---
id: TASK-208
title: >-
  sesh — fixture identity policy: real usernames/home layouts in committed
  test fixtures
status: To Do
assignee: []
created_date: '2026-07-14 09:55'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 207000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Surfaced by the grok-adapter review (2026-07-14): the committed test
fixtures (claude, codex, and now grok transcripts under
tools/sesh/tests/fixtures/) are verbatim captures carrying the real OS
username, full home-directory layout, project paths, and live-harness
fragments. The fixtures README documents this as a deliberate call
("repo is private") predating the grok lane, and the grok fixture was
ruled to conform to that precedent rather than diverge.

Owner decision wanted, corpus-wide (not per-fixture): (a) keep the
private-repo call and record it as accepted policy with an explicit
statement of what classes ARE prohibited (credentials/keys/tokens —
already grep-enforced); or (b) scrub-all: neutral placeholder identities
across every fixture, synthetic recuts where substitution would break
parser-fidelity guarantees, byte-count assertions updated, and a
documented exception to the "do not edit fixture bytes" verbatim rule.
Note the answer flips if repo visibility ever changes — if (a), add a
visibility-change tripwire note to the fixtures README.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Owner ruling recorded (keep-private-call vs scrub-all) in the fixtures README as explicit policy
- [ ] #2 If scrub-all: every fixture neutralized or recut, gates updated, verbatim-rule exception documented; if keep: prohibited-class statement + visibility tripwire note added
<!-- AC:END -->
