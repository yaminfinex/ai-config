import type { ComponentType } from "react";
import type { MissionDetailVM } from "@/view-models/mission-detail";
import type { MissionListVM } from "@/view-models/mission-list";

// The behavioural half of the skin seam (D4): a skin is a set of components
// over these exact prop contracts, selected once at the top of the tree.
// Components receive ONLY view-model state and actions — a skin component
// importing a store or an entity hook breaks the seam and fails review.

export const skinNames = ["minimal", "terminal"] as const;
export type SkinName = (typeof skinNames)[number];

export interface MissionListViewProps {
  vm: MissionListVM;
  loading: boolean;
  /** The load-failure law (view-models/load-failure.ts): non-null means the
   * fetch failed with nothing cached — render the failure, not a healthy blank. */
  failure: string | null;
  onOpenMission: (slug: string) => void;
}

export interface MissionDetailViewProps {
  vm: MissionDetailVM | null;
  loading: boolean;
  /** Same load-failure law as the list view. */
  failure: string | null;
  activeThreadId: string | null;
  onToggleThread: (id: string) => void;
}

export interface Skin {
  name: SkinName;
  /**
   * The declared behavioural rendering difference: where the active thread
   * renders. "in-row" expands beneath the selected row; "panel" renders in a
   * dedicated panel above the list. The behaviour beneath it — one active
   * thread at most, toggle to switch, same preview content — cannot vary.
   */
  activeThreadRendering: "in-row" | "panel";
  MissionListView: ComponentType<MissionListViewProps>;
  MissionDetailView: ComponentType<MissionDetailViewProps>;
}
