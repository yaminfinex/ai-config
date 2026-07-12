<!-- Provenance: research memo, herder-dx run, 2026-07-12. Author: codex research unit; verified sources: repo + public xAI docs only. -->
# Grok as a herder/hcom agent family — onboarding memo

Date: 2026-07-12  
Unit type: research; no production code, live registry, live panes, or live bus/config changed  
Bus thread: `grok`  

## Owner answers required before any live demo

1. **Which authentication path should the production seat use?** Choose one: an existing
   SuperGrok/X Premium subscription via browser or device-code OAuth, or a metered xAI API
   key exposed through the documented environment variable. The installed CLI supports all
   three. Do not put a credential in herder args, task text, the registry, or repo config.
2. **May the implementer spend xAI credits / subscription allowance on an isolated smoke?**
   The research unit did not launch a model. A functional demo necessarily makes inference
   calls and may consume quota.
3. **What counts as the ASAP demo?** The zero-code path can prove “Grok TUI in a herder pane,
   appears in the hcom roster, can send outbound when told its assigned name,” but it cannot
   receive bus messages. A bidirectional bus demo requires bridge work.
4. **For the first-class seat, should `--safe` remain ask-mode and the normal default map to
   `--always-approve`, or should Grok use the stronger `bypassPermissions` mode?** The former
   matches the CLI's documented normal autonomy control; the latter changes the security
   boundary and needs an explicit owner ruling.
5. **Which Grok model is the initial production pin?** The CLI default is the lowest-friction
   choice. If the owner wants a named model, herder can pass Grok's `--model` after first-class
   support exists.

Auth-mechanism claims in this memo come only from repository sources and public vendor docs.
No value from local auth/config/state was used or reproduced.

## Recommendation

The fastest credible path is the already-installed **official xAI Grok Build CLI**, version
`0.2.93`. It is an interactive terminal coding agent, not merely an API wrapper, and exposes
the exact launch primitives herder needs: TUI mode, headless mode, preassigned session IDs,
resume/fork, `GROK_HOME`, `--rules`, `--model`, and autonomous permission flags.

There are two distinct milestones:

- **ASAP degraded demo (zero production diff):** spawn raw `grok` as an arbitrary herdr/herder
  command in an isolated namespace. Existing Claude-hook compatibility causes it to appear
  on hcom, but misleadingly as `tool: claude`; typed pane prompts work. This proves pane,
  model, and partial lifecycle viability only.
- **First-class Grok family:** add explicit Grok recognition and a Grok-native delivery
  bridge. Passive hook output is discarded by Grok 0.2.93, so simply adding `"grok"` to
  `IsHcomCapable` would create a session that binds but cannot receive its prompt. The
  evidence-backed bridge is a silent persistent Grok monitor for wake-up plus MCP operations
  for full-message fetch, acknowledgement, recovery, and replies.

The existing empirical characterization at
[`docs/grok-integration-characterization.md`](../../docs/grok-integration-characterization.md)
already tested the installed version in isolated roots. It found monitor wake-up works both
idle and mid-turn (buffered to the turn boundary), while passive Claude-compatible hook
stdout, stop-hook block output, and `hcom term inject` do not provide delivery. This memo
therefore does not recommend another broad characterization pass.

## What can run Grok on this box today

### Installed inventory

| CLI | Installed | Interactive terminal agent | Grok auth/routing | Fit |
|---|---:|---:|---|---|
| **xAI Grok Build** | **Yes**, `0.2.93` | **Yes**, full TUI; also headless and ACP | Browser OAuth, device-code OAuth, or the documented xAI API-key environment variable | **Best and fastest** |
| Codex CLI | Yes | Yes | Codex has configurable model providers in principle, but Grok-on-Codex was not proven here and adds protocol/tool-behavior risk | Do not use for ASAP |
| Claude Code | Yes | Yes | No direct xAI provider path established | Not a Grok runner |
| OpenCode | No | Yes | First-class xAI OAuth/device/API-key support upstream | Credible fallback, but install adds work |
| Aider | No | Yes | Direct xAI provider using the documented xAI key variable | Credible fallback, less aligned with current hcom launch support |
| Kilo CLI | No | Yes | xAI subscription OAuth documented by xAI | Credible fallback, but install adds work |
| Goose, `aichat`, `llm`, Gemini, Pi, OMP, Cursor, Kimi, Copilot | No | Varies | Not relevant without installation | Not candidates for ASAP |

