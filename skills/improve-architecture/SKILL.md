---
name: improve-architecture
description:
  Use when reviewing a package/directory for architectural friction, deepening a shallow module cluster, or improving
  testability. Triggers "let's review packages/X", "how should we refactor Y", "improve architecture of Z", or
  complaints that a module is hard to understand or has brittle/over-mocked tests. Produces a napkin issue file; does
  NOT write implementation code.
---

# Improve Architecture

Take a slice of the codebase and produce an opinionated deepening proposal. You are the explorer, friction-detector, and
interface designer. The user picks which cluster to deepen and which interface to go with.

## The Core Idea

A **deep module** (Ousterhout, _A Philosophy of Software Design_) has a **small interface hiding a large
implementation**. Deep modules are more testable, more AI-navigable, and let you test at the **boundary** instead of
inside.

Most architectural friction comes from the opposite pattern — **shallow modules**: many tiny files whose interfaces are
nearly as complex as their implementations. Understanding one concept requires bouncing between them; tests multiply
because every seam gets its own file, but real bugs hide in the composition.

**Replace, don't layer**: write tests at the new boundary against observable behavior, then delete the shallow-module
tests they supersede.

## Process Overview

```
1. Explore the slice           → note friction organically
2. Present candidate clusters  → user picks one or more; inline or handoff per pick
3. Frame the problem space     → user-facing explanation + rough sketch
4. Design 3+ interfaces        → parallel sub-agents, each with a different constraint
5. Compare and recommend       → opinionated read, not a menu
6. User picks an interface
7. Write the napkin issue file → handoff-ready, pickup-able by a planning/impl agent
```

Work phases in order; wait for user input between handoffs. For multiple cluster picks in Phase 2, see "Handling
Multiple Candidates".

## Phase 1: Explore the Codebase

Explore the slice **organically**, like a curious engineer new to the code. Don't follow rigid heuristics — the friction
you experience IS the signal ("why is this so scattered?", "what does this module own?").

Use the Agent tool for wide exploration (`Explore` subagent works well). Ask it to read the code, trace call paths, and
note structure — not to prescribe fixes yet.

Watch for: concepts that require bouncing between many small files; shallow modules (interface ≈ implementation); pure
functions extracted only for testability while real bugs live in composition; tightly-coupled clusters where integration
risk lives in the seams; untested parts or tests that mock so much they verify nothing.

Friction notes are for you — synthesize them into candidates in Phase 2.

## Phase 2: Present Candidate Clusters

Surface a **numbered list** of deepening opportunities. For each:

| Field                   | What goes here                                                                   |
| ----------------------- | -------------------------------------------------------------------------------- |
| **Cluster**             | Modules/concepts involved (file paths, key types)                                |
| **Why they're coupled** | Shared types, call patterns, co-ownership of a concept                           |
| **Dependency category** | One of the four below — determines deepening strategy                            |
| **Test impact**         | Existing tests replaced by boundary tests (rough count + "replace, don't layer") |

**Do NOT propose interfaces yet.** This phase is diagnosis, not prescription.

End with: "Pick one or more clusters. For each, we can continue inline or prepare a handoff for a fresh agent. Which
ones, and how?"

### Dependency Categories

Classify each candidate — this determines whether and how it can be deepened:

| Category                   | What it looks like                                                         | Deepening strategy                                                                              |
| -------------------------- | -------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| **1. In-process**          | Pure computation, in-memory state, no I/O                                  | Always deepenable — merge modules and test directly at the new boundary                         |
| **2. Local-substitutable** | Deps have local test stand-ins (PGLite, in-memory fs, testcontainers)      | Deepenable if the substitute exists; test with the stand-in running in the suite                |
| **3. Remote but owned**    | Your own services across a network boundary (microservices, internal APIs) | Ports & adapters: define a port, in-memory adapter in tests, real HTTP/gRPC/queue in production |
| **4. True external**       | Third-party services you don't control (Stripe, Twilio, etc.)              | Mock at the boundary — inject as a port, tests provide a mock implementation                    |

Category drives interface design — in-process clusters want merging; remote-owned clusters want ports.

## Handling Multiple Candidates

Support picking more than one cluster and handling each differently:

