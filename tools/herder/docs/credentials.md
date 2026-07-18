# Per-seat credentials

Herder selects a caller from an explicit, minted credential. Ambient
`HCOM_*`, `HERDER_*`, and `HERDR_*` values may corroborate that already-selected
seat or make an operation refuse; they never select or replace it.

Each successful seat completion creates a fresh random token in:

```text
<state-dir>/credentials/<guid>/<generation>.token
```

The registry stores only `seat.credential_generation`. Token files are owned by
the effective user, mode `0600`, and are never placed in argv, environment
variables, registry rows, logs, or bus messages. Managed launches propagate
`HERDER_STATE_DIR` only so a child can find the same state root. Use
`herder credential path --guid "$HERDER_GUID"` to print the non-secret current
path, then pass that path explicitly as `--credential-file PATH`.

Before enabling credential-authenticated verbs on an existing registry, run
`herder credential sweep`. It re-confirms legacy seats through the normal live
seat-completion path and exits successfully only when coverage is exactly
100%. Only then does it fsync the owner-only `credentials/cutover-v1` marker;
until that commit, the previous binary-compatible caller path remains active so
issuance itself does not create a flag day. Once present, the marker makes
identity-bearing verbs require `--credential-file` and ambient-only selection
is disabled. Clean marker absence is the only pre-cutover state: a marker that
exists but is unreadable, has non-0600 permissions, or has invalid content makes
verbs fail closed with a repair/intentional-rollback remedy; it never silently
restores ambient authority. A stale credential reports the current non-secret
lookup command, `herder credential path --guid GUID`. A row that names a
generation whose file is missing is token loss, not a legacy row; recover it
only with the interactive, audited command:

```text
herder repair reissue-credential --guid GUID
```

Reissue rotates only the credential. It does not change the guid, label, bus
name, seat, transcript continuity, or provenance.

Fresh-self operations remain intentionally possible without an existing token:
a promptless spawn and a fresh enroll mint a new guid and its first credential.
Adopting an already-unseated label uses that same fresh-enroll leg; adopting a
still-seated source requires the exact source-seat credential. Inherited
identity values can make these flows refuse, but cannot select their guid or
provenance.

## Transaction and rollback

Rotation takes a per-guid lock, stages and fsyncs the new immutable token, then
appends the registry row that flips the current generation. That registry flip
is the sole commit point. A crash before it leaves the old credential working;
a crash after it leaves the new credential working. The just-retired file stays
on disk but is dead immediately because its generation is no longer current.
At the start of a later completion, lazy cleanup under the same per-guid lock
removes only files that were already non-current; it never follows unknown or
symlink entries and can safely be retried.

During rollout, keep the previous binary available until the issuance sweep
reports 100%. Rolling the executable back does not require rewriting the
registry: older readers ignore `credential_generation`, while the credential
files remain inert state. Do not delete credential files during rollback. When
rolling forward again, rerun the sweep to verify every current generation and
surface any token loss before re-enabling credential-authenticated verbs.

The code-level rollback remains separable in cutover order. Revert credential
selection to the prior ambient verifier first for `spawn` and `send`, then for
`adopt`, `cull`, `compact`, and `enroll`; each revert is local to that verb and
does not change token files or registry generations. Deleting the owner-only
cutover marker rolls all of those checks back together and is therefore the
larger emergency lever. Either rollback deliberately reopens inherited ambient
authority, so keep it time-bounded and restore the 100% issuance gate before
rolling forward.
