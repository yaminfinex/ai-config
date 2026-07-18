import { expect, test } from "@playwright/test";
import { allSkins, type McServer, startMc, useSkin } from "./harness";

// Warning-token transitions, live: the roster is observed per request, so a
// dying upstream moves the roster token, the scoped version poll notices, the
// entity refetches, and the warning appears WITHOUT a reload — then clears
// the same way when the upstream recovers. This drives the whole §6 loop
// (provenance stamp → poll → cache-provenance comparison → invalidate →
// refetch) through the real running stack. The missions family is not
// exercised live here: its resolver caches observations for a minute by
// design, so its degraded/healthy states are covered as separate servers in
// emptiness.spec.ts.

let server: McServer;

test.beforeAll(async () => {
  server = await startMc(9340);
});

test.afterAll(() => {
  server.stop();
});

for (const skin of allSkins) {
  test.describe(`${skin} skin`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test.afterEach(() => {
      server.unflip("herder-down");
    });

    test("a roster warning arrives live — and degraded is not empty on screen", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await expect(page.getByTestId("agent-row")).toContainText("builder-lobo");
      await expect(page.getByTestId("roster-warning")).toHaveCount(0);

      server.flip("herder-down");
      // One poll cycle later the page tells the truth: warning up, agents
      // gone, and NO "no agents" claim — the roster became unobservable,
      // not empty.
      await expect(page.getByTestId("roster-warning")).toBeVisible({ timeout: 20_000 });
      await expect(page.getByTestId("agent-row")).toHaveCount(0);
      await expect(page.getByTestId("crew-empty")).toHaveCount(0);

      server.unflip("herder-down");
      // Recovery is also live: warning clears, crew returns.
      await expect(page.getByTestId("roster-warning")).toHaveCount(0, { timeout: 20_000 });
      await expect(page.getByTestId("agent-row")).toContainText("builder-lobo");
    });
  });
}
