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
  /**
   * The empty claim — degraded is NOT empty: true only when the source was
   * healthily observed and returned zero missions. Zero rows behind a warning
   * is an unobservable source, not a known zero. Skins render this flag and
   * never compute emptiness themselves.
   */
  empty: boolean;
  /** The cached payload's own observation time; null before any payload. */
  observedAt: string | null;
}

export function missionListVM(payload: MissionsPayload | undefined): MissionListVM {
  if (!payload) {
    return { rows: [], warning: null, empty: false, observedAt: null };
  }
  const rows = [...payload.missions].sort((a, b) => a.slug.localeCompare(b.slug)).map(missionRowVM);
  const warning = payload.warning === "" ? null : payload.warning;
  return {
    rows,
    warning,
    empty: warning === null && rows.length === 0,
    observedAt: payload.provenance.observedAt,
  };
}

export function missionRowVM(mission: Mission): MissionRowVM {
  return {
    slug: mission.slug,
    title: missionTitle(mission),
    owner: mission.owner === "" ? null : mission.owner,
    healthy: mission.ok && mission.warnings.length === 0,
    taskSummary: missionTaskSummary(mission),
    warnings: mission.warnings,
  };
}

/** The title-fallback law: a manifest without a name falls back to the slug. */
export function missionTitle(mission: Mission): string {
  return mission.name !== "" ? mission.name : mission.slug;
}

/**
 * Gap honesty: an unavailable board yields null — render nothing, never a
 * fabricated zero. An available board summarizes non-zero counts, falling
 * back to the total when every count is zero.
 */
export function missionTaskSummary(mission: Mission): string | null {
  return mission.boardAvailable ? taskSummary(mission) : null;
}

function taskSummary(mission: Mission): string {
  const parts = mission.taskCounts
    .filter((count) => count.count > 0)
    .map((count) => `${count.count} ${count.status.toLowerCase()}`);
  return parts.length > 0 ? parts.join(" · ") : `${mission.taskTotal} tasks`;
}
