import { describe, expect, it } from "vitest";
import { skinNames } from "@/skins/contract";
import { nextSkin } from "@/skins/use-skin-choice";

// The registry-closure guard: the toggle cycle is DERIVED from skinNames, and
// this spec pins the consequence — every registered skin is reachable from
// every other by toggling, and the cycle wraps. A skin added to the registry
// can never be silently missing from the toggle again.

describe("the skin toggle cycle", () => {
  it("visits every registered skin once, then wraps to the start", () => {
    for (const start of skinNames) {
      const visited = [start];
      let current = start;
      for (let i = 1; i < skinNames.length; i++) {
        current = nextSkin(current);
        visited.push(current);
      }
      expect([...visited].sort()).toEqual([...skinNames].sort());
      expect(nextSkin(current)).toBe(start);
    }
  });
});
