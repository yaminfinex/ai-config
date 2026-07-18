import { useEffect, useMemo } from "react";
import { useMissionDetail } from "@/entities/missions";
import { missionRoute } from "@/router";
import { useSkin } from "@/skins/index";
import { useWorkingSet } from "@/stores/working-set";
import { missionDetailVM } from "@/view-models/mission-detail";

export function MissionPage() {
  const skin = useSkin();
  const { slug } = missionRoute.useParams();
  const wsNavigate = useWorkingSet((state) => state.navigate);
  const activeThreadId = useWorkingSet((state) => state.thread?.id ?? null);
  const toggleThread = useWorkingSet((state) => state.toggleThread);
  const { data, isPending } = useMissionDetail(slug);
  useEffect(() => {
    wsNavigate({ kind: "mission", slug });
  }, [wsNavigate, slug]);
  const vm = useMemo(() => missionDetailVM(data), [data]);
  return (
    <skin.MissionDetailView
      vm={vm}
      loading={isPending}
      activeThreadId={activeThreadId}
      onToggleThread={toggleThread}
    />
  );
}
