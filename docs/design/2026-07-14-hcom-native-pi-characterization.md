# hcom-native Pi integration characterization

Date: 2026-07-14  
Task: TASK-199  
Subjects: hcom 0.7.23 and `@earendil-works/pi-coding-agent` 0.80.6  
Status: investigation complete; no production integration code changed

## Decision

**Keep the custom DR-2 inbound state machine. The Pi design stands unchanged.**

hcom's native Pi integration is real and works with Pi 0.80.6. It is a useful
interactive convenience integration: it installs a Pi extension, binds a stable hcom
identity to the Pi session, wakes an idle agent promptly, queues a busy delivery as a
follow-up turn, preserves event order, reports useful status, parses the transcript,
and resumes the same session and hcom name.

It is not an adequate delivery owner under herder's launch contract. Its receipt is
the successful call to `pi.sendUserMessage`, not evidence that the corresponding Pi
turn settled. The probe killed Pi after that receipt and before the turn completed;
the hcom cursor remained acknowledged, resume did not replay the item, and the
transcript reported the request with no response. The integration also has no durable
delivery journal, ownership epoch, progress-attested driver lease, or capability
separation, and it inherits the launcher's ambient environment rather than enforcing
provider-scoped credentials.

Consequently, native delivery cannot replace the journal, fencing, recovery, and
authority machinery in [Pi first-class design](pi-first-class-design.md#dr-2--inbound-delivery-state-machine-and-recovery).
The native extension is compatibility evidence for the Pi extension API and a useful
reference implementation, but it should not become the production delivery boundary.
Everything outside DR-2 remains unaffected, as TASK-199 anticipated.

## Section 0 evaluation

This is the filled evaluation required by
[New-harness onboarding §0](../new-harness-onboarding.md#0-before-anything-does-hcom-already-integrate-this-harness).

| Question | Finding | Verdict |
|---|---|---:|
| Is Pi advertised? | `hcom --help` lists `pi` for launch, resume, and fork. The installed README's supported-tools table says `Pi | automatic | hcom pi`. | Yes |
| What is installed or wrapped? | hcom writes a Pi TypeScript extension, creates launch wrappers, runs Pi through `hcom pty pi`, and binds the extension to the reserved process with `hcom pi-start`. | Automatic extension + wrapper |
| Does it work with current Pi? | hcom 0.7.23 successfully launched and bound Pi 0.80.6 with the extension loaded. | Yes, with constraints below |
| Idle wake | Two messages sent to an idle Pi woke it immediately and produced an automatic model turn. | Pass |
| Busy delivery | A message sent while a 12-second tool was active was queued as a separate follow-up user turn after the current answer. It was not injected between tool calls in that turn. | Partial |
| Ordering | Two messages sent 10 ms apart were fetched as one batch in ascending hcom event-id order and answered in that order. | Pass in normal path |
| Duplicate behavior | A burst of two TCP wakes while delivery was in flight produced one visible batch. Deduplication is extension-memory state, not durable state. | Pass in-process only |
| Identity and status | The generated name, Pi session UUID, cwd, transcript path, active tool status, and listening status were reported correctly. Resume reclaimed the same name and UUID. | Mostly pass |
| Transcript | While the resumed seat was live, `hcom transcript` parsed the Pi JSONL faithfully, including the ordered idle batch, the busy follow-up, and the crashed request as `(no response)`. A stopped-seat lookup briefly lost the Pi parser classification. | Mostly pass |
| Crash and restart | A crash after injection-time ack lost automatic completion and replay. Resume restored context but did not restart the interrupted turn. | Fail for durable delivery |
| Credential hygiene | The isolated Pi `auth.json` stayed empty and no credential value was persisted. The credential name appeared only in hcom's forwarded-key log. However, native launch forwards ambient non-hcom variables rather than enforcing a provider allowlist. | Mechanically private, not policy-scoped |
| Lifecycle authority | hcom reserves, launches, binds, resumes, and cleans up the process identity. There are no herder epochs or capability lanes. | Fail for DR-2 |

## Probe setup and isolation

The probe used only scratch state:

- an isolated `HOME`, `HCOM_DIR`, `PI_CODING_AGENT_DIR`, npm prefix/cache, XDG
  roots, Pi project, and hcom database;
- an isolated install of `@earendil-works/pi-coding-agent@0.80.6`;
- hcom 0.7.23 from the pinned local installation;
- Node 24.18.0 and npm 11.16.0;
- `PI_OFFLINE=1`, telemetry disabled, explicit cwd, and no approval prompts;
- one provider credential supplied by environment **name only**. No credential value
  was printed, copied, or written into this report.

No probe read or wrote the live hcom database, live registry, or live Pi home.

The evaluated hcom source was release 0.7.23 at commit
`4cef94de232ca41ad23ce1b192bb9c6e761ece5f`. Source paths and line references below
refer to that commit.

## Launch mechanism and compatibility

The native path has four layers:

1. `hcom pi` reserves an hcom process/name and generates private launch state plus a
   visible wrapper. Forwarded environment values travel through a temporary 0600
   sidecar that is sourced and removed; the 0755 wrapper does not contain them.
2. hcom writes its bundled extension to Pi's `extensions/hcom.ts`. The extension is
   replaced when its contents differ from the bundled source.
3. The generated command runs Pi under `hcom pty pi`. The extension invokes
   `hcom pi-start` with Pi's session UUID and the hcom process id, starts a loopback
   TCP notification server, injects hidden bootstrap doctrine, and reports lifecycle
   status through Pi events.
4. On a bus wake, the extension runs `hcom pi-read`, formats the unread batch, and
   calls `pi.sendUserMessage`. It uses `deliverAs: "followUp"` when Pi is busy.

This loaded and bound successfully against Pi 0.80.6. The roster showed
`tool: pi`, `hooks_bound: true`, the correct cwd and transcript, and the same Pi UUID
that appeared in the session JSONL.

Three operational constraints surfaced:

- The directory containing the hcom executable must be on `PATH`; the extension uses
  `spawn("hcom", ...)` (`src/pi_plugin/hcom.ts:39`). Without it, the extension emits
  `spawn hcom ENOENT`, never binds, and reports no session id.
- Extension placement couples `HCOM_DIR` and `HOME`. `PI_CODING_AGENT_DIR` is honored
  only when hcom's derived tool-config root equals `HOME`; otherwise the plugin is
  written below the tool-config root (`src/hooks/pi.rs:350-362`). A naively separated
  scratch `HCOM_DIR` and Pi home therefore installs the extension where Pi does not
  load it. Using `HCOM_DIR=$HOME/.hcom` makes the documented Pi override work.
- `PI_CODING_AGENT_SESSION_DIR` is deliberately cleared for the child
  (`src/shared/tool_detection.rs:174-178`) and is not an hcom Pi instance-state
  variable (`src/integration_spec.rs:884`). The explicitly supplied session root was
  absent; Pi stored sessions beneath `PI_CODING_AGENT_DIR` instead. This conflicts
  with a launch contract that expects both roots to be pinned independently.

There is no explicit compatibility gate for Pi versions. Current compatibility is
therefore empirically established for the exact 0.80.6 pairing, not promised for a
range.

## Inbound delivery truth

### Idle wake, order, and normal-path deduplication

Two informational messages were sent 10 ms apart to an idle seat. The loopback wake
arrived immediately. Pi received one user entry containing hcom events 6 then 7 and
the model replied in that order. A second wake that arrived while the first delivery
was in flight was logged as skipped and replay-scheduled, but it produced no duplicate
user entry.

The deduplication uses the extension's in-memory `deliveryInFlight`, `pendingAckId`,
and queued-wake flags (`src/pi_plugin/hcom.ts:80-97, 206-279`). It protects a running
extension from a wake burst. It is not a persisted message-id ledger and gives no
cross-crash exactly-once-ish guarantee.

### Busy delivery is a follow-up turn

Pi was asked to run a 12-second command and then answer `LOCAL-DONE`. While the tool
was active, hcom event 22 arrived. The transcript order was:

1. local user prompt;
2. assistant tool call;
3. tool result;
4. assistant answer `LOCAL-DONE`;
5. new hcom user turn for event 22;
6. assistant answer `MIDTURN`.

The source matches the observation: an idle context gets plain `sendUserMessage`,
whereas a busy context gets `sendUserMessage(..., { deliverAs: "followUp" })`
(`src/pi_plugin/hcom.ts:220-232`). The README's generic “injected between tool calls”
claim is therefore not the observed Pi behavior. Pi delivery is safe and prompt at a
turn boundary, but it does not alter the already-running turn's context.

### Receipt precedes settled delivery

The extension sets `pendingAckId`, calls `sendUserMessage`, and immediately runs
`pi-read --ack --up-to` (`src/pi_plugin/hcom.ts:220-276`). The `agent_end` handler is
used only to report listening status and drain more messages
(`src/pi_plugin/hcom.ts:440-455`); it is not the receipt boundary.

The logs confirmed this ordering. The idle batch's unread cursor advanced seconds
before the model completed. The busy event was likewise acknowledged when queued as
a follow-up, before its later model turn settled.

## Crash and resume probe

The decisive probe sent event 26 to an idle seat, asking Pi to run a 15-second tool
before responding. The observed sequence was:

1. the extension injected event 26 and logged `plugin.deferred_ack` up to 26;
2. Pi began the requested tool;
3. the Pi process was killed before tool or assistant completion;
4. hcom reported zero unread items and removed the live roster row;
5. `hcom r muna --go` reclaimed the original hcom name and Pi session UUID;
6. the restored transcript contained event 26 and the interrupted tool call, but Pi
   did not resume the tool or start an automatic response;
7. `hcom transcript muna --json` reported that exchange as `(no response)`.

This is neither message loss from the transcript nor duplicate delivery. It is the
more important failure for DR-2: **the durable bus receipt claims completion that the
runtime evidence cannot support**. Since the unread cursor is already advanced, a
restart has no hcom item to replay. Human or orchestrator intervention must invent a
new prompt to continue.

Resume otherwise showed useful fidelity: the same name, UUID, cwd, and transcript
were recovered. Immediately after resume the roster transiently reported
`blocked: launch_blocked` despite `hooks_bound: true`; later Pi events can refresh
status, so this is an observability blemish rather than the decisive delivery gap.

## Credential and lifecycle findings

The narrow mechanical hygiene was good in the isolated run:

- Pi's `auth.json` remained an empty 0600 file;
- the provider secret was not found in Pi state, session JSONL, generated visible
  launch scripts, or hcom logs;
- the environment variable **name** appeared in a `forwarded_keys` diagnostic, but
  not its value;
- temporary forwarded values used a 0600 sidecar that was deleted after sourcing.

The policy boundary required by the Pi design is still absent. hcom builds the child
environment from its caller's ambient non-hcom environment and privately transports
those values. Privacy of transport is not provider scoping. A correctly constructed
caller can supply only one provider credential, as this probe did, but native hcom
does not enforce that allowlist and the Pi process plus model tools inherit the result.

Lifecycle ownership is also the wrong shape for production. The native integration
expects hcom to reserve and launch the process and gives the extension broad access to
the ordinary hcom CLI. It has no monotonic seat epoch, activation fencing, durable
armed-driver lease, token-authenticated extension lane, or separately presented
operator lane. Letting herder invoke this launcher would divide lifecycle authority;
reproducing hcom's private process binding from a direct herder launch would itself be
custom integration work without fixing the receipt semantics.

## Gap analysis against DR-2

| DR-2 property | Native hcom Pi | Consequence |
|---|---|---|
| Durable queued → injected → settled journal | Only hcom unread cursor plus Pi transcript | Ack can survive while response does not |
| Receipt correlated to Pi turn settlement | Ack immediately after `sendUserMessage` | Crash window is falsely complete |
| Exactly-once-ish recovery | In-memory wake dedupe only | No durable replay decision or completion proof |
| Ordering | Ascending unread batch order | Sufficient in the normal running process |
| Crash replay | None after injection-time ack | Interrupted item requires a new external prompt |
| Epoch fencing | None | Old and new runtime ownership is not structurally fenced |
| Armed driver / progress lease | TCP server plus polling, no persisted lease | Readiness is historical bind state, not progress-attested |
| Provider credential scope | Ambient inheritance, private sidecar | Correct scope depends entirely on caller discipline |
| Token and operator capability lanes | None | Model-visible hcom access is not separated from lifecycle authority |
| Herder lifecycle authority | hcom owns reserve/launch/bind/resume cleanup | Cannot be adopted unchanged under one lifecycle owner |

## Implementation consequence

Proceed with Pi U1 against the existing design:

- keep the managed Pi home, pinned install, provider pinning, and herder launch
  contract;
- keep DR-2's durable journal, settlement-correlated receipts, epoch fencing, driver
  lease, spool bounds, crash recovery, and capability lanes;
- use the native extension only as evidence that Pi 0.80.6 supports the necessary
  extension lifecycle, `sendUserMessage`, status, and transcript seams;
- do not claim native hcom “mid-turn injection” for Pi; describe its actual behavior
  as idle wake plus busy follow-up delivery.

No design amendment follows from TASK-199.
