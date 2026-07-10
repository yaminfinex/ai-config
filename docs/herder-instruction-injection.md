---
title: Herder instruction injection across launch and resume paths
date: 2026-07-10
status: proposed
provenance: >-
  Prompted by an operator-hit incident in which a raw-resumed Codex session
  received only hcom's stock lifecycle recipe and used it to launch unmanaged
  agents. This memo is a source-and-artifact trace; no live agents were started.
---

# Herder instruction injection across launch and resume paths

## Verdict

Managed launch, resume, and fork paths are covered. Raw Claude, raw Codex, and
raw Codex resume are not. On the inspected machine the vendor binaries in
`$HOME/.local/bin` precede the herder command shims, so a hand invocation does
not enter `herder launch`. A fresh raw process gets no hcom lifecycle bootstrap;
the dangerous case is a raw resume of an existing hcom thread, because the
thread retains hcom's historical stock recipe while no current mechanism adds
the herder supersede. Managed Codex resume/fork does receive the overlay, but as
a post-bind bus message rather than launch-time system context.

Recommend local observer delivery for an unmanaged but authoritatively
correlated hcom/herdr seat. Reuse the existing post-resume addendum text and
deliver it only after matching the live pane/process, tool session, and hcom
row. Do not auto-enroll the session. This closes the observed gap without
changing every hcom session on the machine or creating another doctrine copy.

An hcom bootstrap override would be cleaner in principle, but the installed
hcom release has no such setting. Its nearest settings do not reach Codex
resume: `notes` is not passed into the Codex bootstrap renderer, while a Codex
system prompt is represented as `developer_instructions` and stripped before a
resume/fork bootstrap is rebuilt. That upstream gap should be drafted for the
upstream ledger, not filed from this repository.

## Scope and terminology

“Raw” below means the operator-facing `claude`, `codex`, or `codex resume`
command as it resolves on the inspected machine. `type -a` and `command -v`
both place `$HOME/.local/bin/{claude,codex}` before
`tools/herder/shims/{claude,codex}`. The repository health check independently
reports both vendor commands as shadowing the expected shims. This ordering is
itself configuration drift, but fixing it would not fully close Codex resume:
even `herder launch codex resume ...` deliberately withholds the overlay because
hcom strips user developer instructions on that argument shape.

The overlay is the explicit `SUPERSEDED` paragraph plus `## AGENTS (herder
lifecycle)`. “Yes, message” means doctrine arrives after boot as a bus message,
not as developer/system context.

## Verified injection matrix

