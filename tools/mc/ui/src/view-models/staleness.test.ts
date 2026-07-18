import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { missionKeys } from "@/entities/missions";
import type { MissionsPayload, VersionStamps } from "@/entities/types";
import { missionListVM } from "@/view-models/mission-list";
import { stalenessWarning } from "@/view-models/staleness";

const OBSERVED = "2026-07-18T12:00:00Z";

function listPayload(): MissionsPayload {
  return {
    missions: [],
    warning: "",
    provenance: { source: "mish status", observedAt: OBSERVED, version: "v1" },
  };
}

describe("stalenessWarning — the staleness law", () => {
  it("no cached payload means no staleness claim — that state is the failure law's", () => {
    expect(stalenessWarning(null, new Error("down"), null)).toBeNull();
    expect(stalenessWarning(null, null, new Error("down"))).toBeNull();
  });

  it("cached data with both currency channels healthy is current — no warning", () => {
    expect(stalenessWarning(OBSERVED, null, null)).toBeNull();
    expect(stalenessWarning(OBSERVED, undefined, undefined)).toBeNull();
  });

  it("a failing entity refetch marks cached data unverified, carrying its own observedAt", () => {
    expect(stalenessWarning(OBSERVED, new Error("GET /api/v1/missions: 503"), null)).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });

  it("a failing version poll marks cached data unverified — the invalidation contract is down", () => {
    expect(stalenessWarning(OBSERVED, null, new Error("GET /api/v1/version: 503"))).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });
});

// The two failure modes the law exists for, replayed against a real
// QueryClient: TanStack keeps the cached payload when a refetch fails
// (data + status "error" — the state the route actually reads), and the
// derivation must flag it instead of presenting it as current.
describe("stalenessWarning — real QueryClient integration", () => {
  it("cached payload + failed refetch: data is retained AND marked unverified", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await queryClient.prefetchQuery({
      queryKey: missionKeys.list,
      queryFn: async () => listPayload(),
    });
    await queryClient.prefetchQuery({
      queryKey: missionKeys.list,
      queryFn: async (): Promise<MissionsPayload> => {
        throw new Error("GET /api/v1/missions: 503");
      },
    });

    const state = queryClient.getQueryCache().find({ queryKey: missionKeys.list })?.state;
    // The TanStack state the route reads: payload retained, error settled,
    // no longer pending — so the failure law yields null (data outranks it)
    // and the staleness law must carry the claim instead.
    expect(state?.data).toBeDefined();
    expect(state?.error).not.toBeNull();
    expect(state?.status).toBe("error");

    const vm = missionListVM(state?.data as MissionsPayload);
    expect(stalenessWarning(vm.observedAt, state?.error, null)).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });

  it("cached payload + failed version poll: healthy entity data is still marked unverified", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await queryClient.prefetchQuery({
      queryKey: missionKeys.list,
      queryFn: async () => listPayload(),
    });
    const versionKey = ["version"] as const;
    await queryClient.prefetchQuery({
      queryKey: versionKey,
      queryFn: async (): Promise<VersionStamps> => {
        throw new Error("GET /api/v1/version: 503");
      },
    });

    const entity = queryClient.getQueryCache().find({ queryKey: missionKeys.list })?.state;
    const poll = queryClient.getQueryCache().find({ queryKey: versionKey })?.state;
    expect(entity?.error).toBeNull();
    expect(poll?.error).not.toBeNull();

    const vm = missionListVM(entity?.data as MissionsPayload);
    expect(stalenessWarning(vm.observedAt, entity?.error, poll?.error)).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });
});
