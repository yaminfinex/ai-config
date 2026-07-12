<!-- Provenance: demo evidence report, herder-dx run, 2026-07-12. Author: codex demo unit; isolated roots; owner-authorized inference spend. -->
# Grok isolated pane + roster demo report

Date: 2026-07-12 UTC  
Bus thread: `grok-demo`  
Scope: roster/pane investigation only; no production code diff

## Executive verdict

The isolated pane and typed-task portions succeeded with the pinned Grok Build CLI
`0.2.93`. The process ran in a herder-created pane, used `--always-approve`, received
launch doctrine through `--rules`, read `README.md`, and returned the requested
one-sentence summary.

The expected isolated hcom registration did **not** occur. After correcting two
environment-specific launch gaps, all four Claude-compatible hooks reported success
against the real hcom `0.7.23` binary, but hcom created no Grok row and assigned no bus
name. This contradicts the onboarding memo's expected degraded-path gain that inherited
Claude hooks register Grok as a misleading `tool: claude` row.

Consequences:

- This is a **pane demo only**, not production bus support or a working integration.
- Outbound was **not demonstrable** because Grok never received an assigned isolated bus
  name. It was not told to impersonate a supported tool or invent an address.
- Inbound is **absent/unverified**. No inbound send was attempted because there was no
  isolated Grok address; no blind retry or live-bus probe was made.
- The herder row was honest: the tool was the scratch wrapper, hcom was `n/a`, continuity
  was `assumed`, and the reconciled live status was `unknown`.

## Authorization and credential handling

- Authentication used the pre-existing `XAI_API_KEY` through process inheritance only.
- Existence was checked once as `set`/`unset`; the value was never printed, logged,
  written, placed in argv, or copied into a wrapper or report.
- Inference spend was owner-approved and limited to small read-only prompts.
- Permission mode was `--always-approve`; no bypass-permissions mode was used.
- The CLI default model was used; no model was pinned.

## Initial containment incident

The first attempted spawn invoked herder with scratch `HOME`, `GROK_HOME`, `HCOM_DIR`,
and `HERDER_STATE_DIR`, but the raw pane's login shell reset `HOME` to `/home/grace` and
did not propagate the other three variables. Filtered `/proc` inspection caught this
before any outbound/inbound bus probe. The pane was immediately culled.

### Live hcom containment check

A read-only check of the live hcom roster and status/lifecycle events for
`2026-07-12T22:29:30Z` through `2026-07-12T22:35:30Z` found no Grok-created or
Grok-claimed row and no Grok-attributable live-bus event. Nothing was deleted from the
live bus.

### Live vendor-state contamination of record

The noncompliant startup did write normal vendor state under live `~/.grok`. Every byte
was left untouched. Recorded paths and UTC mtimes:

| UTC mtime | Path |
|---|---|
| `22:30:13.907567473` | `~/.grok/CHANGELOG.json` |
| `22:30:14.196563292` | `~/.grok/CHANGELOG.md` |
| `22:30:14.412560167` | `~/.grok/downloads/grok-0.2.99-linux-x86_64` |
| `22:30:14.460559472` | `~/.grok/config.toml` |
| `22:30:15.069550662` | `~/.grok/version.json` |
| `22:30:16.491530089` | `~/.grok/sessions/%2Fhome%2Fgrace%2FCoding%2Fai-config/019f5873-de81-7993-952f-ae68d3b6d703/prompt_context.json` |
| `22:30:16.911524013` | `~/.grok/sessions/%2Fhome%2Fgrace%2FCoding%2Fai-config/prompt_history.jsonl` |
| `22:30:17.415516722` | `~/.grok/sessions/session_search.sqlite` |
| `22:30:19.398488033` | `~/.grok/sessions/%2Fhome%2Fgrace%2FCoding%2Fai-config/019f5873-de81-7993-952f-ae68d3b6d703/events.jsonl` |
| `22:30:19.399488018` | `~/.grok/sessions/%2Fhome%2Fgrace%2FCoding%2Fai-config/019f5873-de81-7993-952f-ae68d3b6d703/chat_history.jsonl` |
| `22:30:19.420487714` | `~/.grok/sessions/%2Fhome%2Fgrace%2FCoding%2Fai-config/019f5873-de81-7993-952f-ae68d3b6d703/summary.json` |

Additionally, 161 files under `~/.grok/marketplace-cache/` were written between
`22:30:17.126520903` and `22:30:17.306518298` UTC. The range began at
`783232b622f8182e/.git/shallow` and ended at
`de0d639b79e73a7c/.git/ORIG_HEAD`. The session directory also contained the expected
supporting lock, resource, signal, rewind, and update files in the same interval.

The CLI also changed `~/.local/bin/grok` to resolve to the downloaded 0.2.99 binary.
That auto-download was observed, not manually installed or cleaned up.

## Isolated retry construction

Official xAI documentation describes two update suppressors: `--no-auto-update` and
`[cli] auto_update = false`. Both were applied in the scratch run. Sources:

- <https://docs.x.ai/build/cli/headless-scripting>
- <https://docs.x.ai/build/settings>

The original 0.2.93 executable remained at
`/home/grace/.grok/downloads/grok-linux-x86_64`. It was pinned explicitly.

The final Grok wrapper, verbatim:

