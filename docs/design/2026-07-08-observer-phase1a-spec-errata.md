---
title: "Proposed spec errata — herder node observer, phase 1a"
date: 2026-07-08
status: PROPOSALS ONLY — addressed to the spec steward lane (@spec-ravu adjudicates, owner
  blesses at merge). This file never edits docs/specs/herder-spec.md; every item below is a
  proposed erratum against the RATIFIED 2026-07-08 spec, with exact touch point and proposed
  wording. Source design: docs/design/2026-07-08-observer-phase1a-design.md; decision record:
  docs/design/2026-07-08-herder-node-daemon-designs.md (sessions-missions-design, 1fbe376).
---

# Proposed errata for docs/specs/herder-spec.md (phase 1a: universal seat observer)

Each erratum: touch point → proposed change → rationale. Wording is proposed, not demanded;
the *content* of E-1..E-3 and E-8 restates the owner-ratified invariants and should survive
adjudication in substance.

## E-1 — §2 Ubiquitous language: two new terms

Add rows:

> | **observer (node observer)** | Per-node disposable daemon that observes every seated
> row's liveness regardless of seat mechanism (spawn / enroll / resume / fork). Holds no
> write authority: its observation facts append through the §5.2 shared locked writer,
> byte-indistinguishable from CLI appends. Its death or rebuild is a non-event. |
>
> | **observation fact** | A registry row recording something the observer (or a sidecar)
> positively witnessed about a seat. Liveness claims without an appended row are advice,
> never truth. |

Rationale: the design introduces a durable component; the language table is where components
get their contract stated in one breath.

## E-2 — §3.1 Invariants: append two

> 13. **The observer holds no write authority.** No registry write ever routes *through* the
>     node observer; observer appends use the §5.2 discipline as an ordinary peer and obey
>     the confirmed-write contract (every append reports applied | noop | refused; none
>     discarded). Observer liveness is never a precondition for any verb.
> 14. **The observer is disposable.** Its death or rebuild is a non-event; no handoff
>     protocol may exist between observer generations; every boot re-derives its view from
>     the registry and live substrates (catch-up sweep, §8.4).

Rationale: ratified invariants 1, 2, 4 (decision record) in spec-invariant form. Invariant 3
(v2-states-only) is deliberately NOT proposed as a spec invariant — the spec has no legacy
view to speak of; it is an implementation-tier constraint enforced by the contract suite.
Steward may prefer folding 13/14 into one; content over count.

## E-3 — §3.3 Seat model: observer as a first-observer

Amend the third bullet:

> - Unseating is a recorded event appended by whichever component first observes seat death —
>   a sidecar, the node observer, or a CLI verb — never a per-caller inference.

Rationale: today's wording already says "whichever component"; this only names the observer
as one such component. Minimal touch.

## E-4 — §5.1 Shape: one optional field

Add to the session-record comment block:

> "observed_via": "…",   // optional; probe trail for observer/sidecar appends (advice)

Rationale: auditability of observation facts without new event vocabulary. Ignored by all
readers; absence means nothing.

## E-5 — §5.2 Write discipline: confirmed-write vocabulary + observer note

Append two bullets:

> - Every locked append resolves to exactly one typed outcome — **applied** (rows written),
>   **noop** (projection already showed the target state; idempotent re-assertion), or
>   **refused** (validation failed; the error names why). Writers must surface the outcome;
>   none may be discarded.
> - The node observer appends under this same discipline as an ordinary peer. Its
>   check-then-append decisions are made against the projection loaded under the write lock,
>   never against its own cached view.

Rationale: the outcome vocabulary exists structurally in the writer today and as
`none|pending|applied|error` in reconcile; ratified invariant 2 wants it normative.

## E-6 — §7 Command surface: one verb row

> | `observer run \| sweep \| status \| stop` | Node observer lifecycle. `run`: the per-node
> daemon loop (singleton per state dir, flock-elected). `sweep`: one level-triggered
> observation pass, no daemon — the degraded-mode and testing surface. `status`/`stop`:
> lockfile-based, read-only / SIGTERM. Observer liveness is never a precondition for any
> other verb. |

