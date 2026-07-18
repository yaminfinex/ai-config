# tools/mc/ui — the constitution (v1)

This document is law for everything under `tools/mc/ui/src`. It is judged
as hard as code: a reviewer must be able to REJECT a diff from this
document alone, and "the constitution didn't say" is an argument for
amending the constitution, not for merging the diff. It binds until the
pre-ratification exit review (readability, testability, extensibility,
simplicity) amends or ratifies it.

The settled decisions beneath it — D1–D6 plus the behaviour–skin seam —
live in the mission repo, `artifacts/frontend-tech-proposal.md`
(2026-07-15-mission-control). They are not re-arguable here. If a rule
below seems wrong, inconvenient, or harder than an alternative: stop and
report the tension. Never substitute a design.

How to run, build, and gate the tree is in [README.md](./README.md).

## 1. The map

| path | layer | contains | never contains |
| --- | --- | --- | --- |
| `src/entities/` | 1 — entity | wire types mirroring the Go DTOs; TanStack Query hooks, one module per source family; the fetch helper; version-poll invalidation | JSX, rendering knowledge, derived/sorted/formatted data |
| `src/view-models/` | 2 — view-model | pure functions from entity payloads to render-ready data; the program's rendering laws as code | JSX, hooks, fetches, store access, `Date.now()`/randomness |
| `src/skins/<name>/` | 3 — skin | one component set per skin over the shared prop contracts; that skin's `tokens.css` | store access, entity hooks, router access, business/sort/derivation logic, raw colors |
| `src/skins/contract.ts` | seam | the `Skin` interface and the per-view prop contracts | anything else |
| `src/routes/` | composition | route components wiring hooks + view-models + working-set actions into skin components; the root layout | rendering laws, per-skin markup |
| `src/router.tsx` | composition | the typed route table, one route per shareable entity URL | components (imports them; never the reverse) |
| `src/stores/` | client state | the working-set Zustand store (closed action set) | server-derived entity data, comment drafts |
| `src/comments/` | client state | the comment store, when it lands: the async `CommentStore` interface, its persistence implementation(s), and the TanStack Query wrapper hooks | anything that reads its storage directly from outside; sync variants of the interface |
| `src/components/ui/` | vendored | shadcn primitives, owned source, added per-component | hand-rolled mc logic (edits for tokens/variants are fine — vendored code obeys every rule in this document) |
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

- Components use utilities and shadcn slots only. A raw color in a
  component or its class string — hex, oklch, or a named color utility
  (`text-white`, `bg-black`, any palette-scale utility) — is an
  automatic rejection; meaning lives in the token name. Vendored
  primitives are owned source and obey this the day they land: adding a
  missing token is the fix, keeping the named color is not.
- Semantic names only: a token says what it is FOR, never what it looks
  like. `--c-needs-you`, not `--red`. New tokens are added to every
  skin's sheet in the same diff, or the diff is rejected.
- Two-grain type is law (settled style principle): derived facts render
  `font-fact` (mono), human speech renders `font-speech` (proportional).
  The grain is semantic, the face is the skin's: a skin may value both
  grains with the same face (terminal does), but a component must still
  mark speech as speech — content classified by what it IS, never by
  what the current skin happens to render.

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

## 5. The comment store (D3 — the swap seam)

Comments are a first-class capture entity (pending → submitted →
resolved/outdated, anchored to entity + revision + span). Their MVP
persistence is client-local, conditional on a seam that lets a
server-side store swap in by changing one provider binding and zero
components. The law, binding from the first comment-shaped diff:

- **One interface; operations async, subscription sync.** Every comment
  operation goes through the `CommentStore` interface in
  `src/comments/`. The operation set — `capture`, `update`, `discard`,
  `abandonAll`, `pending`, `markOutdated`, `submitBatch` — is
  `Promise`-returning even while the implementation is synchronous
  localStorage; a sync signature on any of these is a rejection, because
  call sites that learn synchronicity make the server swap a rewrite.
  The one deliberate exception is the subscription:
  `watch(cb): Unsubscribe` is synchronous, and correctly so — a React
  effect must return its cleanup synchronously, and a
  `Promise<Unsubscribe>` creates an unsubscribe race/leak path. Making
  `watch` async is as much a rejection as making `capture` sync.
- **IDs are minted client-side** (ulid) at capture. An implementation
  that waits on any server to hand back an identity is a rejection.
- **Components never touch the interface.** They consume comment state
  through TanStack Query wrapper hooks (`usePendingComments()`,
  `useSubmitBatch()`, …) exactly as they consume entities through
  entity hooks. A component importing the store implementation — or any
  code outside `src/comments/` reading its storage keys directly — is a
  rejection.
- **Drafts live nowhere else.** Not in the working-set store (the
  working set holds no comment data; the tray count subscribes through
  the hooks), not in `useState`, not welded to layout persistence. A
  draft in Zustand is a rejection.
- **Outdated-marking lives behind the interface** (anchor-revision
  comparison on read). UI code that computes outdatedness is a
  rejection — the UI renders the flag.
- **What must never leak through the seam:** storage keys,
  synchronicity, and the single-device assumption. Submission is
  already server-side (a batch publishes as one thread reply through
  the API); only the draft lifecycle is local, which is exactly why the
  swap is a transport change and must stay one.

