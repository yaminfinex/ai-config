import { describe, expect, it } from "vitest";
import type { MissionsPayload } from "@/entities/types";
import { loadFailure } from "@/view-models/load-failure";
import { missionListVM } from "@/view-models/mission-list";
import { stalenessWarning } from "@/view-models/staleness";

// The render contract, pinned at the VM boundary: for every load state the
// view-model layer raises EXACTLY the claims that state deserves — failure >
// loading > empty claim > data, with staleness riding beside cached data.
// Skins render claims and never decide them; the per-skin proof that the
// branches are honored on screen is the flow suite (e2e/), but the contract
// itself is law here. Each case below is one full page state, composed exactly
// as the routes compose it (loadFailure(error, hasPayload) +
// missionListVM(payload) + stalenessWarning(vm.observedAt, error, pollError)).

const OBSERVED = "2026-07-18T12:00:00Z";

function healthyEmpty(): MissionsPayload {
  return {
    missions: [],
    warning: "",
    provenance: { source: "mish status", observedAt: OBSERVED, version: "v1" },
  };
}

function degraded(): MissionsPayload {
  return { ...healthyEmpty(), warning: "mish unreachable" };
}

describe("the render contract — one honest claim set per load state", () => {
  it("loading (no payload, no error): no failure, no empty claim, no staleness", () => {
    const vm = missionListVM(undefined);
    expect(loadFailure(null, false)).toBeNull();
    expect(vm.empty).toBe(false);
    expect(stalenessWarning(vm.observedAt, null, null)).toBeNull();
  });

  it("first-load failure (error, no payload): ONLY the failure claim fires", () => {
    const error = new Error("GET /api/v1/missions: 503");
    const vm = missionListVM(undefined);
    expect(loadFailure(error, false)).not.toBeNull();
    expect(vm.empty).toBe(false);
    expect(stalenessWarning(vm.observedAt, error, null)).toBeNull();
  });

  it("degraded 200 (warning, zero rows): the warning fires, the empty claim does NOT", () => {
    const vm = missionListVM(degraded());
    expect(loadFailure(null, true)).toBeNull();
    expect(vm.warning).toBe("mish unreachable");
    expect(vm.empty).toBe(false);
  });

  it("healthy empty (observed zero): ONLY the empty claim fires", () => {
    const vm = missionListVM(healthyEmpty());
    expect(loadFailure(null, true)).toBeNull();
    expect(vm.warning).toBeNull();
    expect(vm.empty).toBe(true);
    expect(stalenessWarning(vm.observedAt, null, null)).toBeNull();
  });

  it("cached + failing refetch: data stands, failure stays out, staleness fires", () => {
    const error = new Error("GET /api/v1/missions: 503");
    const vm = missionListVM(healthyEmpty());
    expect(loadFailure(error, true)).toBeNull();
    expect(stalenessWarning(vm.observedAt, error, null)).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });

  it("cached + failing version poll: same — the invalidation contract down means unverified", () => {
    const vm = missionListVM(healthyEmpty());
    expect(loadFailure(null, true)).toBeNull();
    expect(stalenessWarning(vm.observedAt, null, new Error("GET /api/v1/version: 503"))).toBe(
      `may be stale — last observed ${OBSERVED}`,
    );
  });
});
