<!-- Provenance: isolated characterization evidence collected on 2026-07-13; owner-authorized provider calls. -->

# Pi agent-family characterization demo report

**Date:** 2026-07-13

**Scope:** Pi installation, containment, lifecycle binding, sessions, provider routing, and like-for-like harness comparison

**Decision:** manage Pi directly under herder and bind hcom through a native Pi extension

## Executive verdict

Pi is a viable herder-managed agent family. Its state roots are explicit, its sessions are portable JSONL trees, and its extension API exposes the lifecycle and inbound-message primitives needed for a durable hcom binding.

The recommended integration is a native TypeScript Pi extension, not an external transcript/RPC binder. The extension should claim a stable seat identity on `session_start`, replay pending inbound work, translate Pi lifecycle events into bus status, inject inbound messages with Pi's public extension API, and release resources idempotently on `session_shutdown`. Herder remains responsible for process supervision and restart.

The demo did **not** earn a Grok-style per-launch config rewrite. The `/proc` verdict is conditional: direct probe processes were deterministic, but herder's pane-spawn/login-shell path has not yet been characterized for Pi. Retain a one-time post-spawn environment assertion until that path is proven to preserve the constructed environment, then remove the ceremony if Pi is direct-exec. The demo did earn strict managed-environment construction for every Pi invocation, offline startup, telemetry suppression, provider-specific credential filtering, install-time version and artifact verification, and integration tests that prove those properties.

## Installation provenance

The active package namespace is `@earendil-works/pi-coding-agent`. The similarly named legacy package `@mariozechner/pi-coding-agent` is deprecated and stopped at `0.73.1`; it is not the package herder should install.

| Item | Observed value |
|---|---|
| Package | `@earendil-works/pi-coding-agent` |
| Version | `0.80.6` |
| Runtime | Node `v24.18.0`, npm `11.16.0` |
| Declared Node floor | `>=22.19.0` |
| Packed artifact | `earendil-works-pi-coding-agent-0.80.6.tgz` |
| Tarball size | `4,868,728` bytes |
| Tarball SHA-256 | `2a77634640b2d86d90d24087bb67559ecf2366e0fb52a42c55eed416147da411` |
| Registry SHA-1 | `8892736a2c7d01b5b95ac3dbc1752a5dbd517ba1` |
| Registry integrity | `sha512-vcfD6tOk402isLl3Cm/qbn2O10TvgroMp1+/fEGM24ZdvETFCdOYv5VZ7m59EI5fPsjfSJh+CpQ5bhBrhfOg7g==` |
| CLI entry SHA-256 | `af302f231437eaf6f37691bce4b34234fcb626bcb5eb3910d4fc3f6519bf78ca` |
| Upstream | <https://github.com/earendil-works/pi> |

The package was installed under an isolated prefix with an isolated home and npm cache. The installed `pi` executable resolved to the package's `dist/cli.js`; the packed registry artifact was then hashed independently.

Herder should pin an explicitly reviewed version and integrity value in its installer. “Latest” is appropriate for characterization, not for routine managed launches.

### Reproducible scratch layout and audit commands

The isolated installation remains available at this exact scratch layout:

```text
/tmp/pi-characterization-20260713/
├── home/
├── npm-cache/
├── latest-prefix/
│   └── node_modules/@earendil-works/pi-coding-agent/
└── package-artifact/
    └── earendil-works-pi-coding-agent-0.80.6.tgz
```

The install used Node `v24.18.0` and npm `11.16.0`. Expressing the observed runtime path relative to the user home, the exact isolated install command was:

```bash
ROOT=/tmp/pi-characterization-20260713
NODE_BIN="$HOME/.local/share/mise/installs/node/24.18.0/bin"

env -i \
  HOME="$ROOT/home" \
  PATH="$NODE_BIN:/usr/bin:/bin" \
  npm_config_cache="$ROOT/npm-cache" \
  npm_config_prefix="$ROOT/latest-prefix" \
  npm install \
    --prefix "$ROOT/latest-prefix" \
    '@earendil-works/pi-coding-agent@0.80.6'
```

The npm debug log records the same install argv and prefix. A second worker can repack and verify both the registry artifact and installed CLI with:

