#!/usr/bin/env bash
# check-registry-migration.sh — gate the one-shot v1→v2 registry migration.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

cd "$HERDER_ROOT" || exit 1

if go test ./internal/registry -run 'Test(LegacyV1MigrationArchivesAndReseeds|LegacyV1MigrationTwiceIsByteStable|LegacyV1MigrationHandlesMixedFile|LegacyV1MigrationRecoversEmptyLiveFromArchive|LegacyV1MigrationRecoversPartialLiveWithNodeFromArchive|LegacyV1MigrationRefusesMismatchedExistingArchive)$'; then
  printf '\nALL GREEN — registry v1 migration invariants pass.\n'
  exit 0
fi

printf '\nREGISTRY V1 MIGRATION CONTRACT DRIFT — see failures above.\n'
exit 1
