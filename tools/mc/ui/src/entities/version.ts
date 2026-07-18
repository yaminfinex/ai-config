import { type QueryClient, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { getJSON } from "@/entities/api";
import { missionKeys } from "@/entities/missions";
import type { MissionDetailPayload, MissionsPayload, VersionStamps } from "@/entities/types";

// Scope-aware version-poll invalidation (ARCHITECTURE.md §6). Provenance is
// the invalidation contract: /api/v1/version serves per-source-family stamps,
// and a page polls with the scope that matches what it renders — the bare
// poll's `missions` token cannot vouch for a detail payload, so a detail
// page polls ?mission=<slug> and watches the three stamps of its three
// sections. Components know nothing of any of this; route components mount
// the hook with their page's scope.
//
// The baseline for staleness is THE CACHE ITSELF, never the previous poll:
// every entity payload carries its own section provenance, so each poll is
// compared against what the cache actually presents. A poll-to-poll
// comparison chain would take its first observation as an unfounded
// baseline — an entity response already in flight when the source changed
// could land stale behind it and stick forever (the first-poll race, which
// also re-arms after any transient poll failure). Judged against the cached
// payload's own stamps, the first poll has something honest to compare with
// and the race dies by construction.

export const VERSION_POLL_MS = 5000;

export type VersionScope = { kind: "missions" } | { kind: "mission"; slug: string };

export function versionUrl(scope: VersionScope): string {
  return scope.kind === "mission"
    ? `/api/v1/version?mission=${encodeURIComponent(scope.slug)}`
    : "/api/v1/version";
}

/**
 * The list page's staleness law, pure: the cached missions payload is stale
 * when its own provenance token differs from the polled `missions` family
 * stamp. No cached payload = nothing to be stale.
 */
export function listIsStale(polled: VersionStamps, cached: MissionsPayload | undefined): boolean {
  return cached !== undefined && cached.provenance.version !== polled.missions.version;
}

/**
 * The detail page's staleness law, pure: the cached detail payload is three
 * sections, each stamped by its own system of record — `mission` (per-slug),
 * `journal` (threads), `roster` (crew). The payload is stale when ANY
 * section's own token differs from its polled stamp. The bare `missions`
 * token is deliberately not consulted: it is the list's contract, not this
 * page's. A scoped poll that carries no mission stamp counts as a mismatch —
 * refetching is the honest response to a degraded poll.
 */
export function detailIsStale(
  polled: VersionStamps,
  cached: MissionDetailPayload | undefined,
): boolean {
  if (cached === undefined) {
    return false;
  }
  return (
    cached.mission.provenance.version !== (polled.mission?.version ?? "") ||
    cached.threads.provenance.version !== polled.journal.version ||
    cached.roster.provenance.version !== polled.roster.version
  );
}

/**
 * One poll tick: compare the polled stamps against the cached payload's own
 * provenance for this scope and invalidate the entity query if it is stale.
 * Extracted from the hook so the exact concurrent orderings (poll lands
 * before an in-flight entity response) are deterministically testable
 * against a real QueryClient. Returns whether an invalidation was issued.
 */
export function applyVersionStamps(
  queryClient: QueryClient,
  scope: VersionScope,
  polled: VersionStamps,
): boolean {
  if (scope.kind === "missions") {
    const cached = queryClient.getQueryData<MissionsPayload>(missionKeys.list);
    if (!listIsStale(polled, cached)) {
      return false;
    }
    void queryClient.invalidateQueries({ queryKey: missionKeys.list });
    return true;
  }
  const queryKey = missionKeys.detail(scope.slug);
  const cached = queryClient.getQueryData<MissionDetailPayload>(queryKey);
  if (!detailIsStale(polled, cached)) {
    return false;
  }
  void queryClient.invalidateQueries({ queryKey });
  return true;
}

export interface VersionInvalidation {
  /**
   * The poll's own health, surfaced: when the invalidation contract itself is
   * unreachable, cached entity data can no longer be verified as current, and
   * the page must say so (the staleness law, view-models/staleness.ts).
   * Swallowing this error would present stale data as current — a rejection.
   */
  pollError: Error | null;
}

export function useVersionInvalidation(scope: VersionScope): VersionInvalidation {
  const queryClient = useQueryClient();
  const slug = scope.kind === "mission" ? scope.slug : null;
  const { data, dataUpdatedAt, error } = useQuery({
    queryKey: slug === null ? (["version"] as const) : (["version", slug] as const),
    queryFn: () => getJSON<VersionStamps>(versionUrl(scope)),
    refetchInterval: VERSION_POLL_MS,
  });
  // Keyed on dataUpdatedAt, not data identity: identical stamps re-observed
  // keep the same structurally-shared object, but the check must still run
  // each tick — an entity response may have landed stale since the last one.
  useEffect(() => {
    if (dataUpdatedAt === 0 || data === undefined) {
      return;
    }
    applyVersionStamps(
      queryClient,
      slug === null ? { kind: "missions" } : { kind: "mission", slug },
      data,
    );
  }, [data, dataUpdatedAt, queryClient, slug]);
  return { pollError: error };
}
