# Herder herdr notes

> HISTORICAL (2026-07-05): this is a pre-Go-port working note retained for
> low-level herdr observations and sharp-edge provenance. `delivery-drivers.md`,
> current `bin/herder --help`, `herder <cmd> --help`, and the Go source
> are the shipped source of truth. Re-verify command syntax with `herdr <cmd> -h`
> before relying on examples here.

Upstream's own herdr usage doc lives at https://github.com/ogulcancelik/herdr/blob/master/SKILL.md — read it if you want the full concepts walk-through, recipes, and the "ids are not durable" warning in context. The herder skill assumes you know enough herdr to use the commands below; everything else can be confirmed with `herdr <cmd> -h`.

This file documents the parts the herder uses on top of upstream's base surface — primarily `agent start`, agent metadata reporting (where session ids live), `worktree`, and `integration` — plus a few read-side details the herder relies on.

## Safety preflight (must hold before any write)

```bash
[ "${HERDR_ENV:-}" = "1" ] || { echo "not in a herdr pane — stop"; exit 0; }
```

The two write-side scripts (`herder spawn`, `herder cull`) enforce this themselves; do the same in any ad-hoc shell snippets you run from the herder.

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

The herder's `scripts/herder spawn` wraps this: it mints a HERDER_GUID and, by default, launches the agent inside a login+interactive shell — `$SHELL -lic 'export HERDER_GUID=… HERDER_ROLE=… HERDER_LABEL=…; exec <agent> [extra-args]'` — so PATH, mise activation, and auth env are sourced the way an interactive pane is (this is why spawned agents get `mise`-managed tools like `agent-browser`, and why shebangs like `#!/usr/bin/env node` resolve). Opt out with `--no-login-shell` for a raw `env … <agent>` exec (e.g. when spawning `bash` itself). It records the response in the registry.

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

Without the right integration installed, an agent's `agent_status` may stay `unknown` and no `agent-session-id` will be reported. `herder spawn`'s wait-for-idle step depends on this, so install the integration for any agent you intend to drive with initial prompts.

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

- **`pane_id` compaction.** Pane / workspace / tab ids are session-live; after a herdr restart or pane churn they can be reassigned to different live panes. Any id captured earlier in a session may now point to something else. This is why the registry pivots on the durable `terminal_id`, not the stored `pane_id`: `herder send` and `herder wait` resolve a guid/label to the agent's *current* pane by `terminal_id` (and refuse if the terminal isn't live, rather than mis-send to a recycled pane), `herder list` reports live status/pane by `terminal_id`, and `herder cull` re-verifies `terminal_id` before closing. Do the same in any ad-hoc send/close path — never trust a captured `pane_id`.
- **Cull-after-compaction race.** Before id compaction handling was added, `herder cull --pane <id>` could close the wrong pane if `pane_id` had been reassigned. Still possible if you bypass `herder cull` and call `herdr pane close` directly with a stale id. Always go through `herder cull`.
- **Workspace auto-close on last-tab-close emits no server log.** When the final tab in a workspace is closed, herdr implicitly closes the workspace, but no `api.request.start` line is emitted in `~/.config/herdr/herdr-server.log` for the implicit close. Post-mortems on "where did my workspace go" require correlating against the *explicit* tab close.
- **First-run directory-trust modals (both agents) absorb sent text.** On first start in an untrusted directory (every fresh worktree, and any non-repo dir), claude shows "Is this a project you created or one you trust?" and codex shows "Do you trust the contents of this directory?" (older codex: "...files in this folder"). Three traps: (a) the modal sits at `status=idle`, so a status gate thinks the agent is ready; (b) its selector arrow (`❯ 1.` / `› 1.`) spoofs the input sigil, so a sigil match also reports ready; (c) the tool-permission flags (`--dangerously-skip-permissions` / `--dangerously-bypass-approvals-and-sandbox`) do **not** dismiss it. A prompt sent now is pasted into the modal and its characters (or the trailing Enter) silently confirm trust. `herder spawn` now detects these modals by text and, in autonomous mode, accepts them deliberately (`trust-accepted` in the summary); under `--safe` it refuses and surfaces them. `herder send` refuses to send while one is open.
- **Codex clips the leading characters of a paste during MCP startup.** Even after codex reports `status=idle` it keeps printing boot chrome (MCP warnings, tips); pasting in that window drops the first few characters of the message (e.g. "Reply " → "under…"). `herder spawn`'s readiness gate now waits for the pane output to stop changing between reads (boot quiesced) before sending, and `herder send` fingerprints the message by its **tail** (never clipped) rather than its head.
- **`exec <agent>` bypasses the user's shell alias.** Spawning via `$SHELL -lic 'exec claude …'` does not expand a `claude='claude --dangerously-skip-permissions'` alias, so without explicit flags spawned agents come up in ask-mode even though the user's own panes are autonomous. `herder spawn` injects the per-agent permission flag itself (see SKILL.md → Spawning); `--safe` opts out.
- **Pane-id compaction can hide a live agent from cull.** Closing a sibling pane renumbers others, so a registry row's `pane_id` may point at nothing (or someone else) while the agent is alive at a new id. `herder cull` now re-resolves the target by its durable `terminal_id` across the whole live agent list and retargets the close, only declaring `already_gone` when the terminal isn't live anywhere.
- **`herdr pane send-keys` has a small, undocumented key vocabulary.** The confirmed accepted set is `Enter`, `esc`, and `C-c`; `BSpace`, `C-u`, `Ctrl-u`, `^U`, and `Escape` (capital E) are all rejected as `invalid_key`. Crucially, **none of the accepted keys clears a polluted composer** — `esc` and `C-c` interrupt the agent rather than erasing the line, and there is no backspace/kill-line key. So if a composer ends up with doubled or stray text, you cannot key it clean; just submit (the receiving agent usually tolerates a doubled idempotent instruction). `herdr pane send-keys -h` does not list the accepted set. Do not experiment by sending keys to a *running* peer agent — see "Driving peer agents safely" below.
- **Codex collapses a large paste into a `[Pasted Content N chars]` blob.** Any paste over ~1k chars is hidden in codex's composer behind a `[Pasted Content N chars]` placeholder (only the overflow tail shows inline), so a tail-fingerprint check can't see it. Naive verification reads this as "not landed" and re-pastes, **doubling** the input. The blob also takes **two Enters**: the first expands it, the second submits — and codex flips to `working` *while merely expanding*, so a status flip is **not** proof of submission. `herder send` now (verified live, codex v0.137.0): treats a *fresh* blob (one not already in scrollback) as positive landing evidence so it never re-pastes; drives the extra Enter; and confirms submission by the blob marker **leaving the composer** (on submit it expands into the transcript and the marker count drops back to baseline) rather than by the sigil line (a wrapped blob spills onto continuation lines with no `›` prefix, so a tail-on-the-input-line check false-negatives) or by status. A multi-line brief can additionally trip codex's "Create a plan?" overlay, after which codex parses only the tail. The durable fix is to not send long text to codex at all — stage it in a file and send a short single-line pointer (see "Driving peer agents safely" (5) below).
- **A busy target queues your message — `verify=queued` is success, not a retry signal.** If the recipient is mid-turn when you send (the common case for ringing a working orchestrator), it cannot process the message now: claude/codex *queues* it to run after the current turn, and the queued text renders on the input/sigil line. That state is **indistinguishable** from an unsubmitted buffer via the sigil heuristic, so naive verification reports `not_delivered` and the extra-Enter recovery fires — and on a busy agent **each extra Enter stacks another duplicate queued message** (exactly the doubled-doorbell symptom seen live). `herder send` now snapshots `agent_status` before sending and, when the target was already `working`, reports `verify=queued` (exit 0) and **suppresses** the extra-Enter recovery. Callers (notably the notify-back doorbell) must ring **once and stop** whatever the result — `queued` and even `not_delivered` are expected for a busy peer; the run-log is the record. Never loop-resend a doorbell on a non-`delivered` result.
- **Server PATH ≠ caller PATH.** The herdr server runs with a restricted PATH that lacks mise/asdf shims. `herder spawn`'s default `--login-shell` wrapper papers over this by sourcing the user's interactive shell init; agents spawned without that wrapper will silently fail with exit 127 on shebang resolution.

