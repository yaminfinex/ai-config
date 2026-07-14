# Flagship hcom-native crash / parity characterization (Claude + Codex)

Date: 2026-07-14
Subjects: hcom 0.7.23 (Claude CLI 2.1.209, Codex CLI 0.144.1)
Companion: [hcom-native Pi characterization](2026-07-14-hcom-native-pi-characterization.md)
Status: investigation complete; no production integration code changed

## Answer

**Yes — the flagships have the same crash window native Pi has, and it is the same
code path.** In one reproduced crash run per harness, a message delivered to an idle
Claude or Codex seat advanced hcom's unread cursor to the message's event id at hook
**injection** time — before the requested tool even started and well before the model
turn settled. A `SIGKILL` before settlement removed the live roster row; resume
recreated the same hcom name and tool session at the already-advanced cursor; the
request was left stranded in the tool transcript with no response; there was zero
unread, no replay, and no automatic continuation. Recovery required a human/
orchestrator re-prompt. This is the identical failure the Pi record reproduced, and
the source shows all three integrations (Claude, Codex, and — by analogy — Pi) commit
the delivery receipt at message injection through the **same** `commit_delivery_ack`
boundary, not at turn settlement. The flagships therefore already run, and have run,
on exactly the receipt placement DR-2 was written to replace.

Two honesty caveats stated up front: (1) each crash result below is **one reproduced
run** for the exact version pair, not a proof of "always"; (2) the flagships are
*richer* than Pi on busy delivery (they inject mid-turn where Pi defers to a follow-up
turn), so "same bar" is precise about the crash/receipt window, not about every
behavior.

## Evaluation — native Claude

Claude CLI 2.1.209, model `haiku`, reasoning low. One disposable seat, isolated bus.

| Question | Finding | Verdict |
|---|---|---:|
| Idle wake | A message to an idle seat woke it immediately; the model produced an automatic turn and answered the requested marker (~6–7 s end-to-end on this model). | Pass |
| Busy delivery | A message that arrived while a 15 s `sleep` tool was running was injected by the `PostToolUse` hook **into the still-running turn** and changed that turn's answer (the model emitted the injected instruction's token instead of the originally requested one). It did not open a follow-up turn. | Pass (mid-turn injection; richer than Pi) |
| Ordering | Two messages ~15 ms apart arrived as **one** hook batch in ascending event-id order and were answered in that order. | Pass in normal path |
| Duplicate behavior | Not separately stress-tested beyond the coalesced ordering batch; the delivery loop is a single in-process PTY loop (see source), so concurrent wakes fold into the current unread set. | Pass in-process only |
| Identity / status / resume | The generated name, tool session, cwd, and transcript were reported correctly; resume reclaimed the same name and session. | Pass |
| Crash and restart | Cursor advanced to the request's event id at injection, before the Bash turn settled; `SIGKILL` removed the live row; resume restored the same name/session at the advanced cursor; the request rendered with no response; zero unread; no replay; no automatic continuation. | Fail as shipped (same class as Pi) |
| Lifecycle authority | hcom owns reserve/launch/bind/resume of the process identity; no epoch, lease, or capability lanes. | Fail for DR-2 |

## Evaluation — native Codex

Codex CLI 0.144.1, model `gpt-5.4-mini`, reasoning low. One disposable seat, isolated bus.

