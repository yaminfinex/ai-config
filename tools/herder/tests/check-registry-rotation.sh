#!/usr/bin/env bash
# check-registry-rotation.sh — gate registry size rotation and archive consultation.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

cd "$HERDER_ROOT" || exit 1

if go test ./internal/registry -run 'Test(RotationAtThresholdArchivesAndReseeds|RotationRecoversPartialLiveFromArchive|LoadWithArchivesMergesDeterministicallyLiveWins|ArchiveConsultationProvidesForkParentSessionID)$'; then
  printf '\nALL GREEN — registry rotation/archive-consultation invariants pass.\n'
  exit 0
fi

printf '\nREGISTRY ROTATION CONTRACT DRIFT — see failures above.\n'
exit 1
