import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { getJSON } from "@/entities/api";
import type { VersionInfo } from "@/entities/types";

export const VERSION_POLL_MS = 5000;

// Live updates keep the proven mc pattern (D1): poll a cheap version signal;
// when it changes, invalidate the affected entity queries. SSE can slot in
// behind this same seam later without touching any view.
//
// STALE (chunk C rewrites this module): this polls the pre-remediation
// {cursor, generation} shape. The live /api/v1/version is scope-aware and
// serves per-source-family provenance stamps — a missions-list client polls
// bare and watches `missions`; a mission-detail client polls with its slug
// and watches `mission` + `journal` + `roster` (see apiVersionDTO, api.go).
export function useVersionInvalidation() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["version"],
    queryFn: () => getJSON<VersionInfo>("/api/v1/version"),
    refetchInterval: VERSION_POLL_MS,
  });
  const previous = useRef<string | null>(null);
  useEffect(() => {
    if (!data) return;
    const key = `${data.cursor}:${data.generation}`;
    if (previous.current !== null && previous.current !== key) {
      void queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] !== "version",
      });
    }
    previous.current = key;
  }, [data, queryClient]);
}
