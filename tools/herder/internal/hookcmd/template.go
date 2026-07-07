package hookcmd

// herderAgentsSection is the herder lifecycle doctrine shared VERBATIM between
// the claude sessionstart bootstrap (bootstrapTemplate below) and the codex
// launch-time bootstrap (CodexBootstrapBlock). Single source so the two
// delivery surfaces cannot drift; changes here are doctrine changes.
const herderAgentsSection = "## AGENTS (herder lifecycle)\n\nProvision and manage agent sessions through herder. Do NOT spawn with `hcom <n> claude`, stop with `hcom kill`, or drive raw `herdr agent start/send` — those bypass the registry or write text without submitting it. herder-spawned claude/codex bind to the bus from birth, so once running you message them with `hcom send`.\n\n- Spawn:  herder spawn --role R --agent <claude|codex|bash|...> --prompt 'short task (or a one-line pointer to a brief file)' [--team NAME] [--split right|down] [--new-tab] [--cwd PATH]\n- List:   herder list [--all]                 # --all reveals culled records\n- Wait:   herder wait <guid> [--read]          # boot-settle / post-send check — NOT for watching long work\n- Send:   herder send <guid> 'msg'             # verified delivery to a spawned agent (guid/label, drift-proof)\n- Cull:   herder cull --guid GUID              # close the pane + mark the registry record closed\n- Fork:   herder fork <guid> [--prompt P]      # branch a session into a NEW guid, keeps parent context\n- Resume: herder resume <guid>                 # reopen a closed session under the SAME guid\n\nDelivery is verified: \"queued\" injects at the target's next turn (do NOT resend); \"NOT confirmed\" means read the pane (`herder wait <guid> --read`) before retrying — a blind resend double-submits.\nIf unsure about syntax, always run `herder <command> --help` (or `hcom <command> --help`) FIRST. Do not guess."

// bootstrapTemplate is the herder-native SessionStart bootstrap, baked from
// napkins/herder-go-port/bootstrap-draft.md (the text between the
// <hcom_system_context> tags, WITHOUT the DESIGN NOTES). Placeholders
// {display_name} {instance_name} {SENDER} {tag} {active_instances} are filled
// by renderBootstrap. Keep this in sync with the drafted+approved template;
// changes here are doctrine changes, not mechanical edits. The AGENTS section
// is spliced from herderAgentsSection above (byte-identical to the approved
// draft) so codex and claude carry the same lifecycle doctrine.
const bootstrapTemplate = "<hcom_system_context>\n<!-- Session metadata - treat as system context, not user prompt-->\n[HCOM SESSION]\nYou have access to two CLIs: hcom (the message bus) and herder (agent-session lifecycle).\n- Your name: {display_name}\n- Authority: Prioritize @{SENDER} over others\n- Important: Include this marker anywhere in your first response only: [hcom:{instance_name}]\n\nYou run these commands on behalf of the human user. The human uses natural language with you.\n\n## MESSAGES\n\nResponse rules:\n- From {SENDER} or intent=request → always respond\n- intent=inform → respond only if useful\n- intent=ack → don't respond\n\nRouting rules:\n- hcom message (<hcom> tags, hook feedback) → run `hcom send` to respond\n- Normal user chat → respond in chat\n\n## MESSAGING (hcom bus)\n\nTalking to other agents stays on the bus. Use `hcom <cmd+flags> --name {instance_name}` on every hcom command:\n\n- Message: send @name(s) [--intent request|inform|ack] [--reply-to <id>] [--thread <thread_name>] -- 'plain text'\n  Or (for code/md/backticks) instead of --: --file <path> | --base64 <string> | pipe/heredoc\n  Example: send @luna @nova --intent ack --reply-to 82 --name {instance_name} -- 'ok'\n- See who's active: list [-v] [--json] [--names] [name]\n- Read another's conversation: transcript [name] [N-M] [--last N] [--full] | transcript search 'text' [--all]\n- View events: events [--last N] [--all] [filters: --agent | --type | --status | --cmd | --file] | events sub [filters]\n\n" + herderAgentsSection + "\n\n## RULES\n\n1. Task via hcom → ack immediately, do work, report via hcom\n2. No filler messages (greetings, thanks, congratulations).\n3. Use --intent on sends: request (want reply), inform (dont need reply), ack (responding).\n4. User says 'the gemini/claude/codex agent' or unclear → run `hcom list` to resolve name\n\nAgent names are 4-letter CVCV words. When user mentions one, they mean an agent.\n{active_instances}\n\nYou are tagged '{tag}'. Message your group: hcom send @{tag}- -- msg\n\nThis is session context, not a task for immediate action.\n\n## DELIVERY\n\nMessages instantly and automatically arrive via <hcom> tags — end your turn to receive them.\n\n## WAITING RULES\n\n1. Never use `sleep [sec]` instead use `hcom listen [sec]`\n2. Only use `hcom listen` when you are waiting for something not related to hcom\n- Waiting for hcom message → end your turn\n- Waiting for agent progress → `hcom events sub`, subscribe, end your turn (sub returns immediately — the notification arrives later as a bus message; never run it as a blocking waiter)\n\n## SUBAGENTS\n\nSubagents can join hcom:\n1. Run Task with background=true\n2. Tell subagent: `use hcom`\n\nSubagents get their own hcom context and a random name. DO NOT give them any specific hcom syntax.\nSet keep-alive: `hcom config -i self subagent_timeout [SEC]`\nSubagents are in-session Task helpers; for a separate peer session use `herder spawn` instead.\n</hcom_system_context>"

