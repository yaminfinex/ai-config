---
id: TASK-219
title: >-
  sesh — pi session adapter: wire amendment first, shipper discovery +
  exclusion boundary, store admission, pi entry parser, branch-aware surface
status: In Progress
assignee: []
created_date: '2026-07-15 07:40'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 218500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Dispatch-ready build unit from the merged pi design
(docs/design/pi-first-class-design.md §13 B2, relayed by hera at bus
#76688). Add pi as a fourth tool through the existing ship pipeline,
following the grok adapter (TASK-185) as the template end to end.

Scope per the design (binding):
1. Wire-spec amendment FIRST — pi joins the closed tool enum
   (tools/sesh/internal/wire/wire.go: claude/codex/grok today) before
   any code lands; store-before-clients rollout order; wire Amendment 3
   (grok) is the template, including the mixed-fleet compatibility
   paragraph (pre-amendment store behavior for PUT and recovery GET).
2. Shipper: pi session-root discovery (default agent dir's session
   tree) + the exclusion boundary stated in the amendment — what is
   deliberately NOT shipped, proven by a boundary test with a proven
   detector (grok exclusion boundary is the bar: exact-shape admission,
   never blocklist; symlink/decoy/traversal negatives).
3. Store admission of the pi tool kind (store/index/surface each
   reject or omit unknown tools today).
4. Index parsing of the pi entry shape — header id, entry id/parentId,
   role nested under message — generic parsing fails on it; a
   pi-specific parser with committed fixtures (identity-policy
   compliant per the TASK-208 pending ruling; follow the documented
   "repo is private" precedent until that ruling lands), including a
   BRANCHED session fixture.
5. Surface rendering — branch-aware, the single contract: render the
   active branch and label branch points (retained DR-6 behavior, §1);
   no lesser alternative without owner sign-off this unit does not
   carry; never silent flattening.
6. Gates: T27 against the fixtures asserts the single contract
   (active-branch rendering + labeled branch points on the branched
   fixture); wire compatibility gates green; `unknown` preserved under
   mutation.

Coordination: B1 (launch contract, TASK-218, hera's side) is
independent — either order. Activation is opt-in until the house
end-to-end pass per the design §12; not this task's exit criterion.
Frozen surfaces apply as always: index schema (no DDL without STOP),
ACK durability, R23, I1-I11, write discipline, fact_observations
INSERT-only, identifier-free journal contract, empty/absent-uuid
non-participation semantics preserved for whatever pi ids map to.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Wire amendment (pi in the closed enum) merged as the FIRST commit of the lane, byte-untouched elsewhere, mixed-fleet compat paragraph included
- [ ] #2 Shipper discovers pi sessions with the exclusion boundary proven by a tripwire-tested detector (config/creds/runtime state provably never ship)
- [ ] #3 Store/index/surface accept tool=pi end to end; pi entry shape (header id, entry id/parentId, nested role) parsed by a pi-specific parser with committed fixtures incl. a branched session
- [ ] #4 Surface renders the active branch and labels branch points (DR-6 single contract, T27 gate green on the branched fixture); never-500 floor holds for pi
- [ ] #5 Full pinned gate green; wire compat gates green; unknown-tool preserved under mutation
<!-- AC:END -->