## 6. The wire contract and invalidation

Provenance IS the invalidation contract. Every source-backed section
carries `provenance: {source, observedAt, version}`; `/api/v1/version`
serves exactly those stamps, and it is scope-aware:

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

**The staleness baseline is the cache itself, never the previous poll.**
Every entity payload carries its own section provenance, and the hook
satisfies this section only by comparing the polled stamps against the
CACHED payload's own tokens. A poll-to-poll comparison chain is a
rejection: its first observation becomes an unfounded baseline, and an
entity response already in flight when the source changed lands stale
behind it and sticks — silently, forever, re-arming after every
transient poll failure (the first-poll race). Judged against the cached
payload's own stamps, the first poll has an honest baseline and the
race cannot be built.

Degraded upstreams arrive as HTTP 200 with warnings in the payload.
The UI renders that honesty: warnings render as warnings, absent data
renders as absence, and no blank error page stands in for a partially
degraded payload. Inventing provenance, or presenting a stale observation
as current, is a rejection — `observedAt` is what the server says it is.

**Degraded is not empty.** An empty claim ("no missions", "no agents")
asserts an observed zero, and only a healthy observation can assert one:
the claim is derived in the view-model, true only when the section was
observed without a warning and returned zero rows. Zero rows behind a
warning is an unobservable source, not a known zero. Skins render the
VM's empty flags; a skin computing emptiness from row counts is a
rejection.

**Load health is two situations, never one.** Fatal-no-data (the fetch
failed and nothing is cached) renders a failure line, not a healthy
blank or an eternal loading state (`view-models/load-failure.ts`).
Cached-but-unverified (a payload is cached while the entity refetch OR
the version poll is failing) keeps presenting the cached data — truth
outranks blankness — but must mark it with a staleness warning carrying
the cached payload's own provenance `observedAt`
(`view-models/staleness.ts`). The version poll's own errors are part of
this contract: the invalidation hook surfaces its poll health, because a
dead invalidation channel means cached data can no longer be verified as
current. Swallowing either channel's errors presents stale data as
current and is a rejection. The render precedence — failure > loading >
empty claim > data (with staleness beside the data) — is VM-boundary law
with specs (`render-contract.test.ts`); the per-skin on-screen proof
lands with the flow suite.

## 7. Routing

The route table in `router.tsx` is code-based, typed, and the registry
of the implementation law: **every core entity has a shareable URL.** A
new entity surface means a new route here — a view reachable only by
client state is a rejection. URLs anchor entities only; layout and pane
arrangements never ride a URL. The SPA mounts at basepath `/ui` (the
transition arrangement: the Go server keeps serving the legacy HTML
while surfaces move over).

The Go server owns `/api` and falls back to the SPA shell for every
missed path EXCEPT the reserved built-asset namespaces (`assets/` and
the finite root-file set — `ui.go`, `assetShaped`), which 404. Route
URLs may legitimately look like file paths — artifact identities ARE
paths — so reserving namespaces, not extensions, is the law; growing
the built-asset surface means growing `assetShaped` and its test in the
same diff.

Route components read params with `useParams({ from: ... })`. Importing
`router.tsx` from any component module is a rejection — it closes an ESM
cycle (router → page → router) and couples pages to the tree that mounts
them.

## 8. Testing patterns

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
- **Flows — Playwright, against the real stack.** The e2e harness runs
  the REAL Go server with fake `mish`/`hcom`/`herder` shell scripts and
  a seeded journal fixture — the same pattern `api_test.go` and
  `tools/mc/testdata/` already use. No frontend mocks, no request
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

## 9. Adding a view — the recipe

Route in `router.tsx` → entity hook(s) it needs (new endpoint = new Go
DTO + raw-key contract test first) → view-model function + its Vitest
spec → one component per skin satisfying a new prop contract in
`contract.ts` → a Playwright flow for its acceptance lines. Five steps in
predictable places; a view that needs a sixth kind of change means the
architecture has a gap — stop and report it before building around it.

## 10. Current state (ledger, not law)

Honest state of this tree, with the exit condition that clears each
entry — checkable from the repo alone:

- **The flow suite exists and is a gate** (`e2e/`, `bun run test:e2e`):
  the §8 harness (real binary, fake upstream scripts, seeded journal)
  drives first mount, deep links, mission-to-mission scope changes,
  live warning-token transitions (roster family, both directions),
  degraded-vs-healthy emptiness, the two-situation load-health law, and
  render precedence on screen — every flow under both skins. The
  skin-swap proof asserts the declared behavioural rendering difference
  from the `Skin` declaration itself, computed-style change across the
  toggle, and identical behaviour laws under both skins.
- **The missions-family warning transition is not driven live** in the
  flow suite: the mission resolver caches observations for a minute by
  design (a poll must not fan out git checks), so the suite covers that
  family's degraded and healthy states as separate cold starts and
  drives the live §6 loop through the per-request roster family
  instead. Clears if the resolver TTL ever becomes injectable from the
  CLI.
- **No comment-shaped code exists.** §5 binds from the first diff that
  adds any.
- **Material slots are stored but undriven** (§4) — cleared by the
  first material-opening surface driving the existing actions. The
  working-set cap is therefore attackable only in Vitest today; the
  flow-level "clicking around cannot exceed the cap" attack lands with
  that surface.
