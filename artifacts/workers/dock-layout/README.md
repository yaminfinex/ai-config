# TASK-48 dock layout evidence

Validated from the `dock-layout` worktree on 2026-08-28. The live server was launched with:

```text
env -u AI_CONFIG_ROOT bin/herder serve --port 4493 --watch
```

The post-review-fix SSE hello reported build identity `source:b49f4030890dbda0`. The owner binary on port 4400 remained untouched.

## As-built feature set

Dockview React/core 8.2.0 provides reorderable tabs, tab stacks, horizontal and vertical splits, sash resizing, moving tabs between groups, overflow handling, direct close buttons, custom tab headers/actions, an empty-workspace watermark, announcements, and JSON layout restore. Preview slots are scoped by group and panel kind; preview panels are pruned from persistence while pinned panels and file view mode survive rearrangement and refresh. The shell retains one shared EventSource for all open agent and proven screen consumers.

Floating groups, popout windows, auto-hide/edge groups, the drop compass, multi-row tabs, pinned-tab chrome, group colors, and layout history are disabled. Cross-window moves are not supported. Runtime validation also established that Dockview 8.2's `keyboardNavigation` and tab context-menu options require `dockview-enterprise`; they are intentionally absent so the shipped dependency graph remains MIT core only. Host shortcuts and the direct tab close control cover the required keyboard and close flows.

## Browser evidence

| Artifact | Evidence |
| --- | --- |
| [two-live-agents-split-dark.png](two-live-agents-split-dark.png) | `impl-selo` and `ziru` stream simultaneously in two resizable Dockview groups in dark theme. |
| [agent-file-split-light.png](agent-file-split-light.png) | An agent panel and quick-opened `App.tsx` file panel render side by side in light theme. |
| [single-group-restore-light.png](single-group-restore-light.png) | After refresh, one Dockview group still contains pinned Board, `ziru`, and `App.tsx` panels; storage retains a branch root. |
| [narrow-split-strip-fade-dark.png](narrow-split-strip-fade-dark.png) | At a 900px viewport, both 324px agent groups overflow and show the token-derived right-edge fade. |
| [empty-watermark-dark.png](empty-watermark-dark.png) | Closing every panel leaves usable Board, Quick Open, and Reset layout actions. |
| [two-agent-tabs-dark.png](two-agent-tabs-dark.png) | Multiple live agent tabs remain a normal top-aligned stack before opt-in splitting. |

After clearing browser logs and reloading the final build, both the page-error list and console log were empty.

## Persistence and stream proof

- Correction: the initial evidence proved refresh restore only for a two-group split and did not cover the default single-group layout. Review found that pruning collapsed the top branch to a leaf, which Dockview rejected. The serializer now always preserves or wraps a branch root, and restore exceptions are logged before fallback.
- The post-fix single-group capture was produced by pinning `ziru` and `App.tsx` beside Board, waiting for persistence, and refreshing. After refresh the DOM had one group and all three tabs; `herder.web.layout.v2` had `grid.root.type: "branch"` and all three panel records.
- A preview file is absent from `herder.web.layout.v2`; double-clicking pins it and persists the API's canonical absolute `root`, `path`, `viewMode`, and `preview: false`.
- File identity safety does not use a duplicate client field. The canonical absolute served-root path is the root ID, and the server refuses an unknown root with the honest `Root no longer served` state instead of remapping it.
- Malformed/stale v1 and v2 storage, panel/component mismatch, and panel-ID mismatch fall back without blocking shell mount.
- Restored screen panels do not enter the EventSource subscription until pane, workspace, tab, agent, and session identity match the live fleet snapshot.
- With two restored agents, a cleared network log followed by reload recorded exactly one stream request:

```text
[3492651.248] GET http://127.0.0.1:4493/api/events?agents=impl-selo%2Cziru (EventSource) 200
```

## Automated gates

- Web: ESLint, TypeScript, 91 Node tests, production build, and embedded dist drift all pass.
- Production bundle: JS 922.38 kB / 254.74 kB gzip; CSS 168.01 kB / 17.56 kB gzip.
- Go: `env -u GOROOT go vet ./...` and `env -u GOROOT go test -count=1 ./...` pass with zero Go source changes.
- Repository battery: `env -u HERDER_BIN bash -c 'for f in tools/herder/tests/check-*.sh; do bash "$f" || exit; done'` passes, including web-serve `PASS=23 FAIL=0`.
- Shell integration contract: `bash -n tools/herder/tests/check-web-serve.sh` passes.

## Runtime validation

Healthy signals are one `/api/events` connection for the union of open consumers, continued agent transcript updates in every visible split, a branch-shaped persisted grid root, clean browser logs, and an SSE hello whose build identity matches the worktree server. A duplicate EventSource, a logged restore exception, a default Dockview skin color, or loss of pinned panels after refresh is a failure signal; mitigation is to reset `herder.web.layout.v2` and roll back the client bundle while leaving the stateless server unchanged.
