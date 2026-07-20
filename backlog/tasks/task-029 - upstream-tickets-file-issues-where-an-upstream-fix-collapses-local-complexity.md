---
id: TASK-029
title: 'upstream tickets: file issues where an upstream fix collapses local complexity'
status: To Do
assignee: []
created_date: '2026-07-07 12:31'
updated_date: '2026-07-20 05:19'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
USER DIRECTIVE (2026-07-07): as we close pieces of work, capture anything worth an upstream ticket when a fix there collapses complexity here. This task is the ledger + the eventual filing pass. Candidates accumulated so far (append at every unit closeout):

(1) hcom — user developer_instructions STRIPPED on codex resume/fork (flagship example). hcom re-adds only its own bootstrap; the launch-args seam cannot deliver there. Cost to us: TASK-014 merge-into-last launch hack, TASK-017 entire post-boot bus-delivery mechanism, TASK-027 residual, and the mirrored strip predicate that TASK-028 must re-audit every hcom upgrade. Upstream ask: preserve/merge user developer_instructions across resume/fork, or expose a supported per-agent bootstrap-extension seam (overriding/extending hcom system prompts).

(2) hcom — codex sessionstart is a no-op (no SessionStart-equivalent seam for codex). Forces the -c developer_instructions= ride-along for fresh launches. Possibly the same fix as (1): one sanctioned injection point.

(3) hcom — print-mode (claude -p) one-shots become persistent background agents. TASK-010 recorded option (d) "upstream patch" as skipped (3 coordinated changes fighting deliberate design); we carry the HCOM_LAUNCH_INFLIGHT bypass instead. Upstream ask: native print-mode passthrough; would let us delete the bypass + its goldens.

(4) Claude Code (not hcom) — alarming ".claude.json not found / backup exists / restored" triple when CLAUDE_CONFIG_DIR is re-rooted; reads like data loss, is cosmetic. Draft UX feedback already written in TASK-011 DONE report, unfiled.

(5) hcom minor — replying to an inform with --intent ack is rejected; forces intent=inform for acknowledgements (ergonomics only, may be by-design).

Doctrine: NOTHING is filed externally by agents — drafts are prepared here, the user reviews and files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each candidate above (plus any appended later) gets an explicit verdict in notes: file / do-not-file / superseded, with one-paragraph rationale tied to what local complexity it would collapse
- [ ] #2 For every FILE verdict: ready-to-paste issue draft (title, repro, current local workaround, concrete ask) stored in the task or a linked napkin — nothing submitted externally; user files
- [ ] #3 Candidates cross-checked against the hcom version current at execution time (coordinate with TASK-028 — an upgrade may moot or reshape asks (1)/(2)/(5))
- [ ] #4 Standing practice recorded in the orchestrate skill or run playbook template: unit closeout includes an upstream-candidate sweep
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Upstream candidate (from the plain-truncation investigation, 2026-07-10) — Backlog.md 1.47.1, drafted ready to file:

TITLE: task create accepts nested structured-section markers, then --plain silently omits description content

BODY: Backlog.md 1.47.1 accepts reserved structured-section markers inside --description, wraps that input in a second Description marker pair, and later parses only through the first end marker. task view --plain silently omits the remaining description content and emits no warning. Repro in a throwaway board: create a task whose description contains its own SECTION:DESCRIPTION:BEGIN/END pair followed by more text; the raw file then has two begin and two end markers; --plain renders only the inner pair content and advances to Acceptance Criteria without any truncation or malformed-section warning. This is not an output-length cap: clean 20k-character Description/AC/Notes/Comment fixtures render in full. Expected: creation/editing should reject or escape reserved markers in section bodies, or parsing should detect duplicate/nested markers and warn/fail — never silently omit raw task content.

OWNER (2026-07-10, chat): HOLD the filing batch — do not commission the verdict/drafting pass yet. Ledger stays open for closeout appends; nested-marker Backlog.md draft remains ready-to-paste in notes.

Two hcom upstream candidates from the grok activation unit (2026-07-13, evidence on thread task170act + scratch repros): (15) hcom has no identified-one-shot path to mark an externally-supervised binder ready — generic 'hcom start' leaves an adhoc row as a 'new' placeholder which instance_lifecycle finalizes launch_failed at 30s even though the supervised process is alive and serving; downstream send excludes the row. Ask: a start/ready op (or flag) for processes whose supervisor is external. (16) 'hcom start' silently installs claude hooks AND exits 1 when CLAUDE*/CLAUDECODE env vars are present without a launched/adhoc suppression signal — a side-effecting, failing path for any embedded/binder invocation that inherits a claude pane's env. Ask: hook installation should be an explicit opt-in, never triggered by ambient env detection inside 'start'.

