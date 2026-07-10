---
id: TASK-135
title: >-
  sesh — logical-session inheritance must not fire on parse-written ids (codex
  meta-only first chunk diverges from Reindex) (run-sesh-107 remediation F1b)
status: To Do
assignee: []
created_date: '2026-07-10 00:11'
labels:
  - run-sesh-107
dependencies: []
priority: high
ordinal: 135000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Born from the run-sesh-107 review tail re-verification (thread sesh107-review #34797): CONFIRMED regression introduced by the resumed-append unification fix, bisected to that commit.

Defect (tools/sesh/internal/index/index.go, inheritFileLogicalSession): the rule "inherit when the file generation has exactly one existing logical id that differs from the wire id" cannot distinguish a logical id written by UNIFICATION (the case it exists for) from one written by PARSE. Codex hits the parse case: session_meta carries a payload id L != wire id W, and codex response_items carry no top-level session id (they parse to the wire-id fallback). When a file's first shipped chunk contains only the session_meta line (an ordinary tailing boundary — meta is the first line codex writes), the file's single existing logical is parse-written L, so every later response_item incrementally inherits L — but Reindex parses those items to W and nothing unifies a single file with itself, so reindex yields mixed {L, W}. Reproduced: incremental checksum 87da0e5d.../3 vs reindex fd9eae87.../3.

Fix direction (settled): inheritance may fire only when the existing logical id was written by unification, not by parse. Mechanism suggestion (not mandatory if you find something simpler that meets the bar): record the parse-time logical id per row in an index-owned disposable column (Reindex rebuilds it); "unification-written" = stored logical_session_id differs from the row's parse-time id. With that rule the meta-only file does not inherit (its meta row's stored id equals its parsed id), while a unified resume file does (its rows were rewritten). Whatever the mechanism, the acceptance bar is unchanged: incremental state == post-Reindex state, exactly.

Settled decisions — do not re-litigate; tension = STOP and report on your unit thread:
- Reindex semantics stay authoritative and UNCHANGED. The reviewer observed that propagating session_meta ids file-wide at parse time might be the semantically better grouping — that is a frozen-parse-semantics question for the spec owner, recorded here as an observation only. Do not implement it.
- No wire-schema or files-table changes; index-owned disposable state only, rebuilt by Reindex.
- The three existing adversarial equivalence tests and the whole gate stay green; add the meta-only shape (meta-only first chunk, then items) as a new equivalence test, plus a meta+items-in-one-chunk control case.
- Perf property preserved: append cost flat vs unrelated-file count (benchmark).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Reviewer's meta-only repro shape yields identical incremental and post-Reindex checksums (new equivalence test covering meta-only first chunk, plus meta+items single-chunk control)
- [ ] #2 Resumed-append unification (both id orderings + transitive chain tests) still green
- [ ] #3 Benchmark still flat vs unrelated-file count
- [ ] #4 Full pinned gate green uncached
<!-- AC:END -->
