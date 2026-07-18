import { describe, expect, it } from "vitest";
import type { Provenance, VersionStamps } from "@/entities/types";
import { invalidationsFor, versionUrl } from "@/entities/version";

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

describe("invalidationsFor — missions-list scope", () => {
  const scope = { kind: "missions" } as const;

  it("invalidates the list when the missions family token moves", () => {
    expect(invalidationsFor(scope, stamps("j1", "m1", "r1"), stamps("j1", "m2", "r1"))).toEqual([
      ["missions"],
    ]);
  });

  it("ignores journal and roster moves — they stamp nothing the list renders", () => {
    expect(invalidationsFor(scope, stamps("j1", "m1", "r1"), stamps("j2", "m1", "r2"))).toEqual([]);
  });

  it("invalidates nothing when nothing moved", () => {
    expect(invalidationsFor(scope, stamps("j1", "m1", "r1"), stamps("j1", "m1", "r1"))).toEqual([]);
  });
});

describe("invalidationsFor — mission-detail scope", () => {
  const scope = { kind: "mission", slug: "mission-one" } as const;
  const detailKey = [["mission", "mission-one"]];

  it("invalidates the detail when the per-slug mission stamp moves", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j1", "m1", "r1", "s2")),
    ).toEqual(detailKey);
  });

  it("invalidates the detail when the journal stamp moves (threads section)", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j2", "m1", "r1", "s1")),
    ).toEqual(detailKey);
  });

  it("invalidates the detail when the roster stamp moves (crew section)", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j1", "m1", "r2", "s1")),
    ).toEqual(detailKey);
  });

  it("ignores the bare missions token — the list's contract cannot vouch for the detail", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j1", "m2", "r1", "s1")),
    ).toEqual([]);
  });

  it("invalidates nothing when all three watched stamps hold", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j1", "m1", "r1", "s1")),
    ).toEqual([]);
  });

  it("treats a mission stamp appearing or vanishing as a move", () => {
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1"), stamps("j1", "m1", "r1", "s1")),
    ).toEqual(detailKey);
    expect(
      invalidationsFor(scope, stamps("j1", "m1", "r1", "s1"), stamps("j1", "m1", "r1")),
    ).toEqual(detailKey);
  });
});
