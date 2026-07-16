---
id: TASK-255
title: >-
  Pinned enroll repair overwrites stored label and role with enroll defaults —
  identity continuity damaged on the repair path
status: Done
assignee: []
created_date: '2026-07-16 00:58'
updated_date: '2026-07-16 03:04'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 254500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found on the first live run of the repair path (worked otherwise: guid, pane, bus name, SIDs all preserved; atomic repair-first ordering held). The repaired row came back labeled <role-default>-<shortguid> with role=manual, replacing the stored label and role. Label was restored via herder rename (which syncs herdr too); the ROLE remains overwritten and rename does not touch it. Fix: the same-guid repair path must preserve the stored label and role when the caller requests none (explicit --label/--role still win). Red-first fixture: repair a row with a distinctive stored label+role, assert both survive. Check the adoption and core-key rebind paths for the same class while in there.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Same-guid repair preserves stored label and role absent explicit flags (red-first)
- [x] #2 Core-key rebind and adoption paths audited for the same overwrite class
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-16 as grok-vs-codex A/B calibration trial (row 2 of the implementation ledger): same brief, two independent seats/worktrees/threads, design checkpoints first, standard review chain holds merge authority, one arm merges.

FIX-ROUND CYCLE (2026-07-16): both arms' first DONEs carried a SHARED P1 (ownership proof fed the stored label on pinned repair — tautological labelMatches, live takeover reproduced by the incumbent reviewer's published probe). Codex arm fix round 1 landed and GATED (full battery 4 modules + 61/61 on the fix head; two prior runs voided on battery-harness PATH deps — logged under the env-robustness task). Delta review arm C: P1 CONFIRMED FIXED (probe re-run pre/post; in-suite fixtures strictly stronger than the probe — byte-identical-registry assertion); one NEW P2 (adopt pinned-re-enroll recovery hint dropped HERDER_LABEL, which post-fix is the caller-claim proof input — slim hint dead on no-recorded-sid rows; remedy verified: restore HERDER_LABEL, drop HERDER_ROLE only, rename mis-named test); fix round 2 dispatched, corrected instruction relayed to the grok arm (shared by necessity, like the P1). SETTLED AS CONTRACT (orchestrator ruling on reviewer lens-3 flag): on the CORE-KEY path the ownership proof may receive the stored label — selection already proves terminal+pane+bus (live-verified), which strictly dominates the label comparison; consequence: a renamed agent on its own verified seat with stale birth env is now ADMITTED where base refused (that base refusal was this task's bug in another form). Both arms must match this cell. Grok arm fix round in flight (seat stalled ~30min on xAI rate limit mid-round, then recovered and resumed with all corrections).

ARM C REVIEW-COMPLETE (2026-07-16, round-2 head 08f6199): reviewer APPROVE, no findings. Round 2 verified by execution: recovery hint restored to sid+guid+label (role correctly stays omitted — inert on repair), test renamed to state the true claim, help "never substituted" clause scoped to the pinned path, and the new ambient-label fixture MUTATION-TESTED (env term stripped from the proof chain → that fixture is the only case in the battery that fails — pins exactly the load-bearing term). P1 fence probe re-run on the round-2 head: both takeover cases still REFUSED. Repair proof now pinned from both directions: stored must NOT satisfy it (takeover guards), ambient claim MUST (compact re-enroll fixture). Arm C holds for arm selection; full-battery debt on final head defers to pre-merge if selected.

DONE (2026-07-16): ARM C SELECTED and merged to main f000e6f (--no-ff; identifier sweep clean — all matches synthetic fixture data; pre-merge full battery on final head 4+61/61; post-merge battery on main 4+61/61; pushed). AC evidence: #1 red-first preserved-identity + takeover-refusal fixtures (probe-verified pre/post, byte-identical registry asserted) and the ambient-label proof fixture mutation-verified as the only case pinning the load-bearing env term; #2 adoption clean by design (fresh-for-adoption + explicit role + atomic label transfer), core-key rebind fixed in-scope, reconcile updateRow never touches label/role (traced). Selection rationale: arms behaviorally equivalent on all contract cells; C mergeable-as-is with a fully covered mutation matrix; on the divergent empty-stored-field cell, C's env backstop recovers a real identity rather than a synthetic default — closer to unit intent. Grok arm (fix head equivalent-correct, richer write-clobber coverage, best-in-class rationale comment) closed UNMERGED WITH FULL CREDIT per trial protocol; its explanatory artifacts ride the docs-lift follow-up task. Contract recorded in help text: repair write preserves stored (flags-only override), pinned proof reads the caller claim (flag/env/default), core-key proof may take stored, env backstops empty stored fields on repair.
<!-- SECTION:NOTES:END -->
