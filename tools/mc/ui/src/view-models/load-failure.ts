// The load-failure law: a failed fetch must render as failure, never as a
// healthy empty page or an eternal "loading…" — degradation renders honestly
// (ARCHITECTURE.md §6), and a dead server is the deepest degradation there is.
// Cached data outranks the failure: while a payload exists the page keeps
// presenting it (version-poll invalidation recovers it once the server does),
// so the failure line renders only when there is nothing truthful to show.

/**
 * Render-ready failure line, pure: non-null only when a fetch has failed AND
 * no payload is cached to present instead.
 */
export function loadFailure(error: unknown, hasPayload: boolean): string | null {
  if (error === null || error === undefined || hasPayload) {
    return null;
  }
  return error instanceof Error ? error.message : String(error);
}
