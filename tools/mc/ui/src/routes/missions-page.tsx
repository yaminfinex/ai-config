import { useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo } from "react";
import { useMissions } from "@/entities/missions";
import { useVersionInvalidation } from "@/entities/version";
import { useSkin } from "@/skins/index";
import { useWorkingSet } from "@/stores/working-set";
import { loadFailure } from "@/view-models/load-failure";
import { missionListVM } from "@/view-models/mission-list";
import { stalenessWarning } from "@/view-models/staleness";

export function MissionsPage() {
  const skin = useSkin();
  const { pollError } = useVersionInvalidation({ kind: "missions" });
  const navigate = useNavigate();
  const wsNavigate = useWorkingSet((state) => state.navigate);
  const { data, isPending, error } = useMissions();
  useEffect(() => {
    wsNavigate({ kind: "missions" });
  }, [wsNavigate]);
  const vm = useMemo(() => missionListVM(data), [data]);
  return (
    <skin.MissionListView
      vm={vm}
      loading={isPending}
      failure={loadFailure(error, data !== undefined)}
      stale={stalenessWarning(vm.observedAt, error, pollError)}
      onOpenMission={(slug) => void navigate({ to: "/mission/$slug", params: { slug } })}
    />
  );
}
