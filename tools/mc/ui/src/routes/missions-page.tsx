import { useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo } from "react";
import { useMissions } from "@/entities/missions";
import { useVersionInvalidation } from "@/entities/version";
import { useSkin } from "@/skins/index";
import { useWorkingSet } from "@/stores/working-set";
import { missionListVM } from "@/view-models/mission-list";

export function MissionsPage() {
  const skin = useSkin();
  useVersionInvalidation({ kind: "missions" });
  const navigate = useNavigate();
  const wsNavigate = useWorkingSet((state) => state.navigate);
  const { data, isPending } = useMissions();
  useEffect(() => {
    wsNavigate({ kind: "missions" });
  }, [wsNavigate]);
  const vm = useMemo(() => missionListVM(data), [data]);
  return (
    <skin.MissionListView
      vm={vm}
      loading={isPending}
      onOpenMission={(slug) => void navigate({ to: "/mission/$slug", params: { slug } })}
    />
  );
}
