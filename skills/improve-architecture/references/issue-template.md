# Issue File Template

Template for the final artifact written in Phase 7 of the `improve-architecture` skill. Copy the structure below into
`<napkin-dir>/improve-architecture/<cluster-name>.md`.

"Implementation Recommendations" is deliberately **not tied to file paths** — pickup agents use "Current State
Inventory" for orientation and "Implementation Recommendations" for design intent.

---

```markdown
# <Cluster name>

## Problem

The architectural friction: which modules are shallow and tightly coupled, and what integration risk lives in the seams
between them.

## Current State Inventory

Concrete files that make up the cluster today — orientation for the pickup agent:

- Files in scope (paths + one-line role)
- Key types / functions / exports the current design exposes
- External callers that depend on the current interface (who must migrate)

Paths rot; durable guidance lives in "Implementation Recommendations" below.

## Proposed Interface

- Interface signature (types, methods, params)
- Usage example
- What complexity it hides internally

## Dependency Strategy

Which category applies and how deps are handled:

- **In-process**: merged directly
- **Local-substitutable**: tested with <specific stand-in>
- **Ports & adapters**: port definition, production adapter, test adapter
- **Mock**: mock boundary for external services

## Testing Strategy

- **New boundary tests**: behaviors to verify at the interface
- **Old tests to delete**: shallow-module tests that become redundant (replace, don't layer)
- **Test environment needs**: stand-ins, adapters, testcontainers, fixtures

## Migration Phases

Rough sequencing — these rarely land atomically. Each phase should be independently shippable where possible.

1. Phase 1 — <name>: <what changes, what coexists>
2. Phase 2 — <name>: <what changes, what coexists>
3. Phase N — <name>: old modules deleted, old tests deleted

Flag inter-phase dependencies and anything that must land together vs. can ship separately.

## Implementation Recommendations

Durable architectural guidance NOT coupled to current file paths:

- What the module should own (responsibilities)
- What it should hide (implementation details)
- What it should expose (interface contract)
- How callers migrate (patterns, not file paths)

## Acceptance Criteria

Observable signals the refactor landed, concrete enough to self-verify:

- e.g. "Files in `foo/bar/*` collapse from N to 1 module"
- e.g. "Test count in `foo/bar/` drops by ~M; no new mocks introduced"
- e.g. "Public interface is exactly these M entry points"
- e.g. "All external callers import from `<new-path>`; old module has zero inbound references"

## Out of Scope

Explicit fences against scope creep:

- Things a pickup agent might touch but should NOT
- Deliberately-deferred related refactors
- Public API / schema / migration boundaries that must not shift

## Open Questions

Decisions the pickup agent resolves — deliberately-deferred, not skill-failed:

- Callers we didn't audit
- Naming decisions that depend on broader repo conventions
- Trade-offs better made with more context
```
