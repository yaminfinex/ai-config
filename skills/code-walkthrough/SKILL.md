---
name: code-walkthrough
description: Walk the user through code they want to trace and understand before judging it. Use when the user asks to be walked through, explained, or shown a PR, a branch, a commit range, a diff, a directory, a module, or a set of files; asks for a show-me walkthrough; or wants a review where they trace first and rule on findings as they go.
---

# Code walkthrough

Each turn you trace one **unit** of the scope, show its structure, volunteer findings under four fixed headings, and rewrite one **tracker** file that outlives the chat. You read and draft; the operator posts and the build seat edits. A change the operator wants becomes a `build-seat` item and the walkthrough continues.

- **Anchor** every claim about the code with `file:line` at the head sha, so the operator can open it.
- **Shape** before prose in every explanation: call the Skill tool with `show-me` for the shapes. Words are one-line verdicts beside each shape.

## Step 1: resolve the scope

Done when the scope is a named set of files at a named head sha, `git diff <sha> -- <files>` is empty (the working tree's copies equal that sha's, whatever HEAD is), and the design of record is named or recorded as none (a design doc, the PR description, a header contract).

- PR: `gh pr view <n> --json headRefOid,baseRefOid,files`.
- Branch: `git merge-base <base> <head>`, then `git diff --name-only <merge-base> <head>`.
- Commit range: `git diff --name-only <from>..<to>`.
- Directory, module, or file list: those files at HEAD.

When the diff is not empty, name the sha and the differing files and stop: the operator or orchestrator provides the checkout. Read whole files at the head and use the diff only to learn what changed. A diff scope keeps each file's change status: a deleted file is read and anchored at the base sha and labelled "(deleted, at base <sha>)" in the unit list and the tracker; a renamed file is one unit under its new path, with the old path noted.

## Step 2: map the units and open

When a tracker already exists at the announced path (step 4), read it first and resume from its Session line, taking the next action it names in place of a fresh opening.

Split the scope into an ordered list of units, each a file, a type, or a flow that fits one turn: entry points first, then what they call, tests after the code they exercise. Done when every file in scope belongs to a unit.

The opening turn carries, in order: the scope in one line (refs, shas, design of record), the tracker path, the unit list, 3a for any question in the request that started the walkthrough, then 3b through 3d for the first unit. Its last line is "Steer me from here."

## Step 3: one turn

Done when every queued question is answered, one unit is swept under the four headings, the tracker is rewritten, and the menu is on screen. The closing turn (Done) is the one exception: it writes the hand-off and reports both paths in place of a unit and a menu.

**3a. Answer** every question the operator asked since your last turn, each with an anchor, before the planned unit.

**3b. Walk one unit.** Show its structure, then run the **sweep**: the unit's findings under these four headings, numbered continuously across the session. A heading with nothing under it says "none found", so the operator can trust the sweep.

- **Hard to read**: naming, size, nesting, what needs a second pass.
- **Hard to follow**: control flow, indirection, state that travels far, what is implicit.
- **Smells**: duplication, wrong home, dead paths, leaky boundaries, tests that do not test.
- **Design versus code**: where the code and the design of record or the PR description disagree, said plainly, naming which side is stale.

**3c. Rewrite the tracker** (step 4) to current truth. In chat, show only what changed: new items in full, changed items by number and new status.

**3d. Menu**: numbered choices for the next unit, each parked question, and any finding worth digging into.

## Rulings

Operator pushback on a finding is a **ruling**. Rewrite the item to the operator's rule verbatim with status `overruled`, and move on. Agreement sets `agreed`; a change the operator wants made sets `build-seat`; an item the operator drops sets `dismissed`; the operator confirming an item was posted sets `posted`, and a posted item is already delivered, so every later hand-off leaves it out.

A doubt about completeness ("are you sure that is all?") triggers a second full sweep of the same unit under the same four headings, before the next unit.

## Step 4: the tracker

One markdown file, the single source of truth, wholesale-rewritten at 3c of every turn. Announce its path in the opening turn. Default: the active mission's artifacts directory (`using-missions` skill) under `code-walkthrough/<scope-slug>.md`; with no mission, a path the operator names in the opening turn. The file lives outside the repository under review. Template, structure only: [`references/tracker.md`](references/tracker.md).

Near its top, one line rewritten every turn: `Session: <in progress | stopped by the operator | done> · next: <the next action> · hand-off: <path or none>`. Below it: the scope (refs, shas, file list), the units (visited, current, pending), every finding with a status from `open | agreed | overruled | dismissed | build-seat | posted`, the parked questions, and the design of record when there is one.

A finding's number is never reused; its number, unit, and heading stay fixed once written; its text and anchor may be corrected, and a ruling replaces the text with the operator's rule verbatim.

## Done

Done when every unit is visited and every finding carries a status other than `open`, or the operator says stop. The closing turn writes the hand-off beside the tracker in the form the operator asks for, PR review comments or a build brief ([`references/handoff.md`](references/handoff.md)), sets the Session line, and reports both paths.