```bash
env -i \
  HOME="$ROOT/home" \
  PATH="$NODE_BIN:/usr/bin:/bin" \
  npm_config_cache="$ROOT/npm-cache" \
  npm pack \
    '@earendil-works/pi-coding-agent@0.80.6' \
    --pack-destination "$ROOT/package-artifact"

sha256sum \
  "$ROOT/package-artifact/earendil-works-pi-coding-agent-0.80.6.tgz" \
  "$ROOT/latest-prefix/node_modules/@earendil-works/pi-coding-agent/dist/cli.js"
```

Those hashes must match the values in the provenance table above. The packed filename differs from the registry URL basename because npm prefixes the package scope when it writes the local artifact.

## Managed home and state model

Pi does not consume a single environment variable named `PI_HOME`. Herder should define that concept and translate it into Pi's actual state controls:

```text
PI_HOME=<herder state root>/pi/<seat>
HOME=$PI_HOME/home
PI_CODING_AGENT_DIR=$PI_HOME/agent
PI_CODING_AGENT_SESSION_DIR=$PI_HOME/sessions
XDG_CONFIG_HOME=$PI_HOME/xdg/config
XDG_CACHE_HOME=$PI_HOME/xdg/cache
XDG_DATA_HOME=$PI_HOME/xdg/data
XDG_STATE_HOME=$PI_HOME/xdg/state
```

This mapping keeps both documented Pi state and incidental homedir consumers inside the managed root. Two simultaneous cold launches with distinct mappings remained independent.

| State | Managed location | Notes |
|---|---|---|
| Authentication store | `$PI_HOME/agent/auth.json` | Pi created an empty `{}` file at mode `0600`; routed environment credentials were not written to it |
| User settings | `$PI_HOME/agent/settings.json` | Seed once; no launch-time rewrite is required |
| Custom models/providers | `$PI_HOME/agent/models.json` | Optional; avoid embedding secrets |
| Extensions | `$PI_HOME/agent/extensions/` | Install the managed hcom extension here |
| Skills, prompts, themes, tools | `$PI_HOME/agent/{skills,prompts,themes,tools}/` | User-level Pi resources |
| Sessions | `$PI_HOME/sessions/` | Forced explicitly; otherwise Pi derives a cwd-specific directory below the agent root |
| Debug logs and package resources | `$PI_HOME/agent/` subdirectories | Remain below the managed root |
| Project resources | `<cwd>/.pi/` | Separate from user state and subject to Pi's project-trust handling |
| Other homedir consumers | `$PI_HOME/home/` | Captures shell tilde expansion and tools that ignore Pi-specific variables |

Project `.pi` resources are intentionally not relocated. They are project content, not seat state, and Pi gates their discovery through its trust lifecycle.

## Startup network and update behavior

Pi has explicit self-update commands, but the observed CLI does not silently replace itself on ordinary launches. A normal interactive startup can still perform network operations unrelated to the model request:

- fetch the latest Pi version from `pi.dev`;
- check installed package updates; and
- report install telemetry when `enableInstallTelemetry` remains enabled.

The managed contract should set both `PI_OFFLINE=1` and `PI_TELEMETRY=0`. Offline mode also sets the internal version-check skip and disables Pi's documented startup network work while preserving provider inference calls. `PI_SKIP_VERSION_CHECK=1` alone is too narrow.

A fresh Anthropic model call was also traced with `strace -ff -s 256 -e trace=connect,sendto,sendmsg` under the full isolated environment and offline flags. The trace showed resolver activity and one TLS connection to `160.79.104.10`; an independent lookup returned that address for `api.anthropic.com`. It showed no other internet connection and no `pi.dev` connection. This directly demonstrates provider-only network activity for that one offline Anthropic call. It does not by itself prove the same property for every provider implementation. The trace files are `/tmp/pi-characterization-20260713/offline-network-probe/network.trace.*`.

Fresh-root syscall observation produced these results:

| Invocation | Files created | Network observation | Conclusion |
|---|---|---|---|
| `pi --version` | None | No `connect` or `send` syscall | Exits before migrations and session setup |
| `pi --help` | Empty `auth.json`; transient trust lock | No `connect` or `send` syscall | Help still initializes mutable runtime state |
| Traced Anthropic model call with offline mode | Empty `auth.json`; no session (`--no-session`) | DNS plus one TLS connection to the resolved `api.anthropic.com` address; no other internet connection | Provider inference works while Pi startup traffic remains absent in this probe |

