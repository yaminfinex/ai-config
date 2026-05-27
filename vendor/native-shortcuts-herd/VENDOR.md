# Vendored: native-shortcuts-herd

- **Upstream**: https://github.com/yigitkonur/native-shortcuts-herd
- **Pinned commit**: `4a25c4976b322b2723313a3db36ef44f01197635` ("add vid")
- **Vendored on**: 2026-05-28

## Why this is vendored

We want the ghostty + herdr keymap config without `npx`-ing arbitrary code from npm/GitHub each run. The whole project is small (≈64 KB of TypeScript) so it lives here for direct review and local execution.

## Differences from upstream

- `.git/` removed (the directory now lives inside this repo's history).
- `docs/assets/` removed (12 MB demo mp4 not needed for use).
- **Local patch to `src/profiles.ts`**: the six action bindings (`new_workspace`, `rename_workspace`, `close_workspace`, `new_tab`, `rename_tab`, `close_tab`) now emit `prefix+…` form (e.g. `"prefix+t"` instead of `"t"`). Current herdr (`0.5.10+`) rejects unqualified single-letter / shift-only bindings as "unsafe direct keybinding: would intercept typing" and **disables them**, so without this patch `cmd+t`, `cmd+w`, `cmd+n`, `cmd+k`, `cmd+l` route through ghostty but do nothing in herdr. The cycle bindings (`ctrl+tab` etc.) and `[keys.indexed]` aren't affected — they have a non-typing modifier and herdr accepts them as-is.
- **Local patch to `src/ghostty.ts`**: `discoverGhosttyConfigs` now returns only the *first* existing candidate path instead of all of them. Upstream patches every config it finds (`~/.config/ghostty/config`, `~/Library/Application Support/com.mitchellh.ghostty/config`, the cmux variant…). On macOS where ghostty auto-loads more than one of those, adding the sidecar include to multiple files makes ghostty's loader see the same sidecar pulled in via two paths and fail with `config-file <path>: cycle detected`. Returning only the first existing path (XDG `~/.config/ghostty/config` if present, else the macOS Application Support one) sidesteps that. To still patch multiple, pass `--ghostty-config path1 --ghostty-config path2` explicitly — explicit paths bypass discovery.

Everything else is byte-identical to the pinned commit. Re-sync should diff `src/profiles.ts` carefully or upstream this fix.

## Running locally

```sh
cd vendor/native-shortcuts-herd
npm install
npm test            # optional, sanity check
npm run typecheck   # optional

# inspect what would change (no writes):
npm run dev -- diff --profile chrome-spaces
npm run dev -- install --yes --dry-run --json

# do it for real (with the split additions below baked in):
npm run dev -- install --yes \
  --ghostty-key 'cmd+KeyD=text:\x02v' \
  --ghostty-key 'cmd+d=unbind' \
  --ghostty-key 'cmd+shift+KeyD=text:\x02-' \
  --ghostty-key 'cmd+shift+d=unbind'

# or guided (prompts walk you through profile + glass theme):
npm run dev -- install
```

`npm run dev` invokes `tsx src/cli.ts` so there is no build step required. If you'd rather have a built CLI: `npm run build` produces `dist/cli.js` which is also executable as `./dist/cli.js`.

## Pane splits (added on top of upstream)

The upstream package wires up `cmd+t/n/w/k/l`, `cmd+1..9`, `ctrl+tab`, and `ctrl+option+tab`, but **does not** route ghostty's default split keys (`cmd+d`, `cmd+shift+d`) to herdr. The four `--ghostty-key` overrides shown above add that, following the same `cmd+Key…=text:\x02<letter>` + `cmd+…=unbind` pattern the package uses for `cmd+t`.

| ghostty key | sidecar route | herdr action | herdr default key | resulting pane |
|---|---|---|---|---|
| `cmd+d` | `text:\x02v` (prefix + `v`) | `split_vertical` | `prefix+v` | new pane to the right |
| `cmd+shift+d` | `text:\x02-` (prefix + `-`) | `split_horizontal` | `prefix+minus` | new pane below |

Both herdr action names and their default bindings are confirmed in `src/config/model.rs` of [ogulcancelik/herdr](https://github.com/ogulcancelik/herdr), so **no herdr-side config change is required** — herdr already listens on `prefix+v` and `prefix+minus`.

Why each line is needed:

- `cmd+KeyD=text:\x02v` — physical-key route; ghostty sends the herdr prefix (`ctrl+b` = `\x02`) followed by `v`, which herdr decodes as `prefix+v`.
- `cmd+d=unbind` — clears ghostty's built-in `cmd+d` → new split right so it doesn't fire alongside the route. Same shape as the package's `cmd+t=unbind` line.
- `cmd+shift+KeyD=text:\x02-` / `cmd+shift+d=unbind` — same idea for the horizontal split.

If you'd rather have different shortcuts (e.g. `cmd+\` and `cmd+-` to match iTerm), swap the trigger half — the `text:\x02v` / `text:\x02-` payload stays the same.

### Other pane actions (not wired by default)

For reference, herdr exposes more pane actions in `[keys]` with these defaults:

| herdr action | default | suggested ghostty override |
|---|---|---|
| `close_pane` | `prefix+x` | `cmd+shift+w` → `text:\x02x` (note: `cmd+w` is already taken by `close_tab`) |
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
