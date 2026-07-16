---
id: TASK-262
title: >-
  Adopt-created rows have empty launch_context: spawn sender verification
  refuses, and no repair verb can backfill launch coordinates
status: Done
assignee: []
created_date: '2026-07-16 09:20'
labels:
  - herder
dependencies: []
priority: high
ordinal: 261500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER DIRECTIVE (2026-07-16): HIGHEST PRIORITY BY FAR — a peer orchestrator is spawn-dead in the field on this defect; operational seat repair executed same night, this task fixes the class.

Live outage, peer orchestrator, blocking their dispatch. A session recovered via `herder adopt <old-guid> --confirm-dead` (after a restart left it with an unresolvable HERDR_PANE_ID — see the self-location task's stale-env variant) ended with a healthy bus row (hooks_bound, process_bound, correct sid and directory) whose launch_context is EMPTY {} — the row was created at boot under a different bus name and renamed by the adopt, so it never received launch coordinates.

Consequences, all verified live:
1. `herder spawn` refuses: "initial prompt sender identity is not verified: no joined bus row matches the calling session, process, or pane" — sender verification has no launch coordinates to match against.
2. Repair enroll refuses: "stored bus name <name> cannot be corroborated because live bus identity proof is unavailable."
3. `herder reconcile --apply` re-confirms the registry row (terminal live) but does NOT backfill launch coordinates; spawn still refuses after it.

So an adopt-recovered orchestrator is permanently spawn-dead with no healing verb. Workaround (proven): explicit env prefix on spawn (HERDR_PANE_ID=<real pane> HERDER_GUID=<guid>, promptless, then herder send after bind) — or proxy-spawn by another orchestrator.

Corroborating class evidence, same day: a different long-lived session's bare spawn refused on a STALE `--from-pane` derived from ancient launch context (pane long gone). The spawn-side pane/identity derivation trusts stale or absent launch-context sources without validating them against resolvable panes.

Fix directions: (a) adopt's final bind should record launch coordinates for the surviving row (it knows the live pane/terminal it just verified); (b) reconcile (or a repair verb) should be able to backfill launch_context from a live-verified pane; (c) spawn sender verification should fall back to the live-verified registry row (terminal+pane+bus) when launch_context is empty, and validate derived pane ids as resolvable before refusing on them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Adopt-recovered rows can spawn without env-prefix workarounds (launch coordinates recorded at adopt final-bind, or spawn verification falls back to the live-verified row)
- [x] #2 A repair path exists that backfills empty launch_context from a live-verified pane, with a red-first fixture reproducing the empty-context spawn refusal
- [x] #3 Spawn-side pane derivation validates candidate pane ids as resolvable and names the refusal cause + the recovery in its output
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
OPERATIONAL SEAT REPAIR EXECUTED (2026-07-16, owner-directed): fork-based seat replacement — fork rides the real launch path so the child got proper launch coordinates AND inherited full context; child proved bare spawn BEFORE cutover; label transferred atomically (rename --take-from --confirm-live); old seat culled; bus identity reclaimed by the child via start --as. Two fork attempts failed first because the impoverished row also fed fork a WRONG cwd (claude resume dies instantly outside the session's project dir) — cwd had to be passed explicitly, sourced from the hcom row's directory field.

LIVE FIELD CASE the fix must heal (named acceptance case; row live in the fleet): repaired seat with label + launch-context-healthy registry row but stored bus name STALE vs live reclaimed bus identity. Verbatim refusals: reconcile --apply → "conflict — stored terminal is live as name=\"<old-label>\"; D11 refuses to unseat, use manual adoption/enroll" (stale herdr tracker name for the same terminal); repair enroll → "live bus identity could not be verified (no joined bus row matches the calling session, process, or pane)" then "refused to enroll an unverified bus identity: terminal <t> and pane <p> are already seated on guid <same-guid>; join hcom and retry, ... or herder adopt for a true replacement". Note the second refusal fires even though the caller IS the seated guid — self-repair of one's own row's bus binding is impossible while the row is seated. Bare spawn WORKS from that seat (launch coordinates healthy), so the desync is repair-verb-only; it will bite at the next compact/restart cycle.

DISPATCHED (2026-07-16): codex builder in fresh worktree task-262-launch-context, design checkpoint mandated (identity write-spine). Spawn-time incident, adjacent class: the fresh worktree's mise.toml was untrusted, the pane stranded at mise's INTERACTIVE trust prompt, launcher gave up capture, and bind completed only after the orchestrator answered the prompt via pane send-keys — logged on the env-robustness task.

DONE (2026-07-16): merged to main 48ad12c (--no-ff; identifier sweep clean; final-head battery 4+61/61; post-merge battery on main 4+61/61; pushed). Review chain: incumbent opus found a P1 the whole suite missed — adopt wrote an UNVALIDATED (possibly stale) pane into the vendor db, foreclosing both recovery paths in the exact originating scenario (the shipped mock could never make pane-get fail); fixed with a fresh live pane resolution pre-write + typed nonfatal [launch_context_live_pane_unresolvable] refusal preserving {} byte-identical, red-first via an extended mock. P3s: per-code remedies (incl. a second-pass correction — pane_conflict's remedy had prescribed a verb that is Empty()-gated and structurally cannot cure that state). Grok calibration seat APPROVEd with demonstrated-mutation methods but missed the P1 (hostile-input-enumeration gap, ledgered). LIVE FIELD ACCEPTANCE on the named row: bare spawn RESTORED (no env prefix — the outage class is gone from the field seat); reconcile healing fired fleet-wide (multiple rows backfilled written+confirmed) but the named row itself still D11-conflicts on a stale TRACKER label flavor, and its spawn pass exceeded the fallback's documented preconditions — both residuals filed as the follow-up task (repair-verb completeness; no outage). hcom upstream candidate (no launch_context setter; adapter retires when upstream ships) ledgered on the upstream-filing task.
<!-- SECTION:NOTES:END -->
