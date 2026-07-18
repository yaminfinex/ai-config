import { describe, expect, it } from "vitest";
import { loadFailure } from "@/view-models/load-failure";

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

  it("cached data outranks the failure — the page keeps presenting truth", () => {
    expect(loadFailure(new Error("GET /api/v1/missions: 500"), true)).toBeNull();
  });

  it("renders a non-Error throw as its string form, never a blank", () => {
    expect(loadFailure("wire gone", false)).toBe("wire gone");
  });
});
