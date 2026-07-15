# Herder mission membership

## Decision

Herder session snapshots carry at most one explicit mission membership. The
effective membership remains a list-time view: an explicit value from the
latest session snapshot wins; otherwise herder resolves mission context from
that snapshot's provenance cwd with mish-compatible cwd and `.mission` marker
semantics.

This keeps mission files herder-unaware, gives mission-control one stable JSON
shape, and keeps every registry mutation on the existing locked write path.

## `list --json` shape

Every rendered session row has a `mission` field. A resolved membership is:

```json
"mission":{"slug":"checkout-reliability","source":"explicit"}
```

The closed `source` vocabulary is:

- `explicit`: recorded by `join` or by spawn-time sugar;
- `cwd`: inferred because the recorded cwd is inside `missions/<slug>/`;
- `marker`: inferred from the single applicable `.mission` ancestor marker.

When no membership resolves, the field is:

```json
"mission":null
```

Mission-control groups on `mission.slug` and may use `mission.source` for
explanation. It does not need to understand registry events.

Precedence is evaluated while rendering `list --json`, not stored as a resolved
answer. The renderer first checks the latest snapshot's explicit membership.
Only when that field is absent does it run mish-compatible resolution against
`provenance.cwd`. This preserves two useful properties: changing or removing a
marker affects inferred rows without registry rewrites, while an explicit join
does not drift when cwd or markers change. An inference refusal is row-local and
renders `mission:null`; it does not suppress the session row or the rest of the
list.

## Registry representation and write flow

The durable explicit value on a v2 session snapshot is:

```json
"mission":{"slug":"checkout-reliability","source":"explicit"}
```

`source` is fixed to `explicit` in durable rows. Inferred values are view data
and are never written to the registry.

`join` appends a complete successor session snapshot with event
`mission_joined`, a fresh `recorded_at`, and the explicit `mission` value.
`leave` appends a complete successor session snapshot with event
`mission_left`, a fresh `recorded_at`, and no `mission` field. The projected
latest snapshot therefore has no explicit membership and list rendering
returns to inference. There is no null-valued membership, tombstone object, or
anti-membership value.

Both mutations resolve the current target and build the successor inside
`registry.UpdateLocked`. That existing path supplies the file lock, current
projection, node-authority validation, append, fsync, and rotation behavior.
No command-specific file writer is introduced.

Session-row succession is the right vehicle because the registry projection is
already one latest full session snapshot per guid. A separate membership kind
would require a second projection and cross-entity consistency rules, and
would no longer be a normal session mutation through the established writer.
Membership is added to the registry's central snapshot carry-forward rules so
ordinary successors such as seat reconciliation, rename, cull, and observer
enrichment preserve it. Only `mission_left` deliberately clears it. Join and
leave copy lifecycle state, seat, identity, capabilities, lineage, and
provenance unchanged, so the toggle has no seating, label, bus, or continuity
side effect.

The append-only stream provides the audit trail: `mission_joined` records the
slug and `mission_left` records the explicit clearing. Registry rotation keeps
the byte-faithful pre-rotation stream in its normal archive and reseeds the
live file with the latest snapshot, matching every other session event's
history contract.

### Lifecycle carry decisions

Membership follows the running agent identity, not every lineage edge. The
normal rule is “same guid carries”; transitions that mint a new guid make an
explicit transfer decision. Registry tests pin every successor-producing
transition so a new event cannot silently choose semantics by omission.