hera (TASK-199 closeout): upstream hcom candidate — the pi extension acks the bus cursor at injection time (pi_plugin/hcom.ts deliverPending: sendUserMessage then immediate ackPending; agent_end used only for status/drain), so a crash between injection and turn settlement leaves a falsely-complete durable receipt with nothing to replay (empirically reproduced: unread=0 + transcript ends at interrupted toolCall). Upstream settlement-correlated ack (defer ack to agent_end/turn_end, or per-batch settled marker) would close the serialized crash window for ALL hcom pi users; note it narrows but does not collapse herder's DR-2 (multi-batch followUp correlation + authority gaps remain).

hera (grok steady-state + hook-suppression closeout): two candidates. (1) hcom — expose a keepalive affordance (or socket-liveness check) before cleanup_stale_instances reaps an ad-hoc row: bridge-bound identities look like expired one-shots because status_time only advances on identified calls; herder now ships a 15m identified-refresh + rebind machine that an upstream affordance would collapse. (2) grok CLI 0.2.93 — ignores the [compat.claude] hooks=false config toggle that current vendor docs assign to GROK_HOME; the documented GROK_CLAUDE_HOOKS_ENABLED env override works. Config-surface bug worth a vendor report.

Upstream hcom hypothesis from a resumed-session adoption incident: the observed inactive placeholder is consistent with `hcom start --as <prior-name>` minting a fresh identity even when the resumed transcript session id already owns a joined row, followed by later hooks rebinding the transcript-owned row. No standalone upstream repro was captured, so verify that sequence before filing. Herder avoids the suspected path during adoption by resolving an explicitly authorized source transcript session id before attempting a name reclaim, but raw/manual recovery may still expose it. If reproduced, ask that `start --as` reuse or explicitly rename the transcript-owned row, or refuse with the existing owner named. This would be hcom-owned because the herder hcom shim transparently forwards the command and cannot arbitrate hcom's roster mutation.

NEW CANDIDATE (2026-07-16): hcom <agent> --run-here launcher strands FOREVER when the launched pane's shell init fails before the pty child starts (observed: mise trust refusal at shell init). No timeout, no diagnostic, launcher process sleeps indefinitely, half-born registry state downstream. Upstream fix: bounded launch-phase timeout + surface pane stderr/init failure to the spawn caller. Reproduced 6+ times across two orchestrators' fleets; recovery required manual launcher kills.

Upstream (bus) candidates from second-deployment field report (2026-07-20): (1) rebind-without-rename — recovering a stranded session identity currently forces stop+start which renames the agent every time; (2) terminal preset silently resets to default on db wipe/recreation — should survive or fail loudly; (3) term screen scrapes return blank for herdr panes mid-boot (observability gap).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-07 12:36
---
CANDIDATE (6) — hcom events sub UX (hera field report, 2026-07-07): (a) `--once` reads like "block until one event" (tail -f expectation) but means "auto-remove subscription after first match" — the command always returns immediately and notifies later via a bus message from [hcom-events]; an agent that wraps it in background execution misreads process exit as the event firing (happened live). (b) "historical matches: N" on create is ambiguous — unclear whether a historical match consumes a --once subscription or only fresh events do. (c) subscriptions stack silently — re-arming without unsub yields duplicate notifications per event (3 live pings from one idle event observed). Asks: sub-specific --help lead line "returns immediately; notification arrives as a bus message", rename/alias --once or document it as auto-unsub, state historical-match semantics on create, dedupe-or-warn on identical filter subscription.
---

created: 2026-07-07 20:55
---
CANDIDATES (7)+(8) — from Unit R phase A (TASK-032 map, live-probe evidence): (7) hcom — dirty-composer starvation is SILENT: a bus message to an agent whose composer holds unsubmitted text queues indefinitely with no receipt, no error, no timeout event, BOTH families (probes vila/keto; reviewer-kimi starved 8h). Ask: an hcom-side "delivery blocked: composer holds a draft" event/receipt — would have named the state in seconds. (8) codex TUI — boot-window input is lossy (Enter-swallow, head-clipping of early pastes); moot for herder post-B1 (bus-first spawn delivery) but still the physics under any remaining TUI-paste user.
---

created: 2026-07-07 21:30
---
CANDIDATE (9) — from Unit R phase B (TASK-032): hcom lacks an "await receipt of message X" primitive — herder reconstructs delivery receipts by polling the event stream, and ALL THREE reconstruction layers were live bugs (receipt query keyed to the wrong side: receipts live on the RECEIVER instance as deliver:<SENDER>; --after boundary excluded same-second receipts; live events emit JSONL while the parser expected a JSON array — masked by mock-shape drift). A first-class receipt-await (send returns a receipt handle, or events exposes await --msg-id) would delete the whole heuristic class.
---