| Question | Finding | Verdict |
|---|---|---:|
| Idle wake | A message to an idle seat delivered and advanced the cursor ~115 ms after send; the model answered the requested marker and the turn settled ~5–6 s later. The cursor advanced **before** the turn settled. | Pass |
| Busy delivery | A "change of plan" message that arrived while a 15 s `sleep` tool was running was injected mid-turn (one continuous active turn) and **overrode** the original instruction — the model emitted the injected token instead of the original one. Same shape as Claude; richer than Pi's follow-up turn. | Pass (mid-turn injection) |
| Ordering | Two messages a few ms apart arrived as **one** delivery batch (cursor advanced once) and were answered in ascending event-id order. | Pass in normal path |
| Duplicate behavior | A burst of three messages within ~14 ms produced **one** delivery batch (cursor advanced once), one response, and no duplicate delivery. In-process dedupe only. | Pass in-process only |
| Identity / status / resume | Name, tool session UUID, cwd, and transcript reported correctly; resume reclaimed the same name and session. | Pass |
| Crash and restart | Cursor advanced to the request's event id at injection (before the `sleep` tool started); `SIGKILL` removed the live row; the Codex rollout stranded the crashed turn (user message + reasoning + the `sleep` function-call, **no** output, **no** final response); resume restored the same name/session at the advanced cursor; zero unread; no replay; no automatic continuation. The only recovery signal was hcom's request-watch notifying the **sender** that the seat stopped without responding. | Fail as shipped (same class as Pi) |
| Approval-gate surface (environment) | With `--sandbox workspace-write`, Codex's sandbox launcher was unavailable in this environment, so every command produced a blocking approval prompt (`pty:approval`, seat `blocked`). Switching to `--dangerously-bypass-approvals-and-sandbox` (hcom sandbox mode `danger-full-access`) cleared it. This is an environment artifact, but it is a Codex-specific blocking surface with no Claude equivalent. | Environment note |
| Lifecycle authority | Same as Claude: hcom owns the process identity; no epoch, lease, or capability lanes. | Fail for DR-2 |

## Probe setup and isolation

Every probe used only scratch state and never touched the live hcom database, live
registry, or any live fleet seat:

- an isolated `HOME`, `HCOM_DIR`, tool config dir, hcom database, and scratch project
  git repo per harness;
- hcom 0.7.23 from the pinned local installation; the real tool binary resolved via a
  sanitized `PATH` that deliberately **excluded** the live launcher shims (so no live
  spawner path was invoked);
- disposable models only (`haiku` / `gpt-5.4-mini`), reasoning low — the probe tests
  hcom mechanics, not model quality;
- one provider credential per harness supplied to the isolated tool home by **name
  only**; no credential value was printed, copied, or written into this report;
- crash kills targeted strictly the isolated tool process tree (matched by the scratch
  path in its command line); concurrent live sessions were verified untouched.

The evaluated hcom source is release 0.7.23 at commit
`4cef94de232ca41ad23ce1b192bb9c6e761ece5f`. Source paths and line references below
refer to that commit. The Codex sandbox-launcher unavailability above is an
environment property of the probe host, recorded for reproducibility.

## Source: where the flagship cursor advances

The flagships are PTY tools. hcom delivers to them by injecting text into the tool's
terminal and **verifying** delivery by watching the database unread cursor advance:

- the PTY delivery loop snapshots `cursor_before = db.get_cursor(name)` before
  injection (`src/delivery.rs:1447`), then in `State::VerifyCursor` treats
  `current_cursor > cursor_before` as proof of successful delivery
  (`src/delivery.rs:1850-1870`).

So on the sender side, "delivery succeeded" is defined as "the unread cursor advanced."
The cursor advance itself is performed hook-side, by a boundary that is **shared** by
both flagships:

- the shared commit — `commit_delivery_ack` (`src/hooks/common.rs:289`) — advances the
  cursor by writing `last_event_id` via `update_instance_position` and sets the
  `deliver:<sender>` / active status. The ack token is built *deferred* in
  `prepare_raw_messages` / `prepare_pending_messages` (`src/hooks/common.rs:283-345`);
  its own docstring says it is applied "after hook output is written"
  (`src/hooks/mod.rs:194`).
- **Claude path**: `write_hook_output` (`src/hooks/claude.rs:127-140`) writes the
  injected message to hook stdout, flushes, then calls `commit_delivery_ack`
  (`src/hooks/claude.rs:135`). The delivering hooks are `UserPromptSubmit` /
  `PostToolUse`.
- **Codex path**: `dispatch_result_to_stdout` (`src/hooks/codex.rs:395-428`) writes the
  `additionalContext` hook JSON to stdout, flushes, then calls `commit_delivery_ack`
  (`src/hooks/codex.rs:427`). The delivering hooks are `codex-userpromptsubmit` /
  `codex-pretooluse` / `codex-posttooluse`.

