# TASK-050 controlled restart — boot brief for the post-restart orchestrator

You are the replacement session for @hera, standing orchestrator of run-herder-dx.
This restart IS the experiment (TASK-050, legs TASK-042 + TASK-043). Execute Part 1
verbatim BEFORE fixing anything about your own identity — the broken state is the data.

## Part 1 — repro evidence (capture FIRST, in this order, before any identity fix)

1. `env | grep -E "^(HCOM|HERDER)_" | sort` — expected hypothesis: you inherited the
   pane shell's stale env (HCOM_INSTANCE_NAME=dora, HCOM_PROCESS_ID=4cad8783-...).
   Whatever it says, record it.
2. `hcom list -v --json` — what name did the bus give YOU (random? dora? hera?), is the
   old `hera` row still present and what does it point at (old session_id
   1fd7e98c-efd3-4c14-bc9f-1f103db82a2a should now be a dead process — does herdr 0.7.3
   / hcom mark it gone, per upstream #620/#684/#943: fresh session refs replacing stale
   saved references)?
3. `herder list --all | grep -iE "hera|404a13df"` — old row 404a13df was already
   state=unseated live_status=gone pre-restart (snapshot below); confirm unchanged.
4. `herdr pane read` your own pane or `herdr client info` if needed — confirm pane id
   (was w6554208c1918a12:p1) and whether ids reshuffled.
5. UPSTREAM AUDIT CALL: given 1–2, state whether herdr 0.7.3's identity fixes changed
   the picture vs the 2026-07-08 accident (old evidence on TASK-042): does anything now
   auto-adopt/auto-correct, or is manual reclamation still required end-to-end?

## Part 2 — identity reclamation per FROZEN doctrine (TASK-042 comment #3)

A restarted process is a NEW transcript ⇒ NEW guid. NEVER reuse 404a13df (spec D1).
Composite: enroll (new guid) → rename --take-from (lease transfer) → retire old.
Some verbs may not exist yet (retire is wave C; --take-from may be unimplemented) —
ATTEMPTING them and recording the refusal/absence is a deliverable, not a failure.

1. `hcom start --as hera` — reclaim the bus name. Record whether it works cleanly on
   0.7.23 and what happens to the old hera row.
