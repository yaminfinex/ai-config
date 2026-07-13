#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
export GOTOOLCHAIN=local
unset GOROOT

cd "$ROOT/tools/herder"
go test ./internal/grokbridge ./internal/cli