| Operator path | Overlay today? | Delivery mechanism and evidence |
|---|---:|---|
| `herder spawn --agent claude` | Yes | Spawn prepends the herder shim directory to the child PATH (`tools/herder/internal/spawncmd/spawn.go:690-720`). Claude's global SessionStart hook calls `hcom sessionstart` (`$HOME/.claude/settings.json:104-112`); the PATH shim routes that call to `herder hook` (`tools/herder/shims/hcom:1-23`), which replaces hcom's `additionalContext` with the herder-native bootstrap (`tools/herder/internal/hookcmd/hook.go:138-166`). The shared AGENTS text is embedded in that bootstrap (`tools/herder/internal/hookcmd/template.go:3-17`). |
| `herder spawn --agent codex` | Yes | Spawn calls `herder launch` (`tools/herder/internal/spawncmd/spawn.go:690-697`). A fresh Codex launch merges the overlay into the last user `developer_instructions` value, or adds one (`tools/herder/internal/launchcmd/launch.go:168-182,265-290`). hcom then merges its stock bootstrap before the herder block. Generated scripts under `$HOME/.hcom/.tmp/launch/codex_*.sh` confirm the ordering: stock `[HCOM SESSION]`, separator, `[HERDER SESSION ADDENDUM]`. |
| `herder resume` — Claude | Yes | Resume regenerates a pane whose command is `herder launch --resume ...` with the shim PATH (`tools/herder/internal/lifecyclecmd/lifecycle.go:514-526,572-584`). Claude fires SessionStart again, so the same hook rewrite used by spawn replaces hcom's bootstrap. |
| `herder resume` — Codex | Yes, message | hcom strips user `developer_instructions` on resume, so launch-time injection is intentionally skipped (`tools/herder/internal/launchcmd/launch.go:168-180,249-263`). After the new row binds a bus name, lifecycle code sends the shared resume addendum through verified `herder send` (`tools/herder/internal/lifecyclecmd/lifecycle.go:535-537,656-696`). Timeout or send failure warns and leaves the resume successful, so this is covered but not fail-closed. |
| `herder fork` — Claude | Yes | Fork uses the same `startAndAppend` → `herder launch --fork` path as resume (`tools/herder/internal/lifecyclecmd/lifecycle.go:143-168,558-584`). Claude SessionStart supplies the rewritten bootstrap. |
| `herder fork` — Codex | Yes, message | Native fork invokes the same post-bind addendum delivery (`tools/herder/internal/lifecyclecmd/lifecycle.go:143-168`). The self-fork fallback reads the spawned child's GUID and calls the same delivery helper (`tools/herder/internal/lifecyclecmd/lifecycle.go:266-311`). |
| Raw `claude` | **No** | The vendor binary currently wins PATH resolution, so neither `herder launch` nor its sidecar runs. Claude's global SessionStart hook still calls hcom (`$HOME/.claude/settings.json:104-112`), but without an hcom process binding upstream hcom returns only an availability hint (`src/hooks/claude.rs:462-520`). If PATH were repaired, the hand-launch hcom shim is designed to rewrite SessionStart (`tools/herder/shims/hcom:6-15`), but that is not today's machine state. |
| Raw `codex` | **No** | The vendor binary currently wins PATH resolution, so there is no launch-time `developer_instructions` overlay. The global Codex SessionStart hook binds known sessions but deliberately injects no bootstrap (`$HOME/.codex/hooks.json:25-34`; upstream hcom `src/hooks/codex.rs:283-320`). A fresh raw session therefore gets neither the herder overlay nor hcom's competing spawn recipe. |
| Raw `codex resume` | **No** | The vendor binary currently wins PATH resolution. Codex resumes the prior thread context; its hcom SessionStart hook may rebind a known hcom session but injects no bootstrap. Thus an old thread can retain the stock hcom recipe without receiving the supersede, exactly as the incident demonstrated. Repairing PATH alone is insufficient: if this command reached `herder launch`, the literal `resume` argument would make `codexStripsDevInstructions` true, launch would withhold the overlay, and the path would still never call managed lifecycle post-bind delivery (`tools/herder/internal/launchcmd/launch.go:168-182,249-263`). |

The global hooks alone do not repair the gap. Codex SessionStart is registered
for `startup|resume|clear` (`$HOME/.codex/hooks.json:25-34`), but hcom's handler
uses it for binding and deliberately returns no bootstrap because Codex renders
hook context visibly (upstream hcom `src/hooks/codex.rs:283-320`). Claude does
inject a bootstrap at SessionStart, but a true vendor-binary launch without an
hcom process binding gets only an “hcom available” hint (upstream hcom
`src/hooks/claude.rs:462-520`).

## Where the stock hcom template comes from

The installed hcom binary is release `v0.7.23`. Its stock lifecycle recipe is a
compiled Rust string, `UNIVERSAL`, in upstream `src/bootstrap.rs:36-89`; the
spawn/resume/fork/kill recipe is at lines 68-72. `get_bootstrap` selects
conditional blocks, renders placeholders, then optionally appends notes at
`src/bootstrap.rs:432-515`.

There is no template path or template-body key in the supported configuration
map. The supported string keys are enumerated in upstream
`src/config.rs:81-110,113-133`; locally, `hcom config --help` exposes `notes`,
`hints`, tool args, and per-tool system prompts, but no bootstrap override.
Configuration precedence is defaults, `$HCOM_DIR/config.toml`, then environment.

The apparent alternatives do not cover the failing path:

- `notes` / `HCOM_NOTES` is documented as one-time text appended to the
  bootstrap. hcom propagates the environment value into launched processes
  (`src/launcher.rs:1762-1783`), and the generic renderer can append it
  (`src/bootstrap.rs:510-515`). However, the Codex launcher calls
  `get_bootstrap(..., "", ...)`, explicitly supplying empty notes
  (`src/launcher.rs:2029-2058`).
- `codex_system_prompt` becomes a `developer_instructions` argument before
  preprocessing (`src/launcher.rs:2037-2044`). Resume/fork preprocessing strips
  every such argument, including the configured prompt, then adds a newly
  rendered stock bootstrap (`src/tools/codex_preprocessing.rs:345-431`).
- `hints` is appended to received bus messages, not startup system context, and
  cannot prevent action on the bare recipe before a message arrives.

These statements were checked against the installed CLI and the tagged upstream
source at commit `4cef94de232ca41ad23ce1b192bb9c6e761ece5f`. No upstream issue was
filed.

