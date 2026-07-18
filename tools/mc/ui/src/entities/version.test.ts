import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { missionKeys } from "@/entities/missions";
import type {
  MissionDetailPayload,
  MissionsPayload,
  Provenance,
  VersionStamps,
} from "@/entities/types";
import { applyVersionStamps, detailIsStale, listIsStale, versionUrl } from "@/entities/version";

function stamp(version: string): Provenance {
  return { source: "test", observedAt: "2026-07-18T12:00:00Z", version };
}

function stamps(
  journal: string,
  missions: string,
  roster: string,
  mission?: string,
): VersionStamps {
  const base: VersionStamps = {
    journal: stamp(journal),
    missions: stamp(missions),
    roster: stamp(roster),
  };
  return mission === undefined ? base : { ...base, mission: stamp(mission) };
}

function listPayload(version: string): MissionsPayload {
  return { missions: [], warning: "", provenance: stamp(version) };
}

function detailPayload(mission: string, journal: string, roster: string): MissionDetailPayload {
  return {
    mission: {
      status: {
        slug: "mission-one",
        ok: true,
        name: "Mission One",
        owner: "riley",
        authority: "hera",
        status: "active",
        created: "2026-07-15",
        boardAvailable: true,
        taskTotal: 0,
        taskCounts: [],
        warnings: [],
      },
      provenance: stamp(mission),
    },
    threads: { rows: [], provenance: stamp(journal) },
    roster: { agents: [], warning: "", provenance: stamp(roster) },
  };
}

describe("versionUrl", () => {
  it("polls bare for the missions-list scope", () => {
    expect(versionUrl({ kind: "missions" })).toBe("/api/v1/version");
  });

  it("polls with its slug for the mission-detail scope", () => {
    expect(versionUrl({ kind: "mission", slug: "mission-one" })).toBe(
      "/api/v1/version?mission=mission-one",
    );
  });

  it("URL-encodes the slug", () => {
    expect(versionUrl({ kind: "mission", slug: "a/b c" })).toBe(
      "/api/v1/version?mission=a%2Fb%20c",
    );
  });
});

describe("listIsStale — the list page's staleness law", () => {
  it("nothing cached is never stale — the query fetches on mount anyway", () => {
    expect(listIsStale(stamps("j1", "m1", "r1"), undefined)).toBe(false);
  });

  it("stale exactly when the cached payload's own token differs from the polled stamp", () => {
    expect(listIsStale(stamps("j1", "m2", "r1"), listPayload("m1"))).toBe(true);
    expect(listIsStale(stamps("j1", "m1", "r1"), listPayload("m1"))).toBe(false);
  });

  it("ignores journal and roster stamps — they vouch for nothing the list renders", () => {
    expect(listIsStale(stamps("j9", "m1", "r9"), listPayload("m1"))).toBe(false);
  });
});

describe("detailIsStale — the detail page's staleness law", () => {
  const cached = detailPayload("s1", "j1", "r1");

  it("nothing cached is never stale", () => {
    expect(detailIsStale(stamps("j1", "m1", "r1", "s1"), undefined)).toBe(false);
  });

  it("fresh when every section token matches its polled stamp", () => {
    expect(detailIsStale(stamps("j1", "m1", "r1", "s1"), cached)).toBe(false);
  });

  it("stale when the per-slug mission stamp moves", () => {
    expect(detailIsStale(stamps("j1", "m1", "r1", "s2"), cached)).toBe(true);
  });

  it("stale when the journal stamp moves (threads section)", () => {
    expect(detailIsStale(stamps("j2", "m1", "r1", "s1"), cached)).toBe(true);
  });

  it("stale when the roster stamp moves (crew section)", () => {
    expect(detailIsStale(stamps("j1", "m1", "r2", "s1"), cached)).toBe(true);
  });

  it("ignores the bare missions token — the list's contract cannot vouch for the detail", () => {
    expect(detailIsStale(stamps("j1", "m9", "r1", "s1"), cached)).toBe(false);
  });

  it("a scoped poll missing its mission stamp counts as stale — refetch is the honest response", () => {
    expect(detailIsStale(stamps("j1", "m1", "r1"), cached)).toBe(true);
  });
});

// The first-poll race, replayed deterministically against a real QueryClient:
// the entity query observes v1 → the source changes → the FIRST poll observes
// v2 before the v1 payload has landed → the v1 payload lands. A poll-to-poll
// baseline would have adopted v2 silently and never invalidated; judged
// against the cached payload's own provenance, the next tick catches it.
describe("applyVersionStamps — orderable interleavings", () => {
  it("v2 poll before the v1 payload lands: the next tick still invalidates", async () => {
    const queryClient = new QueryClient();
    let landV1: (payload: MissionsPayload) => void = () => {
      throw new Error("unresolved");
    };
    const inFlight = queryClient.fetchQuery({
      queryKey: missionKeys.list,
      queryFn: () =>
        new Promise<MissionsPayload>((resolve) => {
          landV1 = resolve;
        }),
    });

    // Tick 1: source already at m2, entity response still in flight.
    expect(applyVersionStamps(queryClient, { kind: "missions" }, stamps("j1", "m2", "r1"))).toBe(
      false,
    );

    landV1(listPayload("m1"));
    await inFlight;

    // Tick 2: identical poll result — the stale v1 payload is now caught.
    expect(applyVersionStamps(queryClient, { kind: "missions" }, stamps("j1", "m2", "r1"))).toBe(
      true,
    );
    expect(queryClient.getQueryState(missionKeys.list)?.isInvalidated).toBe(true);
  });

  it("a fresh payload is left alone; a source change then invalidates", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(missionKeys.list, listPayload("m1"));
    expect(applyVersionStamps(queryClient, { kind: "missions" }, stamps("j1", "m1", "r1"))).toBe(
      false,
    );
    expect(queryClient.getQueryState(missionKeys.list)?.isInvalidated).toBe(false);
    expect(applyVersionStamps(queryClient, { kind: "missions" }, stamps("j1", "m2", "r1"))).toBe(
      true,
    );
    expect(queryClient.getQueryState(missionKeys.list)?.isInvalidated).toBe(true);
  });

  it("detail scope: a stale section behind an already-observed poll is caught on the next tick", async () => {
    const queryClient = new QueryClient();
    const key = missionKeys.detail("mission-one");
    let landV1: (payload: MissionDetailPayload) => void = () => {
      throw new Error("unresolved");
    };
    const inFlight = queryClient.fetchQuery({
      queryKey: key,
      queryFn: () =>
        new Promise<MissionDetailPayload>((resolve) => {
          landV1 = resolve;
        }),
    });
    const scope = { kind: "mission", slug: "mission-one" } as const;
    const polled = stamps("j2", "m1", "r1", "s1");

    expect(applyVersionStamps(queryClient, scope, polled)).toBe(false);
    landV1(detailPayload("s1", "j1", "r1"));
    await inFlight;
    expect(applyVersionStamps(queryClient, scope, polled)).toBe(true);
    expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
  });
});
