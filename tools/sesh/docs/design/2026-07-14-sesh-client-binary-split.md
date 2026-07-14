# sesh client/store binary split — measurement and decision

Owner observation (2026-07-14): the installed fleet client was 32 MB and
`go version -m` showed it embedding tsnet (wireguard-go, gvisor, the
tailscale web client) and modernc sqlite — store-side machinery a shipping
client never executes. This note records the composition measurement, the
split decision, and the resulting artifact contract.

## 1. Measurement

Method (reproducible):

- Baseline artifact: `CGO_ENABLED=0 go build -trimpath -ldflags '-s -w'
  ./cmd/sesh` at the pre-split tree, pinned Go 1.26.4 — 33,530,018 bytes on
  linux/amd64, matching the installed 32 MB observation.
- Section accounting via `readelf -SW` on the stripped artifact: `.text`
  15.2 MB, `.gopclntab` 12.8 MB, `.rodata` 4.7 MB, initialized data ≈0.7 MB.
  The pclntab (function metadata) is proportional to code volume, so every
  MB of dead code costs roughly its own size again. (A 33.5 MB
  `crypto/internal/fips140/drbg.memory` symbol that dominates naive `nm`
  totals is NOBITS — zero file cost — and must be excluded from any
  symbol-based attribution.)
- Per-module attribution: `go tool nm -size` on an unstripped build of the
  same tree, text+initialized-data symbols aggregated by module path.
- Ground truth: a differential build with serve/reindex/admin (and their
  store/index/surface imports) removed from the command tree.

Per-module text+data attribution of the pre-split client (linux/amd64,
17.6 MB attributable; the remaining file weight is pclntab + linker tables
that scale with it):

| bytes      | module                                               |
|-----------:|------------------------------------------------------|
|  5,766,450 | Go stdlib + runtime                                   |
|  3,491,589 | tailscale.com (tsnet)                                 |
|  2,002,103 | modernc.org/sqlite                                    |
|  1,936,365 | linker func/type tables                               |
|  1,314,874 | gvisor.dev/gvisor (tsnet dep)                         |
|    429,899 | github.com/gaissmai/bart (tsnet dep)                  |
|    295,764 | github.com/go-json-experiment/json (tsnet dep)        |
|    244,456 | github.com/klauspost/compress (tsnet dep)             |
|    151,137 | modernc.org/libc + friends (sqlite deps)              |
|    125,760 | github.com/tailscale/wireguard-go (tsnet dep)         |
|    ~79,000 | sesh/internal/surface                                 |
|    ~57,000 | sesh/internal/store                                   |
|    ~45,000 | sesh/internal/index                                   |
|      rest  | long tail of tsnet transitive deps (dbus, goupnp, …)  |

Subcommand → dependency map:

| command                  | needs                                          |
|--------------------------|------------------------------------------------|
| ship, status             | ship, httpx, setup, wire, buildinfo (HTTP only) |
| setup                    | setup                                          |
| update                   | update, buildinfo                              |
| version                  | buildinfo                                      |
| serve                    | store, index, surface, wire → tsnet + sqlite   |
| reindex, admin drop-file | store, index, wire → sqlite                    |

Conclusion: the fleet client executes none of the heavy modules; they enter
the graph solely through the serve/reindex/admin command constructors.

## 2. Decision: package split, two entry points

Chosen: a package split with two mains, not build tags.

- `./cmd/sesh` — fleet client. Links `internal/cli` only: ship, status,
  setup, update, version, plus error stubs for the store-side command names
  (one line naming the sesh-store binary; flag parsing disabled so every
  invocation shape reaches the error, never an unknown-flag death).
- `./cmd/sesh-store` — full build. `cli.Execute(storecli.Commands()...)`
  registers the real serve/reindex/admin from `internal/storecli`, which is
  the only importer of store/index/surface in any command tree.

Build tags were rejected because they fork the test matrix (`go test ./...`
would silently skip whichever half the tag excludes) and make `go build
./...` ambiguous; the package split keeps every configuration compiling and
testing in one pass, and the artifact difference is just the main package.

Measured artifacts (CGO_ENABLED=0, -trimpath, -s -w, Go 1.26.4):

| platform     | client (cmd/sesh) | store (cmd/sesh-store) |
|--------------|------------------:|-----------------------:|
| linux/amd64  |         7,286,946 |             33,534,114 |
| linux/arm64  |         6,750,370 |             31,457,442 |
| darwin/arm64 |         6,865,202 |             31,671,634 |
| darwin/amd64 |         7,414,432 |             33,506,240 |

Client: −78% (26 MB saved per node). Client module graph after the split:
fsnotify, cobra, pflag, x/sys — nothing else.

## 3. Distribution contract (fleet-invisible)

Unchanged, verified: `just release` / scripts/release.sh still builds
`./cmd/sesh` per platform under the same `sesh-<os>-<arch>` artifact names,
so install.sh and `sesh update` serve/fetch the slim client with no URL,
checksum, or version-shape change. The version stamp
(`-X sesh/internal/buildinfo.Version`) and the census User-Agent
(`sesh-ship/<version>`) are identical in both builds. `just deploy-store`
builds `./cmd/sesh-store` and installs it on the store host under the same
`/usr/local/bin/sesh` path as before.

The one asymmetry the split creates is guarded in code: the channel only
carries clients, so on the store build the mutating `sesh update` path fails
closed BEFORE any download, with one line naming `just deploy-store`
(`update --check` stays allowed as a read-only skew probe). The flavor is
determined by command-tree assembly — a non-empty store command set marks
the store build. Regression coverage: a unit test asserts zero channel
requests before the refusal, and the battery gate drives the real store
binary against a real client-only channel over HTTP and byte-compares the
target afterwards. ops/README.md keeps the `sesh.prev` / redeploy recovery
as belt-and-braces.

## 4. Gate

`tests/check-client-slim.sh` asserts on every battery run:

- the built client artifact's `go version -m` external module list equals
  the ALLOWLIST exactly (fsnotify, cobra, pflag, x/sys) — any new module
  fails by default and growing the list is a deliberate act in the same
  change that imports it;
- `go list -deps ./cmd/sesh` contains none of internal/store, index,
  surface, storecli, sqlitedsn — internal machinery is gated independently
  of module names;
- the store artifact still CONTAINS the heavy modules and internal/store
  (probe self-check: a rename or probe typo fails loudly instead of passing
  silently);
- the client is under half the store's size AND under a 12 MB absolute
  ceiling (embed/stdlib drift alarm);
- each store-only command name on the client exits nonzero with the single
  line naming sesh-store;
- both builds report the same stamped version;
- the store build's `update` refuses against a real client-only channel
  with zero requests hitting it and the target byte-identical after, while
  `update --check` still reaches the channel.

The detector is proven three ways: a new module outside the original
denylist families (klauspost/compress via zstd) trips the allowlist; a
re-added `internal/store` import trips it with the full module diff; a
module-free internal package (sqlitedsn) slips the allowlist by design and
trips the package-graph deny.
