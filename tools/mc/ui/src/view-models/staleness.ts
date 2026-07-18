// The staleness law — the second half of load-health (the first is
// view-models/load-failure.ts). Two situations, modeled separately:
// fatal-no-data renders the failure line; cached-but-unverified keeps
// presenting the cached data (truth outranks blankness) but must SAY it is
// unverified — presenting a stale observation as current is a rejection
// (ARCHITECTURE.md §6). Unverified means either currency channel is down:
// the entity refetch is failing, or the version poll (the invalidation
// contract itself) is failing. The warning carries the cached payload's own
// provenance observedAt — the server's claim of when the data was last
// honestly observed, never a client-side clock.

/**
 * Render-ready staleness line, pure: non-null only when a payload is cached
 * (observedAt from its own provenance) and a currency channel is failing.
 */
export function stalenessWarning(
  observedAt: string | null,
  refetchError: unknown,
  pollError: unknown,
): string | null {
  if (observedAt === null) {
    return null;
  }
  const refetchDown = refetchError !== null && refetchError !== undefined;
  const pollDown = pollError !== null && pollError !== undefined;
  if (!refetchDown && !pollDown) {
    return null;
  }
  return `may be stale — last observed ${observedAt}`;
}
