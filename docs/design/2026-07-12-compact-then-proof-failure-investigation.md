# Compact-then unknown-status proof failure

## Verdict

The detached sender did not lose the target during compaction. It was armed
with the wrong bus identity before compaction began.

The affected Claude session's live identity was `lale`, but both preserved
registry rows for that session carried `hcom_name=viru`. `compact --then`
faithfully copied that poisoned field into the detached child's `--name`
argument. Every subsequent status and event query was therefore scoped to
`viru`, which was not joined. `hcom list viru --json` failed as not found and
`hcom events --agent viru` succeeded with empty output. The sender normalized
the former to unknown status and the latter to a trusted watermark of zero,
then repeated those observations until its 15-minute deadline.

This is the concrete impact of the already captured registry defects in
TASK-043 and TASK-065, plus two compact-then-specific defects: the event
fallback is gated on a successful live status sample, and detached failures
are not surfaced outside their diagnostic log.

## Evidence chain

1. Both failure logs have the same signature and target:
   `@viru`, arm event `#0`, `last status="unknown"`, `saw_active=false`,
   `event_proof=true`, timeout after exactly `900000ms`. Their registry GUIDs
   are the original and successor rows for the same session (`a9fcee3d` and
   `8f1d10a3`).
2. The preserved registry history identifies both rows as the `lale`
   orchestrator's session and records `seat.hcom_name="viru"`. The successor
   row continued carrying that value through hourly reconcile events even
   after the original row was unseated.
3. The session's own transcript explicitly recorded the mismatch before one
   compact attempt: “I am @lale on the bus” while its current row “STILL
   carries wrong bus name viru”; it also stated that `--then` was unusable
   until the bind was fixed.
4. The launch wrapper for this resumed Claude process exported
   `HCOM_INSTANCE_NAME=viru`. The original row's provenance is `enroll`, and
   `enrollcmd.Run` writes `HcomName: os.Getenv("HCOM_INSTANCE_NAME")` without
   consulting the live bus. After the process reclaimed `lale`, its launch
   environment remained `viru`; enrolling minted the poisoned coordinate.
   This names the original write path, rather than merely observing later
   carry-forward.
5. Current bus queries reproduce the two child observations exactly:
   `HCOM_DIR=/home/grace/.hcom hcom list viru --json` exits nonzero with “Not
   found”, while `hcom events --agent viru --all` exits zero and emits no
   events. The retained history contains events for `lale`, not `viru`.
6. Of the 18 retained compact-then logs, all 16 successes armed with a nonzero
   watermark and observed active to listening. The only two failures are the
   two poisoned `viru` rows, both at watermark zero.

There is no evidence of a pane re-key, compaction restart, tracker enrollment
gap, or same-second watermark boundary causing this incident. The child never
uses pane coordinates. It queried a stopped, wrong bus name for the entire
window.

## Why event history did not rescue it

The timeout field `event_proof=true` is misleading. It means only that
`maxEventID` returned `ok=true`; it does not mean that a turn-end event was
found. Empty output is deliberately treated as the trusted pair `(0, true)`,
so an unknown agent looks like a valid agent with empty history at arm time.

The exact delivery predicate is:

```text
status == "listening" &&
  (sawActive || (snapshotOK && turnEndedSince(name, watermark)))
```

Because `listStatus("viru")` was always unknown, `turnEndedSince` was never
called. Even if the outer live-status gate were removed, querying events for
`viru` would still not find the real `lale` turn end. The strict `event.ID >
watermark` comparison did not exclude a matching event; there was no matching
event under the captured coordinate.

The outer gate is nevertheless a separate proof-path defect. A valid post-arm
listening event is sufficient proof by itself, but the implementation refuses
to inspect it while live status is unknown. A hermetic characterization test
now pins that behavior.

## Hermetic reproduction

`compactthen_test.go` contains two new virtual-clock tests:

- `TestThenLoopWrongBusCoordinateStallsUnknownDespiteRealTurnEnd` publishes a
  real post-arm listening event under the live identity while the config uses
  a stale identity. It reproduces arm event zero, unknown status,
  `saw_active=false`, `event_proof=true`, and fail-closed timeout with no
  delivery.
