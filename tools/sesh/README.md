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

## Operator Surface

Implemented M2 commands:

```sh
sesh ship --store-url http://127.0.0.1:8765
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
sesh reindex
sesh status
sesh admin drop-file <tool> <session_id> <file_uuid> --yes
```

`sesh status` reports cursor offsets, poisoned files, last ACK age, and store
reachability. It exits nonzero when the configured store is unreachable or any
cursor is poisoned.

`sesh admin drop-file` is an irreversible operator repair. It refuses to run
without `--yes`, removes exactly one mirrored file identity plus its index rows,
leaves sibling files in the same logical session intact, and records the action
in `drop_log`. Hard precondition: stop `sesh serve` before running `drop-file`;
the admin command is a separate process and does not quiesce live ingest or
queued append-index events.

`internal/surface` (U7) reads the frozen index schema through its `Store`
seam; `surface.SQLStore` satisfies it from the live store DB + mirror, and
`sesh serve` runs the surface on its own loopback read listener
(`--surface-addr`, default 127.0.0.1:8766 — the port the M2 Tailscale Serve
exposure proxies; ingest stays on `--addr`). The surface includes `/` recency,
`/s/{tool}/{id}` transcript pages, `/s/{tool}/{id}/raw` raw mirror fallback,
and `/nodes` last-PUT status. Gates: `tests/check-surface-fixtures.sh`
(fixture-backed renders) and `tests/check-surface-live.sh` (real serve + ship,
S2 renders once).

## M2 Exposure Runbook

Before M4 auth, keep ingest private to the local machine. The ingest listener
rejects non-loopback binds:

```sh
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
```

Expose only the read-only surface port with Tailscale Serve:

```sh
tailscale serve --bg --http=443 http://127.0.0.1:8766
```

Do not expose `127.0.0.1:8765`; it accepts transcript bytes. The read listener
serves the browser surface only, while ingest remains loopback-only until M4
auth and rollout gates land.

M2 exposure owner sign-off: PENDING (`@bigboss`).