The official Grok CLI starts a TUI with `grok`; `-p/--single` is batch/headless and exits;
`grok agent stdio` is an ACP JSON-RPC server rather than a human pane. For herder's present
pane model, use the TUI. ACP may later be a cleaner programmatic integration, but it is not
the shortest route to a visible terminal worker.

Official xAI docs confirm:

- installation and interactive/headless use: [Grok Build overview](https://docs.x.ai/build/overview);
- flags and subcommands: [CLI reference](https://docs.x.ai/build/cli/reference);
- browser OAuth, device code, API-key precedence, and refresh behavior:
  [Enterprise authentication](https://docs.x.ai/build/enterprise);
- `GROK_HOME` and custom-provider settings: [Settings](https://docs.x.ai/build/settings);
- OpenCode is a viable alternative with xAI OAuth/device/API-key auth:
  [OpenCode providers](https://opencode.ai/docs/providers/);
- Aider is a viable API-key alternative: [Aider xAI provider](https://aider.chat/docs/llms/xai.html).

### Auth failure behavior

- OAuth is refreshable; device-code login is the best interactive route on SSH/remote boxes.
- API-key auth is non-refreshable and fails immediately when the key is missing, invalid,
  expired, or lacks model access/credits.
- xAI API limits are per model, on requests and tokens. Over-limit requests return HTTP 429;
  current limits are account-tier dependent and visible in the xAI console. See
  [xAI rate limits](https://docs.x.ai/developers/rate-limits).
- Grok Build is early-beta software. The installed `0.2.93` is one patch behind the public
  `0.2.94` changelog as of this memo, but no update/install is authorized by this research.

## What herder needs for a new first-class family

### Current launch and binding contract

`tools/herder/internal/launchcmd/launch.go` is the central family gate:

- `IsHcomCapable` currently recognizes only `claude`, `codex`, and `gemini`.
- Bus-capable agents run as `herder launch <tool> --tag <role> ...`, which execs
  `hcom <tool> --run-here`; all other agent strings are launched raw.
- `PinConfigDir` protects each real tool home when an isolated `HCOM_DIR` is used.
- bus-capable spawns get `HCOM_DIR` and the herder `hcom` PATH shim; the sidecar starts from
  `herder launch`; the initial prompt waits for a child-specific bind and then rides hcom.
- unknown/raw agents are registry-recorded but receive initial prompts through boot-paste,
  have no recorded hcom namespace/name, and do not start the launch sidecar.

The hcom binary is an upstream gate. Installed hcom `0.7.23` has launchers for several
families but **not Grok**. `herder launch grok` therefore cannot work until hcom learns that
tool or herder owns a Grok-specific launch/bridge path.

### What is Claude/Codex-specific

| Surface | Current specialization | Grok requirement |
|---|---|---|
| Hcom launch table | No `grok` launcher | Add explicit Grok family and label rows `tool: grok` |
| Config pinning | `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `GEMINI_CLI_HOME` | Pin `GROK_HOME` |
| Doctrine | Claude SessionStart output rewrite; Codex `developer_instructions` addendum | Pass Grok doctrine at launch through `--rules`; passive hook output cannot carry it |
| Permissions | Claude skip-permissions; Codex bypass approvals/sandbox | Map normal autonomy to `--always-approve` (owner to rule on stronger mode) |
| Model flag | First-class only for Claude/Codex | Allow Grok's `--model` |
| Resume/fork | Claude/Codex-specific argv and post-resume doctrine | Grok resume is `--resume <sid>`; fork adds `--fork-session`; reapply doctrine/monitor as needed |
| Self detection | Claude/Codex environment/session logic | Recognize `GROK_AGENT=1` |
| Composer boot-paste | Sigils only for Claude, Codex, Bash | Grok uses `❯`, but first-class prompt delivery should avoid paste |
| Transcript | Claude/Codex/hcom paths | Grok's full transcript is `chat_history.jsonl`; hook `transcriptPath` points elsewhere |
| Herdr integration | Installed schema has no Grok integration target | Without upstream herdr support, status/session tracking can remain `unknown`; bridge/observer must not pretend otherwise |

### Zero-diff degraded path

Herder accepts arbitrary `--agent` values and herdr accepts arbitrary argv. Therefore a
throwaway smoke can attempt a raw Grok TUI without changing production code, conceptually:

```sh
HERDER_STATE_DIR=<throwaway> HCOM_DIR=<throwaway> \
  herder spawn --role grok-demo --agent grok \
  --extra-arg --always-approve \
  --extra-arg --rules --extra-arg '<short demo doctrine>'
```

The implementer must use genuinely isolated `HOME`/`GROK_HOME`, `HCOM_DIR`, and
`HERDER_STATE_DIR`, or a non-secret auth handoff that preserves isolation. Do not paste this
command into the live registry context unchanged.

Expected capabilities and losses:

- **Gains:** visible interactive Grok TUI in a herder-created pane; herder registry row;
  typed prompt path; Grok's inherited Claude hooks may register it on the isolated hcom bus;
  outbound `hcom send` can be demonstrated after the operator supplies the assigned name.
- **Losses:** no verified inbound delivery; no bus-first initial prompt; hcom mislabels it as
  Claude; no safe bind/name capture; no sidecar enrichment/statusline snapshots; herdr may
  report unknown status/session; no first-class resume/fork; no doctrine from SessionStart;
  subagent lifecycle can steal/stop the parent identity; `herder send` cannot use the row as
  a proven bus address.

This is a **roster/pane demo, not a working bus-agent demo**. Do not call it integrated.

### Minimal first-class diff

The smallest honest first-class slice is larger than switch arms because delivery is the
hard boundary:

1. Add Grok as an explicit hcom/herder tool and pin `GROK_HOME`.
2. Launch with a preassigned UUID session id, `--rules` doctrine, model option, and explicit
   permission mode.
3. Start a silent per-seat bridge feeding one compact routing line to a persistent Grok
   monitor. Any diagnostic output must go to a file, because every monitor line becomes
   model context.
4. Expose MCP operations for fetch-by-id, acknowledgement/receipt, pending recovery, and
   reply. Define `delivered` from monitor injection plus message acknowledgement, not from
   Claude's Stop-hook receipt.
5. Carry the family through registry/observer/lifecycle: correct tool label, session-id
   correlation, resume/fork, transcript path, status degradation when herdr cannot detect it,
   and parent/subagent identity separation.
6. Add a Grok shim and installer/doctor/PATH coverage only after the launch contract is
   proven; otherwise a hand-typed `grok` shim would route users into a nonfunctional family.

## Model-routing fit (options, not a decision)

Current owner doctrine says GPT-5.6-sol is the default coding/research/advisor model,
frontend remains Claude-family, Opus cross-family reviews Codex work, Codex reviews
Claude-family work, and Fable is planning/design/adjudication rather than coding.

Grok introduces a genuinely third family. Plausible routing choices are:

- **Experimental implementer lane:** Grok implements bounded tasks; Opus or Codex reviews.
  This provides cross-family review in either direction and is the cleanest way to gather
  evidence without replacing the default coder.
- **Third-family reviewer:** Grok reviews Codex or Claude work. This could reduce dependence
  on a single reviewer family, but should wait until transcript/delivery fidelity is proven;
  a reviewer that silently misses bus feedback is worse than no reviewer.
- **Research/advisor challenger:** Grok becomes a second/third independent lens alongside
  GPT-5.6 and Fable. This is lower mutation risk, but current doctrine already assigns those
  seats; changing it is an owner product decision.
- **No routing change initially:** first-class transport ships behind explicit
  `--agent grok`; doctrine remains unchanged until several scored tasks establish strengths,
  failure modes, cost, and latency.

Recommended rollout posture: the final option first, then a small experimental implementer
or reviewer lane with mandatory cross-family review. Do not replace GPT-5.6, Opus, or Fable
by assumption.

## Risks and required proofs

1. **False integration from Claude compatibility — high.** Grok fires Claude hooks, but
   passive output is ignored; hcom can show a healthy-looking row that cannot receive.
2. **Identity collision — high.** Existing probes saw directory-keyed identity reuse and a
   subagent SessionEnd stopping the parent. First-class support must bind on explicit session
   and process/pane evidence, never cwd uniqueness.
3. **Dirty composer / delivery semantics — high.** The current bus contract depends on an
   empty-composer wake and family-specific receipts. Grok's monitor queues busy messages to a
   turn boundary; it must be proven under idle, working, modal, compaction, resume, bridge
   restart, duplicate, and out-of-order cases.
4. **Monitor noise — high.** stdout and stderr become notification turns. The bridge must be
   silent when idle and route logs elsewhere.
5. **Rate/auth failure mid-unit — medium/high.** OAuth refresh can fail; API keys do not
   refresh; quota and 429 failures can strand a turn. Persist pending message IDs before
   inference and re-list them after recovery so acknowledgement is never aspirational.
6. **Herdr status blind spot — medium.** The installed herdr integration enum omits Grok.
   `unknown` must remain an honest state; do not synthesize idle/working from weak evidence.
7. **Transcript mismatch — medium.** Hook `transcriptPath` is not Grok's full chat transcript.
   Observers must locate `chat_history.jsonl` by explicit session identity.
8. **Beta/version drift — medium.** The CLI is moving quickly. Pin characterization tests to
   version/capability, not screen text alone.
9. **Security — medium/high.** `--always-approve` and especially `bypassPermissions` permit
   broad tool execution. Keep `--safe` meaningful and avoid writing credentials to config,
   logs, doctrine, prompts, or registry records.

## Ranked filed-ready tasks

### 1. Demo Grok in an isolated herder pane with degraded hcom roster presence

**Type:** investigate/demo, no production diff  
**Description:** Using the installed Grok Build CLI and completely throwaway `HOME`,
`GROK_HOME`, `HCOM_DIR`, and `HERDER_STATE_DIR`, prove the fastest current path: raw Grok
TUI launched by herder, one typed task completed, isolated hcom registration observed, and
one outbound hcom message sent after the operator supplies the assigned bus name. Explicitly
demonstrate that inbound delivery is absent or unverified. Do not enroll anything into the
live registry, use live panes/observer/config, install/update software, or claim first-class
integration.

**Acceptance criteria:**

- Owner chooses auth path and authorizes one inference smoke; no credential value appears in
  commands captured in the repo, logs, memo, task, registry, or bus.
- The test uses isolated roots for all four state/config namespaces and a separate pane or
  private terminal server; teardown is documented.
- `grok 0.2.93` opens interactively in a herder-created pane and completes a harmless prompt.
- The resulting herder row and isolated hcom row are recorded, including honest unknown or
  mislabelled fields; no live row is created.
- Outbound hcom messaging is proven once, with receipt; inbound behavior is probed once and
  reported as delivered, queued, refused, or absent without blind retries.
- The report states plainly: roster/pane demo only, not production bus support.
- Any deviation is stop-and-report; no installs, production writes, or external filing.

### 2. Add first-class Grok family support with monitor + MCP delivery

**Type:** design, then separate implementation unit  
**Description:** Design and implement Grok as an explicit herder/hcom family. Launch Grok
with pinned config, preassigned session identity, `--rules` doctrine, model/permission flags,
and a silent persistent monitor. Bridge hcom messages through compact monitor wake records
and MCP fetch/ack/pending/send operations. Carry the family through registry, sidecar,
observer, lifecycle, shim/setup, and tests. Never treat Claude-hook registration as proof of
Grok delivery.

**Acceptance criteria:**

- A reviewed design fixes the receipt state machine, persistence/recovery boundary, bridge
  ownership, parent/subagent identity rules, and hcom-vs-herder responsibility before code.
- `herder spawn --agent grok --prompt ...` launches through the first-class path, records
  `tool: grok`, pins `GROK_HOME`, preassigns/captures the session id, and delivers doctrine
  via `--rules`.
- Normal autonomy and `--safe` have explicit, tested Grok mappings; `--model` works and
  conflicts with passthrough model flags are refused.
- Initial, idle, busy, duplicate, out-of-order, bridge-restart, auth/rate-failure, compaction,
  resume, fork, and subagent lifecycle cases have receipt/recovery tests.
- Delivery is not reported until the monitor wake is correlated and the message id is
  acknowledged; queued is never blindly resent.
- Observer/transcript uses Grok's full transcript, status remains honest when herdr reports
  unknown, and no cwd-only identity fallback exists.
- A `grok` PATH shim plus setup/doctor/docs land only with working launch support and avoid
  recursion/shadowing.
- Isolated live smoke proves bidirectional messaging without touching production state;
  behavior diff receives cross-family adversarial review and all repository gates pass.

### 3. Decide and document Grok model routing after scored trials

**Type:** decision/doctrine  
**Description:** After first-class delivery is stable, run a small scored trial set across
implementation, review, and research/advisor work. Compare quality, caught defects, latency,
cost/quota behavior, recovery burden, and context fidelity against the existing GPT-5.6,
Opus, and Fable lanes. The owner decides whether Grok becomes an experimental implementer,
third-family reviewer, advisor/research challenger, or explicit-only tool. Update the one
canonical model-doctrine surface and point other docs to it rather than duplicating rationale.

**Acceptance criteria:**

- Trial tasks are bounded, use mandatory independent review, and record model/version,
  reasoning mode, outcome, defects, latency, and quota/auth interruptions.
- At least one review-direction trial is run in each proposed direction before assigning a
  standing reviewer role.
- Owner makes an explicit routing decision, including fallback when Grok auth/quota fails.
- Only the canonical doctrine surface is edited; no contradictory routing prose is copied
  into herder implementation docs.
- Doctrine edit receives intent review and contains no credential, run-only identifier, or
  unsupported performance claim.

## Bottom line

Grok is already on the box and is a strong CLI fit. The blocker is not model access or TUI
launch; it is honest inbound delivery. Ship the isolated roster/pane demo immediately if the
owner accepts that limitation. For production, treat Grok as its own delivery family and
build the already-characterized monitor + MCP bridge instead of leaning on misleading
Claude-hook compatibility.

---

## ERRATUM (2026-07-12, post-demo — supersedes the "zero-diff degraded path" gains)

The isolated demo (see grok-demo-report-2026-07-12.md) FALSIFIED the expected degraded-
path gain "Grok's inherited Claude hooks may register it on the isolated hcom bus".
Empirical result with grok 0.2.93 + hcom 0.7.23: all four Claude-compatible hooks
(SessionStart/UserPromptSubmit/PostToolUse/Stop) exit 0 against the real hcom binary,
yet hcom creates NO roster row and assigns NO bus name — there is no misleading
tool:claude row, and therefore no address for outbound or inbound messaging. The
zero-diff path yields a PANE demo only. Additional environment truths the first-class
design must absorb: (1) raw-agent login shells reset HOME and drop spawn-time env —
child-side wrappers (or first-class env injection) are required; (2) in herder panes,
`hcom` resolves to tools/herder/shims/hcom, which routes hook calls through `herder
hook`, not raw hcom; (3) Grok 0.2.93's settings-level env.HCOM override is ineffective —
only a direct child env export reached the real binary; (4) a successful hook exit must
never be equated with registration or delivery capability; (5) update suppression:
`--no-auto-update` flag and `[cli] auto_update = false` are both documented and worked.
