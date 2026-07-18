import type { Skin } from "@/skins/contract";
import { TerminalMissionDetailView } from "@/skins/terminal/mission-detail-view";
import { TerminalMissionListView } from "@/skins/terminal/mission-list-view";

export const terminalSkin: Skin = {
  name: "terminal",
  activeThreadRendering: "panel",
  MissionListView: TerminalMissionListView,
  MissionDetailView: TerminalMissionDetailView,
};
