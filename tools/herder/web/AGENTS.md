# Herder web rules

Harvested 2026-08-29 from [bulletproof-react](https://github.com/alan2207/bulletproof-react/blob/master/docs/project-structure.md) and the official [Rules of React](https://react.dev/reference/rules). House rules follow ratified Herder design records.

## Architecture

- Dependencies flow shared -> features -> app. Features use another feature through its public API.
- Keep components declarative. Put non-rendering logic in a pure model sidecar with a matching test.
- App.tsx composes routes, providers, workspace, and shell chrome; panel-kind behavior lives in the workspace registry.

## React and state

- Keep components and Hooks pure, call Hooks at the top level, and use effects only to synchronize external systems.
- TanStack Query owns server state. The single useFleetStream EventSource writes live data into its cache.
- Use React state for UI state. Panel-keyed records use usePanelRecords.
- Keep the server stateless. Persist browser state through last-good/salvage helpers.

## Lifecycles

- Model multi-listener or reconnecting subscriptions as (deps) => dispose.
- Use useDOMEvent and useSizeObserver for simple DOM lifecycles.
- Cancel scheduled frames and dispose subscriptions on cleanup.

## Tests and delivery

- Keep tests render-free under Node: pure model tests plus focused source guards.
- Move tests with code and preserve or strengthen every assertion.
- Each commit passes lint, typecheck, the full test suite, and build.

## Presentation

- Define every UI color through custom properties in both theme blocks and verify AA contrast. Pierre colors use its override variables.
- Render honest pending, empty, refusal, and error states with the correct status/alert role.
- Keep read-only surfaces read-only. Write owner-facing text in plain language.

## Platform laws

- Bind physical shortcuts through shellShortcuts/tinykeys; preserve Mac Option placement semantics.
- Restore Dockview only from a branch-grid root and salvage healthy panels.
- Route localStorage writes through versioned helpers.

## Owner rulings

- Links use solid underlines; agent mentions use quieter dotted underlines.
- Persistent overlay affordances must earn their space. Put rare, heavy, content-covering actions in a shortcut or menu.