Because even `--help` writes state, **every** Pi invocation—including probing, help, version, and model listing—must receive the managed environment. The installer may run `--version` as an artifact check, but it must do so inside a scratch home.

## Binding fork: native extension versus external binder

### Evidence from the extension lifecycle

Pi loads TypeScript extensions directly and exposes lifecycle events that align with a managed bus seat. The evidence column distinguishes behavior exercised by the probes from surfaces found only in the installed API and documentation inventory:

| Integration need | Pi surface | Evidence basis |
|---|---|---|
| Claim resources after runtime construction | `session_start` | Probe-demonstrated |
| Release resources | `session_shutdown` | Probe-demonstrated |
| Observe session replacement | shutdown, extension reload/rebind, then a new start | API/documentation inventory; replacement sequence not directly probed |
| Observe work | `before_agent_start`, `agent_start`, `agent_end`, `agent_settled` | Probe-demonstrated |
| Observe or constrain models | `model_select`, `thinking_level_change` | API/documentation inventory |
| Observe tool activity | `tool_call`, `tool_result`, execution events | API/documentation inventory |
| Inject inbound user work | `pi.sendUserMessage(...)` | Probe-demonstrated for an idle RPC session |
| Add steering or follow-up during streaming | `pi.sendUserMessage(...)` delivery options | API/documentation inventory; streaming delivery modes not directly probed |
| Add custom contextual messages | `pi.sendMessage(...)` | API/documentation inventory; not directly probed |
| Add durable integration entries | custom session entries | API/documentation inventory |
| Inspect session identity/state | extension context and session manager | API inventory plus independently observed session files |
| Run hcom commands | extension execution API or a carefully scrubbed child process | API/documentation inventory; hcom binding not implemented in this demo |

The installed API documentation states that the same extension contract loads in `tui`, `rpc`, `json`, and `print` modes. UI availability differs by mode, but lifecycle integration does not require transcript scraping. Only RPC mode was exercised by the extension probes.

An isolated probe confirmed the ordering `factory -> session_start -> work -> session_shutdown` in RPC mode. A handler deliberately throwing during `session_start` produced an `extension_error` event while Pi continued serving RPC requests. Extension handler failure is therefore contained by Pi; whole-process failure remains herder's supervision responsibility.

A second fresh extension called `pi.sendUserMessage("Reply exactly INJECT-OK.")` immediately after `session_start` in an otherwise idle RPC session. Pi emitted an `input` event with source `extension`, started the model turn, returned exactly `INJECT-OK`, reached `agent_settled`, and then emitted `session_shutdown` when the controller closed stdin. This directly validates the primary inbound-injection path. `sendMessage` and mid-stream steering/follow-up remain API-inventory findings pending dedicated probes. The probe source and event log are `/tmp/pi-characterization-20260713/injection-probe.ts` and `/tmp/pi-characterization-20260713/injection-probe/extension.jsonl`.

A complete RPC prompt reached `before_agent_start`, `agent_start`, `agent_end`, and `agent_settled` before clean shutdown. Closing RPC stdin immediately after prompt acceptance shut the session down while the turn was still completing and later emitted a stale-extension-context error. Any RPC controller must keep the stream open until `agent_settled` or confirmed abort completion.

### Decision

Use a **native Pi extension** for hcom binding.

The extension should:

1. Start long-lived resources only in `session_start`, never in the extension factory.
2. Reclaim the stable managed identity and query pending work on every start.
3. Persist or recover the inbound cursor through durable hcom state rather than process memory.
4. Translate Pi start/end/settled and tool events into bus-visible status.
5. Inject routed user work through the demonstrated `sendUserMessage` path, not terminal input or transcript parsing. Use `sendMessage` only for custom contextual messages after a dedicated integration probe.
6. Treat session switch, new, resume, fork, extension reload, and process restart as normal rebinding events.
7. Make shutdown cleanup idempotent.
8. Scrub provider credentials from any child process it spawns unless that child explicitly requires one.
9. Surface extension errors to herder diagnostics without killing a healthy Pi process.

RPC remains useful as a headless Pi control surface, but it should not be the primary hcom binder. An external binder would duplicate lifecycle tracking, need a second crash/restart protocol, and have weaker access to session transitions. Revisit that design only if independent binder crash isolation or an external journal proves necessary.

## Restart, crash, and message recovery

Two fresh Pi processes used the same fixed logical seat identity while receiving distinct process and boot identities. That is the correct split: the seat survives; the process does not.

