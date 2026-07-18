import { type QueryKey, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { getJSON } from "@/entities/api";
import { missionKeys } from "@/entities/missions";
import type { VersionStamps } from "@/entities/types";

// Scope-aware version-poll invalidation (ARCHITECTURE.md §6). Provenance is
// the invalidation contract: /api/v1/version serves per-source-family stamps,
// and a page polls with the scope that matches what it renders — the bare
// poll's `missions` token cannot vouch for a detail payload, so a detail
// page polls ?mission=<slug> and watches the three stamps of its three
// sections. Components know nothing of any of this; route components mount
// the hook with their page's scope.

export const VERSION_POLL_MS = 5000;

export type VersionScope = { kind: "missions" } | { kind: "mission"; slug: string };

export function versionUrl(scope: VersionScope): string {
  return scope.kind === "mission"
    ? `/api/v1/version?mission=${encodeURIComponent(scope.slug)}`
    : "/api/v1/version";
}

/**
 * The invalidation law, pure for tests: which entity queries must refetch
 * given what moved between two polls of the same scope.
 *
 * - missions scope: the list refetches when the `missions` family token
 *   moves. The other families stamp nothing the list renders.
 * - mission scope: the detail payload is three sections stamped by
 *   `mission` (per-slug), `journal`, and `roster` — a move in ANY of them
 *   invalidates the detail query. The bare `missions` token is deliberately
 *   ignored: it is the list's contract, not this page's.
 */
export function invalidationsFor(
  scope: VersionScope,
  prev: VersionStamps,
  next: VersionStamps,
): QueryKey[] {
  if (scope.kind === "missions") {
    return prev.missions.version !== next.missions.version ? [missionKeys.list] : [];
  }
  const moved =
    prev.journal.version !== next.journal.version ||
    prev.roster.version !== next.roster.version ||
    (prev.mission?.version ?? "") !== (next.mission?.version ?? "");
  return moved ? [missionKeys.detail(scope.slug)] : [];
}

export function useVersionInvalidation(scope: VersionScope): void {
  const queryClient = useQueryClient();
  const slug = scope.kind === "mission" ? scope.slug : null;
  const { data } = useQuery({
    queryKey: slug === null ? (["version"] as const) : (["version", slug] as const),
    queryFn: () => getJSON<VersionStamps>(versionUrl(scope)),
    refetchInterval: VERSION_POLL_MS,
  });
  // The comparison chain remembers which scope its stamps came from: on a
  // scope change it starts fresh — stamps observed for another scope say
  // nothing about this one.
  const previous = useRef<{ slug: string | null; stamps: VersionStamps } | null>(null);
  useEffect(() => {
    if (!data) {
      return;
    }
    const prev = previous.current;
    previous.current = { slug, stamps: data };
    if (prev === null || prev.slug !== slug) {
      return;
    }
    const current: VersionScope = slug === null ? { kind: "missions" } : { kind: "mission", slug };
    for (const queryKey of invalidationsFor(current, prev.stamps, data)) {
      void queryClient.invalidateQueries({ queryKey });
    }
  }, [data, queryClient, slug]);
}