author: hera
created: 2026-07-08 03:28
---
Candidate 10 (wave 7, 2026-07-08): hcom list <name> --json returns a SINGLE object keyed by the BASE name (not an array, not the full scoped name). This surprised two independent implementations in one night (compact --then pickStatus live bug, fixed 2a434fd; mock-shape divergence). Upstream ask: document the single-object/base-name contract in --json help, or emit an array consistently. Candidate 11 (wave 7): codex boot-to-bus-join latency exceeded 60s twice; if hcom's launch path contributes measurable startup cost for codex, a changelog note or a faster join would collapse herder's TASK-036 workaround. (TASK-036 unit is measuring; fold its finding in before filing.)
---

author: hera
created: 2026-07-08 04:05
---
Candidate 12 (Unit Y measurement, 2026-07-08): codex roster entries omit launch_context.pane_id (claude publishes it; codex carries only process_id — verified on fully-booted sessions). This defeats herder's fast child-correlation for codex entirely: initial-prompt bind, sidecar pane-correlation, and recovery all degrade to async tag+cwd-independent enrichment that lags minutes under load. Upstream fix (publish pane_id for codex like claude) collapses the class: TASK-036's recovery affordance, the deferred redeliver verb, and the structural codex bind_timeouts all stop being needed. Strengthens/absorbs candidate 8.
---

