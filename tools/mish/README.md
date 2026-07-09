# mish

`mish` is the source-built mission CLI for creating and working inside durable mission boards.
A mission is a self-contained Backlog.md board under a shared missions repo, with a `.mission`
marker, pinned board config, and status checks that make multi-agent custody visible. The
normative behavior lives in `../../docs/specs/mission-spec.md`.

## Development

Run `mish` from source. Install and ship packaging, launchers, and `bin/mish` are deferred until
the house installer shape from sesh is copied after its rollout; v1 does not install a command on
PATH.

Install the pinned toolchain and Backlog.md dependency from the repository root:

```sh
mise install
GO_VERSION="$(awk '/^go /{print $2; exit}' tools/mish/go.mod)"
mise install "go@$GO_VERSION"
export PATH="$(mise where "go@$GO_VERSION")/bin:$(mise where npm:backlog.md)/bin:$PATH"
export GOTOOLCHAIN=local
backlog --version
```

The expected Backlog.md line is `1.47.1`. The acceptance harness also refuses non-`1.47.x`
versions before it mutates any fixture workspace.

From this directory:

```sh
go build ./...
go run ./cmd/mish --help
```

## Gates

Run these commands from `tools/mish/`.

```sh
go test ./...
go vet ./...
GOOS=darwin GOARCH=arm64 go build ./...
GOOS=linux GOARCH=amd64 go build ./...
go test ./internal/cli -run Golden
bash tests/run-all.sh
```

`bash tests/run-all.sh` is the acceptance-suite runner. Use it instead of a bare
`for f in tests/check-*.sh; do bash "$f"; done` loop, because the runner records every failing
check and returns a suite-level failure.

The standalone Backlog.md version-change gate is:

```sh
bash tests/check-backlog-floor.sh
```

Run it whenever `../../mise.toml` changes the Backlog.md pin. It re-verifies the nesting,
pinning, and references semantics that are trusted only for the verified 1.47.x line.

## Help Goldens

Help output is pinned from the spec, not regenerated from implementation drift. After an
intentional help change, update and inspect the goldens:

```sh
go test ./internal/cli -run Golden -update
go test ./internal/cli -run Golden
```
