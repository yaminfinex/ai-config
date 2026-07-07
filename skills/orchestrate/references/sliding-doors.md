# Sliding doors — decision capture and branch-both-sides

Autonomous runs trade the user's in-the-moment judgment for throughput. The repayment is an
auditable record of every fork taken without them — and, when a fork is genuinely contestable,
the option to take both doors and compare.

## Decision capture (mandatory on autonomous runs)

A **sliding door** is a decision that could plausibly have gone another way *and* shapes what
comes after — architecture seams, approach/library choices, scope calls on open questions, which
design to land. Mechanical consequences of already-made decisions don't count; don't dilute the
signal.

Journal entry, written when the door is taken — workers report theirs on the unit thread:

```markdown
## SLIDING DOOR — <short name> (Unit N)
- Fork: <the question, one line>
- Options: <each door, one line apiece>
- Chosen: <door> — <the constraint or evidence that decided it>
- Other door: <what taking it would have looked like — enough for the user to judge later
  whether it deserves exploration, and for a fresh agent to pick it up>
- Reversibility: <cheap to revisit / expensive after unit M / one-way>
```

"Other door" + "Reversibility" are the point — chosen-and-why alone doesn't make the unexplored
branch auditable. The end-of-run review walks these blocks; the golden agent (if bottled) judges
each against original intent.

If "Chosen — why" can't be filled with actual evidence, it isn't a door to capture — it's a gate
to stop at, or a candidate for:

## Branch-both-sides

When a fork is load-bearing and argument won't resolve it, execute both doors and let the
artifacts argue. Sibling of the design jury: jury when designs-on-paper can settle it, branch
when only built things can. Doors can be **designs as well as implementations** — two design
docs, two specs, two prompts — in which case a door is just a file and an agent, no worktree
needed.

1. **Fork point:** commit/snapshot the shared trunk; write the SLIDING DOOR block with
   `Chosen: BRANCHED`.
2. **Isolate the doors.** Code doors get a worktree + branch each (one writer per worktree);
   design doors just get separate files. Spawn each door's agent with the standard one-line
   prompt + a per-door addendum in the playbook (same scope, different approach pinned). When the
   door is a branch of an existing registered conversation, `herder fork <guid>` (or `herder fork
   --self` from the pane itself) can preserve session lineage with `provenance.forked_from`;
   otherwise spawn a fresh agent. Doors
   share nothing and may run concurrently.
3. **Same gate for both.** A door that can't go green has answered the question.
4. **Comparison is its own unit:** a fresh agent (or the user) reads both artifacts + DONE blocks
   and writes the verdict — what decided it, what's worth grafting from the loser. Keep both door
   agents open until the verdict lands: "talking to a branch" is literal — either agent can be
   interrogated about its choices before judging.
5. **Land the winner**; reference the losing artifact/branch from the SLIDING DOOR block.

Branching doubles the unit — weigh it against the cost of discovering the wrong door late
(reversibility: expensive / one-way) and whether the comparison artifact is genuinely
decision-grade.
