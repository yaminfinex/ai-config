# tools/mc/ui — the constitution (v1)

This document is law for everything under `tools/mc/ui/src`. It is judged
as hard as code: a reviewer must be able to REJECT a diff from this
document alone, and "the constitution didn't say" is an argument for
amending the constitution, not for merging the diff. It binds until the
pre-ratification exit review (TASK-56 chunk F) amends or ratifies it.

The settled decisions beneath it — D1–D6 plus the behaviour–skin seam —
live in the mission repo, `artifacts/frontend-tech-proposal.md`
(2026-07-15-mission-control). They are not re-arguable here. If a rule
below seems wrong, inconvenient, or harder than an alternative: stop and
report the tension. Never substitute a design.

How to run, build, and gate the tree is in [README.md](./README.md).

## 1. The map

| path | layer | contains | never contains |
| --- | --- | --- | --- |
| `src/entities/` | 1 — entity | wire types mirroring the Go DTOs; TanStack Query hooks, one module per source family; the fetch helper | JSX, rendering knowledge, derived/sorted/formatted data |
| `src/view-models/` | 2 — view-model | pure functions from entity payloads to render-ready data; the program's rendering laws as code | JSX, hooks, fetches, store access, `Date.now()`/randomness |
| `src/skins/<name>/` | 3 — skin | one component set per skin over the shared prop contracts; that skin's `tokens.css` | store access, entity hooks, router access, business/sort/derivation logic, raw colors |
| `src/skins/contract.ts` | seam | the `Skin` interface and the per-view prop contracts | anything else |
| `src/routes/` | composition | route components wiring hooks + view-models + working-set actions into skin components; the root layout | rendering laws, per-skin markup |
| `src/router.tsx` | composition | the typed route table, one route per shareable entity URL | components (imports them; never the reverse) |
| `src/stores/` | client state | the working-set Zustand store (closed action set) | server-derived entity data |
| `src/components/ui/` | vendored | shadcn primitives, owned source, added per-component | hand-rolled mc logic (edits for tokens/variants are fine) |
| `src/styles/globals.css` | tokens | the `@theme inline` mapping from semantic tokens to utilities | per-skin values |
| `src/lib/` | util | `cn()` and similarly tiny, layer-free helpers | anything with an opinion about mc |

File names are kebab-case (`mission-list-view.tsx`); exported React
components are PascalCase and skin components carry their skin as prefix
(`MinimalMissionListView`). Imports use the `@/` alias, never `../`
chains. Biome (`bun run check`) is the arbiter of format and lint; its
config is part of the reviewed tree.

## 2. The three-layer rule

Data flows one way: **entities → view-models → components.** Each layer
may import from the one below it, never from the one above.

**Layer 1 — entities.** Typed wire shapes and TanStack Query hooks
(`useMissions()`, `useMissionDetail(slug)`), one module per source
family, mirroring `tools/mc/api.go` DTO-for-DTO. The Go contract tests
(`api_test.go`, `TestAPIWireShapeRawKeys`) pin the raw JSON keys; the
TypeScript types must match them, field for field, and a change on either
side without the other is a contract break. Entity modules export query
keys alongside hooks. Views never own entity data — they subscribe to the
query cache; there is no entity data in `useState` or in Zustand, ever.

**Layer 2 — view-models.** Pure, synchronous, deterministic functions
from entity payloads to render-ready values. **This layer is where the
program's ratified laws live as code** — the thread sort law, preview
truncation, gap honesty, title fallbacks — and it is Vitest-tested
without a browser. A view-model function receives everything it needs as
arguments and touches nothing else. Absent data renders as absence
(`null`), never as a fabricated value: gap honesty is a layer-2 duty.

**Layer 3 — components.** Thin by policy. A skin component receives ONLY
view-model state and action callbacks through the prop contracts in
`skins/contract.ts`. Automatic rejections, each equivalent to raw hex:

- a component containing a sort order, a tier/rank rule, a derivation,
  or a truncation rule;
- a component importing a store, an entity hook, the router, or `fetch`;
- a component branching on skin name (skins differ by being different
  components, not by `if`);
- conditional rendering beyond what the view-model already decided
  (rendering `warning !== null` is fine; computing what counts as a
  warning is not).

