# New-harness onboarding playbook (draft)

Date: 2026-07-09 · Author: task106-zaru (TASK-106) · Status: draft for review

Purpose: the checklist any future CLI coding agent ("harness") must satisfy to become a first-class herder/hcom citizen — spawnable, bus-bound from birth, deliverable-to, observable, forkable/resumable. Written harness-agnostically; **grok CLI 0.2.93 is the worked example** (full evidence in `docs/design/2026-07-09-grok-cli-characterization.md`).

How to use it: characterize the candidate harness against §1–§9 first (each has a concrete probe you can run in an hour). The answers pick one of the three integration shapes in §10, which in turn determines the herder/hcom work items in §11. A future implement task should execute §11 against a filled-in copy of the §1–§9 table.

---

## The contract, capability by capability

Legend per item: **WHY** it matters · **PROBE** to run · **GROK** worked answer.

### 1. Session identity

The registry, observer, resume, and fork all key on a per-session id that herder can learn (ideally *choose*) at spawn time.

- Required: a stable session id; strongly preferred: a `--session-id`-style flag so the spawner pre-assigns it; the id must be discoverable from hook payloads or an env var, and printed/queryable for recovery.
- PROBE: spawn with a generated UUID; confirm the same UUID shows up in hook payloads / session storage / exit message.
- GROK: ✅ `--session-id <uuid>` (new sessions), uuid-v7 default; echoed in every hook payload (`sessionId`), debug spans, and the exit hint (`Resume this session with: grok --resume <sid>`); `GROK_SESSION_ID` set for hook processes.

### 2. Hook surface for bus binding

hcom binds an agent by running `hcom <verb>` on lifecycle events. Minimum viable set: a session-start (register), a turn-end (status + delivery point), a prompt-submit and pre/post-tool (liveness/status). Session-end (cleanup) is nice-to-have.

