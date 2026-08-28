# Shell polish live evidence

Validated 2026-08-28 against the worktree-built binary on port 4493. The
owner service on port 4400 was not touched.

## Build identity

SSE `hello`:

```text
executable:2fb56959d4a3b1bbc2385b9b9f209ac99624fbf0828cb4abf64773ccb17c9db5
```

## Visual evidence

- `status-agent-dark-chrome.png`: global SSE/layout status and far-right
  shortcut/theme controls in dark mode; composer label/hint/attribution line
  absent; agent strip retains the dot and facts while omitting the bus word
  and `herdr idle`.
- `status-agent-light-chrome.png`: the same shell surfaces in light mode.
- `shortcut-reference-light-chrome.png`: `?` reference, including Alt+W and
  the honest browser Ctrl/Cmd+W warning and preview-restoration copy.
- `drag-pinned-restored-chrome.png`: a real tab drag to the dock edge created
  a split, converted the preview params to `preview:false`, and both groups
  remained after reload.
- `old-board-layout-restored-chrome.png`: a real v2 localStorage layout was
  amended with a legacy `board` panel and active board view; reload restored
  the retained agent only, removed Board from storage, and showed no Board UI.
- `alt-w-firefox.png`: Firefox after Alt+W closed the active preview pane.
- `composer-send-tailnet-chrome.png`: attributed Tailnet viewer after a real
  composer send; the textarea cleared and queued delivery appeared.

Prior baseline screenshots under `artifacts/workers/dock-layout/` and
`artifacts/workers/agent-context-strip/` show the former dock-header shell
chips and the former visible strip/composer noise. The paired current images
above show the owner-ruled removals.

## Browser proofs

Chrome on Linux:

```json
{"browser":"HeadlessChrome/149.0.0.0","before":["ziru","luge"],"after":["ziru"],"altWClosedActive":true}
```

Firefox on Linux:

```json
{"browser":"firefox","version":"153.0","before":["ziru","luge"],"after":["ziru"],"altWClosedActive":true}
```

Unload condition, exercised in the live Chrome page with cancelable
`beforeunload` events:

```text
two pinned panels: defaultPrevented=false
two pinned panels plus one untouched preview: defaultPrevented=true
```

## Persistence probes

Pin on drag and reload:

```json
{"groups":2,"tabs":["ziru","luge"],"stored":[{"kind":"agent","name":"ziru","preview":false},{"kind":"agent","name":"luge","preview":false}]}
```

Legacy Board normalization and reload:

```json
{"tabs":["ziru"],"boardText":false,"storedPanels":["agent:ziru"]}
```

## Verification

- `npm run lint`: pass
- `npm run typecheck`: pass
- `npm test`: 97/97 pass
- `npm run build`: pass
- dist drift: two consecutive builds produced identical SHA-256 hashes
- `go vet ./...`: pass
- `go test -count=1 ./...`: pass
- `env -u HERDER_BIN` repository battery: all 13/13 gates pass across
  `tools/herder/tests/check-*.sh` and `tools/fleet/tests/check-*.sh`
- `bin/ai-doctor`: completes with the same 33 environment/local-skill warnings
  seen before the change
