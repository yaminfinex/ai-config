# sesh — session-transcript mirroring service

Per-user shippers mirror Claude Code and Codex CLI session transcripts
byte-faithfully to one central store, which parses on ingest and serves a team
recency page.

Authority chain: `docs/specs/session-service-spec.md` (invariants I1–I11) >
`docs/specs/sesh-wire.md` (wire freeze, M0) >
`docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md`.

## Build

Standalone Go module (Go ≥ 1.26); no imports from the host repository — this
tool is expected to move repos. Plain `go build`, no wrappers:

```sh
cd tools/sesh
go build ./cmd/sesh
```

Cross-compiles: `GOOS=darwin GOARCH=arm64` and `GOOS=linux GOARCH=amd64`.

## Layout

```
cmd/sesh/        entry point
internal/wire/   frozen types transcribing docs/specs/sesh-wire.md 1:1
internal/ship/   watcher, tailer, cursor registry, correlation
internal/store/  ingest handler, mirror, generations, recovery
internal/index/  parser, logical-session resolution, dedup, quarantine
internal/surface/ recency + transcript pages
internal/cli/    cobra command tree
tests/fixtures/  real captured session JSONL (see tests/fixtures/README.md)
tests/check-*.sh per-scenario gate harnesses (S1..S11)
etc/             systemd / launchd unit templates
```

## Status

M0 scaffold: all subcommands (`ship`, `serve`, `reindex`, `status`,
`admin drop-file`) are stubs that exit 1 with not-implemented. Bodies land
per milestone (M1 byte flow, M2 index + surface, M3 facts, M4 auth + rollout).
