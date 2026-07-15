# Pi launch probe results

Observed 2026-07-15 against hcom 0.7.23 and
`@earendil-works/pi-coding-agent` 0.80.6. A vendor or hcom version change
reopens the surfaces it touches.

All pre-activation executable probes used a disposable `$SCRATCH` with
`HOME=$SCRATCH/home` and `HCOM_DIR=$HOME/.hcom`. They did not read or write
the owner's hcom database, herder registry, or fleet seats. The launch used a
non-working placeholder credential, `PI_OFFLINE=1`, and no prompt, so it made
no inference request.

## P8 — global-layout coupling

Discharged. The real `hcom pi` launch wrote the native extension to
`$HOME/.pi/agent/extensions/hcom.ts`, and Pi's startup resource view reported
that exact extension as loaded. The scratch roster then reported the complete
bind shape: `tool=pi`, `hooks_bound=true`, a nonempty session UUID, and the
matching transcript path. This confirms that the default global
`HCOM_DIR=$HOME/.hcom` layout makes hcom's write location and Pi's load
location coincide. It does not weaken the team-bus refusal.

## P9 — shim-first PATH

Discharged. The generated launch environment put the Pi executable directory
first and the repository's `tools/herder/shims` directory before the real hcom
directory. An `execve` trace of the loaded native extension's bare
`spawn("hcom", ...)` call showed this chain:

1. `$SCRATCH/bin/hcom` was absent.
2. `tools/herder/shims/hcom pi-start ...` executed successfully.
3. The shim executed `bin/herder hook pi-start ...`.
4. The hook resolved and executed the real hcom 0.7.23 binary.

Thus the extension and ordinary seat commands hit the herder shim before the
real binary; merely having the real binary somewhere on PATH is not the
asserted contract.

## P10 — notes reach the Pi bootstrap

Discharged; the fallback transform is not needed. With a unique marker in
`HCOM_NOTES`, the real `pi-start` response contained a 3,980-byte bootstrap.
The marker began at byte offset 3,935 under a trailing `## NOTES` heading,
immediately before the closing hcom context marker. The source chain agrees:
the Pi launcher propagates `HCOM_NOTES`, `hooks/pi.rs` passes `ctx.notes` to
the generic bootstrap renderer, and the renderer appends the notes section.

The committed launch-contract goldens exercise the same environment and
doctrine block for fresh spawn, resume, and fork. The block includes bus
addressing, ordinary `hcom send` discipline, credential non-disclosure,
repeat-after-crash handling, and the silence expectation.

## Authorized Anthropic activation smoke

The owner-authorized live smoke passed on 2026-07-15 through the real herder
spawn path, default `HOME`, global hcom bus, and an Anthropic credential. The
credential preflight succeeded without disclosing its value, and the launch
environment selected Anthropic while removing the two foreign provider
credentials.

The fresh seat satisfied the complete bind predicate (`tool=pi`, hooks bound,
nonempty session UUID, transcript path), received the doctrine plus the
single-turn prompt with a verified receipt, and emitted the requested nonce to
the smoke controller using ordinary `hcom send`. After the first cull, resume
reconstructed `provider=anthropic` from the registry, re-observed Pi 0.80.6,
and reclaimed the exact bus name and session UUID. The final cull recorded an
unseated row with `close_result=ok` and `live_status=gone`; the bus row and pane
were absent. The disposable working directory, temporary executable link, and
smoke transcript were removed. The append-only registry retains only its
row-stopped audit history.

The installed Pi package lived in a nonstandard prefix. The smoke exposed that
caller-visible `PATH` could pass preflight but be lost by a child login shell.
Spawn, resume, and fork now carry the observed pre-symlink executable directory
into the child `PATH`, after the herder shim directory. Regression tests pin the
Pi-only scope so that path does not leak into other tool families.

## Adjacent build-unit probes

- **A6 (approval/autonomy):** Pi exposes tool allow/exclude flags and the
  normal interactive tool-confirmation behavior; no herder autonomy mapping
  is implied. `--approve` and `--no-approve` are project-trust overrides, not
  general tool-autonomy modes. Seats continue to use vendor defaults pending
  an owner ruling on any autonomy mapping.
- **P5 (residual provider network):** `PI_OFFLINE=1` suppressed Pi startup
  downloads in the scratch launch. The authorized Anthropic activation smoke
  then completed real inference successfully, confirming that offline mode
  suppresses startup operations without blocking selected-provider traffic.
- **P6 (project trust):** the installed CLI provides `--no-approve` (`-na`)
  to ignore project-local files for one run and `--approve` (`-a`) to trust
  them for one run. Without an override, a workspace containing trust-requiring
  resources uses Pi's saved decision or interactive trust prompt. No flag is
  pinned by this build unit; the disposition remains an owner decision.
- **P7 (credential-bearing file sources):** no per-invocation flag disabling
  the owner auth store or owner `models.json` exists in the installed CLI.
  `--no-approve` covers project resources only. The default-homes honesty
  boundary therefore remains open for both owner credential files.
- **A10 (file source versus environment):** isolated stand-ins confirmed that
  a stored `auth.json` API key wins over the provider environment value, and
  that a provider key in `models.json` wins over the provider environment on
  the model request path. No credential value was printed. The launch policy's
  exactly-one-provider claim consequently remains scoped to the environment
  channel, as designed.
