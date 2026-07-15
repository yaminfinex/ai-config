---
id: TASK-219
title: >-
  sesh — pi session adapter: wire amendment first, shipper discovery +
  exclusion boundary, store admission, pi entry parser, branch-aware surface
status: Done
updated_date: '2026-07-15'
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
- [x] #1 Wire amendment (pi in the closed enum) merged as the FIRST commit of the lane, byte-untouched elsewhere, mixed-fleet compat paragraph included
- [x] #2 Shipper discovers pi sessions with the exclusion boundary proven by a tripwire-tested detector (config/creds/runtime state provably never ship)
- [x] #3 Store/index/surface accept tool=pi end to end; pi entry shape (header id, entry id/parentId, nested role) parsed by a pi-specific parser with committed fixtures incl. a branched session
- [x] #4 Surface renders the active branch and labels branch points (DR-6 single contract, T27 gate green on the branched fixture); never-500 floor holds for pi
- [x] #5 Full pinned gate green; wire compat gates green; unknown-tool preserved under mutation
<!-- AC:END -->

## Evidence (Done, 2026-07-15)

Lane: branch task-219-pi-adapter (builder-gemi, codex gpt-5.6-sol; sole
substance reviewer reviewer-kiru, codex; gate cleared by hera, merge +
post-merge battery + push delegated to mika under hera's convention).
6 linear commits, 29 files; merge 62c4ab3 --no-ff.

- AC1: wire Amendment 4 first commit (77775e9): pi in the closed enum,
  File Identity (+ real-directory root clause from review), index tool
  column, dated note, mixed-fleet PUT/recovery-GET paragraph. Frozen
  sections hash-verified byte-identical by builder AND reviewer.
- AC2: exact-shape admission ($HOME/.pi/agent/sessions/<cwd-key>/
  <timestamp>_<uuid>.jsonl, Lstat-rejected symlink root, pi-specific
  policy — legacy tools byte-unchanged); boundary suite with decoys,
  depth, traversal, symlinks, root-symlink negative; widened-mutant
  detector proven live.
- AC3: store parseTool + wire + index pi parser (header id = session
  identity w/ empty message_uuid non-participant; entry id ->
  message_uuid; parentId in immutable mirror, no DDL); branched fixture
  committed (synthetic, private-repo policy, TASK-208 pending).
- AC4: DR-6 active-branch + labeled branch points via version-keyed
  single-flight projection; canonical append-order leaf (shuffled-store
  T27 regression); window-bounded (1,000-row chain: exactly 200
  MirrorRange reads/page, warm page 0 rescans; work-counter mutant
  gate); adversarial cycles/dangling/duplicate/forest/10k-depth
  degrade non-500; label-as-active-leaf handled.
- Review: 4x P1 found, all FIXED + independently re-verified
  (#78411 root-symlink exfiltration, order-dependent leaf, per-request
  corpus-scale reads; #79502 stale-projection false-404 on new pi
  session — audited pi-only, legacy paths unaffected). VERDICT PASS
  #80011 at 0207d70.
- AC5: full uncached race suite green at final head; slim-client
  allowlist unchanged (client 7,286,946 B); unknown-tool preserved
  under mutation; post-merge house battery BY MIKA per hera convention
  (#80127): 4 module gates (herder/bottle/sesh/mish) + 59/59 checks,
  no TempDir flake; author-check clean; pushed.
- Deploy (store-before-clients per amendment): tag sesh-v0.1.15 exact;
  store live "sesh-v0.1.15"; LIVE differential probe: GET tool=pi
  clears enum (fails only on probe's missing wire version) vs
  tool=bogus -> unknown_tool; release published clean; this node
  v0.1.14 -> v0.1.15, shipping healthy, nodes page 200/0.36s.
- Accepted post-deploy gaps: no real-Mac pi discovery yet; no live pi
  corpus on this box (fixture-verified end to end; first real pi
  session on any fleet node is the live proof); activation (herder
  spawn --agent pi) is explicitly out of scope per design §12.