The recovery contract should be:

```text
herder starts Pi
  -> extension receives session_start
  -> extension claims/reclaims the stable seat
  -> extension fetches pending inbound work after its durable cursor
  -> Pi resumes normal event publication

Pi exits or crashes
  -> in-process extension disappears with Pi
  -> herder records the exit and restarts the managed launch
  -> the new extension instance repeats claim + pending replay
```

At-least-once delivery around a crash boundary is preferable to message loss. The binder should use stable message identifiers so duplicate replay can be ignored safely.

## Session compatibility

Pi sessions are versioned JSONL trees rather than a private database. A session begins with a header containing the format version, session UUID, timestamp, cwd, and optional parent-session reference. Entries have short IDs and parent IDs, supporting branching rather than only a linear transcript.

Observed entry families included messages, model and thinking-level changes, compaction and branch summaries, custom data/messages, labels, and session metadata.

| Operation | CLI surface | Observed result |
|---|---|---|
| Continue latest/specific | `-c`, `-r`, `--session`, `--session-id` | Appended to the selected JSONL session |
| Fork | `--fork` | Created a new JSONL file with a parent-session link |
| Fresh isolated session root | `--session-dir` or `PI_CODING_AGENT_SESSION_DIR` | Wrote directly beneath the supplied directory |
| Stateless call | `--no-session` | Avoids persistent conversation state |

Sesh/herder can index the session header, use the session UUID as its stable session identifier, resume with an exact session selection, and create lineage with `--fork`. It does not need SQLite knowledge or transcript scraping.

The managed extension may add its own custom entries for reconciliation, but bus delivery state should not depend solely on a session file that a user can branch or replace.

## Provider routing and least privilege

Pi resolves credentials from an explicit CLI key, `auth.json`, environment variables, or custom-provider configuration. The demo used empty managed auth files and one routed environment credential per process.

| Provider family | Credential name routed to Pi | Result |
|---|---|---|
| Anthropic | `ANTHROPIC_API_KEY` | Success |
| OpenAI | `OPENAI_API_KEY` | Success |
| xAI | `XAI_API_KEY` | Success |

No credential value was written to the managed Pi auth store. Pi's shell and extension subprocesses inherit the Pi environment, so a seat must receive only the credential required for its selected provider.

A long-lived seat cannot switch freely to another provider while preserving least privilege if credentials for that provider are absent. Herder should therefore pin a provider family per launch. A cross-provider model change should cause a controlled relaunch with a newly filtered environment, while the extension rejects or flags an in-process provider-family transition.

For the native Codex comparison, the one-shot CLI expected `CODEX_API_KEY`, not Pi's `OPENAI_API_KEY`. Herder may map the same underlying OpenAI credential to the harness-specific variable at process construction, but it should never expose both names to a child merely for convenience. See the official [Codex authentication documentation](https://learn.chatgpt.com/docs/auth).

## Cross-harness characterization

Each harness received the same workspace file and instruction: read the file and return exactly its second line without modifying files. The input SHA-256 remained `f4ae75c00ef5585a65678f8164571e7e53be4a50901ca99dcefca036ab9136f0` after every run.

Cold-run measurements are directional rather than a benchmark: provider latency varies, and harnesses do different startup work.

| Harness | Requested model | Exact answer | Elapsed | Max RSS | State artifact observation |
|---|---|---:|---:|---:|---|
| Pi / Anthropic | `claude-sonnet-5` | Yes | 3.16 s | 171,984 KB | Empty auth file plus 3,056-byte original session after the later resume exercise; the fork is 4,005 bytes |
| Claude Code `2.1.207` | `claude-sonnet-5` | Yes | 7.82 s | 294,388 KB | Config, backup, policy/remote metadata, and a roughly 16 KB project session |
| Pi / OpenAI | `gpt-5.3-codex:low` | Yes | 3.51 s | 173,048 KB | Empty auth file plus 3,827-byte session |
| Codex CLI `0.144.1` | `gpt-5.3-codex` | Yes | 3.63 s | 110,028 KB | Config, SQLite state, rollout, install identity, copied system skills, and roughly 21 MB of cloned marketplace data |
| Pi / xAI | `grok-code-fast-1` | Yes | 4.15 s | 172,196 KB | Empty auth file plus 3,983-byte session |
| Grok `0.2.93` through its launch contract | `grok-code-fast-1` requested; `grok-4.20-0309-non-reasoning` effective | Yes | 2.4 s task turn | Not comparable | Multiple session/event/resource files plus bridge, journal, monitor, and bus state |

