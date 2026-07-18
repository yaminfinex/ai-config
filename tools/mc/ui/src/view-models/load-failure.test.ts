import { describe, expect, it } from "vitest";
import { loadFailure } from "@/view-models/load-failure";
import { stalenessWarning } from "@/view-models/staleness";

describe("loadFailure — the load-failure law", () => {
  it("renders nothing while the fetch is healthy", () => {
    expect(loadFailure(null, false)).toBeNull();
    expect(loadFailure(undefined, false)).toBeNull();
  });

  it("surfaces the failure when nothing is cached to present instead", () => {
    expect(loadFailure(new Error("GET /api/v1/missions: 500"), false)).toBe(
      "GET /api/v1/missions: 500",
    );
  });

  it("cached data outranks the failure — but the claim MOVES to the staleness law, never vanishes", () => {
    const error = new Error("GET /api/v1/missions: 500");
    expect(loadFailure(error, true)).toBeNull();
    // The two-state model: with a payload cached the failure line stays out
    // of the way, and the SAME error must surface as a staleness warning —
    // hiding it entirely would present stale data as current (§6).
    expect(stalenessWarning("2026-07-18T12:00:00Z", error, null)).not.toBeNull();
  });

  it("renders a non-Error throw as its string form, never a blank", () => {
    expect(loadFailure("wire gone", false)).toBe("wire gone");
  });
});
