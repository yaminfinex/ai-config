# Herder herdr notes

Upstream's own herdr usage doc lives at https://github.com/ogulcancelik/herdr/blob/master/SKILL.md — read it if you want the full concepts walk-through, recipes, and the "ids are not durable" warning in context. The herder skill assumes you know enough herdr to use the commands below; everything else can be confirmed with `herdr <cmd> -h`.

This file documents the parts the herder uses on top of upstream's base surface — primarily `agent start`, agent metadata reporting (where session ids live), `worktree`, and `integration` — plus a few read-side details the herder relies on.

## Safety preflight (must hold before any write)

```bash
[ "${HERDR_ENV:-}" = "1" ] || { echo "not in a herdr pane — stop"; exit 0; }
```

The two write-side scripts (`herder-spawn`, `herder-cull`) enforce this themselves; do the same in any ad-hoc shell snippets you run from the herder.

## Self-discovery

```bash
herdr pane list      # the focused pane is yours; others are neighbours
herdr workspace list # your workspace is the one with focused=true
```

`$HERDR_PANE_ID` also identifies your own pane directly.

## `pane read` source modes (important when peeking spawned agents)

- `--source visible` — current viewport only
- `--source recent` — recent scrollback as rendered (subject to terminal wrapping)
- `--source recent-unwrapped` — same transcript with soft wraps joined; this is what `wait output --source recent` matches against, so use it when you need to see exactly what the waiter saw

## Run `herdr <cmd> -h` first

Syntax shifts between releases. Always confirm against the live binary if a flag looks off.

## `agent start` — spawn a named pane with a child process

```bash
herdr agent start <name> [--cwd PATH] [--workspace ID] [--tab ID] \
  [--split right|down] [--focus|--no-focus] -- <argv...>
```

- `<name>` becomes the visible agent label (renameable later).
- argv after `--` is the command. Use `env VAR=… <bin>` to inject env into the child — herdr does not have its own `--env`.
- Default focus is `--focus`; pass `--no-focus` to keep the user where they are.

Response shape (JSON, single line):

```json
{"id":"cli:agent:start","result":{
  "agent":{"pane_id":"…","workspace_id":"…","tab_id":"…","terminal_id":"…",
            "agent_status":"unknown","cwd":"…","name":"…","focused":false},
  "argv":[…],"type":"agent_started"}}
```

The herder's `scripts/herder-spawn` wraps this: it mints a HERDER_GUID, prepends `env HERDER_GUID=… HERDER_ROLE=… HERDER_LABEL=… <agent>` to argv, and records the response in the registry.

## Agent control (beyond what upstream documents)

```bash
herdr agent rename <target> <name>|--clear     # change visible label
herdr agent focus  <target>
herdr agent attach <target> [--takeover]
herdr agent send   <target> <text>             # literal text, no Enter (alias of pane send-text)
herdr agent read   <target> [--source visible|recent|recent-unwrapped] [--lines N] [--format text|ansi]
herdr agent wait   <target> --status idle|working|blocked|unknown [--timeout MS]
```

`<target>` accepts: terminal id (`term_…`), unique agent name, detected/reported label, or pane id. Prefer pane id from the spawn response when there's any ambiguity.

## Metadata + session-id reporting (the durable-id story)

This is what integrations call. The herder reads these fields when correlating its GUID against the upstream agent's own session id.

```bash
herdr pane report-agent <pane_id> --source ID --agent LABEL \
  --state idle|working|blocked|unknown \
  [--message TEXT] [--custom-status TEXT] [--seq N] \
  [--agent-session-id ID] [--agent-session-path PATH]

herdr pane report-metadata <pane_id> --source ID [--agent LABEL] \
  [--applies-to-source ID] \
  [--title TEXT|--clear-title] \
  [--display-agent TEXT|--clear-display-agent] \
  [--custom-status TEXT|--clear-custom-status] \
  [--state-label STATUS=TEXT] [--clear-state-labels] \
  [--seq N] [--ttl-ms N]
```

The `--agent-session-id` field is where Claude Code's session uuid (or Codex's, etc.) lands when the matching integration hook is installed. See `herdr integration install <name>` below.

A child that wants to declare its own session id without an integration can call `pane report-agent` from any hook of its own — passing `HERDER_GUID` along too if it likes (custom-status is a free-form field).

## Worktrees

Not covered upstream. Useful when spawning an agent into an isolated git checkout.

```bash
herdr worktree list   [--workspace ID | --cwd PATH] [--json]
herdr worktree create [--workspace ID | --cwd PATH] [--branch NAME] [--base REF] [--path PATH] [--label TEXT] [--focus|--no-focus] [--json]
herdr worktree open   [--workspace ID | --cwd PATH] (--path PATH | --branch NAME) [--label TEXT] [--focus|--no-focus] [--json]
herdr worktree remove --workspace ID [--force] [--json]
```

Run `herdr worktree create … --json | jq` once interactively to confirm the response field names before piping them into automation.

## Integrations

