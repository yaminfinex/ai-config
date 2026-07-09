#!/usr/bin/env bash
# One-shot runner for the mish acceptance gate. Individual checks still print
# ALL GREEN for local focus; this script owns the suite-level verdict.
set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
failures=0

for check in "$TESTS_DIR"/check-*.sh; do
  echo "=== ${check#$TESTS_DIR/}"
  if bash "$check"; then
    :
  else
    status=$?
    echo "FAIL: ${check#$TESTS_DIR/} exited $status" >&2
    failures=$((failures + 1))
  fi
done

if [ "$failures" -ne 0 ]; then
  echo "SUITE RED: $failures check(s) failed" >&2
  exit 1
fi

echo "ALL GREEN"