```sh
#!/bin/sh
export HOME=/tmp/grok-demo-rovi
export GROK_HOME=/tmp/grok-demo-rovi/grok-home
export HCOM_DIR=/tmp/grok-demo-rovi/hcom
export HERDER_STATE_DIR=/tmp/grok-demo-rovi/herder
export HCOM=/tmp/grok-demo-rovi/hcom-capture
exec /home/grace/.grok/downloads/grok-linux-x86_64 --no-auto-update "$@"
```

The scratch hcom diagnostic wrapper, verbatim:

```bash
#!/usr/bin/env bash
exec /home/grace/.local/share/mise/installs/github-aannoo-hcom/0.7.23/hcom "$@" 2> >(tee -a /tmp/grok-demo-rovi/hcom-hook.stderr.log >&2)
```

The scratch Grok config contained:

```toml
[cli]
auto_update = false
```

The effective herder shape was:

```sh
HOME=/tmp/grok-demo-rovi \
GROK_HOME=/tmp/grok-demo-rovi/grok-home \
HCOM_DIR=/tmp/grok-demo-rovi/hcom \
HERDER_STATE_DIR=/tmp/grok-demo-rovi/herder \
herder spawn \
  --role grok-demo \
  --agent /tmp/grok-demo-rovi/grok-isolated \
  --cwd /home/grace/Coding/ai-config \
  --extra-arg --always-approve \
  --extra-arg --rules \
  --extra-arg 'Demo only. Read-only work. Never reveal credentials. Use hcom only when explicitly asked and only with the assigned bus name.' \
  --no-focus --json
```

No initial prompt was passed to `herder spawn`, so isolation could be verified before
typing into the TUI.

## Isolation and version proof

Before the first prompt, filtered `/proc` evidence was:

```text
/proc/<pid>/exe -> /home/grace/.grok/downloads/grok-linux-x86_64
HOME=/tmp/grok-demo-rovi
GROK_HOME=/tmp/grok-demo-rovi/grok-home
HCOM_DIR=/tmp/grok-demo-rovi/hcom
HERDER_STATE_DIR=/tmp/grok-demo-rovi/herder
HCOM=/tmp/grok-demo-rovi/hcom-capture
```

Inside the isolated run, the pinned wrapper reported:

```text
grok 0.2.93 (f00f96316d)
```

The binary that actually executed the demo was therefore
`/home/grace/.grok/downloads/grok-linux-x86_64`, version 0.2.93, not the downloaded
0.2.99 binary.

## Pane and typed-task evidence

Herder created a fresh tab and pane and recorded the scratch wrapper as the tool. The
reconciled row reported:

```text
state: seated
tool: /tmp/grok-demo-rovi/grok-isolated
continuity: assumed
live_status: unknown
hcom: n/a / not_hcom_agent
```

After the isolation proof, the operator typed and submitted exactly:

```text
Read README.md and reply with exactly one sentence describing this repository. Do not modify any files.
```

Grok read `README.md` and answered:

```text
This repository contains personal portable agent skills and selected agent configuration, with symlinks for live edits and helper commands for setup, syncing, and management.
```

The TUI reported `Turn completed in 2.3s.` No repository file was modified by Grok.

## hcom findings and memo contradiction

Three environment truths matter:

1. Setting scratch roots only on the `herder spawn` invocation does not propagate them
   into this raw agent's login-shell pane. A child-side wrapper is required today.
2. In a herder-spawned pane, `hcom` resolves first to
   `tools/herder/shims/hcom`. That shim routes hook calls through `herder hook`; it is not
   the raw hcom compatibility path used by the earlier characterization.
3. Grok 0.2.93 loaded the Claude hook definitions but did not make the
   settings-level `env.HCOM` override effective. Exporting `HCOM` directly in the child
   wrapper was required to reach real hcom 0.7.23.

After the direct child export, Grok's session record reported all four observed hooks as
successful:

```text
SessionStart       success (9 ms)
UserPromptSubmit   success (6 ms)
PostToolUse        success (7 ms)
Stop               success (7 ms)
```

The stderr capture file existed and was empty. Despite those zero-exit hook calls, the
isolated hcom roster contained only the temporary observer (`worker-rovi`, `tool: codex`,
`hooks_bound: false`) and no row for the Grok session. There was therefore no misleading
`tool: claude` row, no assigned bus name, and no address for outbound or inbound testing.

This is a first-order contradiction of the onboarding memo's expected degraded path and
must be carried into TASK-170. First-class support must explicitly decide which hcom
binary hooks resolve under herder-spawned panes and must not equate a successful hook exit
with hcom registration or delivery capability.

## Teardown

- The final Grok pane was culled successfully.
- The temporary isolated observer was stopped.
- `/tmp/grok-demo-rovi` and all throwaway `HOME`, `GROK_HOME`, `HCOM_DIR`, and
  `HERDER_STATE_DIR` contents were removed successfully.
- Live `~/.hcom`, the live herder registry, and the live observer were not modified by
  the compliant retries.
- Live `~/.grok` contamination from the initial noncompliant attempt was left untouched,
  per owner ruling.

## Repository status

The pre-task worktree already contained two unrelated untracked files:

```text
?? .agents/scheduled_tasks.lock
?? docs/design/2026-07-12-sesh-store-served-distribution.md
```

This unit added only this report. `napkins/` is intentionally ignored by
`.gitignore:21`, so the report does not appear in status. Final
`git status --porcelain` was:

```text
?? .agents/scheduled_tasks.lock
?? docs/design/2026-07-12-sesh-store-served-distribution.md
```

The final `bin/ai-doctor` completed and reported the same 17 pre-existing warnings seen
before the demo (including the two unrelated untracked files); it found no demo-created
production diff.