In both cases the cursor advances the instant the message is **injected into the model's
context** (hook stdout flush), strictly before the model turn that will respond to it
settles. This is the same placement the Pi record found for its post-`sendUserMessage`
ack — except Claude and Codex reach it through one shared Rust function rather than the
Pi extension's TypeScript. The empirical crash runs match the source: in each, the
`deliver:<sender>` cursor-advance event preceded the tool-start event, which preceded
the kill.

## Parity table

Rows are the DR-2 property list from the Pi record's gap analysis. `native-pi` is
carried over from that record unchanged (not re-run here). Every cell is evidence-backed
(source `file:line`, or one reproduced probe run) unless marked *unverified*.

| DR-2 property | native-Claude | native-Codex | native-Pi | pi-DR-2-as-designed |
|---|---|---|---|---|
| Durable queued→injected→settled journal | None — unread cursor + tool transcript only | None — same | None — cursor + transcript; a settlement-ack fork could improve the cursor | Yes — durable per-message state |
| Receipt correlated to turn **settlement** | No — ack at hook injection (`claude.rs:135`) | No — ack at hook injection (`codex.rs:427`) | No — ack after `sendUserMessage` | Yes — receipt = settle event |
| Exactly-once-ish recovery | In-process PTY loop dedupe only | In-process dedupe only (3-msg burst → 1 batch) | In-memory wake dedupe only | Durable replay + dup-reconciliation + nudge budget |
| Ordering | Pass — one batch, event-id order | Pass — one batch, event-id order | Pass — ascending batch order | Pass |
| Crash replay | None after injection-time ack (1 reproduced run) | None after injection-time ack (1 reproduced run) | None after injection-time ack (1 reproduced run) | Replay + transcript-vs-bus reconciliation |
| Epoch fencing | None | None | None | Yes — monotonic seat epoch |
| Armed driver / progress lease | None — PTY loop + cursor poll, no persisted lease | None — same | None — TCP + polling, no persisted lease | Yes — progress-attested lease |
| Token / operator capability lanes | None — hook has ordinary hcom CLI | None — same | None | Yes — separated lanes |
| Lifecycle authority shape | hcom owns process bind; herder owns the registry seat via a launch that execs `hcom claude` | Same via `hcom codex` | hcom owns reserve/launch/bind/resume | herder sole lifecycle authority |
| Credential scoping (launch contract, **outside** DR-2) | Mechanically private transport; ambient env forwarded, not policy-scoped | Same | Same | Retained per owner ruling — kept regardless of delivery choice |

Reading the table: on every **delivery/recovery** row (the top five), the two flagships
sit in the same cell as native-Pi. They diverge from DR-2 in exactly the places Pi does.
The bottom rows (epoch, lease, lanes, authority) are *not* delivery properties at all —
no flagship has them, and a better delivery receipt would not add them.

## Costing: "herder wraps `hcom pi` exactly like it wraps Claude/Codex"

Settled inputs (do not relitigate): credential scoping in launch env construction is
**retained** per owner ruling; herder remains the spawner exactly as it is for Claude/
Codex today.

Today's flagship launch contract already is a thin herder-over-hcom wrapper:

- `IsHcomCapable` is the single gate (`tools/herder/internal/launchcmd/launch.go:19`),
  currently `claude | codex | gemini | grok`;
- herder pins the real tool config dir (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
  `GEMINI_CLI_HOME`) via `setEnvDefault` (`launch.go:38-46`), sets
  `HCOM_LAUNCH_INFLIGHT=1`, then `syscall.Exec`s into the PATH-resolved `hcom <tool>`
  (`launch.go:204-212`). hcom then does the PTY launch, hook/extension install, and bus
  bind. Herder owns the registry seat; hcom owns the process binding.

To wrap `hcom pi` the same way, the **additive** work is small and mostly mechanical:

- add `pi` to `IsHcomCapable`;
- pin the Pi-home env var (`PI_CODING_AGENT_DIR`) to its **default** location with a
  `setEnvDefault` line beside the existing three — exactly as `CLAUDE_CONFIG_DIR` /
  `CODEX_HOME` pin the flagships' default vendor homes — honoring the Pi extension's
  placement coupling documented in the Pi record (`HCOM_DIR`/`HOME` must line up or the
  extension is written where Pi will not load it);
