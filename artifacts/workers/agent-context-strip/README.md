# Agent context strip evidence

Captured 2026-08-28 from the `agent-context-strip` worktree. The branch binary
was built directly from this checkout and served with `AI_CONFIG_ROOT` unset on
the isolated test port 4493. The owner serve on port 4400 was not touched.

```text
executable SHA-256: b7f321d1feec73700bdc6a7171509534798d37659fd50853268acfa5c1f0df53
SSE buildIdentity: executable:b7f321d1feec73700bdc6a7171509534798d37659fd50853268acfa5c1f0df53
```

## Live browser matrix

- `live-claude-light.png` — live `grill-kila` Claude session in the light
  theme. The strip shows cwd, proven `ai-config · main`, listening status,
  pane, Herdr status, tool, model, and Claude raw-token context.
- `live-codex-dark.png` — live `impl-hemo` Codex session in the dark theme.
  The middle-ellipsized cwd retains its full path in `title`; the strip shows
  proven repo/branch and Codex percentage remaining. Browser geometry reported
  `clientWidth == scrollWidth == 974`, so every required fact fits without
  horizontal scrolling at the 1280px capture viewport.
- `retired-agent-dark.png` — retained `dima` transcript. The strip honestly
  shows `retired`, `read-only`, retained cwd/tool/model/context, no live pane,
  and the Screen control is disabled.
- `fact-absent-no-repo-light.png` — retained `nuzi` detail has no `git` block.
  Browser inspection reported zero `.context-repository` elements, proving the
  absent repo is omitted rather than inferred from `/home/ubuntu`.
- `screen-mode-strip-light.png` — live Codex Screen mode. Browser inspection
  reported the read-only screen viewport, context strip, and composer all
  mounted together.

The bottom order is queued dock, reserved 32px context strip, composer. The
header contains only the agent name and view controls. No former header fact
was dropped: pane placement, Herdr status, gap meaning, tool, lifecycle status,
model, and context all moved into the strip; tab-strip status dots are unchanged.

## Verification

- Web: 84 node tests, ESLint, TypeScript typecheck, production build, and
  committed dist drift check passed.
- Go: `go test -count=1 ./...` and `go vet ./...` passed.
- Full repository-root `tools/herder/tests/check-*.sh` battery passed with
  `HERDER_BIN` unset, including 23/23 web-serve checks.
- The implementation adds no server field, endpoint, polling cadence, or
  EventSource. Existing agent-detail invalidations remain the live-update path.

## Screenshot checksums

```text
f7ce5d09c929a1a7d146fcdb63b2d380050d968e6ef447daf663523e773e566c  fact-absent-no-repo-light.png
73734677d708be44ed3effa91e0441349123750d9482ab23243eaab2cde444d4  live-claude-light.png
4bd39c78e0f1b7b8b3e47208039d2d9ab60d5c0f234bce1d80f5316466b78914  live-codex-dark.png
85fd21f1e43b31b4a5e8c81131755a8146dd52dd30df967f566c1a4e0649d3d5  retired-agent-dark.png
d6bdfa0d2269ffd091f504abd25a3f5bd35eaaaf16f98a036e883411958973d8  screen-mode-strip-light.png
```
