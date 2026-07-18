import type { Mission, MissionsPayload } from "@/entities/types";

// View-model layer: pure functions from entities to render-ready data.
// This is where rendering laws live as testable code — no JSX, no hooks,
// no fetches. Components render exactly what these functions hand them.

export interface MissionRowVM {
  slug: string;
  title: string;
  owner: string | null;
  healthy: boolean;
  /** null = board unavailable: gap honesty, render nothing rather than fake. */
  taskSummary: string | null;
  warnings: string[];
}

export interface MissionListVM {
  rows: MissionRowVM[];
  /** List-level degradation (mish unreachable etc.); null = healthy. */
  warning: string | null;
}

export function missionListVM(payload: MissionsPayload | undefined): MissionListVM {
  if (!payload) {
    return { rows: [], warning: null };
  }
  const rows = [...payload.missions]
    .sort((a, b) => a.slug.localeCompare(b.slug))
    .map(missionRowVM);
  return { rows, warning: payload.warning === "" ? null : payload.warning };
}

export function missionRowVM(mission: Mission): MissionRowVM {
  return {
    slug: mission.slug,
    title: mission.name !== "" ? mission.name : mission.slug,
    owner: mission.owner === "" ? null : mission.owner,
    healthy: mission.ok && mission.warnings.length === 0,
    taskSummary: mission.boardAvailable ? taskSummary(mission) : null,
    warnings: mission.warnings,
  };
}

function taskSummary(mission: Mission): string {
  const parts = mission.taskCounts
    .filter((count) => count.count > 0)
    .map((count) => `${count.count} ${count.status.toLowerCase()}`);
  return parts.length > 0 ? parts.join(" · ") : `${mission.taskTotal} tasks`;
}