created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Reverse-direction entry: herdr 0.6.10->0.7.3 shipped fixes that collapse local complexity (stable ids #569, pane move #299, send-keys combos #613, session.snapshot, api schema, worktree #729, identity fixes #620/#684/#943). The four audit tasks TASK-047..050 enumerate the collapse work; when closing them, check whether any of our previously-planned upstream tickets are now moot.
---

created: 2026-07-08 05:13
---
hera (from vibe #5729): upstream herdr candidate — after update --handoff, surviving pre-handoff agent processes are detection-lost (absent from agent list, agent_status=unknown) because their hook reports never re-reach the new server; #684 covers hook-sequence re-anchoring but not server-side re-adoption without a fresh report. Candidate for upstream filing.
---

created: 2026-07-08 06:36
---
UPSTREAM FILING CANDIDATE, HIGH (vibe #6902, TASK-045 F3): hcom 0.7.23 codex hook binding is broken — hooks_bound:false, session_id empty, launch_context lacks pane_id (0.7.22 had it). Breaks any pane-correlation consumer and codex sid-reporting (TASK-053). File upstream regardless of the herder-side F1 mitigation.
---

created: 2026-07-08 06:40
---
F3 upstream issue draft FINALIZED and HELD (vibe #6996): regression-window claim softened to what records support (0.7.22 had full launch_context + slow-but-completing binds; 0.7.23 first version where hooks_bound never completes), evidence plan concrete (fresh redacted side-by-side claude-vs-codex rows at filing time). Filing gated on OWNER sign-off — outward-facing action; go/no-go is in front of the owner now. On greenlight: vibe files, issue URL lands here.
---

created: 2026-07-08 06:44
---
Owner decision: F3 upstream filing DEFERRED to run closeout — draft stays held as finalized; vibe files at closeout with fresh evidence capture, issue URL lands here then.
---

created: 2026-07-08 09:31
---
[hera 2026-07-08] +upstream ledger entry (TASK-063 Phase 0, authoritative from a codex worker on codex-cli 0.142.5): no custom statusline/footer command hook — only [tui].status_line built-in item ids + terminal_title. herder/hcom identity segments cannot render in the codex footer until upstream adds a hook. Candidate for the same closeout filing batch as F3.
---

created: 2026-07-09 04:22
---
Candidate 13 (2026-07-09, hera): Backlog.md CLI — `task view --plain` silently truncates descriptions over ~3.2k chars with no marker (rendered 3256 of 4210 live; lost tail held a settled design decision). Ask: render fully or mark the truncation. Local details + repro on TASK-090.
---

created: 2026-07-12 07:50
---
hera (A1 closeout): two candidates. (1) hcom — roster launch_context.pane_id is captured from launch-time env HERDR_PANE_ID and diverges from the live canonical pane id after a herdr pane move/re-key; herder A1 now correlates on BOTH pane forms plus caller-own HCOM_PROCESS_ID to compensate, but an upstream live-refreshed pane coordinate (or a documented 'this field is launch-frozen' contract) would collapse that multi-correlate complexity. (2) herdr — no adopt/re-recognition path for shell-relaunched sessions: herdr tracker never adopts an agent it did not launch, so live_status stays undetected/unknown for legitimately live sessions (TASK-070 field evidence: observer-confirmed row shows unknown in herder list). A herdr-adopt affordance (bind an existing live pane/process to tracker state) is the upstream-shaped fix.
---

created: 2026-07-13 00:22
---
Candidate 14 (2026-07-13, hera, TASK-146 closeout sweep): hcom — agent removal never garbage-collects that agent's event subscriptions. herder cull drops the bus name ('@worker-X already gone') yet the culled agent's subs (thread-member + filter subs) persist indefinitely; our live sub table now carries hundreds of orphaned thread-member subs for long-gone agents, all still evaluated per event. Upstream ask: GC subs on agent removal/retirement, or expose a bulk 'unsub --for <name> --all'. Local practice until then: orchestrators unsub their own culled workers' subs at closeout (events unsub <id>).
---

author: hera
created: 2026-07-15 11:01
---
Upstream candidate (hcom): the generated pi wrapper runs a SYNCHRONOUS 'git ls-remote --tags' update check BEFORE launching the child ('/hcom pty pi'), with no timeout — under slow/blocked network the child never launches within any spawner's bind window (roster stays empty, no process), and the caller-side cleanup makes it look like the pane died. Reproduced hermetically; pre-seeding the private update-check flag bypasses it (not shippable — private state). Fix there: async/backgrounded update check, a timeout, or a supported env knob to skip it in managed launches.
---

author: hera
created: 2026-07-15 11:34
---
Upstream candidate (hcom): send/query name-form asymmetry is a footgun. 'hcom send --name <full-name>' stamps the resulting message event data.from with the roster-resolved BASE name; '--from X' stamps verbatim; 'hcom events --from X' filters by exact match. Consequence: event anchors, receipt queries, or audit filters keyed on the full name a caller just passed to --name silently never match for tagged agents (name != base_name) — cost a P1 in a downstream consumer today (ack-recognition anchor never pinned; protocol feature dead for all tagged agents while every suite stayed green, because hermetic fixtures stamped the full name where the wire stamps base). Fix there: stamp consistently (or expose base_name in send output), and/or make events --from match either form.
---

author: hera
created: 2026-07-15 11:46
---
Upstream candidate (hcom): bare --reply-to reply semantics. reply_to is envelope-only metadata — a send with --reply-to <id> and no @target BROADCASTS to every instance instead of routing to the replied message's sender. Wire-proven with three live identities: the owner-side peer receives the worker's ack in delivered_to. Surprising default that turns every polite un-@'d ack into desk spam; suggested fix there: no-target + reply_to routes to the replied message's sender (or at minimum warns).
---
<!-- COMMENTS:END -->

hera (pi identity-hijack incident, 2026-07-15): two hcom candidates. (1) `hcom start --as <name>`
refuses reclaim on latest-identity tool mismatch with NO recovery verb for the rightful owner — after a
cross-tool identity theft (a child process inheriting the caller's identity env connected as the caller,
flipped the row's tool, and its exit archived the row), the victim session is permanently locked out
(kill/config say not-found on the stopped row; --orphan has no record; archive is whole-db only). An
owner-side override (e.g. --force with process-continuity proof, or a tool-mismatch repair verb) would
close the strand. (2) The same refusal exits rc=0 — scripted callers read success on a hard failure.

hera (janitor asymmetry, 2026-07-16, peer-corroborated): hcom's stale_cleanup janitor keys on
heartbeat traffic rather than process liveness, and fired on the wrong side of BOTH failure
modes in one day: it reaped a LIVE seat whose keepalive was silently starved by a config-layer
breakage (identity loss for a running holder after one staleness window), while a genuinely
DEAD seat — process gone, row fossilized at "listening" — survived 4+ hours until manually
dropped. Suggested fix there: staleness should weigh liveness evidence (process/pid, recent
non-heartbeat activity) rather than heartbeat silence alone, and a reap of a row with recent
non-heartbeat events deserves a grace probe. Local evidence: env-robustness task mechanism 3
plus the asymmetry corollary (run event ids recorded there).

hera (launch-context ownership, 2026-07-16): hcom candidate. hcom owns instances.launch_context
(the coordinates its delivery/sender verification match against) but exposes NO way to set or
repair it after row creation — rows created by `hcom start` / reclaimed by `start --as` keep
launch_context={} forever, which downstream makes such sessions spawn-dead in herder (sender
verification has nothing to match) with no healing verb. We are shipping an interim schema-gated
direct-db backfill adapter (merge-missing-only, transactional); the right fix is an hcom-owned
setter or launch-context capture on start/reclaim, at which point the adapter retires.