| Disposition  | When it fits                                                                              | What to do                                                                      |
| ------------ | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| **Inline**   | The cluster the user most wants to steer, or one so small it's faster to do than hand off | Loop Phases 3–7 for this cluster now                                            |
| **Handoff**  | Worth exploring but not worth live attention; parallel work                               | Invoke the `handoff` skill with a Phase-3-ready package (contents below)        |
| **Deferred** | Noted but not prioritized                                                                 | List in the napkin index as "candidates not yet explored" — no document written |

Handoff package must include:

- Phase 2 cluster summary (modules, coupling, dependency category, test impact)
- Exploration notes that justified the candidate (friction evidence, not a full rewrite)
- User-set constraints (e.g. "don't touch the public API")
- Instruction to re-enter `improve-architecture` at Phase 3
- The shared napkin output path, so all cluster docs land together

When the user picks multiple inline clusters, finish one fully (through the napkin file) before starting the next —
don't context-switch mid-cluster.

## Phase 3: Frame the Problem Space (after user picks a candidate)

Write a **user-facing explanation** of the problem space for the chosen cluster. The user reads while sub-agents work in
parallel.

Include:

- Constraints any new interface must satisfy (caller dependencies, invariants).
- The dependencies it relies on and their category.
- A **rough illustrative code sketch** to ground the constraints. Not a proposal — something to react to.

Show this, then proceed to Phase 4 immediately.

## Phase 4: Design Multiple Interfaces in Parallel

Spawn **3+ sub-agents in parallel** (single message, multiple Agent calls). Each produces a **radically different**
interface. A single design is a guess; three surface trade-offs.

Each sub-agent gets a technical brief (separate from Phase 3's user-facing text):

- File paths and coupling details
- Dependency category
- What's hidden behind the new interface
- That agent's design constraint

| Agent       | Constraint                                                    |
| ----------- | ------------------------------------------------------------- |
| **Agent 1** | Minimize the interface — 1–3 entry points max                 |
| **Agent 2** | Maximize flexibility — many use cases and extension points    |
| **Agent 3** | Optimize for the most common caller — default case is trivial |
| **Agent 4** | _(if remote-owned or external)_ Ports & adapters pattern      |

Each must output: interface signature, usage example, what complexity it hides, dependency strategy (matching the
category), honest trade-offs.

## Phase 5: Compare and Recommend

Present each design **sequentially** so the user can read each on its own terms. Compare in prose — what each optimizes
for, what each gives up.

Then **recommend**. Be opinionated: which is strongest and why. If designs combine well, propose a **hybrid** and sketch
it. Don't hedge — the user wants a read, not a menu.

## Phase 6: User Picks an Interface

Wait for the user to pick, accept your recommendation, or ask for a hybrid.

## Phase 7: Write the Issue File

The issue file is the **final artifact** and the **input to the next agent** (planning, implementation, or human). Write
it so a fresh agent with zero session context can plan/execute without follow-up.

Resolve the napkin path via the `using-napkins` skill convention, then write to
`<napkin-dir>/improve-architecture/<cluster-name>.md` and update `<napkin-dir>/index.md`.

Use the template at [references/issue-template.md](references/issue-template.md) — it defines every required section
(Problem, Current State Inventory, Proposed Interface, Dependency Strategy, Testing Strategy, Migration Phases,
Implementation Recommendations, Acceptance Criteria, Out of Scope, Open Questions).

Present the final file to the user and confirm it captures the session's decisions. For multiple clusters, each gets its
own file; update `index.md` so they're discoverable as a set.

## Anti-Patterns

- **Don't prescribe in Phase 2.** Clusters are diagnosis; proposing interfaces before the user picks wastes work.
- **Don't produce one design.** One is a guess; three force trade-offs to surface.
- **Don't preserve old tests "just in case."** If a boundary test covers the behavior, the old unit test is debt.
- **Don't match interfaces to current file structure.** The structure is part of the problem.
- **Don't assume Rust/Go/TS idioms from training.** Read the actual code — repos have conventions that override
  defaults.
- **Don't skip the dependency category.** It's the most load-bearing classification here — in-process and remote-owned
  clusters want completely different interfaces.