- `TestThenLoopUnknownStatusDoesNotConsultAvailableEventProof` demonstrates
  that even a valid post-arm event under the queried identity is ignored when
  the live status probe stays unknown.

Both run without live state, subprocesses, or wall-clock delay.

## Discoverability

The drop is not operationally discoverable without remembering and reading
the sender log.

- The initiating `herder compact` process exits successfully after arming and
  only prints the future log path.
- The detached child writes its timeout and excellent manual recovery command
  only to that file.
- No durable job/result record is projected into `herder list`, no status
  command inventories pending/failed continuations, and no observer finding
  is emitted.
- The compacted session receives nothing and simply remains dormant. Its
  operator learns only by noticing the dormancy or opening the log.

The manual remedy is useful once found, but it does not satisfy discoverability.

## Filed-ready fix captures

### 1. Verify live self bus identity before arming compact-then

**Description:** Registry rows created by enroll can contain a launch-time
`HCOM_INSTANCE_NAME` that no longer names the current session. Resolve the
caller's live bus row using the current tool session/process identity and
namespace. At minimum, refuse `--then` before typing `/compact` when the stored
name is not joined or cannot be proven to belong to self. Do not accept
nonempty `hcom_name` as proof. Fold the upstream writer/carry repair into
TASK-043 and TASK-065; keep the compact preflight as defense in depth because
a wrong-but-live name could misdeliver rather than merely time out.

**Acceptance criteria:**

- After a session changes from launch identity A to live identity B, enroll
  records B or refuses with the mismatch and a repair command.
- Successor/reconcile rows do not carry an unverified A forward.
- `compact --then` refuses before `/compact` is typed when its row says A but
  the current session is provably B, or when A is not joined.
- A wrong-but-live neighbor identity is rejected; no continuation can be sent
  to that neighbor.
- Hermetic tests cover stale env, stopped wrong name, and live wrong name.

### 2. Make event history an actual independent proof path

**Description:** Check `turnEndedSince` whenever the arm watermark is trusted,
not only when the latest live status equals listening. Preserve the strict
post-arm event-ID comparison. Distinguish “snapshot established” from “event
proof found” in logs, and distinguish unknown-agent empty output from trusted
empty history when hcom offers enough information.

**Acceptance criteria:**

- Unknown live status plus a matching post-arm listening event proves turn end
  and delivers once.
- Unknown live status without a matching event still fails closed.
- Pre-arm and same-watermark events never deliver.
- Logs report `snapshot_established` and `turn_end_event_found` separately;
  they do not call a zero watermark `event_proof=true`.

This fix would not by itself repair the `lale`/`viru` incident, because its
event query was also aimed at the wrong identity. It does repair the promised
fallback for genuine live-status outages.

### 3. Surface failed detached continuations as durable operator state

**Description:** Persist the detached sender lifecycle (armed, delivered,
queued, failed with recovery command) under herder state and expose unresolved
failures through an existing high-visibility surface such as `herder list`
and observer findings. A log remains diagnostic detail, not the only signal.

**Acceptance criteria:**

- A timeout or exhausted send budget creates a durable failed-continuation
  record containing target, timestamp, reason, log path, and manual recovery.
- `herder list` or an equally routine command shows unresolved failures without
  scanning log files.
- The observer emits a finding for an unresolved failure and clears it only
  after explicit acknowledgement/recovery.
- Success and queued delivery close the record without a false warning.

## Timeout-default verdict

Do not change `defaultThenTimeout` for this failure class. It is already 15
minutes, not four. Both retained failures spent the full 900,000 ms querying a
coordinate that could never resolve, while a separately reported four-minute
arm-to-compaction interval was well inside the current bound. Increasing the
timeout only delays discovery; decreasing it without fast-fail identity checks
would increase false drops for legitimately long turns. Keep 15 minutes as
the outer safety budget, add immediate identity/preflight failure, and arm
`compact --then` as the final tool call so ordinary arm-to-fire latency stays
short.
