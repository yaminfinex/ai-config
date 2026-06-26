---
name: compound
description: >
  Mine the current conversation for durable, repeatable lessons and route each to the context surface where a future
  agent would actually find it. User-invoked via /compound. Use when the user says "/compound", "compound this", "what
  did we learn", "capture the learnings from this conversation", or "what's worth keeping from this session". Distinct
  from napkins capture (which parks branch-local atoms mid-work) — this distills the whole conversation and proposes
  durable homes now.
---

# Compound

Turn this conversation into leverage for the agents who come after. For everything notable that happened, ask two
questions:

1. Would this be **repeatable or useful to a future agent** who wasn't in this conversation?
2. **Where in context** would that agent already be looking at the moment they'd need it?

Capture only what passes (1), and put it exactly where (2) points. User-driven: propose, confirm, then apply — never
bulk-write.

## Not napkins

`using-napkins` capture parks branch-local atoms *during* work; harvest promotes them at branch end. `/compound` is the
on-demand retrospective — it reads the **whole conversation** directly, distills the durable lessons, and routes them to
permanent homes now, at any point. If a lesson isn't durable yet (still branch-local, unresolved, speculative), hand it
to napkins capture instead of forcing a permanent home.

## The signal filter

Capture a learning only if it is **both**:

- **Repeatable** — it will recur; it is not a one-off specific to this task, and
- **Lossy** — it would otherwise vanish; not reconstructable from the diff, the commit message, or the transcript.

Skip restatements of what the code now plainly shows, project trivia, and this-conversation-only mechanics. The test:
*would an agent six months from now, mid-task, be glad someone wrote this down — and would they find it without knowing
this conversation ever happened?* If they wouldn't find it, you picked the wrong home (see below), not the wrong lesson.

## Where to capture — co-location beats archival

The governing rule: **put the lesson on the surface the next agent will already have loaded when they hit the
situation.** A one-line guard comment at the failure site beats a paragraph in a learnings file nobody opens. Always
prefer the nearest durable surface; escalate to a general doc only when nothing closer fits.

| The lesson is about… | Home (nearest first) |
| --- | --- |
| a sharp edge in a tool/workflow a **skill** owns | that skill's `SKILL.md` or `references/` — a "seen live / sharp edges" note |
| a gotcha tied to a specific function or file | a comment or a guard **at that site in the code** |
| a project-wide convention or rule | `CLAUDE.md` / `AGENTS.md` (or `.agents/rules/`, `CONTEXT.md` if the repo uses them) |
| a decision plus the rationale to re-apply it | an ADR / `docs/` working doc; or a run's playbook "Decisions already made — do not re-litigate" |
| a reusable repro or debugging path | the owning skill, or `docs/` |
| not yet durable / branch-local | defer to **napkins capture** (`using-napkins`) |

## Procedure

1. **Discover the homes.** Before routing, see what this repo actually has: `CLAUDE.md`/`AGENTS.md`, `.agents/rules/`,
   `docs/`, `skills/`, an active playbook or `napkins/`. Route only to surfaces that exist — or propose creating one
   explicitly, as its own decision.
2. **Survey the conversation** for candidates that pass the signal filter. Write each as a **standalone statement a cold
   agent understands**, with enough *why* to apply it to cases this conversation didn't hit. A lesson that only makes
   sense with the transcript open has the wrong wording or the wrong home.
3. **Cluster by root cause**, not chronology or category. Sibling lessons of one root ("ids aren't durable here", "this
   API fails silently") collapse into one edit. Present clusters back for confirm/split; single-entry clusters are fine.
4. **Route each cluster** to its nearest home from the table. Show the destination and the **exact edit**; prompt
   `y / edit / skip`. Default to the most local surface.
5. **Apply** the confirmed edits, matching each destination's existing voice and structure — a sharp-edges note reads
   like its neighbours; a code comment reads like the surrounding code.
6. **Verify & report.** Show `git status` (or the touched files) and a one-line summary per captured lesson and where it
   landed. Don't commit or push unless asked.

## Self-iteration

Friction with this skill itself, or a lesson that had no good home, → fold the fix back into this file.
