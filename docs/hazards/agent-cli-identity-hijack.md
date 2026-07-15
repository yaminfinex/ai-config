# Hazard: direct agent-CLI invocation from an identity-bearing shell hijacks the caller's bus row

Provenance: field incident 2026-07-15 (orchestrator seat lost its live hcom row);
forensics db-verified; recovery executed with owner approval. This memo is the
durable record — the run journal copy is gitignored and dies with the run.

## The failure

Any shell that carries hcom/herder identity environment (`HCOM_PROCESS_ID`,
`HCOM_INSTANCE_NAME`, and related `HCOM_*`/`HERDER_*`/`HERDR_*` vars) will leak that
identity to ANY vendor agent CLI invoked directly from it (`pi`, `grok`, `claude`,
`codex`, ...). The child's hcom extension can honor the inherited identity with no
tool/session continuity check and take over the caller's LIVE bus row **in place**:

- the row's tool/session_id/directory/transcript flip to the child's session —
  no new instance row, no created/started lifecycle event;
- when the child exits, hcom records the stop and **deletes the row**, snapshotting
  the child's facts;
- the rightful owner is then locked out: `hcom start --as <name>` refuses on
  latest-identity mismatch (tool and directory are checked against the thief's
  snapshot), there is no recovery verb, and the refusal exits rc=0.

The observed vector was a "cheap diagnostic probe" (`pi -p ...` run bare from the
orchestrator's shell). The probe itself was the destructive act.

## The rules

1. **Never invoke a vendor agent CLI directly from an identity-bearing shell.**
   Spawn agents through the managed spawner, which constructs the child env.
2. **Direct CLI probes require a scrubbed environment**: `env -i` with an explicit
   PATH/HOME, or at minimum unset every `HCOM_*`, `HERDER_*`, `HERDR_*` var — plus
   isolated HOME/state per standing probe hygiene. Identity env IS state.
3. Treat any unexplained "not participating" / missing own row as a possible
   takeover: check the latest stop event's snapshot for a foreign tool/directory
   before assuming a reap.

## Recovery recipe (proven 2026-07-15, owner-approved class)

1. Consistent backup of the live db first (sqlite `.backup`, never a file copy of
   a hot WAL db); keep a copy outside the hcom dir.
2. The reclaim guard reads the LATEST stop event's `snapshot` for the name. Edit
   that event's JSON in `events.data`: set `snapshot.tool` and
   `snapshot.directory` back to the rightful owner's values. (The events FTS index
   has an insert-only trigger; an UPDATE is safe.)
3. `hcom start --as <name>` then passes. Verify the row (tool, session id, cwd)
   before resuming operations.

## Status of mechanical fixes (the doctrine should not be load-bearing)

Managed herder launches discard every ambient `HCOM_*` value and rebuild only the
child-owned bus scope. Launcher-family tests pin that scrub and separately show
that spawn's pre-exported `HERDER_*`/`HERDR_*` seat context passes through; this
launch boundary does not establish ownership of those values. `ai-doctor` warns
when it runs inside an identity-bearing shell so direct vendor probes are not
mistaken for harmless diagnostics.

Upstream candidates remain: the extension honors inherited cross-tool identity
without continuity checks; the reclaim guard strands the rightful owner with no
recovery verb; the refusal exits rc=0.