- reuse the existing exec-into-`hcom pi` path unchanged.

What flagship-parity **deletes** from the current Pi design (DR-2/DR-3):

- the durable spool journal and queued→injected→settled per-message state machine;
- settlement-correlated receipts (parity accepts the injection-time cursor as the
  receipt, as the flagships do);
- ownership epochs, the progress-attested driver lease, and the capability/control
  lanes;
- the herder-owned TypeScript delivery extension and its replay/nudge policy — replaced
  by hcom's native Pi extension (already demonstrated to load and bind against Pi 0.80.6
  in the Pi record).

What is **kept regardless** of the delivery choice:

- credential scoping in launch env construction (owner-settled: retained);
- herder as spawner and registry-seat owner (owner-settled);
- the launch-contract env coordinates — the Pi-home env var pinned to its default
  location, as `CLAUDE_CONFIG_DIR` / `CODEX_HOME` are for the flagships' default vendor
  homes — plus recorded-version discipline (Pi is install-latest with the exact version
  **recorded**, not pinned; version drift is an accepted, registered weakening). These
  are launch-contract requirements independent of DR-2, exactly as they are for
  Claude/Codex.

Net: the delta to reach flagship parity is a few launch-contract lines plus adopting
hcom's native Pi delivery; the cost is **accepting the flagships' crash window**
(injection-time receipt, no durable replay, human re-prompt on crash) as the Pi bar.

## Recommendation (framing, not decision)

The owner's question was whether Pi should meet the same bar the flagships actually
meet, to avoid building a burden the flagships prove unnecessary. The evidence:

- The flagships meet a **lower** delivery bar than DR-2 — the same injection-time
  receipt and the same crash window as native Pi — and the fleet has run on it for a
  week. On the crash/receipt window specifically, "flagship parity" and "native-Pi
  parity" are the same bar.
- DR-2's distinctive value is **not** primarily a better receipt. Four of its
  properties — epoch fencing, progress-attested lease, capability lanes, sole herder
  lifecycle authority — are absent from *all three* native integrations and are
  orthogonal to where the cursor advances. If those properties are the reason for
  DR-2, the crash-window evidence neither supports nor undermines them.

Three framed options, with the trade each buys:

1. **Flagship-parity Pi.** Wrap `hcom pi` like Claude/Codex; accept the injection-time
   receipt and the crash window; delete the DR-2 delivery machinery. Buys: smallest
   surface, immediate parity with a proven-in-production bar, least burden — the owner's
   stated priority. Costs: on a mid-turn crash a request is silently stranded and needs a
   human/orchestrator re-prompt; no epochs/lease/lanes at all.
2. **DR-2 Pi as designed.** Buys: durable replay, settlement-correlated receipts,
   fencing, lease, and lanes. Costs: the full journal + recovery policy + control-plane
   build, i.e. exactly the burden the owner flagged — justified only if the non-delivery
   properties (fencing/lease/lanes/authority) are independently required.
3. **Middle path.** Take flagship-parity delivery **plus** only the herder-owned
   settlement-ack fork the Pi record describes: move the ack from injection to a settle
   handler so the unread cursor becomes a coarse settlement marker and the demonstrated
   serialized crash-after-ack loss closes — without the full journal, epochs, lease, or
   lanes. Buys: closes the one reproduced window at low cost. Costs: correct correlation
   across busy/follow-up and multi-batch crashes still needs durable state, so this is a
   genuine *partial* fix, not DR-2.

**Recommendation:** if the decision rests on the delivery/crash window alone, the
evidence favors **option 1 (flagship parity)** — it is the bar the flagships already
prove sufficient in production and it directly honors "I really don't want to create a
burden." Choose **option 2** only if the non-delivery DR-2 properties (epoch fencing,
progress lease, capability lanes, sole lifecycle authority) are required for reasons
outside this probe; those are not delivery-receipt questions and this characterization
does not settle them. **Option 3** is the low-regret hedge if the specific silent-strand
crash is the sole worry. This is a recommendation, not a decision.
