# tools/mc/ui — dev recipe

The mc SPA. It lives inside the Go tool on purpose (D6): one checkout,
one build, one binary. The rules of this codebase are in
[ARCHITECTURE.md](./ARCHITECTURE.md) — read that before writing code here.

## Run it

Dev (two terminals):

```sh
# 1 — the Go server: owns /api/v1 and all data integration
cd tools/mc && go run .          # 127.0.0.1:8390

# 2 — the Vite dev server: owns the page, proxies /api to :8390
cd tools/mc/ui && bun install && bun run dev
# open http://localhost:5173/ui/
```

Prod (what actually ships):

```sh
cd tools/mc/ui && bun run build  # tsc -b (strict) + vite build → dist/
git checkout -- dist/.gitkeep    # vite empties dist/; the keep file makes
                                 # the go:embed valid on unbuilt checkouts
cd tools/mc && go build          # dist/ rides the binary via go:embed (ui.go)
./mc                             # serves the SPA at /ui/, API at /api/v1
```

An unbuilt tree (only `.gitkeep` in `dist/`) serves an honest 501 at `/ui/`.

## Scripts and their current standing

| script | what | standing |
| --- | --- | --- |
| `bun run build` | strict typecheck + production build | gate — must be green |
| `bun run check` | Biome lint + format check | gate — must be green |
| `bun run format` | Biome, writing fixes | tool |
| `bun run test` | Vitest over `src/**/*.test.ts` | no specs yet — exits 1 until the view-model/store specs land; deliberately not `--passWithNoTests` |
| `bun run test:e2e` | Playwright | no config/harness yet — lands with the skin-swap proof |

Biome does not cover CSS (its parser predates Tailwind v4's at-rules, so
`**/*.css` is excluded in `biome.json`): the token sheets and
`globals.css` are governed by `bun run build` (Tailwind/Vite reject
malformed CSS) and by review against ARCHITECTURE.md's token rules, not
by `check`.

Go-side gates (the API contract tests live there, from `tools/mc`):

```sh
GOTOOLCHAIN=local mise exec go@1.26.5 -- go vet ./...
GOTOOLCHAIN=local mise exec go@1.26.5 -- go test ./... -count=1
```

## Version pins that are load-bearing

- **Vite 7** — settled (D6). Vite 8 exists; do not upgrade past the decision.
- **@vitejs/plugin-react ^5.2** — the last line supporting Vite 7 (6.x needs Vite 8).
- **TypeScript ^5.9** — TS 7 (tsgo) is untested here.
- **Bun** is package manager and script runner only; Vite is the dev server,
  the Go binary is the production server.

shadcn primitives are added per-component (`bunx shadcn@latest add <name>`)
and become owned source under `src/components/ui/` — subject to every
ARCHITECTURE.md rule from the day they land (generated named colors get
retokenized, not kept). Never the `ai` npm package (see
`artifacts/spike-ai-elements.md` in the mission repo).
