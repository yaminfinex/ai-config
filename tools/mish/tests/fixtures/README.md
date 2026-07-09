# mish scenario fixtures

The U10 shell gate is intentionally light on static fixtures: every check
creates a fresh missions repository under `mktemp`, builds the real `mish`
binary from this checkout, and drives the real `backlog` binary discovered on
`PATH`.

The board shape exercised by these checks is the real-cut Backlog.md 1.47.1
fixture embedded by the implementation under
`internal/missionfs/testdata/real-backlog-1.47.1/`. The harness asserts the
same Backlog.md floor in `lib.sh` and keeps this directory for future
scenario-only raw fixtures if the acceptance suite needs them.