- Required: config-file-driven lifecycle hooks that exec arbitrary commands with a JSON payload; know **which file, which scope, and which trust gates**.
- PROBE: register a logger hook for every documented event; drive one interactive turn, one subagent, one quit, one headless run; diff fired-vs-documented.
- GROK: ✅ events fire: SessionStart (lazily at first prompt), UserPromptSubmit, PreToolUse (+matcher, +Claude tool-name aliases), PostToolUse, Stop, Notification, SubagentStart/Stop; SessionEnd only observed for subagent sessions (not on `/quit`). Sources: `~/.grok/hooks/*.json` (always trusted), plus **Claude compat**: `~/.claude/settings.json` (always trusted — hcom's existing hooks load into grok unmodified!) and project `.claude/settings.json` (requires folder trust **and a git worktree root**). ⚠ payloads are grok-shaped (camelCase `sessionId`, `hookEventName:"session_start"`), not Claude-shaped; `PermissionRequest` is silently skipped; hooks also fire in headless `-p` runs.

### 3. Context-injection seam for message delivery — **the make-or-break item**

"Queued injects at the target's next turn" requires *some* way to put text in front of the model without a human keystroke. Known seams, in preference order:

a. **Hook-output injection** (claude): hook stdout / `additionalContext` JSON enters model context; a blocking Stop-hook long-poll doubles as the listener. — b. **Launch-arg doctrine + bus redelivery** (codex): static bootstrap via launch args; dynamic delivery still needs (a) or (c). — c. **Pty paste**: write into the composer and submit; needs sigil/composer-state discipline and turn-boundary awareness.

- PROBE (decisive, 10 min): SessionStart hook echoes plain-text codeword A; prompt-submit hook emits Claude-format `additionalContext` codeword B; ask the model what codewords it knows. Also time the Stop hook with a long-poll command: does it block or get reaped?
- GROK: ❌ (a) is **dead**: passive-hook stdout is discarded by design ("stdout is ignored"); model saw neither codeword and never learned its hcom identity despite hcom emitting a 4.5 KB bootstrap; `hcom poll` on Stop exits in ~35 ms instead of blocking, so the agent goes stale and `hcom send` refuses it. ✅ (b) available: `--rules <text>` appends to the system prompt (also: initial `[PROMPT]` arg; grok even ingests `~/.claude/CLAUDE.md`). ✅ (c) available externally: tmux paste + Enter works (used throughout characterization); hcom's own `hcom term inject` has **no port** unless hcom spawned the pty.

### 4. Turn-end signal for verified delivery

herder's `DeliverBus` verifies by polling hcom `deliver:` receipts, which hcom emits when its hook hands a message to the model. If delivery moves to pty-paste, the paster needs an equally sharp "turn ended / composer idle" signal.

- PROBE: correlate the harness's turn-end hook, any on-disk event stream, and the TUI idle state across a slow multi-tool turn.
- GROK: ✅ three independent signals: Stop hook fires per turn (already surfaces as hcom `listening` status); `~/.grok/sessions/<cwd>/<sid>/events.jsonl` (`turn_started`, `phase_changed`, `turn_completed`-class records, `schema_version:"1.0"`); TUI prints `Turn completed in Xs`. ❌ receipts: none, because nothing is ever delivered (see §3).

### 5. Prompt-injection path (pane paste)

Even bus-bound agents need boot-paste behavior understood (bus-less spawns, `herder compact`, modal recovery).

- Required knowledge: composer sigil, literal-paste + submit sequence, modal/permission-prompt interceptors, slash-command handling.
- PROBE: tmux `send-keys -l` a prompt and a slash command; then do it with a startup modal open.
- GROK: ✅ paste+Enter reliable (all characterization prompts went in this way); composer sigil **`❯` — identical to claude's**, so bootpaste state checks transfer; hazards: startup modal swallows input (Escape clears), and `-l` text + separate `Enter` with a small gap is needed.

### 6. Permissions / danger mode

herder spawns default to skip-permissions unless `--safe`; it must know the flag and recognize user-supplied alternatives.

- PROBE: read `--help`; verify the flag suppresses tool prompts in one run.
- GROK: ✅ `--always-approve`, `--permission-mode bypassPermissions` (plus `default|acceptEdits|auto|dontAsk|plan`); `--allow`/`--deny` rules with documented Claude equivalences; config default `[ui] permission_mode`.

### 7. Fork / resume (or explicit non-support)

- Required: resume-by-id; fork = resume-with-new-id; know whether headless resume works (cheap probes) and what state restores.
- PROBE: run a session with a memorable action; `--resume <sid> -p "what did you do?"`; then fork and confirm the child has parent context under a new id.
- GROK: ✅ all verified headlessly: `--resume <sid>` (recalled earlier commands), `-c` (continue latest in cwd), `--resume <sid> --fork-session [--session-id <new-uuid>]` (parent context, fresh id), `--restore-code` for worktree state; exit message advertises the resume command.

### 8. Session storage & transcript for the observer

- Required: transcript location + format the observer/`hcom transcript` can read, mapped from session id.
- PROBE: locate the session dir; identify the human transcript vs event stream; check what path (if any) hooks advertise.
- GROK: ⚠ layout `~/.grok/sessions/<url-encoded-cwd>/<sid>/`: transcript = `chat_history.jsonl`, events = `events.jsonl` — but hooks advertise `transcriptPath` = `updates.jsonl`, and **not at session_start** (so hcom registers `transcript: (none)`). A grok-format transcript reader is new work. `grok export` gives Markdown as an escape hatch.

### 9. Environment & identity hygiene

- Required: a config-dir env var (survive isolated `HCOM_DIR`/custom homes); a child-process marker for self-detection; understanding of subagent lifecycle vs the bus.
- PROBE: from inside a session, `env | grep -i <tool>`; spawn a subagent with hooks logging session ids.
- GROK: ✅ `GROK_HOME` (config dir), `GROK_AGENT=1` exported to shell children (self-detect), `GROK_SESSION_ID`/`CLAUDE_PROJECT_DIR` for hooks. ⚠ hazards: hcom identity reuse by cwd (a second grok session in the same dir silently claimed an existing agent name), and a **subagent's session_end stopped the parent's bus instance** — any integration must key hook handling on `sessionId`, not directory.

---

## 10. Integration shapes (pick one per harness)

- **Shape A — claude-class** (hook-output injection works): full citizen; hcom stock hooks + herder sessionstart rewrite. Requirements: §3a.
- **Shape B — codex-class** (no injection seam; launch-arg doctrine + some redelivery path): bootstrap via launch args; delivery via whatever the harness *does* honor, else pty. Requirements: §3b + a delivery answer.
- **Shape C — bus-less raw agent**: spawn + boot-paste only; no registration, no delivery, driven like `bash` seats. Requirements: §5 only. Always available as a day-one stopgap.

**Grok lands as Shape B with pty-paste delivery** (or Shape C today, zero code beyond a sigil arm): bootstrap through `--rules`, register via the Claude-compat hooks that already fire, deliver by paste — which requires hcom to own the pty (launcher) so `term inject` has a port, or herder to paste via the pane vehicle it already controls.

## 11. Work items once the shape is chosen (herder-side map)

From the TASK-106 survey (details + line numbers in the characterization doc §6):

1. **hcom first** — launcher entry, hook-writer (`hcom hooks add <tool>`), payload parsing, tool labeling, and (for Shape B) a pty-paste delivery mode. Everything below is inert without it.
2. Trivial switch arms: `launchcmd.IsHcomCapable`, `PinConfigDir` (config-dir env), `spawncmd.defaultPermFlag` + `hasExplicitPermFlag`, `lifecyclecmd.permissionArgs`, `detectSelfAgent`, `forkSelfFallback`, `bootpaste` sigil.
3. Design work: bootstrap block + resume addendum (template.go analogues of the codex pair), sidecar correlation (`pane_id` vs `process_id`), observer/transcript reader for the harness's session format.
4. Re-run this playbook's probes **after** wiring, as acceptance: register on an isolated `HCOM_DIR` bus, deliver a message end-to-end (model quotes it), verify receipt, fork/resume through herder.

## Safety rails for characterizing any new harness (learned the hard way)

- Isolate the bus (`HCOM_DIR=<scratch>/.hcom`) **before the first run** — compat hooks mean the harness may join your *live* bus uninvited (grok loads `~/.claude/settings.json` hooks by default, always-trusted).
- Private tmux server (`tmux -L <name>`), scratch project dir (git-init it — some harnesses gate project config on a git root), dedicated debug log.
- Never sign up for accounts/keys: if credentials aren't already on the machine, stop and report.
- Track every write outside scratch (trust stores, session dirs) and revert what you created.
