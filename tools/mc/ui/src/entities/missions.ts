import { useQuery } from "@tanstack/react-query";
import { getJSON } from "@/entities/api";
import type { MissionDetailPayload, MissionsPayload } from "@/entities/types";

// Entity layer: typed TanStack Query hooks mirroring /api/v1, one module per
// entity family. No rendering knowledge lives here; views subscribe to the
// cache and never own entity data.

export const missionKeys = {
  list: ["missions"] as const,
  detail: (slug: string) => ["mission", slug] as const,
};

export function useMissions() {
  return useQuery({
    queryKey: missionKeys.list,
    queryFn: () => getJSON<MissionsPayload>("/api/v1/missions"),
  });
}

export function useMissionDetail(slug: string) {
  return useQuery({
    queryKey: missionKeys.detail(slug),
    queryFn: () => getJSON<MissionDetailPayload>(`/api/v1/mission/${encodeURIComponent(slug)}`),
  });
}
