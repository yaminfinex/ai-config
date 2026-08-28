# Markdown file viewer live evidence

Captured on 2026-08-28 from the `md-viewer` worktree binary served only on
`127.0.0.1:4493` with configured roots `/home/ubuntu/Coding/missions` and
`/home/ubuntu/Coding/zobrist-plans`. The owner server on port 4400 was not
touched. The worker server and browser session were stopped after capture.

SSE hello build identity:

```text
executable:098ce4c4e3dd8c98613352b4f11fd8029d00273f2433ee55342f95a85ea2ad2a
```

## Observations

- `01-rendered-light.png` and `02-rendered-dark.png`: the same real file,
  `missions/fleet-refit/artifacts/conductor/files-resolution-design.md`, opens
  Rendered by default in explicit Light and Dark themes.
- `03-line-source-highlight.png`: reopening that same file as
  `files-resolution-design.md:30` lands in Source, scrolls to line 30, and
  highlights it. Browser assertions observed `mode=Source`, `target=30`, and
  `scrollTop=250`.
- Toggle round trip on the line-targeted tab observed Rendered with no source
  target element, then Source with target line 30 restored and `scrollTop=250`.
- `04-truncated-rendered.png`: the real 542,357-byte
  `projects/infinex/infinex-red-team/findings-register.md` shows the existing
  “first 256 KiB” truncation banner and formatted content together. The client
  rendered the server-provided prefix as-is.
- `05-relative-image-stub.png`: the relative image in
  `projects/infinex-pro/infinex-pro/pro-portfolio-port.md` is a visible stub
  containing `Portfolio page mockup` and
  `mockups/screenshots/portfolio-page.png`; no relative image `src` exists.
- `06-relative-link-text.png`: relative links in
  `projects/web-app/optimise-2fa/02-seam-discovery/approved-seam-map.md` render
  as visible label plus target text. Browser assertions found zero relative
  anchors in the rendered file.
- `07-non-markdown-source.png`: the real `stub_plane.py` remains the numbered
  source viewer (405 lines) with no markdown toggle.
- `08-transcript-renderer-unchanged.png`: the real `impl-bemo` transcript page
  remains visually unchanged after the mechanical shared-renderer extraction;
  eight assistant markdown blocks and six inline/fenced code nodes were present.

All screenshots are full browser viewport captures from the worktree build.
