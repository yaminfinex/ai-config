# TASK-56 chunk F pickup brief

For a fresh builder with zero conversation history. Read, in order:
(1) the task capture `backlog/tasks/task-56 - Style-minimal-shell-thinnest-slice-plus-architecture-constitution.md`
in the mission repo (`2026-07-15-mission-control`); (2) the binding
architecture `artifacts/frontend-tech-proposal.md` (six settled decisions
+ BUILD PATTERNS + the behaviour–skin seam — settled law, stop-and-report
if one seems wrong); (3) `ARCHITECTURE.md` here — the constitution, YOUR
PRIMARY SUBJECT this chunk; (4) `README.md` for the dev recipe and the
six gates; (5) the `task-56-shell` hcom thread for the chunk protocol
(commit per chunk, report DONE with uncached gate output + context %,
STOP for independent review). This file is E→F handoff state, not law;
DELETE IT in the commit that lands chunk F, like the two briefs before it.

## Where chunks A–E left the tree

All five prior chunks PASSED independent cross-family review (E closed at
7157ed8 after three rounds). The slice is done end to end: Go /api/v1
with provenance-stamped sections and scope-aware /version; entity hooks
with the cache-provenance staleness baseline (§6 — poll-to-poll chains
are constitutionally banned); pure view-model law layer (84 Vitest
specs, including render-contract.test.ts pinning failure > loading >
empty > data at the VM boundary); working-set store with normalized
rehydration; both skins live over one prop contract; and the e2e flow
suite (33 flows) whose skin-swap proof the reviewer adversarially
confirmed cannot be fooled (rendering in both declared places FAILS).

The e2e harness (`e2e/harness.ts`), because F touches it:

- REAL Go binary (built by `e2e/global-setup.ts` with the SPA embedded)
  + fake mish/hcom/herder shell scripts + seeded journal fixture — the
  api_test.go pattern. No frontend mocks, no request interception.
- Mish modes: healthy | empty | degraded | slow (2s — loading→data
  transition) | hang (never answers — stable loading state). Slow/hang
  startup probes hit /ui/ NOT /api, so the probe cannot warm the
  resolver cache the test depends on.
- Full-claim-set assertions: `expectListPageState` /
  `expectDetailPageState` assert EVERY precedence claim per state —
  strengthen these, never bypass them.
- Process-tree discipline: servers spawn detached (own process group);
  `stop()` group-SIGKILLs, then polls `ps -g` and THROWS if anything
  survives — the teardown is the leak assertion. Keep that property.
- Fixture split: `e2e/fixtures/` holds e2e-local variants (journal,
  warning-free status-mission-one, status-mission-broken);
  `tools/mc/testdata/` is the Go tests' property — never edit it for a
  UI test's convenience (E review ratified exactly this split).
- `.mcp.json` tool-dir scoping is RULED correct — do not move it to
  repo root.

## Chunk F scope

1. **Constitution FINAL pass.** Read the whole ARCHITECTURE.md against
   the tree as it now exists. Every law must be checkable from the repo;
   the §10 ledger must be exact (entries: missions-family live
   transition TTL-bound; no comment-shaped code, §5 binds on first diff;
   material slots stored-but-undriven). Five chunks of evolution mean
   stale phrasing may exist anywhere — hunt it. Constitution-as-
   deliverable review scope is standing practice: reviewers judge the
   document as hard as code.
2. **Two held should-fixes** (ordered by the orchestrator, recorded in
   the capture):
   - The warm-cache e2e flow must assert the EXACT cached observedAt
     value in the staleness line (deterministic fixture clock or a
     value captured pre-shutdown), not just the words "last observed".
   - Harness: ephemeral ports (fixed ports collide under concurrent
     gate runs) + awaited listen/stop on `startDeadShell`. The
     group-lifecycle stop for mc servers already landed — this is the
     port-allocation/listen half.
3. **Any polish the constitution pass demands** — if a law and the tree
   disagree, fix whichever is wrong and say which it was.

F ends at the **PRE-RATIFICATION EXIT REVIEW**: fresh-context reviewers
judge the four goals — readability, testability, extensibility,
simplicity. You PREPARE the tree; the review itself is dispatched by the
orchestrator. Do not self-certify ratification.

## Sharp edges

- Gates are SIX now, all uncached in every report — from `tools/mc`:
  `GOTOOLCHAIN=local mise exec go@1.26.5 -- go vet ./...` and
  `go test ./... -count=1`; from `tools/mc/ui`: `bun run test`,
  `bun run build`, `bun run check`, `bun run test:e2e`.
- `vite build` deletes `dist/.gitkeep`; restore before committing
  (`git checkout -- dist/.gitkeep`) — the go:embed needs it. The e2e
  global-setup builds too, so run the restore after e2e as well.
- `tsc -b` typechecks `e2e/` and `playwright.config.ts` (tsconfig
  includes them) — strict mode applies to harness code.
- The mission resolver caches observations 60s BY DESIGN (a poll must
  not fan out git checks): missions-family warning transitions are NOT
  live-testable against the stock binary; the ledger names it. Do not
  "fix" this by shrinking the TTL in the harness.
- Two-grain law: every rendered content is fact (`font-fact`) or speech
  (`font-speech`) by what it IS; raw/named color utilities are automatic
  rejections; new tokens go into BOTH skins' sheets in the same diff.
- A shared prop-contract change means updating BOTH skins in the same
  diff (§3). Playwright chromium build r1228 is already in the local
  cache — no download on `test:e2e`.
- Report the TIP sha after any amend (an earlier report quoted a
  pre-amend sha; the orchestrator caught it).