| Transition / event | Guid relationship | Mission decision |
|---|---|---|
| Initial spawn or enroll (`registered`/`seated`) | New guid | No implicit membership. The later spawn-time sugar may put an explicitly validated value on the initial row. |
| Re-enroll or bus/capability enrichment (`registered`) | Same guid | Carry. These only refresh the same identity's seat or transport facts. |
| Recognition and reconciliation (`recognised`, `reconciled`) | Same guid | Carry. Observer and sidecar enrichment must not change declared membership. |
| Cull, dead-seat observation, or lifecycle cleanup (`unseated`) | Same guid | Carry. Unseating is dormancy, not retirement or leave. |
| Rename (`labelled`) | Same guid | Carry. A display-address change does not change membership. |
| Atomic label transfer (`label_transferred`) | Two existing guids | Each guid carries its own membership; membership does not follow the transferred label. |
| Retire and reopen (`retired`, `reopened`) | Same guid | Carry. `list --all` retains historical attribution, and reopening deliberately revives the same identity. |
| Resume (`registered`, followed by enrichment) | Same guid | Carry. Resume re-seats the same durable identity. |
| Compact | No successor row by itself | No change. If later recognition refreshes the same guid, the recognition rule carries membership. |
| Fork | New child guid with `forked_from` lineage | Do not inherit. A fork is a separately commissioned agent and must explicitly join or infer from its own recorded cwd. The parent keeps its membership. |
| Adoption source release (`adoption_source_released`) | A seated old guid is superseded | Clear on the old guid. The replacement enrolled guid does not inherit: adopting a foreign/restarted process proves label and seat replacement, not mission consent. It may explicitly join or infer from its own cwd. |
| Adoption from an already-unseated source (`label_transferred`, then `retired`) | Old guid stays historical; replacement has a new guid | The old guid retains historical membership through retirement; the replacement still does not inherit. The generic label-transfer event intentionally cannot smuggle membership between guids. |
| Observer-detected clear/turnover (`unseated` old plus `registered` child with `cleared_from`) | New guid replaces the occupant in the same seat | Transfer: the child receives the explicit membership and the displaced old guid clears it. This is the one cross-guid carry because the observer has proved in-place session turnover rather than commissioning another agent; clearing the old side prevents duplicate grouping. |
| Join (`mission_joined`) | Same guid | Set the validated explicit value. |
| Leave (`mission_left`) | Same guid | Clear the explicit value; inference becomes effective. |

Rows with an unrecognised future event retain the conservative same-guid carry
rule. A future transition that creates a new guid must opt into transfer using
an explicit lineage rule and add a row to this table plus tests.

## Command surface and refusal matrix

```text
herder join <mission-slug> [--target <guid|short-guid|label|pane>]
herder leave [--target <guid|short-guid|label|pane>]
```

Without `--target`, both commands use the caller's `HERDER_GUID`. A mutation
requires the locked latest row to be `seated`; an unseated, retired, lost, or
missing row is not an already-running agent.

| Situation | Result | Typed cause | Remedy |
|---|---|---|---|
| Slug is malformed | Refuse, no write | `invalid_mission_slug` | Use lowercase letters, digits, and single hyphens; no trailing hyphen; at most 64 characters. |
| `$MISSIONS_REPO` is unavailable for explicit slug lookup | Refuse, no write | `missions_repo_unset` | Set `MISSIONS_REPO` to the shared missions repository. |
| `missions/<slug>` does not exist | Refuse, no write | `mission_not_found` | Check the slug or create the mission. |
| Neither `--target` nor `HERDER_GUID` identifies a caller | Refuse, no write | `caller_identity_missing` | Run inside an enrolled session or pass `--target`. |
| Target does not resolve in the locked projection | Refuse, no write | `session_not_found` | Check `herder list --json` and retry with its guid or label. |
| Target resolves but latest state is not `seated` | Refuse, no write | `session_not_live` | Start or resume the agent, then retry. |
| Target has no explicit membership | Join and append `mission_joined` | — | — |
| Target is already explicitly joined to the same slug | Succeed as an idempotent no-op; append nothing | — | No action is needed. |
| Target is explicitly joined to another slug | Refuse, no write | `already_joined` | Run `herder leave` for the target, then join the new mission. |
| Leave target has explicit membership | Leave and append `mission_left` | — | — |
| Leave target has no explicit membership | Succeed as an idempotent no-op; append nothing | — | The row is already using inference. |

Refusal output names both the cause and remedy. Usage errors remain exit 2;
typed operational refusals exit 1; applied and idempotent operations exit 0.

## Spawn-time reuse

Spawn-time `--mission` reuses all three join primitives:

1. the same slug validator and mission existence lookup;
2. the same typed explicit membership value (`slug` plus `source: explicit`);
3. the same registry normalization and central snapshot carry-forward rules.

The spawn transaction places that value on the initial session snapshot written
through `registry.UpdateLocked`; it does not call a second writer or create a
separate membership record. A failed spawn never reaches the successful initial
snapshot append, so it leaves no membership residue. Post-hoc join differs only
in appending the `mission_joined` successor event to an existing seated row.
