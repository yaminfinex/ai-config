# TASK-56 chunk B pickup brief

For a fresh builder with zero conversation history. Read, in order:
(1) the task capture `backlog/tasks/task-56 - Style-minimal-shell-thinnest-slice-plus-architecture-constitution.md`
in the mission repo (`2026-07-15-mission-control`); (2) the binding
architecture `artifacts/frontend-tech-proposal.md` (six settled decisions +
BUILD PATTERNS + the behaviour–skin seam — settled law, stop-and-report if
one seems wrong); (3) `artifacts/mc-successor-spec.md` §1–2; (4) the
`task-56-shell` hcom thread for the chunk protocol (commit per chunk, report
DONE with uncached gate output + context %, STOP for independent review).
This file is chunk-A→B handoff state, not law; delete it once chunk B lands.

## Where chunk A left the API (reviewed, PASSED at 45a39d5)

`tools/mc/api.go` + `api_test.go`. The wire contract the UI must live on:

- **Provenance = invalidation.** Every source-backed payload carries
  `provenance: {source, observedAt, version}` (truth-forms law,
  `artifacts/vocabulary-doctrine.md`). The `version` is an opaque
  content-derived token (observation stamps excluded from the hash).
- **`GET /api/v1/version` is the pollable contract.** Bare poll → stamps for
  the three source families: `journal` (thread store), `missions`
  (`mish status --all`), `roster` (`hcom list + herder list`). With
  `?mission=<slug>` it ALSO returns `mission`: the per-slug stamp, equal by
  construction to the detail response's mission-section token (same
  derivation function, `missionStatusStamped`). A missions-list client polls
  bare and watches `missions`; a mission-detail client polls with its slug
  and watches `mission` + `journal` + `roster`. Reason: the per-slug mish
  projection observes things `--all` never does (git staleness) — see
  `TestAPIVersionMissionScopeSeesPerSlugOnlyChanges`.
- **`GET /api/v1/missions`** → `{missions: [...], warning, provenance}`.
- **`GET /api/v1/mission/{slug}`** → three per-source sections:
  `{mission: {status, provenance}, threads: {rows, provenance},
  roster: {agents, warning, provenance}}`. Degraded upstreams stay HTTP 200
  with warnings in the payload — render honesty, never a blank error.
- **`/ui/` serves the built SPA via go:embed** (`ui.go`); unbuilt tree → 501.
  Legacy HTML pages keep serving during the transition (D1).

## What the WIP scaffold contains (UNREVIEWED chunk-B raw material)

Committed as an honest WIP: it compiles (`bun run build` green under strict
tsc, Vite 7) but NOTHING in `src/` has been reviewed, and the entity layer
is STALE against the remediated API above. State by directory:

- `package.json` / `bun.lock` — Bun-managed. Pins that matter: Vite ^7
  (settled D6; Vite 8 exists — do not upgrade past the settled decision),
  `@vitejs/plugin-react` ^5.2 (the LAST line supporting Vite 7; 6.x needs
  Vite 8), TypeScript ^5.9 (TS 7 = the native tsgo port, untested here),
  Vitest ^4, `@playwright/test` ^1.61, Tailwind ^4 (CSS-first), TanStack
  Router/Query, Zustand 5, Biome 2. `radix-ui` was added by the shadcn CLI.
- `components.json` + `src/components/ui/` — shadcn (new-york, zinc,
  cssVariables) primitives: button, badge, card, separator. Add more via
  `bunx shadcn@latest add <name>` per-component (per the AI-Elements spike
  verdict in `artifacts/spike-ai-elements.md`: adopt as owned source,
  per-component installs only, never the `ai` package — no thread surface
  exists in this slice yet, so nothing of it is installed).
- `src/styles/globals.css` — Tailwind v4 `@theme inline` mapping semantic
  tokens → utilities + shadcn slots. `src/skins/{minimal,terminal}/tokens.css`
  — the visual half of each skin as `[data-theme=...]` value files (minimal:
  near-default shadcn zinc dark; terminal: green-on-black, radius 0, 14px
  base — divergent in palette/radius/type scale as D4 requires).
