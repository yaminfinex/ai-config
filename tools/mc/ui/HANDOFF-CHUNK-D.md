# TASK-56 chunk D pickup brief

For a fresh builder with zero conversation history. Read, in order:
(1) the task capture `backlog/tasks/task-56 - Style-minimal-shell-thinnest-slice-plus-architecture-constitution.md`
in the mission repo (`2026-07-15-mission-control`); (2) the binding
architecture `artifacts/frontend-tech-proposal.md` (six settled decisions
+ BUILD PATTERNS + the behaviour–skin seam — settled law, stop-and-report
if one seems wrong); (3) `ARCHITECTURE.md` here — the constitution, judged
as hard as code, your acceptance rubric; (4) `README.md` for the dev
recipe and gates; (5) the `task-56-shell` hcom thread for the chunk
protocol (commit per chunk, report DONE with uncached gate output +
context %, STOP for independent review). This file is C→D handoff state,
not law; DELETE IT in the commit that lands chunk D, like the brief
before it.

## Where chunks B + C left the tree

Chunk B (constitution + tooling + scaffold finish) closed at e032c5b;
chunk C (entities/invalidation + Vitest law layer) closed at 88bc127.
Both PASSED independent review. Constitution sections born from review
findings — the law there is load-bearing, not decorative:

- **§6 cache-provenance staleness**: the invalidation baseline is the
  cached payload's own provenance, never a poll-to-poll chain (a real
  race was found and killed; the tests replay it deterministically).
- **§4 + store: normalized rehydration**: every path from localStorage
  into the working-set store funnels through `normalizeWorkingSetState`
  (over-cap/malformed persisted state was able to bypass the cap).
- **§7 namespace 404s**: SPA fallback reserves `assets/` + a finite
  root-file set; extension sniffing was rejected because artifact
  identities are file paths.
- **§5 the D3 comment seam**: binds from the first comment-shaped diff;
  operations async, `watch(cb): Unsubscribe` deliberately sync.

## Chunk D scope (from the capture)

**Slice views: mission list + mission page read path, in skin A
(minimal), fully three-layer conformant.** The constitution's §9
add-a-view recipe is the path; the four-goal exit review (readability,
testability, extensibility, simplicity) is the judge.

Honest state: both skins already carry read-path views over VM props
(`skins/minimal/`, `skins/terminal/`) — they shipped with the B scaffold
and passed review as scaffold, not as the finished slice. D's job is to
take the skin-A slice to done against the capture: real data end-to-end,
warnings/degradation rendered honestly, working-set wiring driven by the
UI it has (active thread toggle exists; material slots have NO UI — see
below), and every view-model change specced (Vitest is a gate now).
Judge the existing views against the constitution before growing them —
finishing may mean correcting.

## Ready to consume vs must-not-touch

Ready (all specced, all green): `entities/types.ts` (sectioned +
provenance wire, pinned by Go raw-key tests), `entities/missions.ts`
hooks, `entities/version.ts` scope-aware invalidation (mounted per page
— a new page mounts it with ITS scope), `view-models/*` (sort law,
preview, gap honesty), `stores/working-set.ts` (closed actions +
normalized rehydration).

D must NOT touch:
- **Comment store** — no comment-shaped code exists; it is NOT in the D
  slice. §5 binds the moment anyone adds any.
- **Skin B and the swap proof** — next chunk. Skin-A work must not
  require touching `skins/terminal/` beyond what a shared prop-contract
  change forces (and a contract change means updating BOTH skins in the
  same diff, per §3).
- **The wire** — no fetch outside `entities/`; a view needing new data
  means a Go DTO + raw-key contract test FIRST (§9).
- **Material slots** — stored but undriven, by ruling. If the D slice
  grows a material-opening surface it must drive the EXISTING actions;
  more likely D ships without one — leave the ledger entry standing.

## Sharp edges

- **The two-grain law is the violation class a views chunk is most
  likely to reintroduce — plainly: this exact class was caught in B
  review** (a message preview wrapped in the fact grain; `text-white` in
  vendored primitives). Every piece of content you render is either a
  derived fact (`font-fact`) or human speech (`font-speech`), classified
  by what it IS, not by what the skin's faces happen to look like. Raw
  colors — including named utilities like `text-white` — are automatic
  rejections; add a semantic token to BOTH skins' sheets instead (§3).
- Gates, from `tools/mc`: `GOTOOLCHAIN=local mise exec go@1.26.5 -- go
  test ./... -count=1` and `go vet ./...`; from `tools/mc/ui`:
  `bun run test` + `bun run build` + `bun run check` — all five green,
  uncached, in every DONE report.
- `vite build` deletes `dist/.gitkeep`; restore it before committing
  (`git checkout -- dist/.gitkeep`) — the go:embed needs it.
- Biome does not lint CSS (Tailwind v4 at-rules); token sheets are
  governed by build + review — do not "fix" the exclusion.
- shadcn additions: `bunx shadcn@latest add <name>` per component; the
  generated source is owned and must be retokenized on landing (B
  precedent).
- Playwright/e2e is NOT in D (it lands with skin B + the swap proof);
  the ledger names what the flow harness must eventually cover — do not
  shrink that list.