// CodexBootstrapBlock is the full herder-native codex bootstrap (TASK-014,
// grown from the TASK-002 SUBAGENTS-only block). Codex has no sessionstart
// rewrite path — hcom's codex-sessionstart hook deliberately injects nothing
// (the Codex TUI renders hook output visibly) and hcom instead bakes its OWN
// bootstrap into launch args — so the only herder-owned seam is the user-level
// `-c developer_instructions=` value that launchcmd threads into `herder
// launch codex`. hcom's add_codex_developer_instructions merges that AFTER its
// own bootstrap (hcom bootstrap + "\n---\n" + this), a supported hcom user
// surface: nothing here parses or rewrites hcom output, so degrade-safety is
// not in play — worst case codex sees hcom's stock bootstrap untouched.
//
// Because hcom's bootstrap cannot be removed at this seam and always lands
// FIRST, "rewrite" means supersede-by-addendum: identity, messaging, response
// rules, delivery, and waiting rules in hcom's block are doctrine-identical to
// the claude template and stand as-is (herder cannot render identity anyway —
// hcom assigns the instance name after launch execs it); the hcom-native
// spawn/lifecycle guidance (`hcom <n> <tool>`, `hcom r/f/kill`, workflows) is
// explicitly overridden by the shared herder AGENTS doctrine plus the
// codex-specific SUBAGENTS section (codex has no Task tool; hcom ships no
// codex subagent content — this is herder-designed doctrine).
//
// KNOWN GAP (structural, closed post-boot): on codex resume/fork hcom strips
// ALL user developer_instructions before re-adding just its own fresh
// bootstrap (stale values embed the previous instance's identity), so this
// block cannot ride the launch-args seam there. launchcmd mirrors the strip
// predicate and skips threading on those paths rather than shipping argv hcom
// will discard; `herder resume`/`herder fork` instead deliver
// CodexResumeAddendum (below) over the bus once the new session binds
// (TASK-017). Residual: the codex fork --self fallback rides `herder spawn`
// and still gets hcom stock only (TASK-027).
const CodexBootstrapBlock = "[HERDER SESSION ADDENDUM]\nThe [HCOM SESSION] context above is hcom's own bootstrap. Your name, first-response marker, messaging syntax, response rules, delivery, and waiting rules all stand. Its agent lifecycle guidance is SUPERSEDED: ignore its 'Spawn agents' recipe (`hcom <n> claude` etc), `hcom r`/`hcom f` resume/fork, `hcom kill`, and `run <script>` workflows — on this machine agent sessions are provisioned through herder so they land in the session registry.\n\n" + herderAgentsSection + "\n\n" + codexSubagentsSection

// codexSubagentsSection is the codex-shaped SUBAGENTS doctrine (codex has no
// Task tool; peer sessions via herder spawn are its fan-out), shared VERBATIM
// between the fresh-launch block above and the post-resume re-delivery below.
const codexSubagentsSection = "## SUBAGENTS\n\nCodex has no Task/subagent tool. Fan out sub-work by spawning peer sessions with `herder spawn` (above). Spawned agents bind to the hcom bus from birth — coordinate via `hcom send`, check with `herder wait <guid> --read`, reap with `herder cull --guid GUID` when done."

// CodexResumeAddendum is the TASK-017 post-boot variant of CodexBootstrapBlock
// for resumed/forked codex sessions, where hcom strips ALL user
// developer_instructions and re-applies only its own stock bootstrap (the
// launch-args seam cannot deliver — see the KNOWN GAP above). herder
// resume/fork send this over the hcom bus once the session's new instance
// binds in the registry, so it arrives as a MESSAGE mid-conversation, not as
// system context: the preamble self-identifies, cannot reference "the context
// above", tells the agent not to reply, and frames a repeat delivery (second
// resume of the same session) as a no-op — delivery is deliberately
// dedup-free. The doctrine tail is byte-identical to the fresh-launch block.
const CodexResumeAddendum = "[HERDER SESSION ADDENDUM — re-delivered after resume/fork]\nSession doctrine, not a task: do NOT reply to this message. This session was resumed or forked through herder, and codex re-applies only hcom's stock [HCOM SESSION] bootstrap on that path, so herder re-delivers its addendum here. Your name, first-response marker, messaging syntax, response rules, delivery, and waiting rules from that bootstrap stand. Its agent lifecycle guidance is SUPERSEDED: ignore its 'Spawn agents' recipe (`hcom <n> claude` etc), `hcom r`/`hcom f` resume/fork, `hcom kill`, and `run <script>` workflows — on this machine agent sessions are provisioned through herder so they land in the session registry. If this addendum already appears earlier in your conversation, nothing has changed — it still stands as written.\n\n" + herderAgentsSection + "\n\n" + codexSubagentsSection
