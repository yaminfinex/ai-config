# Adversarial structures

Confidence by opposition rather than coverage. All compose into the other topologies — inserted
mid-run for one contested decision, or appended as the tail.

## Design jury

For a contested decision where one agent's first idea would anchor too hard.

1. **Divergent designs:** 2–3 fresh-context agents, same problem, different brief/angle (ideally
   different model families — shared blind spots are the failure mode). Each writes a design
   file; none sees the others'.
2. **Adversarial review:** a fresh agent attacks all designs — failure scenarios, unstated
   assumptions. Output: verdicts + amendments, not a rewrite. Divergence between reviewers is
   itself signal — surface it for adjudication rather than averaging it away.
3. **Implementation:** land the winner with the amendments applied, grafting the best of the
   runners-up. Track each amendment to closure in the journal (applied, or rejected with
   reasons) — the jury's value survives only if its output is tracked.

## Standing adversary

One implementer + a long-lived reviewer pane attacking each commit **as it lands**, when
end-of-run review would be too late to redirect cheaply (auth, data, money). The adversary reads
the journal and `git log`, never edits the worktree; findings go to the doer on the bus
(`--intent request`, a `review` thread), addressed or rejected-with-reasons in the next commit;
the journal tracks closure. Use a different agent/model family than the doer.

## Golden-agent purpose check

The reviewers above attack *quality*; the golden agent attacks *drift from intent*. At run start,
**bottle** (`bottling` skill) the agent that holds the original intent — the one that grilled the
user or wrote the design doc — before the run consumes its context. Decant a fresh copy (the
bottle stays pristine) wherever judgment is needed and the user isn't in the loop: at gates, when
a sliding door bites, and at the end-of-run review walking the journal's decisions. It reviews,
never implements — its context is the mission's origin, not the run's evolution, which is exactly
what makes it a good check and a poor doer. On an autonomous run it's the closest thing to asking
the user.

## Deep-review tail + remnant sweep

The standard end-of-run structure, after the last unit and before the PR:

1. **Deep review:** a fresh-context agent over the **full branch-vs-main diff** against the
   acceptance criteria — not the per-unit diffs already verified. Fresh context sees what the
   run's agents normalized.
2. **Remnant sweep:** hunt what intermediate units left behind — transitional shims marked to die
   in later units, tests pinning mid-run seams, compat assertions that stopped mattering after
   cutover, on-disk husks. Delete or rewrite; don't layer.
3. **Remediation routing:** mechanical → one direct agent, one commit; contested → jury first.
   Separate commits, each revertible.
