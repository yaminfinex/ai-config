---
name: code-walkthrough
description: Walk the user through code they want to trace and understand before judging it. Use when the user asks to be walked through, explained, or shown a PR, a branch, a commit range, a diff, a directory, a module, or a set of files; asks for a show-me walkthrough; or wants a review where they trace first and rule on findings as they go.
---

# Code walkthrough

The operator drives. Each turn you trace one **unit** of the scope, show its structure, volunteer findings under four fixed headings, and rewrite one **tracker** file that outlives the chat. You read and draft; the operator posts and the build seat edits. A change the operator wants becomes a `build-seat` item and the walkthrough continues.

Two rules hold for every sentence you write:

- **Anchor** every claim with `file:line` at the head ref, so the operator can open it.
- **Shape** before prose: a state diagram, a call tree with checkpoints, a sequence diagram, a covered/not-covered table, a shape-as-data table, or a target-shape sketch. Call the Skill tool with `show-me` for the vocabulary. Words are one-line verdicts beside each shape.

## Step 1: resolve the scope

Done when the scope is a named set of files at a named head sha, the working tree holds those files at that sha (`git rev-parse HEAD` equals it, or `git diff <sha> -- <files>` is empty), and the design of record is named or recorded as none (a design doc, the PR description, a header contract).

- PR: `gh pr view <n> --json headRefOid,baseRefOid,files`.
- Branch: `git merge-base <base> <head>`, then `git diff --name-only <merge-base> <head>`.
- Commit range: `git diff --name-only <from>..<to>`.
- Directory, module, or file list: those files at HEAD.

When the working tree holds a different sha, name the sha you need and stop: the operator or orchestrator provides the checkout. Read whole files at the head and use the diff only to learn what changed.

## Step 2: map the units and open

Split the scope into an ordered list of units, each a file, a type, or a flow that fits one turn: entry points first, then what they call, tests after the code they exercise. Done when every file in scope belongs to a unit.

The opening turn carries, in order: the scope in one line (refs, shas, design of record), the tracker path (step 4), the unit list, then step 3b through 3d for the first unit. Its last line is "Steer me from here."

## Step 3: one turn

Done when 3d is on screen. Every turn runs a to d in order.

**3a. Answer** every question the operator asked since your last turn, each with an anchor, before the planned unit.

**3b. Walk one unit.** Show its structure, then run the **sweep**: the unit's findings under these four headings, numbered continuously across the session. A heading with nothing under it says "none found", so the operator can trust the sweep.

- **Hard to read**: naming, size, nesting, what needs a second pass.
- **Hard to follow**: control flow, indirection, state that travels far, what is implicit.
- **Smells**: duplication, wrong home, dead paths, leaky boundaries, tests that do not test.
- **Design versus code**: where the code and the design of record or the PR description disagree, said plainly, naming which side is stale.

**3c. Rewrite the tracker** (step 4) to current truth. In chat, show only what changed: new items in full, changed items by number and new status.

**3d. Menu**: numbered choices for the next unit, each parked question, and any finding worth digging into.

## Rulings

Operator pushback on a finding is a **ruling**. Rewrite the item to the operator's rule verbatim with status `overruled`, and move on. Agreement sets `agreed`; a change the operator wants made sets `build-seat`; an item the operator drops sets `dismissed`.

A doubt about completeness ("are you sure that is all?") triggers a second full sweep of the same unit under the same four headings, before the next unit.

## Step 4: the tracker

One markdown file, the single source of truth, wholesale-rewritten at 3c of every turn. Announce its path in the opening turn. Default: the active mission's artifacts directory (`using-missions` skill) under `code-walkthrough/<scope-slug>.md`; with no mission, a path the operator names in the opening turn. The file lives outside the repository under review. Template: [`references/tracker.md`](references/tracker.md).

It carries the scope (refs, shas, file list), the units (visited, current, pending), every finding with a status from `open | agreed | overruled | dismissed | build-seat | posted` (an overruled item quotes the rule), the parked questions, and the design of record when there is one.

## Done

Done when every unit is visited and every finding carries a status other than `open`, or the operator says stop. The closing turn writes the hand-off beside the tracker in the form the operator asks for, PR review comments or a build brief ([`references/handoff.md`](references/handoff.md)), and reports both paths.
