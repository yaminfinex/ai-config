---
id: TASK-177
title: >-
  mish adoption: migrate run-herder-dx coordination onto missions
  (decision-first)
status: In Progress
assignee: []
created_date: '2026-07-13 01:02'
updated_date: '2026-07-13 01:25'
labels: []
dependencies: []
priority: high
ordinal: 176000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
mish shipped complete (its build run closed with all eleven units merged; binary + skills symlinked; its 8 check scripts run in the house battery). Owner direction: get mish out and migrate this run over to it. DECISION unit first, then a separate migration unit. The decision must rule: (a) what of the live run's coordination substrate migrates into a mission per the ratified mission spec — playbook, standing-orders digest, run journal, per-unit briefs — and what stays where it is (backlog/ board custody in particular: mish has its own backlog-floor gate; double-custody is forbidden); (b) adopt semantics (spec: adopt MOVES, never copies) applied to a LIVE run without disrupting in-flight lanes; (c) slug + mission scaffold shape via the mish CLI; (d) whether napkins/-gitignored artifacts enter mission custody (they become tracked — single-copy risk resolves, but bus/task identifiers in them are run-scoped by doctrine). Decision unit output: a one-page ruling with the migration unit's capture, ACs, and territory fence, owner-confirmable. Constraint: the run stays operational throughout; hera remains the coordination writer during and after migration.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13: decision DONE (76566bd) — adopts orchestration substrate into missions/<slug>/artifacts/, backlog stays sole task custodian, adopt=MOVE by hera at unit boundary w/ sha256 manifest + custody commit, napkins->tracked ruled IN w/ secret scan, slug herder-dx proposed. Owner flags: missions repo (dedicated recommended), MISSIONS_REPO provisioning, push authority, --owner value, slug, sibling lanes. Env correction: mish binary NOT on PATH, skill NOT symlinked (earlier claim stale) — mechanical preconditions. Codex review dispatched (DOA/live-run-safety/custody/owner-completeness/hygiene lenses).

2026-07-13 review round 1 (kune, codex-high): pre-trace PASSED (built the CLI, ran the scaffold — flags/grammar/env facts all verified) but 2 P1: pointer stubs are plain files not redirects — compact continuations + in-flight workers with old-path references (transitively enumerated in the live tree) would lose instructions mid-unit; hash-verified MOVE does not prove tracked custody (destination ignore rules can silently drop files after source deletion; secret scan must precede the custody COMMIT). 4 P2 (identifier ruling oversteps the ratified mission-spec invariant — owner-only; mission board must stay empty incl. housekeeping; pull-before-scaffold + explicit status command; provisioning authority contradiction) + 1 P3 (quarantine leaks). Fix round 1 sent to zemu; kune holds for delta.

2026-07-13 fix round 1 delta (zemu, c0eb7ad): whole-tree symlink supersedes stubs+deferred-adopt (one dir-level link covers the transitive closure; retirement journaled, AC7; cold-resume + dependency-walk drills as AC6); ordered custody-proof pipeline w/ secret-scan-before-commit and staged-blob-comparison-before-deletion (AC4); P2-1 deferred to owner (migration unit BLOCKED on that ruling, AC2); board empty full stop (AC8); sync-before-scaffold + exact status command (AC3/10); provisioning owner-only, unit installs nothing (AC1); quarantine rewritten project-agnostic. kune delta requested.

2026-07-13 delta round (kune): board/mechanics/provisioning RESOLVED; symlink primitive confirmed right (no live-corpus find-dependence). Remaining: P1 continuity invariant DOA vs real corpus (pre-existing broken refs from historical archive moves; AC6 as worded must fail) — reworded to resolved-before==same-bytes-after w/ baseline/delta classes; P1 source deleted before REMOTE custody proven (no push pre-deletion; local clone insufficient) — pipeline reordered push→remote-clone→compare→delete, unconfirmed-authority branch removed; P2 option-(b) redaction precluded by residual verbatim rulings — parameterized; P2 staged-set equality impossible (manifest is an extra) — scoped to artifacts/**; P3 two quarantine leaks. Fix round 2 to zemu.
<!-- SECTION:NOTES:END -->
