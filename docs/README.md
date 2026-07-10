# Documentation map

This directory separates standing documentation from its source records. Current operating guides and characterizations live at the top level; normative contracts live under `specs/`.

Historical implementation plans and design records remain under `plans/` and `design/` while backlog entries or open decisions still depend on their detail. Retire one only after its load-bearing content and references have a named successor.

## Operating guides and characterizations

- `machine-setup.md`: install and configure this repository on a machine.
- `hcom-upgrade.md`: upgrade and verify hcom.
- `herdr-upgrade.md`: upgrade and verify herdr.
- `status-lines.md`: Claude and Codex status-line setup and data flow.
- `new-harness-onboarding.md`: reusable harness characterization and integration checklist.
- `grok-integration-characterization.md`: tested Grok integration and delivery behavior, including the complete delivery-probe evidence.
- `herder-instruction-injection.md`: launch/resume instruction-delivery characterization and proposed closure.

## Specifications

- `specs/herder-spec.md`: ratified herder domain and behavior contract.
- `specs/mission-spec.md`: ratified mission format and `mish` behavior contract.
- `specs/session-service-spec.md`: session-service product and architecture contract.
- `specs/sesh-wire.md`: frozen shipper/store wire and index schema.
- `specs/system-boundaries.md`: dependency and ownership boundaries between missions, session shipping, herder, and orchestrate.

Specifications are normative. When implementation does not conform, the spec states the gap in plain language and the task board owns remediation.

Implementation and operator documentation for shipped tools lives beside the code, primarily in `tools/*/README.md` and `skills/*/SKILL.md`.