2. Re-check `env` (unchanged, frozen) then run `herder enroll` (role orchestrator,
   label hera — if label-uniqueness refuses because 404a13df holds the label, record
   the refusal text; that's the 042-affordance evidence). 043 LEG: after enroll,
   inspect your new registry row — did hcom_name come from the STALE env
   (dora) or from live hcom identity (hera)? Expected still-broken (no upstream change
   touches env staleness). If broken: workaround is re-enroll with
   `HCOM_INSTANCE_NAME=hera herder enroll ...`; record both rows.
3. Try the lease transfer: `herder rename <newguid> hera --take-from 404a13df` (check
   `herder rename --help` first). If --take-from doesn't exist, record it as the
   missing affordance and relabel/retire however the current toolset allows.
4. Try `herder retire 404a13df` (check help; wave C — likely absent, record it).
5. `herder list` — confirm exactly one live hera row with correct pane + hcom_name;
   ask @vibe to re-verify your identity from THEIR side (they are watching).

## Part 3 — write it down, then resume orchestration

- Append findings to TASK-050 (per-leg outcome: close / re-scope / confirm-still-broken
  with fresh evidence) and TASK-042/043 (comment each). Board next id: TASK-069.
  Commit backlog + this file's evidence updates. You are the SOLE writer of backlog/.
- Message @bigboss nothing directly — the owner reads your session; summarize findings
  in your session text.

## Pre-restart evidence snapshot (captured by outgoing hera, 2026-07-08)

- herdr 0.7.3; hcom 0.7.23 (mise pin).
- Outgoing session: transcript 1fd7e98c-efd3-4c14-bc9f-1f103db82a2a, bus name hera
  (session_id bound, hooks_bound+process_bound true), env HCOM_INSTANCE_NAME=dora
  (STALE — frozen from the previous accidental restart; this session was born as dora
  and reclaimed hera — it is itself the 043 evidence), HCOM_PROCESS_ID=4cad8783-....
- Registry row: guid 404a13df-4cec-43bc-a345-f90835657af1, event=migrated_v1,
  state=unseated, live_status=gone, label=hera, role=orchestrator, node 0a403d4c.
  (Reads gone because migration D9 marked all legacy agents dormant — expected,
  was told not to chase; the restart re-enrollment is the planned re-recognition.)
- Pane: w6554208c1918a12:p1, cwd /home/grace/Coding/ai-config, branch main.

## Standing orders (carry these forward — you are @hera now)

(1) IDENTITY — standing orchestrator of run-herder-dx; NEW guid from Part 2; main
checkout /home/grace/Coding/ai-config, stay on main; SOLE writer of backlog/ (commit
every transition); NEVER push/PR/file-externally — owner (@bigboss) ships;
propose-only for machine changes.
(2) DOCTRINE — run-log napkins/run-herder-dx/run-log.md tail = standing doctrine:
codex implements, opus reviews via --extra-arg --model --extra-arg claude-opus-4-8,
Fable never implements; pane hygiene (nothing splits into your tab); recovery paths
must be reachable AND write-confirmed; briefs say commit-on-branch + no self-arranged
reviewers. GATE: never trust a DONE — go vet+test tools/herder AND tools/bottle + ALL
check-*.sh BARE sequentially from the worker's worktree; opus adversarial review
mandatory for engine diffs; merge no-ff; post-merge gate on main FROM REPO ROOT
(26 suites); board Done; cull; worktree+branch cleanup. Second lander merges main
in-branch + regates.
(3) STATE — wave A COMPLETE (A1–A5 merged; A5=6c0124f). TASK-063 status lines merged
59ec21f. Shipped 2026-07-08: 045 047 048 049 052 056 057 058 059 063 064. Spec errata
c3dbc5e+82fceb4 on herder-spec branch await OWNER blessing at next spec-touching
merge; F3 + codex-statusline upstream gaps file at CLOSEOUT only (TASK-029 ledger);
vibe lane EMPTY. Open tail: 034 036 038(user direction pending) 041 042 043 050 051
054 060 061 062 065 066 067 068. TASK-067 (bus-snapshot writer) unfenced now that
wave A + 063 landed — natural next dispatch after this restart completes.
(4) NOTES — codex reachability fully native post-045/064; herder compact refuses on
own pane (TASK-041, 3 hits) — workaround: herdr pane send-keys <pane> ctrl+u, pane
send-text the steer, send-keys enter; nothing is pushed anywhere, owner ships.

## POST-RESTART EVIDENCE APPENDIX (written by the replacement session, 2026-07-08 ~10:20Z)

### Part 1 — repro evidence (captured before any identity fix)

1. ENV — hypothesis FALSIFIED. No stale dora env inherited. Fresh launch identity:
   HCOM_INSTANCE_NAME=mono, HCOM_PROCESS_ID=d49ed878-ad0f-4126-a6ea-bb9951d86c46,
   HCOM_LAUNCHED_BY=user, HCOM_LAUNCHED_PRESET=herdr, HCOM_LAUNCH_BATCH_ID=0ed76d90,
   HERDER_SHIM=1 (the pane's `claude` is a herder shim that mints fresh hcom identity
   at launch — this is what killed the stale-env-inheritance failure mode).
2. BUS — hcom gave me random name `mono`, session 0bc5419f-d762-475c-931e-6d8a015f50a3,
   hooks_bound+process_bound true. Old `hera` row (session 1fd7e98c) ABSENT entirely —
   hcom 0.7.23 dropped the dead row rather than leaving a stale entry (#620/#684/#943
   behaving). No dora row either.
3. REGISTRY — 404a13df unchanged: label=hera, role=orchestrator, event=migrated_v1,
   state=unseated, live_status=gone, seat=null. (Also 3 old spec-hera rows, archived/idle.)
4. PANE — same pane, no reshuffle: w6554208c1918a12:p1, terminal term_65612408bc9034.
   launch_context pane_id `p_744` is an hcom-internal alias, not a herdr id.
   `herdr client info` refuses inside a pane ("nested herdr is disabled").
5. AUDIT CALL — upstream fixed HALF the picture: fresh identity at launch (shim) +
   dead bus rows dropped + `hcom start --as` clean. NOTHING auto-adopts: no registry
   row for the new session, old row untouched, manual reclamation required end-to-end.

### Part 2 — reclamation per frozen doctrine (all refusals recorded verbatim)

1. `hcom start --as hera` — WORKED cleanly on 0.7.23; mono row became hera, bound to
   session 0bc5419f. No displacement needed (old row already gone).
2. `herder enroll --label hera --role orchestrator` — REFUSED:
   `label "hera" already belongs to active guid 404a13df-...` — a DEAD row
   (unseated + live_status=gone) counts as an "active" label holder. (042 evidence.)
   Enrolled as `hera-restart-050` instead → NEW guid 0c607d43, which recorded
   hcom_name=mono FROM THE FROZEN ENV despite live identity hera — 043 CONFIRMED
   STILL BROKEN. Workaround re-verified: `HCOM_INSTANCE_NAME=hera herder enroll
   --label hera-restart-050b` → guid bbbc84c2-b2ef-43bb-a198-bb5dce5ef077 with
   hcom_name=hera; supersession auto-retired 0c607d43. Wrinkle: same-label re-enroll
   refused (label check runs BEFORE pane-supersession), hence the variant label.
3. `herder rename --take-from` — DOES NOT EXIST (usage: rename <target> <new-label>;
   help says a taken label requires culling the holder). Missing affordance recorded.
4. `herder retire` — DOES NOT EXIST (`unknown command "retire"`, wave C absent).
   Escape hatch attempted: `herder cull --guid 404a13df... [--force]` → BOTH runs print
   `cull errored hera (...) pane= → pane_not_found (still marked closed in registry)`,
   exit 0, but append NO closed record — latest record for 404a13df remains
   migrated_v1/unseated. Message and exit code are lies on this path. Filed TASK-069.
   NET: label `hera` is UNRECLAIMABLE with the current toolset (label tomb).
5. Final state: ONE live row for me — guid bbbc84c2, label hera-restart-050b,
   role orchestrator, seated, pane w6554208c1918a12:p1, hcom_name=hera, BUS=@hera.
   vibe VERIFIED from their side (bus #10964): bus binding + registry seat both correct;
   sends resolve me via registry. Extra vibe finding: live_status=undetected despite
   exact terminal_id match — herdr tracker never adopts shell-relaunched agents
   (agent_status unknown, no agent field). Filed TASK-070; TASK-044 stays Done
   (tri-state honesty working as designed).

### Board actions (Part 3)

- TASK-050 → Done (042 leg re-scoped, 043 leg confirm-still-broken, 044 leg previously
  resolved). TASK-042 + TASK-043 commented with fresh evidence. TASK-044 commented
  (second repro x-ref). NEW: TASK-069 (cull closed-record no-op), TASK-070 (herdr
  tracker adoption gap). Next board id: TASK-071.
