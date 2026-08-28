---
id: TASK-1
title: Selection-anchored comments
status: Done
assignee:
  - display-coder-vida
created_date: '2026-07-22 03:53'
updated_date: '2026-07-22 09:11'
labels:
  - comments
  - run-display-review
dependencies:
  - TASK-4
  - TASK-7
  - TASK-19
priority: high
ordinal: 1
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Let reviewers select text in a document and attach a comment to that exact range, instead of whole-doc comments only. The comments collection already accepts a free-form anchor field that nothing reads or writes today (sites/display/quick.json:105) - activate it. Anchor by quoted text + position hints and re-anchor by text search on render (plannotator's proven approach; also W3C annotation text-quote selectors). Needs: viewer selection UI, highlight rendering, sidebar-to-highlight linking. Interacts with the sandboxed-iframe question for HTML docs (selection inside the iframe is not reachable from the parent) - likely lands first for markdown/rendered content.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Shipped: bf856a9 + b1313eb on feat/display-review-2. R1 review ACCEPT (thread dr-t1-review). Anchor schema {v, quote, prefix, suffix, version=doc.updated_at}; shared anchoring engine in frame-bridge.js serves both surfaces; bridge protocol extended with bounded/validated anchor messages.
<!-- SECTION:NOTES:END -->