- `src/entities/` — **STALE**: `types.ts` mirrors the PRE-remediation flat
  DTOs; `version.ts` polls the old `{cursor, generation}` shape. Chunk C
  must rewrite these against the sectioned + provenance contract (and use
  the scope-aware poll per page).
- `src/view-models/` — pure functions (mission list rows, thread sort law:
  open → expects rank decide·act·reply·read → updated desc; preview
  truncation at 280). No Vitest specs yet (chunk C). Consumes the stale
  payload shapes — rework alongside entities.
- `src/stores/working-set.ts` — the ratified working-set object
  `{view, thread, materials cap 2}` as a Zustand store with a closed action
  set (navigate never touches slots; toggleThread replaces; openMaterial
  replace-by-default; pin = the only grow gesture), `persist` to
  localStorage. Pure transition fns exported for tests; no tests yet.
  Material slots are not driven by any UI yet — they exist because the
  ratified shape includes them; say so in the constitution or cut them
  consciously (stop-and-report either way).
- `src/skins/` — `contract.ts` defines the behavioural seam: `Skin` =
  component set over view-model props only, with the declared behavioural
  rendering difference `activeThreadRendering: "in-row" (minimal) | "panel"
  (terminal)` — the active thread renders inside the row vs in a dedicated
  panel, over identical props/actions. `use-skin-choice.ts` holds the
  toggle (localStorage + `data-theme` + component set together).
- `src/routes/` + `src/router.tsx` — code-based typed route tree, basepath
  `/ui`, routes `/` and `/mission/$slug`. Route components are the
  composition layer: hooks + VM + working-set wiring, skin components get
  props only. NOTE a deliberate ESM cycle: `router.tsx` ↔ `mission-page.tsx`
  (`missionRoute.useParams()`); works via live bindings, but
  `useParams({ from: "/mission/$slug" })` breaks the cycle if it offends.
- `index.html` — ships `data-theme="minimal"` default.
- NOT built yet: ARCHITECTURE.md (constitution), all UI tests, Playwright
  config/harness, dev recipe.

## Chunk B scope (from the capture + review orders)

1. UI scaffold + tooling finished and reviewable; dev recipe documented
   (suggest: `go run . # :8390` + `bun run dev` proxying `/api`; prod =
   `bun run build` then the Go binary serves `/ui/`).
2. **Constitution v1** — `tools/mc/ui/ARCHITECTURE.md`: locations/naming,
   three-layer rule (entity hooks / pure view-models / thin components),
   skin seam rules (both halves), token discipline (semantic names only, no
   raw color in components), testing patterns, settled decisions. Judged as
   hard as code: a reviewer must be able to REJECT non-conforming code from
   it alone.
3. **Folded should-fix (A review):** SPA fallback must 404 asset-shaped
   misses (`/ui/assets/*` that don't exist currently get 200 index.html —
   `ui.go` `uiHandler`).
4. **Folded should-fix (A review):** API contract tests that pin RAW JSON
   keys (not round-trips through the same Go DTO structs) + degraded/empty/
   source-change cases beyond the existing ones.

Later chunks (not B): C = entities/VM/store + Vitest against the real
contract; D = slice views in skin A, three-layer conformant; E = skin B +
behavioural swap proof + Playwright (+ MCP wiring); F = constitution final
pass. E2E harness design already agreed in thread: real Go server + fake
`mish`/`hcom`/`herder` shell scripts + seeded journal fixture (the pattern
`api_test.go` and `tools/mc/testdata/` already use) — real wire, no
frontend mocks.

## Sharp edges

- Gates: run Go from `tools/mc` with `GOTOOLCHAIN=local mise exec go@1.26.5
  -- go test ./... -count=1` (and `go vet ./...`). go.mod says `go 1.22`;
  the 1.26.5 pin is the playbook's, via mise.
- `go test` embeds `ui/dist` — `dist/.gitkeep` keeps the embed valid when
  the SPA is unbuilt; `vite build` may remove it locally (git restores).
- The mission resolver caches mish reads (`ttl`, default 1min, injectable in
  tests — set `resolver.ttl = 0`).
- hcom protocol: prefix every command with the required flags per the thread;
  reports go to thread `task-56-shell`; STOP after each chunk report.
