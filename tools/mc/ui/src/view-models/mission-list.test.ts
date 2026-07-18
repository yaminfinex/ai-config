import { describe, expect, it } from "vitest";
import type { Mission, MissionsPayload } from "@/entities/types";
import {
  missionListVM,
  missionRowVM,
  missionTaskSummary,
  missionTitle,
} from "@/view-models/mission-list";

function mission(overrides: Partial<Mission> = {}): Mission {
  return {
    slug: "mission-one",
    ok: true,
    name: "Mission One",
    owner: "riley",
    authority: "hera",
    status: "active",
    created: "2026-07-15",
    boardAvailable: true,
    taskTotal: 3,
    taskCounts: [
      { status: "To Do", count: 0 },
      { status: "In Progress", count: 2 },
      { status: "Done", count: 1 },
    ],
    warnings: [],
    ...overrides,
  };
}

function payload(missions: Mission[], warning = ""): MissionsPayload {
  return {
    missions,
    warning,
    provenance: { source: "mish status", observedAt: "2026-07-18T12:00:00Z", version: "abc" },
  };
}

describe("missionListVM", () => {
  it("renders nothing before the payload arrives — no fabricated rows", () => {
    expect(missionListVM(undefined)).toEqual({ rows: [], warning: null });
  });

  it("sorts rows by slug", () => {
    const vm = missionListVM(
      payload([mission({ slug: "zeta" }), mission({ slug: "alpha" }), mission({ slug: "mid" })]),
    );
    expect(vm.rows.map((row) => row.slug)).toEqual(["alpha", "mid", "zeta"]);
  });

  it("surfaces list-level degradation as a warning, empty string as null", () => {
    expect(missionListVM(payload([], "mish unreachable")).warning).toBe("mish unreachable");
    expect(missionListVM(payload([])).warning).toBeNull();
  });
});

describe("missionRowVM", () => {
  it("falls back to the slug when the manifest has no name", () => {
    expect(missionRowVM(mission({ name: "" })).title).toBe("mission-one");
  });

  it("renders no owner rather than an empty one", () => {
    expect(missionRowVM(mission({ owner: "" })).owner).toBeNull();
  });

  it("is healthy only when ok with zero warnings", () => {
    expect(missionRowVM(mission()).healthy).toBe(true);
    expect(missionRowVM(mission({ ok: false })).healthy).toBe(false);
    expect(missionRowVM(mission({ warnings: ["board missing"] })).healthy).toBe(false);
  });

  it("gap honesty: an unavailable board renders NO task summary, never a fake zero", () => {
    expect(missionRowVM(mission({ boardAvailable: false })).taskSummary).toBeNull();
  });

  it("summarizes only non-zero counts", () => {
    expect(missionRowVM(mission()).taskSummary).toBe("2 in progress · 1 done");
  });

  it("falls back to the total when every count is zero", () => {
    const zeroed = mission({
      taskCounts: [{ status: "To Do", count: 0 }],
      taskTotal: 5,
    });
    expect(missionRowVM(zeroed).taskSummary).toBe("5 tasks");
  });
});

describe("missionTitle — the title-fallback law, shared with the detail page", () => {
  it("prefers the manifest name, falls back to the slug", () => {
    expect(missionTitle(mission())).toBe("Mission One");
    expect(missionTitle(mission({ name: "" }))).toBe("mission-one");
  });
});

describe("missionTaskSummary — shared with the detail page", () => {
  it("summarizes an available board, renders nothing for an unavailable one", () => {
    expect(missionTaskSummary(mission())).toBe("2 in progress · 1 done");
    expect(missionTaskSummary(mission({ boardAvailable: false }))).toBeNull();
  });
});
