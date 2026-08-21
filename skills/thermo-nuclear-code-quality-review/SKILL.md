---
name: thermo-nuclear-code-quality-review
description: An unusually strict, ambitious maintainability review of the current branch's changes.
disable-model-invocation: true
---

# Thermo-Nuclear Code Quality Review

An unusually strict review of the current branch's changes, focused on implementation quality, abstraction quality, and codebase health. Its defining move is **code judo**: a behavior-preserving restructuring that uses the existing architecture so effectively that whole branches, helpers, modes, or layers disappear and the result feels inevitable in hindsight. Hunt for the code judo move in every meaningful change — delete complexity rather than rearrange it. Be extremely thorough and rigorous: measure twice, cut once.

## Rules

Apply every rule to every meaningful change in the diff:

1. **Code judo over local cleanup.** Reframe the change so fewer concepts, branches, helper layers, or modes are needed. A refactor that moves complexity around without reducing what a reader must hold in their head still owes a code judo answer: keep looking for the reframing that deletes it — a simpler state model, a shifted ownership boundary, special cases collapsed into the default flow.

2. **Files stay under 1000 lines.** A diff that pushes a file from under to over 1000 lines is a presumptive blocker: ask for decomposition first (extract helpers, subcomponents, focused modules). Waive only for a compelling structural reason that leaves the file still clearly organized.

3. **New logic gets a home, not a bolt-on.** Ad-hoc conditionals, one-off booleans or nullable modes, "temporary" branches, and edge cases dropped into the middle of already busy flows are design problems, not stylistic nits — even when they technically work. Route the logic into a dedicated abstraction, typed model, explicit dispatcher, state machine, policy object, or separate module so the surrounding path stays legible. Repeated or copy-pasted conditionals signal a missing model or helper: extract it.

4. **Direct and boring beats magical.** Prefer explicit, legible flow over generic mechanisms that hide simple data-shape assumptions or brittle "magic" behavior. An abstraction earns its keep by simplifying; delete thin wrappers, identity abstractions, and pass-through helpers that add indirection without buying clarity.

5. **Explicit type boundaries.** Replace casts, `any`/`unknown`, unnecessary optionality, ad-hoc object shapes, and silent fallbacks with explicit typed models or shared contracts that state the real invariant. When the boundary is explicit, the control flow gets simpler.

6. **Canonical layer, canonical helper.** Logic lives in the package, service, or layer that already owns the concept; reuse the existing canonical utility over a bespoke near-duplicate; keep feature-specific logic out of shared paths and implementation details out of APIs. Move drifted code home rather than normalizing the drift.

7. **Parallel where independent, atomic where related.** When independent work is serialized for no reason, run it in parallel where that also simplifies the orchestration. When related updates can leave state half-applied, restructure them into an atomic flow. Flag orchestration complexity that makes the implementation brittle; leave micro-optimizations alone.

## Tone

Direct, serious, and demanding — never rude. State major maintainability issues at full strength: if the code makes the codebase messier, say so plainly; if it missed a dramatic simplification, say that plainly too. Phrases that carry the register:

- `this pushes the file past 1k lines. can we decompose this first?`
- `this adds another special-case branch into an already busy flow. can we move this behind its own abstraction?`
- `i think there's a code judo move here that makes this much simpler. can we reframe this so these branches disappear?`
- `this refactor moves complexity around, but doesn't really delete it. is there a way to make the model itself simpler?`

## Output and Approval

Order findings structural-first: structural regressions and missed code judo moves, then branching and boundary/type problems, then file-size and decomposition, then modularity and legibility. A handful of high-conviction structural comments outweighs any number of cosmetic notes — when structural issues exist, cosmetic ones wait.

Approve only when the diff passes every rule above, or each violation carries the author's clear, explicit justification. "Behavior seems correct" is the entry fee, never the bar: a working implementation that leaves the codebase messier gets explicit, actionable restructuring feedback, not a rubber stamp.

The review is complete only when every meaningful change carries either a named code judo move or an explicit note that none exists, every rule has been checked against the diff, and the findings are ordered structural-first.