## Direction evaluation

| Direction | Matrix coverage | Drift risk | Blast radius | Upstream/local split | Verdict |
|---|---|---|---|---|---|
| Machine-wide hcom template override | Would cover hcom-generated bootstraps if it existed, but raw Codex SessionStart currently injects no bootstrap at all. A template override alone therefore would not refresh a vendor-binary resume; upstream would also need a raw-resume delivery seam. | A real file-backed override could make hcom and herder share one source. An inline `notes` copy would instead create two doctrine copies. | Every hcom session on the machine, including sessions intentionally unrelated to herder. | Requires upstream hcom support. A useful upstream draft is a file-backed bootstrap addendum which is included in Codex launch/resume reconstruction and re-delivered by SessionStart for a rebound raw resume. Local setup/doctor work would then install and verify the setting. | Best eventual simplification, not an available fix today. |
| Observer/sidecar delivery on unmanaged seat recognition | Can cover a raw resumed hcom thread after the global hook rebinds it, regardless of how the vendor process was launched. Fresh raw sessions with no hcom identity receive no competing recipe and need no delivery. | Low if it reuses `hookcmd.CodexResumeAddendum`; no second text copy. Delivery state must be per child process/session, not a durable “ever sent” bit, because old context may have compacted away. | Keep it narrow: only an unmanaged live pane with an authoritative pane/process + tool-session + joined hcom-row match. Never deliver from label, tag, or cwd guesses, and do not auto-enroll. | Entirely local. The observer already snapshots herdr and hcom state, while existing sidecar correlation and managed resume delivery provide precedent for the safety gate and payload. | **Recommend the observer form.** |
| Managed resume/fork plus operator documentation | Managed Claude and Codex paths are already covered. Documentation can steer normal operation but leaves the exact incident path live and self-advertising. | Low code drift; high behavioral drift because the unsafe command remains convenient and appears valid. | Minimal. | Local docs only. | Keep as defense in depth, not the chosen mechanism. |

## Recommended implementation scope

Implement the observer variant as follows:

1. Extend observer discovery beyond existing registry rows to consider live
   herdr panes whose detected Claude/Codex process has no current seated herder
   row. Discovery is advice-only until an authoritative correlation exists.
2. Correlate an unmanaged pane to a joined hcom row using stable pane/process
   coordinates plus the tool session ID reported by herdr/hcom. Require
   agreement; never use labels, tags, cwd, or a unique-looking roster row as
   proof. The existing sidecar rule that tag+cwd fallback is
   non-authoritative is the floor (`tools/herder/internal/sidecarcmd/sidecar.go:291-320`).
3. If the correlated Codex thread lacks a current herder seat, send
   `hookcmd.CodexResumeAddendum` to that hcom name as an informational bus
   message. Do not enroll, create a GUID, or claim registry ownership merely to
   send doctrine.
4. Key delivery to the live process/session incarnation, not to the historical
   hcom name or Codex thread forever. A later raw resume must receive the
   addendum again because the earlier copy may have compacted out. Record an
   observer fact or local ephemeral receipt sufficient to prevent repeated
   sends during one incarnation.
5. Bound delivery and emit an observer flag with a manual remedy on correlation
   ambiguity, bind timeout, or send failure. Doctrine delivery must not turn an
   observation sweep into an unsafe guess.
6. Add hermetic observer tests with: an unrelated same-cwd hcom row; a later
   authoritative pane/process + session match; exactly one delivery; a new
   process incarnation causing re-delivery; and a managed registry seat causing
   no observer delivery. Retain the current managed resume/fork tests.
7. Repair shim PATH ordering and document `herder resume`, never `codex resume`,
   as defense in depth. Neither substitutes for observer coverage.

This should be a local herder change. Separately prepare, but do not file, an
upstream hcom request: support a file-backed bootstrap addendum, include it in
the Codex bootstrap assembled after resume/fork strips stale developer
instructions, and re-deliver it when a raw Codex SessionStart rebinds an
existing hcom thread. Cover both generated scripts and hook-based raw resume.
Once that exists, reassess whether the local observer delivery and
Codex-specific launch addendum can collapse into one machine-managed bootstrap
addendum.

## Owner ruling requested

Approve “deliver doctrine, do not auto-enroll” when the observer authoritatively
correlates an unmanaged resumed hcom thread. The alternative ruling is to block
local implementation on an upstream hcom bootstrap-addendum and raw-resume
delivery facility; that leaves the known operator path unsafe in the interim.