Installs the agent-side hook scripts that push state and `agent-session-id` into herdr.

```bash
herdr integration status [--outdated-only]
herdr integration install claude|codex|opencode|hermes|qodercli|pi|omp
herdr integration uninstall <same names>
```

Without the right integration installed, an agent's `agent_status` may stay `unknown` and no `agent-session-id` will be reported. `herder-spawn`'s wait-for-idle step depends on this, so install the integration for any agent you intend to drive with initial prompts.

## Persistent herdr sessions (not agent sessions)

```bash
herdr session list [--json]
herdr session attach <name>
herdr session stop|delete <name>
```

This is the herdr server-level session (the named tmux-like persistence layer). Do not confuse with `agent_session_id` above.

## Why we mint our own GUID

Upstream's own doc says ids can compact across close events: *"ids can compact when tabs, panes, or workspaces are closed. do not treat them as durable ids."* So `pane_id` and the compact workspace/tab ids are session-live, not history-durable. The HERDER_GUID we inject is the durable handle the registry pivots on; herdr-side ids and the agent-reported session id are correlated to it.

## Known sharp edges

The herdr + agent stack has a few rough corners the herder needs to navigate around. These are observed behaviours, not theoretical risks.

- **`pane_id` compaction.** Pane / workspace / tab ids are session-live; after a herdr restart or pane churn they can be reassigned to different live panes. Any id captured earlier in a session may now point to something else. `herder-cull` re-verifies `terminal_id` before closing; the herder should do the same in any ad-hoc close path.
- **Cull-after-compaction race.** Before id compaction handling was added, `herder-cull --pane <id>` could close the wrong pane if `pane_id` had been reassigned. Still possible if you bypass `herder-cull` and call `herdr pane close` directly with a stale id. Always go through `herder-cull`.
- **Workspace auto-close on last-tab-close emits no server log.** When the final tab in a workspace is closed, herdr implicitly closes the workspace, but no `api.request.start` line is emitted in `~/.config/herdr/herdr-server.log` for the implicit close. Post-mortems on "where did my workspace go" require correlating against the *explicit* tab close.
- **Codex first-run trust prompt absorbs sent text.** On its first start in a new directory codex shows a modal trust prompt. Any `herdr agent send` issued before the prompt is dismissed gets fed *into* the modal (often clipped at the leading chars) instead of into the chat input. Wait for the prompt to clear before relying on `agent send`, or send a deliberate dismissal keystroke first.
- **`herdr pane send-keys` has a small, undocumented key vocabulary.** Most modifier-combo names are rejected (`Ctrl-u`, `^U`, `Escape` with capital E, etc.). `esc`, `Enter`, and a handful of others work. `herdr pane send-keys -h` does not list the accepted set. Do not experiment by sending keys to a *running* peer agent — see "Driving peer agents safely" below.
- **Server PATH ≠ caller PATH.** The herdr server runs with a restricted PATH that lacks mise/asdf shims. `herder-spawn`'s default `--login-shell` wrapper papers over this by sourcing the user's interactive shell init; agents spawned without that wrapper will silently fail with exit 127 on shebang resolution.

## Driving peer agents safely

When the herder needs to *send a message* to an already-spawned peer agent (vs. spawning a new one), the rules are tighter than they look — the failure modes are silent.

1. **Preflight orchestrator state.** Call `herdr agent read` (or use `herder-send`, which does this for you) and check whether the target is in a normal idle / working state vs. an interrupted or modal state. If interrupted / modal, **stop** — don't stack new input on top of state the operator can't see. Either send a benign recovery first or wait for the user.
2. **Don't send `esc` to a working peer agent.** `esc` is the *only* input-mode-shaped key `herdr pane send-keys` accepts, but for both codex and claude it doubles as **interrupt**. Sending it to clear what looks like buffered input will instead kill the agent's in-flight turn. There is no safe clear-buffer key in the current `pane send-keys` vocabulary. If the buffer has stray text, send your real message anyway and let it append, or use `herdr agent send` (which writes literal text into the prompt without submitting) and ignore the surrounding chrome.
3. **Codex's empty-prompt placeholder mimics buffered input.** Lines like `› Run /review on my current changes` (the last completed prompt or a hint string) are codex's **placeholder hint**, not real text. Reading the pane and seeing text after `›` is *not* evidence the user has typed something. If you must confirm, send a non-destructive keystroke (or `agent read --source recent-unwrapped`) and look for behaviour change.
4. **`pane send-keys Enter` is not a delivery receipt.** After sending Enter, do one more `agent read` and confirm the `›` line has returned to empty / placeholder. If your message text is still visible *after* the `›`, the buffer didn't submit. Codex specifically absorbs the first Enter when transitioning out of `Conversation interrupted` — a second Enter is required. The general rule: read once post-send, look at the `›` line, then claim delivery.

`scripts/herder-send` encodes these checks. Prefer it over hand-rolling `agent send` + `pane send-keys Enter` when sending mid-session messages to a running agent.
