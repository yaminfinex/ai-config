import type { Skin } from "@/skins/contract";
import { MinimalMissionDetailView } from "@/skins/minimal/mission-detail-view";
import { MinimalMissionListView } from "@/skins/minimal/mission-list-view";

export const minimalSkin: Skin = {
  name: "minimal",
  activeThreadRendering: "in-row",
  MissionListView: MinimalMissionListView,
  MissionDetailView: MinimalMissionDetailView,
};
