# Folder panel verification

Captured on 2026-08-28 from the `folder-panel` worktree server on port 4493.

## Runtime receipt

- Build identity: `executable:ae6224e51617eb7a160b80fcc4d809635acc6ee3a503fae9577f4eedc2d3fc32`
- Final browser smoke: Herder shell loaded one restored dock panel with no rendered error banners, browser errors, or console messages.
- The existing owner server on port 4400 remained running as PID 1661051 throughout validation.

## Evidence

- `fleet-refit-folder-mission-light.png` and `fleet-refit-folder-mission-dark.png`: real mission folder, embedded rendered `mission.md`, and conservative fact strip in both themes.
- `fleet-refit-board-54-dark.png`: real fleet-refit backlog with 54 parsed task cards.
- `ai-config-board-317-dark.png`: real ai-config backlog with 317 parsed task cards.
- `mixed-file-folder-chooser-dark.png`: mixed file/directory results remain a chooser.
- `unavailable-plain-folder-dark.png`: a directory without a backlog contract stays a plain tree and viewer without an error banner.
- `unparsed-quarantine-dark.png`: malformed task input appears in the explicit unparsed quarantine.
- `agent-cwd-folder-dark.png`: the agent context-strip cwd affordance opens a served folder.
- `agent-folder-split-dark.png`: an agent and folder panel coexist in separate dock groups.

Interactive checks also covered a lone exact directory auto-opening as a folder, tree single-click embedded preview, explicit same-group file-tab opening, per-node tree expansion, local Files/Board and tree-visibility toggles, refresh, and persisted pinned folder layout.

## Automated validation

- Web lint, 98 Node tests, TypeScript typecheck, and production build: pass.
- `go vet ./...` and `go test -count=1 ./...` in `tools/herder`: pass.
- Full `tools/herder/tests/check-*.sh` battery: pass, including web-serve `PASS=23 FAIL=0`.
- `bin/ai-doctor`: completed with the same 33 pre-existing local configuration warnings seen before implementation.

## Review receipt

Code review: skipped (ce-code-review unavailable). The checkout forbids reviewer-agent dispatch, which the Compound review requires. A read-only main-thread diff scan covered the changed files for security, performance, bug risk, quality, and test coverage. It found and corrected directory dispatch in the vanished-file recovery chooser; the focused gates above passed afterward. No actionable findings remain.
