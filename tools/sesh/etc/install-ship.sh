#!/usr/bin/env bash
# DEPRECATED: service installation moved into the sesh binary; see
# docs/design/2026-07-12-sesh-store-served-distribution.md §5.
#
# install-ship.sh was absorbed by `sesh setup` — same behavior (user-bus
# preflight before any write, linger warning, refuse-to-clobber), now
# unit-tested Go with DP-4b provenance digests on the files it writes.
# Dry-run parity was verified before this reduction (design §5, doc-002 T1).
#
# Run instead:
#   sesh setup --store-url URL [--force] [--dry-run]
#
# Fresh node onboarding is one command (the installer ends by running setup):
#   curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh
echo "install-ship.sh is deprecated: run 'sesh setup --store-url URL' instead." >&2
echo "Fresh node: curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh" >&2
exit 2
