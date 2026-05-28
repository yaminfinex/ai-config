# Vendored: native-shortcuts-herd

- **Upstream**: https://github.com/yigitkonur/native-shortcuts-herd
- **Pinned commit**: `4a25c4976b322b2723313a3db36ef44f01197635` ("add vid")
- **Vendored on**: 2026-05-28

## Why this is vendored

We want the ghostty + herdr keymap config without `npx`-ing arbitrary code from npm/GitHub each run. The whole project is small (≈64 KB of TypeScript) so it lives here for direct review and local execution.

## Differences from upstream

These are baked into source so a plain `npm run dev -- install --yes` produces the right sidecar — no per-run `--ghostty-key` overrides needed.

- `.git/` removed (the directory now lives inside this repo's history).
- `docs/assets/` removed (12 MB demo mp4 not needed for use).
- **`src/profiles.ts` — `prefix+…` qualification of action bindings**: the six action bindings (`new_workspace`, `rename_workspace`, `close_workspace`, `new_tab`, `rename_tab`, `close_tab`) emit `prefix+…` form (e.g. `"prefix+t"` instead of `"t"`). Current herdr (`0.5.10+`) rejects unqualified single-letter / shift-only bindings as "unsafe direct keybinding: would intercept typing" and **disables them**, so without this patch `cmd+t`, `cmd+w`, `cmd+n`, `cmd+k`, `cmd+l` route through ghostty but do nothing in herdr. The cycle bindings (`ctrl+tab` etc.) and `[keys.indexed]` aren't affected — they have a non-typing modifier and herdr accepts them as-is.
- **`src/profiles.ts` — `cmd+w` → `close_pane`** (was `close_tab` upstream): upstream routes `cmd+w` to `prefix+shift+w` which fires herdr's `close_tab`. With a single-tab workspace this auto-closes the whole workspace and every pane in it — a destructive misfire of "close the thing in front of me" muscle memory from browsers/editors. Patch reroutes `cmd+KeyW` to `\x02x` (`prefix+x` → herdr's `close_pane` default) so the keystroke only kills the focused pane. See `napkins/captures.md` 2026-05-28 for the incident that motivated this.
- **`src/profiles.ts` — `cmd+d` / `cmd+shift+d` pane splits**: upstream does **not** route ghostty's default split keys to herdr. Patch adds four entries in `addPrefixActionBindings` so `cmd+d` → `prefix+v` (`split_vertical`) and `cmd+shift+d` → `prefix+minus` (`split_horizontal`), with matching `cmd+d=unbind` / `cmd+shift+d=unbind` lines to clear ghostty's own implementation. herdr already listens on `prefix+v` / `prefix+minus` by default, so no herdr-side config change.
- **`src/ghostty.ts` — first-config-wins discovery**: `discoverGhosttyConfigs` returns only the *first* existing candidate path instead of all of them. Upstream patches every config it finds (`~/.config/ghostty/config`, `~/Library/Application Support/com.mitchellh.ghostty/config`, the cmux variant…). On macOS where ghostty auto-loads more than one of those, adding the sidecar include to multiple files makes ghostty's loader see the same sidecar pulled in via two paths and fail with `config-file <path>: cycle detected`. Returning only the first existing path (XDG `~/.config/ghostty/config` if present, else the macOS Application Support one) sidesteps that. To still patch multiple, pass `--ghostty-config path1 --ghostty-config path2` explicitly — explicit paths bypass discovery.

Everything else is byte-identical to the pinned commit. Re-sync should diff `src/profiles.ts` and `src/ghostty.ts` carefully or upstream these fixes.

## Running locally

```sh
cd vendor/native-shortcuts-herd
npm install
npm test            # optional, sanity check
npm run typecheck   # optional

# inspect what would change (no writes):
npm run dev -- diff --profile chrome-spaces
npm run dev -- install --yes --dry-run --json

# do it for real — local defaults (close_pane on cmd+w, splits on cmd+d / cmd+shift+d)
# are baked into source, so no flags are needed:
npm run dev -- install --yes

# or guided (prompts walk you through profile + glass theme):
npm run dev -- install
```

