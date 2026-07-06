# Orchestrator + sequential phases

A standing orchestrator that **writes no implementation code** drives phase agents through a
plan, one at a time, in one shared worktree. Consider when a design doc with phases + acceptance
criteria exists and verification is mechanical — the orchestrator earns its handoff churn by
re-running the gates independently. If the user is the only real verifier, prefer the relay.

Setup: one workspace, one worktree. Playbook = the design doc (*what*) + an orchestration state
file (*how*: phase-status table, per-phase pointers into the doc, decisions log, this protocol).
The orchestrator keeps the journal current — it's the run's decision record and what makes the
orchestrator itself replaceable.

## Protocol

1. **Strictly sequential.** One phase agent at a time; never edit files yourself while one runs.
2. **Spawn** one phase agent at a time, one-line prompt:

   ```bash
   herder spawn --role phase-N --agent claude \
     --prompt 'Read <orchestration file> and <design doc> fully, then execute Phase N exactly as specified, commit when green, and report DONE on your unit thread.'
   ```

   The playbook may pin a different agent/model per phase; its run-shape header carries your hcom
   name and the thread convention, so the prompt stays one line.
3. **Idle for the report — don't poll.** End your turn after spawning; the phase agent's
   DONE/BLOCKED report on the unit thread wakes you. Backstop: `hcom events sub --idle <name>
   --once` (or `--type life --agent <name>`). Quiet too long → `hcom transcript <name>` to see
   where it actually is before assuming it's stuck.
4. **Verify before advancing — don't take the report's word:** phase commits present, tree
   clean; typecheck/lint green; targeted suites green **uncached**; this phase's acceptance
   criteria. Journal the verdict.
5. **Record** in the journal as you go — dispatches, decisions, verdicts.
6. **Escalate (stop, don't improvise)** on open questions that bite, reds the agent can't fix in
   scope, scope drift. Record initial proposals for known open questions so agents can
   proceed-and-flag rather than stall.
7. **Cull per the liveness policy** — default cull-on-done after verification (ask first).
   Keep-open earns its pane only when live interrogation is genuinely expected — `hcom
   transcript` and `herder resume` cover the rest (run-shape item 3). Never close your own pane.
8. **Nobody pushes or opens PRs** — the user ships.
9. **Tail:** deep review + remnant sweep (`adversarial.md`).
10. **Remediations:** mechanical → one direct agent, one commit; contested → jury first. Each
    lands as its own commit.

## Orchestrator self-respawn

When your own context balloons: bring the journal fully current, write a HANDOFF entry (in
flight / verified / next), spawn a fresh orchestrator pointed at it. If a fresh orchestrator
couldn't pick up the run from the journal, it is missing something every other agent needed too.

## Seen live

- **Design-doc drift:** verify the doc's current-state claims against the actual branch *before*
  phase 1; record corrections in the journal. A phase agent discovering drift burns its budget.
- **"Done" with a dirty tree or red suite:** happens; that's what step 4 is for. Reply on the
  unit thread with the failing output (`--intent request --reply-to <report-id>`) rather than
  fixing it yourself.