## Driving peer agents safely

When the herder needs to *send a message* to an already-spawned peer agent (vs. spawning a new one), the rules are tighter than they look — the failure modes are silent.

1. **Preflight orchestrator state.** Call `herdr agent read` (or use `herder send`, which does this for you) and check whether the target is in a normal idle / working state vs. an interrupted or modal state. If interrupted / modal, **stop** — don't stack new input on top of state the operator can't see. Either send a benign recovery first or wait for the user.
2. **Don't send `esc` to a working peer agent.** `esc` is the *only* input-mode-shaped key `herdr pane send-keys` accepts, but for both codex and claude it doubles as **interrupt**. Sending it to clear what looks like buffered input will instead kill the agent's in-flight turn. There is no safe clear-buffer key in the current `pane send-keys` vocabulary. If the buffer has stray text, send your real message anyway and let it append, or use `herdr agent send` (which writes literal text into the prompt without submitting) and ignore the surrounding chrome.
3. **Codex's empty-prompt placeholder mimics buffered input.** Lines like `› Run /review on my current changes` (the last completed prompt or a hint string) are codex's **placeholder hint**, not real text. Reading the pane and seeing text after `›` is *not* evidence the user has typed something. If you must confirm, send a non-destructive keystroke (or `agent read --source recent-unwrapped`) and look for behaviour change.
4. **`pane send-keys Enter` is not a delivery receipt.** After sending Enter, do one more `agent read` and confirm the `›` line has returned to empty / placeholder. If your message text is still visible *after* the `›`, the buffer didn't submit. Codex specifically absorbs the first Enter when transitioning out of `Conversation interrupted` — a second Enter is required. The general rule: read once post-send, look at the `›` line, then claim delivery. **Exception — a BUSY target:** if the recipient was already `working` when you sent, a successful send is *queued*, and the queued text sits on the `›` line looking exactly like an unsubmitted buffer — so the rule above false-negatives. Do not send more Enters (each one stacks a duplicate in the queue); gate on `agent_status==working` *before* the send and treat a landed message as `queued` (success). `herder send` does this for you.
5. **Long / multi-line briefs to codex: stage a file, send a one-line pointer.** Codex's composer collapses pastes over ~1k chars into a `[Pasted Content N chars]` blob and trips a "Create a plan?" overlay on multi-line input — either way it parses only the tail and acts on the wrong thing (e.g. reads a build brief as a "report" request). Don't push the brief over the wire. Write the full brief to a file (e.g. `napkins/<task>-brief.md`, gitignored, or `/tmp/<task>-brief.md` outside a repo) and send a **short single-line** message: `herder send <target> "Read napkins/<task>-brief.md in full, then plan before writing code."`. Single-line sends submit cleanly — no overlay, no blob, no doubling. If a composer *does* end up polluted (doubled paste from a prior attempt), remember there is no key that clears it (see the `send-keys` vocabulary note in "Known sharp edges") — just submit: codex tolerates a doubled idempotent instruction, or expands the blob on the first Enter and submits on the second.

`scripts/herder send` encodes these checks (including the `[Pasted Content]` blob handling for (5)). Prefer it over hand-rolling `agent send` + `pane send-keys Enter` when sending mid-session messages to a running agent.
