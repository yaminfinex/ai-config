# mish status fixtures

These JSON lines were captured from a scratch `mish` built from this tree with
Go 1.26.5. The fixture repo was created with `mish new`; `mission-one` used the
real Backlog.md 1.47.1 task fixture, and `mission-broken` was an empty mission
directory. Only the temporary repo prefix was normalized to `/missions-repo`.

- `status-mission.json`: `mish status --mission mission-one`, after removing
  `artifacts/` to produce a real warning-bearing success payload.
- `status-mission-not-found.json`: `mish status --mission ghost`.
- `status-repo-unset.json`: `env -u MISSIONS_REPO mish status --all`.
- `status-all.json`: `mish status --all`, with one degraded and one healthy
  sibling.

Keep these producer-shaped. In particular, degraded `--all` entries put their
diagnostics in `warnings`; they do not carry `refusal`, `reason`, or `remedy`.

# herder list fixture

`herder-list-missions.jsonl` contains three complete rows captured from the
real `herder list --json` producer on 2026-07-15: marker inference, cwd
inference, and `mission:null`. The final row is a producer-shaped
forward-compatibility mutation with an unknown `mission.seat` key; it pins the
contract that additions inside `mission` do not change parsing or grouping.