`npm run dev` invokes `tsx src/cli.ts` so there is no build step required. If you'd rather have a built CLI: `npm run build` produces `dist/cli.js` which is also executable as `./dist/cli.js`.

## What `install --yes` produces

The full set of ghostty key routes baked into source after the local patches:

| ghostty key | sidecar route | herdr action | resulting effect |
|---|---|---|---|
| `cmd+t` | `\x02t` | `new_tab` | new tab in current workspace |
| `cmd+n` | `\x02n` | `new_workspace` | new workspace |
| `cmd+w` | `\x02x` | **`close_pane`** (was `close_tab` upstream) | close focused pane only |
| `cmd+k` | `\x02N` (prefix+shift+n) | `rename_workspace` | rename current workspace |
| `cmd+l` | `\x02T` (prefix+shift+t) | `rename_tab` | rename current tab |
| `cmd+d` | `\x02v` (prefix+v) | `split_vertical` | new pane to the right |
| `cmd+shift+d` | `\x02-` (prefix+minus) | `split_horizontal` | new pane below |
| `alt+t` | `\x02t` | `new_tab` | (alt route for the same action) |
| `cmd+1..9` | escape seq | indexed workspace switch | switch to workspace N |
| `ctrl+tab` / `ctrl+shift+tab` | escape seq | `next_workspace` / `previous_workspace` | cycle workspaces |
| `ctrl+alt+tab` / `ctrl+alt+shift+tab` | escape seq | `next_tab` / `previous_tab` | cycle tabs |

All herdr action names and their default bindings live in `src/config/model.rs` of [ogulcancelik/herdr](https://github.com/ogulcancelik/herdr). No herdr-side config change is required — herdr already listens on the corresponding `prefix+…` chords.

If you want a different trigger key (e.g. `cmd+\` and `cmd+-` to match iTeam splits), swap the trigger half in `src/profiles.ts`; the `text:\x02…` payload stays the same.

### Other pane actions (not wired by default)

For reference, herdr exposes more pane actions in `[keys]` with these defaults:

| herdr action | default | suggested ghostty override |
|---|---|---|
| `close_tab` | `prefix+shift+w` | `cmd+shift+w` → `text:\x02W` (we moved `cmd+w` to `close_pane`, so this is the bigger-hammer slot) |
| `focus_pane_left` | `prefix+h` | `cmd+alt+left` → `text:\x02h` |
| `focus_pane_down` | `prefix+j` | `cmd+alt+down` → `text:\x02j` |
| `focus_pane_up` | `prefix+k` | `cmd+alt+up` → `text:\x02k` |
| `focus_pane_right` | `prefix+l` | `cmd+alt+right` → `text:\x02l` |
| `cycle_pane_next` | `prefix+tab` | — (collides with `ctrl+tab` mapping; leave to herdr's prefix mode) |
| `zoom` | `prefix+z` | `cmd+shift+enter` → `text:\x02z` |

The exact ghostty key name for the arrow keys may need verification (`ArrowLeft` vs `left` vs `cursor_left`) — check ghostty's docs before adding focus routes.

## Re-syncing with upstream

```sh
# from a scratch checkout:
git clone https://github.com/yigitkonur/native-shortcuts-herd /tmp/nsh-upstream
cd /Users/yamen/Coding/ai-config
rsync -a --delete \
  --exclude='node_modules' --exclude='dist' --exclude='coverage' \
  --exclude='.git' --exclude='docs/assets' \
  /tmp/nsh-upstream/ vendor/native-shortcuts-herd/

# then update the pinned commit in this file:
git -C /tmp/nsh-upstream rev-parse HEAD
```

Review the diff before committing — that's the whole point of vendoring.

## License

Upstream is MIT. See `LICENSE` in this directory.
