import { expect, test } from "@playwright/test";
import { allSkins, type McServer, startMc, useSkin } from "./harness";

// Degraded is not empty (ARCHITECTURE.md §6), on screen: an observed zero
// claims "no missions"; an unobservable source shows its warning and claims
// nothing. Two real servers — one whose mish reports an empty repo, one
// whose mish is down.

let emptyServer: McServer;
let degradedServer: McServer;

test.beforeAll(async () => {
  emptyServer = await startMc(9320, "empty");
  degradedServer = await startMc(9321, "degraded");
});

test.afterAll(() => {
  emptyServer.stop();
  degradedServer.stop();
});

for (const skin of allSkins) {
  test.describe(`${skin} skin`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test("healthy empty: the observed zero claims 'no missions'", async ({ page }) => {
      await page.goto(`${emptyServer.baseUrl}/ui/`);
      await expect(page.getByTestId("missions-empty")).toBeVisible();
      await expect(page.getByTestId("list-warning")).toHaveCount(0);
      await expect(page.getByTestId("load-failure")).toHaveCount(0);
      await expect(page.getByTestId("mission-row")).toHaveCount(0);
    });

    test("degraded 200: the warning renders and the empty claim stays silent", async ({ page }) => {
      await page.goto(`${degradedServer.baseUrl}/ui/`);
      await expect(page.getByTestId("list-warning")).toContainText("failed");
      await expect(page.getByTestId("missions-empty")).toHaveCount(0);
      await expect(page.getByTestId("mission-row")).toHaveCount(0);
      // Degradation arrives as HTTP 200 — the failure line is for a dead
      // wire, not a degraded source.
      await expect(page.getByTestId("load-failure")).toHaveCount(0);
    });
  });
}
