# grok CLI characterization for herder support (TASK-106)

Date: 2026-07-09 · Author: task106-zaru · Unit type: investigate (report only — no herder/hcom code changed)

Subject: xAI **grok CLI 0.2.93** (`grok 0.2.93 (f00f96316d)`, installed at `~/.local/bin/grok`), tested against the machine's already-configured `XAI_API_KEY`. All experiments ran in a scratch directory (`$SCRATCHPAD/grokchar/proj`) on a private tmux server (`tmux -L grokchar`) with an **isolated hcom bus** (`HCOM_DIR=$SCRATCHPAD/grokchar/.hcom`). The live registry/bus and all production code were untouched (see "Safety-rail compliance" at the end).

Companion doc: `docs/design/2026-07-09-new-harness-onboarding-playbook.md` (the generalized checklist, with grok as worked example).

---

## Executive summary

- **Owner's hypothesis ("may Just Work"): NO — half works.** A plain grok session **does auto-join the hcom bus** through grok's always-on Claude-hooks compatibility (registers, gets a name, session id captured, full status tracking). But it **cannot receive a delivered message**: grok discards all passive-hook stdout, so hcom's context-injection delivery (and its bootstrap) never reaches the model. The specific missing capability is a **hook-output context-injection seam** (Claude's `additionalContext` / stop-block semantics).
- Everything else herder needs exists: pre-assignable session id (`--session-id`), on-disk turn events, working resume/fork flags, pane-paste prompt injection (composer sigil `❯`, same as claude), `--always-approve` danger mode, `GROK_HOME` config-dir pin, `GROK_AGENT=1` self-detect env.
- Viable integration shape: **codex-style** (bootstrap via launch args — grok's `--rules` appends to the system prompt) plus **pty-paste delivery** instead of hook injection. That is an hcom-side design change, not just switch arms.

---

## 1. Launch mechanics

Evidence run (headless sanity, scratch dir):

```
$ grok -p 'Reply with exactly: GROK-HEADLESS-OK'
GROK-HEADLESS-OK
```

- **Interactive**: `grok` starts an alt-screen TUI in the cwd (`--cwd <dir>` to point elsewhere; `--no-alt-screen`, `--minimal` for scrollback-native rendering). Launched fine inside a tmux pane; footer shows `grok-4.5 · always-approve`.
- **Headless**: `grok -p/--single '<prompt>'` (single turn, prints to stdout), `--output-format plain|json|streaming-json`, `--json-schema`, `grok agent` subcommand for non-interactive operation. **Hooks fire in headless mode too** (verified: a `-p` run dispatched `session_start`/`user_prompt_submit`/`stop` and self-registered on the isolated bus as `niru`).
- **Permissions**: `--permission-mode default|acceptEdits|auto|dontAsk|bypassPermissions|plan`, `--always-approve` (auto-approve all tools), `--allow/--deny` rules (help text explicitly maps them: "Permission allow rule (Claude Code: --allowedTools)"). Global default can come from `~/.grok/config.toml` (`[ui] permission_mode`).
- **Session identity at spawn**: `--session-id <uuid>` names a NEW session (must be a valid, unused UUID). **Verified**: launched with a generated UUID; hcom's registration recorded exactly that id (`session_id: e7245d83-cbdd-419e-93f9-ec5f892bd77d` == the `--session-id` value).
- **Config/auth**: `~/.grok/config.toml` + `XAI_API_KEY` (already configured here — no account blocker). `GROK_HOME` env var relocates the config dir (documented in user-guide 05/14/17). `--leader-socket` for the leader process; `GROK_SANDBOX` for sandbox profiles.
- **Trust gate**: project-local config (hooks/MCP/LSP) is gated by folder trust (`~/.grok/trusted_folders.toml`, `/hooks-trust` in the TUI). See §2 for the git-root surprise.

## 2. Claude-hooks compatibility mode

**How it's enabled: on by default.** Grok scans `~/.claude/settings.json` (+ `settings.local.json`) as an always-trusted global hook source, and `<project>/.claude/settings.json` as a trust-gated project source (disable via `[compat.claude] hooks = false`). Consequence on this machine: **every grok session loads hcom's Claude hooks and calls `hcom sessionstart` / `poll` / etc. — grok sessions self-join whatever bus `HCOM_DIR` points at**, with no grok-specific code anywhere.

Two loading gotchas (both empirical):

1. **Project hooks require a git worktree root.** In a non-git scratch dir, `/hooks-trust` refused: `Not in a git repository. Project hooks require a git worktree root.` After `git init` + `/hooks-trust` (`Trusted: …/grokchar/proj/.`), project `.claude/settings.json` hooks loaded and fired.
2. `GROK_FOLDER_TRUST=0` does **not** ungate project hooks (they simply never loaded; `hooks.dispatch … hook_count=1` = global only). Trust must be granted (`/hooks-trust` or `--trust`).

### Event census (empirical)

Method: a project `.claude/settings.json` registering a logger hook (appends `$GROK_HOOK_EVENT` + raw stdin to a TSV) for 10 Claude event names, plus grok's debug log (`RUST_LOG=debug GROK_LOG_FILE=…`), across interactive turns, a subagent run, `/quit`, and a headless run.

| Claude hook event | Fires in grok? | Evidence |
|---|---|---|
| SessionStart | **YES** (lazily at first prompt / hooks reload, not at TUI startup; also headless) | hooklog `07:01:28 session_start`; debug `hook command completed hook_name=global/settings:session_start[0] … stdout_bytes=4556` |
| UserPromptSubmit | **YES** | hooklog ×3; TUI annotation `◆ user_prompt_submit [hooks: 3]` |
| PreToolUse | **YES**, matcher + Claude tool-name aliases work (`Bash\|Task\|Write\|Edit` matched `run_terminal_command`) | hooklog ×3 with `"toolName":"run_terminal_command","toolInput":{"command":"echo census-turn",…}` |
| PostToolUse | **YES** | hooklog ×3 |
| Stop | **YES**, at every turn end | hooklog ×4; TUI `◆ stop [hooks: 2] / Turn completed in 5.3s.` |
| Notification | **YES** | hooklog + debug log ×4+ |
| SubagentStart / SubagentStop | **YES** | debug `hook command completed hook_name=global/settings:subagent_start[0].hooks[0] exit_code=0` |
| SessionEnd | **PARTIAL** — fired when a *subagent's* session ended; **not observed on TUI `/quit`** of a primary session | hooklog `07:02:59 session_end` (subagent sid); no `session_end` dispatch in debug log after `/quit` of session 1 |
| PermissionRequest (used by hcom) | **NO** — not a grok event; silently skipped (grok has `PermissionDenied` instead) | never dispatched in either debug log |
| PreCompact | not exercised (no compaction occurred); documented by grok, with `PostCompact` added | grok user-guide 10-hooks.md |

**Subagent hazard**: a subagent runs as its own session (own `sessionId`, uuid-v7) and fires the *standard* hook set — its `session_end` caused hcom to mark the whole registered instance stopped (`exit:shutdown … life stopped by session`).

### Payload format: grok-shaped, not Claude-shaped

camelCase keys, snake_case event values; no Claude `hook_event_name`/`session_id`/`transcript_path` keys. hcom 0.7.23 evidently tolerates this for registration/status, but any strict Claude-schema consumer would not. Sample (logged verbatim):

```json
{"hookEventName":"session_start","sessionId":"019f45ae-7352-76d0-9494-291d6b3fdc5d",
 "cwd":"…/grokchar/proj","workspaceRoot":"…/grokchar/proj/",
 "timestamp":"2026-07-09T07:01:28.582470804+00:00","source":"new"}
```

`transcriptPath` is **absent on `session_start`** (`source:"new"` — file not yet created) and present on all later events, pointing at `~/.grok/sessions/<urlenc-cwd>/<sid>/updates.jsonl`. That is why hcom registered the agent with `transcript: (none)`.

Hook env: `GROK_HOOK_EVENT`, `GROK_SESSION_ID`, `GROK_WORKSPACE_ROOT`, plus `CLAUDE_PROJECT_DIR` as a Claude-compat alias.

### Context injection: **does not work — the deal-breaker**

Test: SessionStart hook echoing a plain-text codeword (`BLUEFERN`), UserPromptSubmit hook emitting Claude's canonical JSON (`{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"… REDMAPLE."}}`), then asking the model what codewords it knows. Model (with both hooks verifiably fired):

> "1) Codewords from context — From this session, the only codeword-like string is census-turn … Nothing else in the provided context looks like a planted codeword list. 2) Hcom agent name — I was not told an hcom agent name…"

Corroboration: hcom's own `sessionstart` bootstrap emitted **4,556 bytes of stdout** (debug log) — all discarded; the model never learned its bus identity. Grok's docs state it plainly: *"Passive Hooks — For events like SessionStart or PostToolUse, stdout is ignored."* Only `PreToolUse` output is honored, and only as allow/deny.

### Blocking delivery: **does not work**

On Claude, hcom's Stop hook (`hcom poll`, `timeout: 86400`) blocks as the message listener. On grok, the same hook **completed in 32–36 ms** (`hook command completed … stop[0] … elapsed_ms=36`) — hcom self-exits (fail-open) rather than blocking, so ~30–60 s later the instance goes `inactive: stale` and `hcom send` refuses it: `@mentions to non-existent or stopped agents … @lara`. (Because hcom exited on its own, whether grok would honor the 86400 s `timeout` field is untested.)

## 3. Session / transcript behavior

Layout: `~/.grok/sessions/<url-encoded-cwd>/<session-uuid-v7>/` containing:

- `chat_history.jsonl` — full transcript (system prompt, messages) — the file an observer/transcript reader wants;
- `events.jsonl` — machine-readable lifecycle: `turn_started`, `loop_started`, `phase_changed` (`waiting_for_model`/`tool_execution`/`permission_prompt`), `tool_started/completed`, `permission_requested/resolved`, with `session_id`, `turn_number`, `schema_version:"1.0"` — a clean **turn-boundary signal on disk**;
- `updates.jsonl` (what hooks report as `transcriptPath`), `prompt_context.json`, `system_prompt.txt`, `summary.json`, `resources_state.json`, `rewind_points.jsonl`, `terminal/`.

Session id: uuid-v7, pre-assignable via `--session-id`, echoed in every hook payload and debug-log span, and printed at exit (`Resume this session with: grok --resume e7245d83-…`). `prompt_history.jsonl` sits per-cwd next to the session dirs; `grok sessions` lists/searches; `grok export` dumps a Markdown transcript.

Notable compat quirk: `prompt_context.json` shows grok ingests `~/.claude/CLAUDE.md` as an agents-md file — a static bootstrap surface that exists today (but is global/static, not per-spawn).

## 4. Prompt injection & turn-end detection

- **Pane paste works** (the herder path): every prompt in this characterization was delivered via `tmux send-keys -l '<text>'` + `Enter` into the TUI — plain prompts and slash commands. Composer sigil is **`❯` — identical to claude's**, so herder's bootpaste composer-state checks transfer. Hazards observed: a startup modal can swallow the first paste (Escape clears it — herder's modal-clear logic applies), and typing + `Enter` needs a brief gap (the `-l` literal + separate `Enter` pattern was reliable).
- **hcom pty injection does NOT work today**: `hcom term inject <name> … --enter` → `No inject port for 'niru'. Instance not running or not PTY-managed.` The inject port only exists when hcom itself spawns the tool's pty, and hcom has no grok launcher (its tool list: claude|gemini|codex|opencode|kilo|pi|omp|antigravity|cursor|kimi|copilot — no grok).
- **Turn-end signals** (for verified delivery): (a) the Stop hook fires reliably at each turn end — hcom already turns that into `status: listening` bus events for grok sessions (observed: `prompt → tool: → listening` flow); (b) `events.jsonl` `phase_changed`/turn records on disk; (c) TUI prints `Turn completed in Xs`. What's missing is not the signal but the delivery: herder's `DeliverBus` verification polls hcom `deliver:` receipts, which never come because hcom never injects into grok.

## 5. Bus-join hypothesis — definitive answer, and identity hazards

**Register: YES.** Reproduced flow (isolated bus): plain `grok` TUI in a scratch dir → first prompt → global Claude-compat hcom hooks fire → `hcom list` shows a named agent:

```
[claude*] lara   … session_id: e7245d83-cbdd-419e-93f9-ec5f892bd77d
          tool: claude   bindings: hooks, pty   transcript: (none)
```

with live status events (`start/listening → prompt/active → tool:/active → /listening`) and a lifecycle stop on exit. Note it registers as **`tool: claude`** — hcom cannot tell grok apart.

**Receive a delivered message: NO.** Missing capability, precisely: **grok's hook runner discards passive-hook stdout and has no blocking/turn-injection semantics**, so both of hcom's delivery paths (Stop-hook long-poll injection; UserPromptSubmit `additionalContext`) are inert, and the fallback (`hcom term inject`) has no port because hcom didn't spawn the pty. Practical symptom: the agent goes stale seconds after each turn and `hcom send` refuses it; a message sent mid-turn is never surfaced (model, asked directly: "None arrived during this turn. I received no hcom or bus messages to quote.").

**Identity hazards** (bugs waiting for any naive integration):
- hcom identity appears **directory-keyed**: a later grok session in the same cwd silently claimed the pre-existing identity (`bigboss`, then `niru`) instead of registering fresh.
- A grok **subagent's** `session_end` marked the parent's bus instance stopped.

## 6. Gap list (herder touchpoints)

File:line references from the current tree; classification per item. Overall shape: the trivial arms are genuinely trivial, but they are all **downstream of one blocked-upstream item (hcom)** and one **needs-design item (delivery/bootstrap seam)**.

| # | Touchpoint | What's needed | Class |
|---|---|---|---|
| 0 | **hcom (upstream binary)** | No `hcom grok` launcher, no `hcom hooks add grok`, no grok row in its tool table; delivery model (hook-stdout injection + blocking poll) is structurally incompatible with grok. Needs either grok gaining an injection seam (upstream xAI) or hcom growing a **pty-paste delivery mode** (it already wraps ptys and has `term inject` plumbing) + a grok launcher so the inject port exists. Also: parse grok's camelCase payloads properly, capture `transcriptPath` post-`session_start`, and label `tool: grok`. | **blocked-upstream / needs-design** |
| 1 | `tools/herder/internal/launchcmd/launch.go:19` `IsHcomCapable` | add `"grok"` arm | trivial-switch-arm (inert until #0) |
| 2 | `launchcmd/launch.go:31` `PinConfigDir` | `case "grok": setEnvDefault("GROK_HOME", ~/.grok)` — env var confirmed to exist | trivial-switch-arm |
| 3 | `launchcmd/launch.go:180` codex-style bootstrap threading | grok needs the **codex pattern, not the claude pattern**: SessionStart-`additionalContext` rewrite can't work (stdout discarded). Add a `GrokBootstrapBlock` delivered via **`--rules <RULES>`** (appends to system prompt — cleanest) or the initial `[PROMPT]` arg. | needs-design (small, precedent exists) |
| 4 | `hookcmd/template.go` / `hook.go:104` sessionstart intercept | Nothing to intercept for grok (no rewrite seam). Add grok analogues of `CodexBootstrapBlock` + `CodexResumeAddendum`. | needs-design (with #3) |
| 5 | `spawncmd/spawn.go:1455` `defaultPermFlag` | `case "grok": return "--always-approve"` (or `--permission-mode bypassPermissions`) | trivial-switch-arm |
| 6 | `spawncmd/spawn.go:1466` `hasExplicitPermFlag` | recognize `--permission-mode`, `--always-approve` | trivial-switch-arm |
| 7 | `lifecyclecmd/lifecycle.go:983` `permissionArgs` | same as #5 | trivial-switch-arm |
| 8 | `lifecyclecmd/lifecycle.go:356` `detectSelfAgent` | grok exports `GROK_AGENT=1` to shell children (verified) | trivial-switch-arm |
| 9 | `lifecyclecmd/lifecycle.go:241` `forkSelfFallback` | `case "grok": ["--resume", sid, "--fork-session"]` — **verified working** (fork recalled parent context under a new uuid); resume = `--resume <sid>` / `-c`, both verified | trivial-switch-arm |
| 10 | `spawncmd/spawn.go:1148` `awaitBind` + `sidecarcmd/sidecar.go:302` correlation | Unknown whether an hcom-launched grok row would carry `launch_context.pane_id` (claude-like) or need `process_id` enrichment (codex-like) — undeterminable until #0 exists | needs-design (pending #0) |
| 11 | `spawncmd/bootpaste.go:73` sigil switch | `case "grok": sigil = "❯"` (same as claude) — only needed for a **bus-less grok** spawn, which is the one mode herder could ship **today** (raw agent + boot-paste, no hcom) | trivial-switch-arm |
| 12 | Observer/transcript (`observercmd/observer.go:566`, hcom transcript) | grok transcript = `chat_history.jsonl` in a grok-specific layout; hooks advertise `updates.jsonl` instead, and not at session_start. A grok transcript parser / path mapping is new work. | needs-design |

**If findings call for code, that's a separate implement task** (per this unit's charter), with this report as reference.

## Safety-rail compliance (AC #6)

- All grok runs in `$SCRATCHPAD/grokchar/` (git-init'd scratch project); private tmux server `-L grokchar`, killed at the end.
- All hcom traffic from experiments went to the isolated `HCOM_DIR=$SCRATCHPAD/grokchar/.hcom` bus (`bigboss`/`lara`/`niru` live only there). The live bus was checked (read-only) immediately after the one pre-isolation headless sanity run: no registration appeared on it.
- No herder/hcom code or config touched. One side effect outside scratch: `/hooks-trust` created `~/.grok/trusted_folders.toml` (file did not previously exist) — **removed after the experiments**; grok also wrote its normal session dirs under `~/.grok/sessions/` (inert data, left in place).
- API key: pre-existing `XAI_API_KEY` used; nothing was signed up for.
