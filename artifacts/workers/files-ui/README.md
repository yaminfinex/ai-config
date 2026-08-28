# Files UI live evidence

Evidence was captured from `/tmp/files-ui-herder` on port 4493 with the mission,
zobrist plans, and an isolated temporary fixture directory mounted as roots.

- Executable SHA-256: `123102b3b9fe2586e8d5bf5c054b58da5c593874b3903cf9bd619b493fba5360`
- SSE `buildIdentity`: `executable:123102b3b9fe2586e8d5bf5c054b58da5c593874b3903cf9bd619b493fba5360`
- Chrome: `HeadlessChrome/149.0.0.0`
- Firefox: `Firefox/151.0`

## Captures

- `chrome-ctrl-k-quick-open.png`: Ctrl+K opened and focused Quick Open in Chrome.
- `firefox-ctrl-k-quick-open.png`: Ctrl+K opened and focused Quick Open in Firefox.
- `chrome-ranked-ambiguity-light.png`: ambiguous `README.md` query, ranked and root-tagged; 100 of 1523 results rendered.
- `chrome-unique-auto-open-line-20.png`: one exact candidate auto-opened at line 20.
- `chrome-transcript-double-click-ranked.png`: delegated transcript double-click selected the complete path token and showed ranked candidates.
- `chrome-silent-prose-selection.png`: ordinary prose selection remained silent.
- `chrome-file-viewer-dark-line-20.png`: dark-theme text viewer with line 20 highlighted.
- `chrome-binary-real-file.png`: binary response shown without attempting text rendering.
- `chrome-truncated-real-file.png`: tracked 3,943,659-byte text file with an explicit truncation notice.
- `chrome-vanished-honest-miss.png`: a file removed after opening reports `File vanished`, re-resolves alternatives, and shows neither cached content nor stale metadata.
- `chrome-quick-open-dark.png`: global Quick Open in dark theme.

Chrome probe:

```json
{"dialog":true,"active":"Find a file","location":"http://127.0.0.1:4493/agents/impl-dima"}
```

Firefox probe:

```json
{"dialog":true,"active":"Find a file","location":"http://127.0.0.1:4493/"}
```

Firefox was run with a Playwright-downloaded browser and GTK libraries unpacked
under `/tmp`; no system packages were installed.

File tabs are intentionally session-only. The server's opaque live root IDs are
not safe persistence keys, and this matches the existing screen-tab precedent.