Important qualifications:

- Pi's Anthropic call was materially lighter and quieter on this cold task than the native Claude Code call. That does not compare interactive capabilities.
- Codex first rejected `OPENAI_API_KEY`; it succeeded after harness-specific credential mapping. Its read-only sandbox also failed because the host lacked `bubblewrap`, so the successful characterization retry used `danger-full-access`. That retry is an operator surprise and is **not** a recommended production setting. The CLI also warned that model metadata was missing and used fallback metadata.
- Grok was invoked only through its characterized managed launch contract. Although the requested xAI model matched Pi's request, the effective Grok session used a different xAI-family model, so the timings are not a like-for-like model comparison.
- Grok's task-turn time excludes launch, bridge, monitor, and boot-prompt work. Its additional state and bus integration explain much of the complexity absent from a bare Pi one-shot; a managed Pi+hcom extension will add some corresponding integration overhead.

## Earned launch-contract clauses

| Clause | Decision | Evidence or rationale |
|---|---|---|
| Dedicated managed `PI_HOME` concept | Required | Pi exposes agent and session roots separately; `HOME` and XDG isolation capture remaining homedir consumers |
| Managed environment on every invocation | Required | `--help` creates state even though `--version` does not |
| `PI_OFFLINE=1` | Required | Suppresses documented Pi startup network work without blocking the traced Anthropic call; provider-specific residual-network checks remain integration-test work |
| `PI_TELEMETRY=0` | Required | Makes telemetry intent explicit even if settings drift |
| Provider-specific environment filtering | Required | Tools and extensions inherit the Pi process environment |
| Provider pin per seat | Required | Cross-provider in-process switching conflicts with one-key least privilege |
| Pinned package version and integrity | Required at install/provision | Extension compatibility and supply-chain reproducibility |
| Per-launch binary hash gate | Not earned | Provisioning verification plus an immutable managed install is sufficient |
| Per-launch config rewrite | Not earned | Settings can be seeded once; environment flags provide stable startup suppression |
| Per-launch `/proc` environment ceremony | Conditional | Retain a one-time post-spawn assertion until the Pi pane-spawn/login-shell path is characterized; remove it only if the path is verified direct-exec and preserves the constructed environment |
| Native managed extension | Required | Best lifecycle fidelity, inbound injection, session awareness, and restart recovery |
| External binder process | Not earned | Adds lifecycle and crash coordination without demonstrated benefit |
| Pending-message replay on every start | Required | Whole-process crash removes the in-process extension |
| Exact resume/fork integration | Required | Pi exposes stable session IDs and parent-linked JSONL forks |

## Recommended herder design

1. Add a pinned Pi installer that verifies package version and registry integrity inside a scratch home.
2. Construct a dedicated managed root per seat and export the Pi, home, and XDG mappings above for every command path.
3. Until the real Pi pane-spawn path is characterized, assert the post-spawn environment once per launch; retire that assertion only after a direct-exec/preservation test passes.
4. Seed immutable or owner-controlled settings and the managed hcom extension during provisioning.
5. Launch with offline startup, telemetry disabled, an explicit session directory, an explicit provider/model, and exactly one provider credential.
6. Have the extension reclaim stable identity, replay pending work, publish lifecycle state, and bind session metadata.
7. Treat provider-family changes as supervised relaunches with a rebuilt environment.
8. Index Pi JSONL headers for resume and fork; keep bus reconciliation state independently durable.
9. Test cold launch, resume, fork, extension reload, handler failure, whole-process crash, duplicate replay, provider switching, streaming injection modes, and RPC shutdown ordering.
10. Keep project `.pi` trust behavior intact rather than silently copying project resources into the managed user root.

## Safety and teardown

All Pi probes—including version, help, extension injection, and the traced offline model call—used scratch homes, explicit state directories, and isolated npm caches/prefixes. Provider calls received only the credential name required by that provider. No credential values are recorded here.

The native Grok comparison used its managed launch contract and scratch state/bus roots. A failed exploratory help launch was also contained within those roots. Post-run inspection found no remaining characterization processes and no recent writes to live vendor state.

The comparison input was unchanged. Repository changes for this work are documentation-only.
