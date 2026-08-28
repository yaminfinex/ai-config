---
id: TASK-25
title: Section and block-level commenting (pinpoint targeting)
status: Done
assignee:
  - '@display-coder-vema'
created_date: '2026-07-23 01:16'
updated_date: '2026-07-23 02:37'
labels:
  - display
  - comments
  - run-display-review
dependencies: []
priority: high
ordinal: 8015
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Commenting on a whole section/heading/paragraph/table/code block as a unit, without hand-selecting its text. Plannotator's model (verified in source): a pinpoint input mode where hovering resolves the element under the cursor UP TO a whole block - heading, paragraph, list item, whole list, table, table row, code block - paints a soft wash over the block with a small label badge (e.g. heading: Setup, code block (ts)), and clicking creates a comment spanning that block. KEY MECHANICAL DECISION (settled): a block comment IS a text-quote anchor whose range spans the block - it rides the existing anchor schema {v, quote, prefix, suffix, version} unchanged, so survival, staleness, drain output, and both painting paths all work on it for free.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A discoverable mode/affordance on MARKDOWN docs lets the reviewer hover to target whole blocks (at minimum: headings, paragraphs, list items, tables, code blocks/pre), with a visible wash + short label naming the target; clicking opens the comment composer for that block
- [ ] #2 The resulting comment is a standard text-quote anchor spanning the block (capped sensibly for huge blocks - cap on quote length with prefix/suffix disambiguation is acceptable and must be stated); it paints, survives republish, goes stale, and drains exactly like a hand-selected anchor
- [ ] #3 Mode entry/exit is obvious and non-sticky-by-surprise (clear affordance to toggle, Esc exits); normal text selection keeps working untouched when not targeting
- [ ] #4 Whole-document comments remain a separate explicit action (unchanged)
- [ ] #5 HTML/iframe docs: in-frame hover targeting may be DEFERRED from this unit if the bridge extension is heavy - if deferred, state it and file the follow-up; markdown surface is the required deliverable
- [ ] #6 Feel details per task-16: wash + badge legible in the existing palette, no layout shift, reduced-motion honored; polish pass stated in the DONE report
- [ ] #7 Gates: go build/test, all four node suites, browser smoke under just serve-local + agent-browser
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Shipped: de13f14 on feat/display-review-4. R-U9 review ACCEPT (thread dr-t25-review). Pinpoint mode on markdown: nearest-ancestor grain {H1-6,P,LI,PRE,TABLE,BLOCKQUOTE}, wash+badge, click->U8 composer, block comment = standard capped text-quote anchor. Iframe surface deferred to task-26.
<!-- SECTION:NOTES:END -->
