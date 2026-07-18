import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { getJSON } from "@/entities/api";
import type { VersionInfo } from "@/entities/types";

export const VERSION_POLL_MS = 5000;

// Live updates keep the proven mc pattern (D1): poll a cheap version number;
// when it changes, invalidate every entity query. SSE can slot in behind this
// same seam later without touching any view.
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