## E-7 — new §8.4 Catch-up sweep

> ### 8.4 Catch-up sweep (observer)
>
> On every observer boot — downtime recovery is not a distinct mode — the observer runs one
> level-triggered sweep: current substrate snapshot × current bus state × current registry
> projection. Verdict discipline: **positive evidence of death unseats** (occupant exited,
> pane gone within an unchanged epoch, dead pid with stale bus row); **absence of evidence is
> an observation gap, never a verdict** (§8.3 sid-less doctrine applies); missed transitions
> during downtime collapse into their observed end state. Correction rows are appended at
> observation time — `recorded_at` is never backdated; staleness evidence rides in the row
> body. Dormant rows with live counter-evidence and epoch-wide doubt are **flagged for the
> explicit verbs** (enroll / reconcile / resume), never auto-repaired.

Rationale: settles the settle-item at spec level; keeps repairs-are-verbs doctrine.

## E-8 — §10 Non-goals: sharpen the daemon rejection

Replace the §10 final bullet's "registry daemon" clause with:

> - **Label TTLs / liveness-coupled labels; full bitemporal schema** — considered and
>   rejected; correction rows + flock are the right-sized mechanisms.
> - **Registry *write* daemon** — remains rejected. The node observer (phase 1a) holds no
>   write authority and no verb depends on its liveness; a daemon that owns or mediates
>   registry writes stays out of scope. (Sharpened, not reversed.)

Also amend §1's out-of-scope line "multi-reader observation" if the steward reads the
observer as conflicting — the observer is a single per-node reader; no change should be
needed, flagged only for the steward's eye.

## E-9 — §4 Components: diagram + sidecar definition

- Add the observer to the component list (after Sidecar):

> - **Node observer** (hidden subcommand, ≤1 per state dir) — tails the registry as its work
>   queue and observes every seated row via the seat substrates (herdr socket as an ordinary
>   client; bus row freshness; process probes). Appends observation facts as a §5.2 peer.
>   Covers sidecar-less seats (enrolled today); coexists with sidecars, whose appends are
>   idempotent alongside its own.

- Amend the sidecar bullet's scope claim: "one per occupant, forked by `herder launch`" gains
  "— spawned/shimmed launches only; enrolled seats have no sidecar and are covered by the
  node observer."
- Diagram: add an `observer` box parallel to `sidecar`, reading the registry and the three
  substrate layers, writing observations into the registry. (ASCII edit left to the steward's
  taste.)

## E-10 — §8.1 Turnover detection: name the observer

Amend the opening sentence:

> The seat's observer — the sidecar for spawned/shimmed seats, the node observer for
> sidecar-less seats — watches its seat's sid. **Sid changed in my seat ⇒ turnover**: …

Rationale: §8.1's own closing line promises "this one rule serves spawned, shimmed, and
enrolled sessions alike" — today no component runs the rule for enrolled seats; this erratum
makes the promise true. NOTE: this rides scope call #1 in the design doc (§11); if
adversarial review rejects observer-side turnover detection, this erratum is withdrawn.

## E-11 — §9 Acceptance scenarios: four new ACs

> - **AC-37 universal observation** — an *enrolled* session's occupant is killed without
>   `cull`: the observer records the unseating (positive evidence in the row) within one
>   sweep interval; behaviour identical for spawned/resumed/forked seats.
> - **AC-38 observer downtime** — seats die and turn over while no observer runs; the next
>   sweep converges the registry to the same end state as continuous observation, with no
>   backdated `recorded_at`.
> - **AC-39 disposability** — `kill -9` of the observer mid-loop loses nothing: restart
>   reacquires the singleton lock, the catch-up sweep re-derives the view, and no file
>   written by the previous generation is read by the new one.
> - **AC-40 advice, not repair** — an unseated row with a live matching occupant (the
>   migration dormant-default case) is flagged with evidence and a suggested verb; the
>   observer appends nothing for it; `enroll`/`reconcile`/`resume` remain the only re-seat
>   paths.

(If E-10 stands, add **AC-41 enrolled turnover**: `/clear` in an enrolled seat produces the
child-first turnover pair from the observer, idempotent under re-observation.)