**Routes are the composition layer, not a fourth idea.** A route
component wires entity hooks, view-model calls (memoized), working-set
actions, and navigation into the selected skin's components — and hands
them props. JSX rendered directly by `routes/` (the root layout's chrome)
is shared chrome: identical under every skin by construction. The moment
chrome needs to differ per skin, it moves behind the seam as a skin
component; chrome is never skinned with conditionals in a route.

## 3. The skin seam (D4 — both halves)

A skin is two files' worth of difference and nothing else: a token value
sheet (visual half) and a component set (behavioural-rendering half).

**Visual half — tokens.** Every skin defines THE SAME token names under
its `[data-theme="<name>"]` scope in `src/skins/<name>/tokens.css`:
the mc semantic layer (`--c-needs-you`, `--c-warn`, `--c-ok`,
`--c-quiet`, `--grain-fact`, `--grain-speech`, `--type-base`,
`--radius`) plus the shadcn slots, valued from it. `globals.css` maps
tokens to utilities once, globally. The discipline:

- Components use utilities and shadcn slots only. A raw color (hex,
  oklch, named CSS color) in a component or its class string is an
  automatic rejection — meaning lives in the token name.
- Semantic names only: a token says what it is FOR, never what it looks
  like. `--c-needs-you`, not `--red`. New tokens are added to every
  skin's sheet in the same diff, or the diff is rejected.
- Two-grain type is law (settled style principle): derived facts render
  `font-fact` (mono), human speech renders `font-speech` (proportional).
  A skin may value both grains with the same face (terminal does); a
  component may not pick the wrong grain for its content.

**Behavioural half — the component set IS the skin.** All skins implement
the same `Skin` interface over the same prop contracts. Behaviour —
what toggle does, what the sort is, what an action publishes — lives in
layers 1–2 and CANNOT vary between skins. How behaviour RENDERS may vary,
and every such variance is declared as a field on the `Skin` object
(today: `activeThreadRendering: "in-row" | "panel"`), so tests can assert
the difference instead of discovering it. An undeclared behavioural
rendering difference between skins is a bug.

**Selection.** The skin is chosen once, at the composition root: the
toggle sets `data-theme` (visual half) and the `Skin` component set
(behavioural half) together, and components consume the set via
`SkinContext`. No component selects its own skin. Skin choice is a
preference, persisted in localStorage beside — never inside — the
working-set store.

**Adding a skin** is: one `tokens.css` (same names, new values), one
directory of components satisfying `contract.ts`, one entry in
`skins/index.ts`. If adding a skin requires touching anything else, the
seam has been broken — fix the seam, not the skin.

## 4. The working-set store

One Zustand store (`stores/working-set.ts`) holds the ratified shape
`{view, thread, materials}` with a **closed action set**. The invariants
are transition rules and exist in exactly one place:

- `navigate` changes the view and NOTHING else — navigation never grows
  the layout.
- The active thread slot replaces by default (`toggleThread`) and never
  multiplies: at most one.
- `openMaterial` replaces the newest unpinned slot; `pinMaterial` is the
  only grow gesture; the cap (`MATERIAL_CAP = 2`) is a hard wall — at
  cap with everything pinned, opening refuses.

Views dispatch these actions and never mutate layout state another way;
a second store, or layout state in `useState` that outlives a component,
is a rejection. Transition rules are exported as pure functions
(`applyOpenMaterial`, `applyToggleThread`) so Vitest attacks the
invariants directly, and Playwright attacks them through the UI
("clicking around cannot exceed the cap") — identically under both skins.

Material slots are in the store but not yet driven by any UI: the
ratified shape includes them, so the store carries them from day one
rather than growing an incompatible shape later. The first surface that
opens materials (documents/source beside a thread) must drive THESE
actions — building its own slot state is a rejection. Layout state is
client-local (`persist` → localStorage) and never rides a URL.

## 5. The wire contract and invalidation

Provenance IS the invalidation contract (chunk A law). Every
source-backed section carries `provenance: {source, observedAt, version}`;
`/api/v1/version` serves exactly those stamps, and it is scope-aware:

- a missions-list page polls bare `/api/v1/version` and refetches when
  `missions.version` moves;
- a mission-detail page polls `/api/v1/version?mission=<slug>` and
  watches `mission` + `journal` + `roster` — the three stamps of its
  three sections. The bare `missions` token cannot vouch for a detail
  payload (the per-slug projection observes more); polling the wrong
  scope for a page is a bug, not a style choice.

Entity hooks own this: version polling and query invalidation live in
the entity layer, keyed to the page's scope. Components know nothing
about versions, polling, or staleness mechanics.

Degraded upstreams arrive as HTTP 200 with warnings in the payload.
The UI renders that honesty: warnings render as warnings, absent data
renders as absence, and no blank error page stands in for a partially
degraded payload. Inventing provenance, or presenting a stale observation
as current, is a rejection — `observedAt` is what the server says it is.

## 6. Routing

The route table in `router.tsx` is code-based, typed, and the registry
of the implementation law: **every core entity has a shareable URL.** A
new entity surface means a new route here — a view reachable only by
client state is a rejection. URLs anchor entities only; layout and pane
arrangements never ride a URL. The SPA mounts at basepath `/ui` (D1
transition); the Go server owns `/api` and falls back to the SPA shell
only for route-like paths (asset-shaped misses 404 — `ui.go`).

Route components read params with `useParams({ from: ... })`. Importing
`router.tsx` from any component module is a rejection — it closes an ESM
cycle (router → page → router) and couples pages to the tree that mounts
them.

## 7. Testing patterns

The test for each layer matches the layer's nature; a test in the wrong
place is a review finding even when green.

- **Wire contract — Go.** `tools/mc/api_test.go` pins the raw JSON keys
  as literals (`TestAPIWireShapeRawKeys`) plus degraded/empty/
  source-change behavior. A wire change breaks there first, on purpose.
  The frontend never mocks the wire shape it wishes it had.
- **View-models and store transitions — Vitest.** Every exported
  view-model function and pure transition function has a spec beside it
  (`foo.test.ts` next to `foo.ts`); no browser, no DOM, milliseconds.
  Laws get named tests (sort law, cap, preview truncation, gap honesty).
- **Flows — Playwright, against the real stack.** The e2e harness (chunk
  E) runs the REAL Go server with fake `mish`/`hcom`/`herder` shell
  scripts and a seeded journal fixture — the same pattern `api_test.go`
  and `tools/mc/testdata/` already use. No frontend mocks, no request
  interception: real wire or it doesn't count.
- **The skin-swap proof.** Behaviour assertions run identically under
  BOTH skins; the declared behavioural rendering difference is asserted
  via the `Skin` declaration (`data-testid="active-thread"` inside a row
  vs inside the panel), and computed styles must differ across the
  toggle. A behaviour test that passes under one skin and not the other
  is a seam breach, not a flaky test.
- **Hooks/selectors for tests:** stable `data-testid` attributes, named
  in kebab-case for the thing they mark (`mission-row`, `skin-toggle`,
  `active-thread`). Tests select by testid or accessible role — never by
  class names, which belong to skins.

## 8. Adding a view — the recipe

Route in `router.tsx` → entity hook(s) it needs (new endpoint = new Go
DTO + raw-key contract test first) → view-model function + its Vitest
spec → one component per skin satisfying a new prop contract in
`contract.ts` → a Playwright flow for its acceptance lines. Five steps in
predictable places; a view that needs a sixth kind of change means the
architecture has a gap — stop and report it before building around it.

## 9. Current state (ledger, not law)

Honest state of this tree as of chunk B, for reviewers and the next
builder:

- `entities/types.ts` and `entities/version.ts` are STALE against the
  remediated wire contract (flat pre-provenance shapes, unscoped poll)
  and are marked so in-file. Chunk C rewrites them per §5 and adds the
  Vitest layer; nothing may build on them until they match the wire.
- View-models and the working-set store exist and follow §2/§4 but have
  no specs yet (chunk C).
- Both skins exist over the current contracts with one declared
  behavioural rendering difference; the full swap proof + Playwright
  harness + MCP wiring is chunk E.
- `bun run test` intentionally fails (no specs, no `--passWithNoTests`);
  `bun run build` and `bun run check` are the UI gates today, alongside
  the Go gates (README).
