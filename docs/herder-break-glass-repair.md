# Herder break-glass identity repair

`herder repair` is the manual recovery path for a seated row whose bus name,
recorded tool session id, or hcom launch context cannot pass the ordinary repair
proof. It does not weaken automated enrollment or reconciliation. Use it only
after inspecting `herder list --all --json` and identifying one exact durable
guid.

The trust claim is deliberately limited: this is a named, logged action by the
OS account controlling the target pane. A process running as the same uid can
complete the ceremony. The registry entry is a normal-path audit record, not a
tamper-evident forensic seal.

## Commands

```text
herder repair rebind --guid GUID --field hcom_name --value NEW_NAME
herder repair rebind --guid GUID --field sid --value NEW_SESSION_ID
herder repair rebind --guid GUID --field launch_context --value LIVE_PANE_ID
herder repair reissue-credential --guid GUID
```

Only `hcom_name`, `sid`, and `launch_context` are accepted. The command cannot
change a registry seat, pane/terminal coordinate, label, role, lineage, or guid.
One invocation changes one field, or performs credential reissue without an
identity-field change.

## Interactive ceremony

The verb requires a controlling `/dev/tty`; flags and piped stdin cannot supply
the attestation. It prints a random challenge and reads the target pane through
`herdr pane read --source visible`. The operator must:

1. Switch to the target pane and place the challenge verbatim in its visible
   composer without pressing Enter. Typing is safest when an agent UI collapses
   bracketed paste into a placeholder.
2. Leave it visible for two reads. A scrollback occurrence never counts.
3. After the verb reports that it observed the challenge, remove it manually
   without submitting it and return to the original tty.
4. Type the exact `ATTEST …` sentence printed by the verb.

The repair command never pastes, sends keys, presses Enter, or clears the target
pane. That keeps the command itself read-only during corroboration. The operator
action still has a hazard: pressing Enter can submit the nonce as an agent
prompt, while automatic clearing can destroy an existing draft. If either may
have happened, abort and inspect the target pane before retrying.

Pane disappearance, terminal-id mismatch, an occupant redraw between the two
visible reads, failure to remove the nonce, missing tty confirmation, or any
changed registry row refuses with no registry mutation.

## Damage-shape recovery table

| Shape | Sequence |
|---|---|
| Stored bus name cannot be corroborated | Rebind `hcom_name`; completion consumes that one attested field and re-verifies every other leg. |
| Recorded sid is wrong or foreign | Rebind `sid`; the old specific binding is tombstoned and completion runs. Ordinary adopt authorization remains unchanged. |
| Launch context is empty | Rebind `launch_context` to the verified live pane. Completion performs the existing merge-missing-only vendor-db backfill. |
| Launch context is wrong and nonempty | The value is never rewritten. The verb records authorization, then instructs the operator to leave/stop the wrong vendor row, rejoin under the same name from the verified live pane, and run completion. |
| Registry seat coordinates are wrong | This field is outside break-glass vocabulary. Use enroll/adopt/reconcile to re-seat; repair bus/sid/context first only if those proofs block that corridor. |
| Seat credential is lost | Run `reissue-credential`. This authenticates from attestation plus pane control and runs shared completion without rebinding identity. Credential generation and token-file rotation are attached to this completion commit point by the credential stage. |

The recorded season terminal states use those same rows of the table:

- A bus-name-unrecoverable row is the first sequence directly.
- A duplicate seated-row aftermath is repaired by pinning the rightful guid,
  repairing only its blocking bus/sid field, then running the ordinary
  reconcile/re-seat corridor to detach the duplicate. Break-glass never edits
  the second row.
- When a retired row historically owns the live sid, rebind the current seated
  guid's `sid`; current seated ownership then dominates the retained retired
  history.
- A wrong-nonempty vendor pane follows the authorization/recreate row and may
  end at the documented upstream gate below.

For a wrong-nonempty launch context, hcom's reclaim guard can refuse the rejoin
while returning success. That shape is upstream-gated; the verb reports it
honestly instead of claiming termination. Follow the owner-approved database
recovery in [the direct-CLI identity hazard](hazards/agent-cli-identity-hijack.md#recovery-recipe-proven-2026-07-15-owner-approved-class), including a SQLite
backup before editing the latest stop-event snapshot.

## Rate limit and audit record

One successfully committed break-glass operation is allowed per guid in a
rolling ten-minute window across all repair operations. A refusal names the
limit and remaining time. Failed proof attempts do not consume the window, so a
typo or redraw cannot lock out the legitimate recovery; every attempt is still
loud on stderr.

A rebind appends one full `attested_binding` registry snapshot containing the
attestation, the new `attested` binding, and a tombstone naming the exact durable
binding id it supersedes. History remains readable and survives registry
rotation. Launch-context recreate authorization and credential reissue append an
attestation without pretending those values are registry identity bindings.
