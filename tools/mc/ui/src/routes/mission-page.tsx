import { useParams } from "@tanstack/react-router";
import { useEffect, useMemo } from "react";
import { useMissionDetail } from "@/entities/missions";
import { useVersionInvalidation } from "@/entities/version";
import { useSkin } from "@/skins/index";
import { useWorkingSet } from "@/stores/working-set";
import { loadFailure } from "@/view-models/load-failure";
import { missionDetailVM } from "@/view-models/mission-detail";

export function MissionPage() {
  const skin = useSkin();
  // from-string form, not missionRoute.useParams(): importing the router
  // from a route component would close an ESM cycle (router → page → router).
  const { slug } = useParams({ from: "/mission/$slug" });
  useVersionInvalidation({ kind: "mission", slug });
  const wsNavigate = useWorkingSet((state) => state.navigate);
  const activeThreadId = useWorkingSet((state) => state.thread?.id ?? null);
  const toggleThread = useWorkingSet((state) => state.toggleThread);
  const { data, isPending, error } = useMissionDetail(slug);
  useEffect(() => {
    wsNavigate({ kind: "mission", slug });
  }, [wsNavigate, slug]);
  const vm = useMemo(() => missionDetailVM(data), [data]);
  return (
    <skin.MissionDetailView
      vm={vm}
      loading={isPending}
      failure={loadFailure(error, data !== undefined)}
      activeThreadId={activeThreadId}
      onToggleThread={toggleThread}
    />
  );
}
