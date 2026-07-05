# Orchestrator + sequential phases

A standing orchestrator that **writes no implementation code** drives phase agents through a
plan, one at a time, in one shared worktree. Consider when a design doc with phases + acceptance
criteria exists and verification is mechanical — the orchestrator earns its handoff churn by
re-running the gates independently. If the user is the only real verifier, prefer the relay.

Setup: one workspace, one worktree. Playbook = the design doc (*what*) + an orchestration state
file (*how*: phase-status table, per-phase pointers into the doc, decisions log, this protocol).
The orchestrator keeps the state file current — it's the run's source of truth and what makes the
orchestrator itself replaceable.

## Protocol

1. **Strictly sequential.** One phase agent at a time; never edit files yourself while one runs.
2. **Spawn** one tab per phase, one-line prompt:

   ```bash
   herder spawn --role phase-N --agent claude --new-tab --from-pane "$HERDR_PANE_ID" --notify \
     --prompt 'Read <orchestration file> and <design doc> fully, then execute Phase N exactly as specified, commit when green, and update the phase-status table.'
   ```

   The playbook may pin a different agent/model per phase. `--notify` wires the ring automatically —
   it injects the ring command (targeting your pane) plus `$HERDER_BIN` into the phase agent, so
   you don't hand-write notify instructions into the prompt.
3. **Idle for the ring — don't poll.** End your turn after spawning; the phase agent rings you
   (`herder send`) when it writes its DONE/BLOCKED block. Wake on the ring, read the run-log,
   verify. Keep a backstop for a dropped ring (a busy/modal orchestrator queues or refuses it, or
   the agent died before ringing): a bounded `herder wait <guid> --timeout <generous>` heartbeat
   or a run-log sweep `grep -qE '^## Phase N — (DONE|BLOCKED|HANDOFF)' <run-log>`. Idle there means
   done *or* stuck — read the pane.
4. **Verify before advancing — don't take the agent's word:** phase commits present, tree clean;
   typecheck/lint green; targeted suites green **uncached**; this phase's acceptance criteria.
5. **Record** in the state file; reconcile the agents' own appends.
6. **Escalate (stop, don't improvise)** on open questions that bite, reds the agent can't fix in
   scope, scope drift. Record initial proposals for known open questions so agents can
   proceed-and-flag rather than stall.
7. **Cull per the liveness policy** — default cull-on-done after verification (ask first); keep
   open a phase whose judgment calls the user may want to interrogate. A registered culled agent can
   be reopened later with `herder resume <guid>` if its tool session id was recorded, but immediate
   interrogation still argues for keeping it open. Never close your own pane.
8. **Nobody pushes or opens PRs** — the user ships.
9. **Tail:** deep review + remnant sweep (`adversarial.md`).
10. **Remediations:** mechanical → one direct agent, one commit; contested → jury first. Each
    lands as its own commit.

## Orchestrator self-respawn

When your own context balloons: bring the state file fully current, write a handoff (in flight /
verified / next), spawn a fresh orchestrator pointed at it. If a fresh orchestrator couldn't pick
up the run from the file, the file is missing something every other agent needed too.

## Seen live

- **Design-doc drift:** verify the doc's current-state claims against the actual branch *before*
  phase 1; record corrections in the state file. A phase agent discovering drift burns its budget.
- **"Done" with a dirty tree or red suite:** happens; that's what step 4 is for. Send the agent
  back with the failing output rather than fixing it yourself.
