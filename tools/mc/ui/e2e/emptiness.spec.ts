import { expect, test } from "@playwright/test";
import { allSkins, expectListPageState, type McServer, startMc, useSkin } from "./harness";

// Degraded is not empty (ARCHITECTURE.md §6), on screen: an observed zero
// claims "no missions"; an unobservable source shows its warning and claims
// nothing. Two real servers — one whose mish reports an empty repo, one
// whose mish is down.

let emptyServer: McServer;
let degradedServer: McServer;

test.beforeAll(async () => {
  emptyServer = await startMc("empty");
  degradedServer = await startMc("degraded");
});

test.afterAll(async () => {
  await emptyServer.stop();
  await degradedServer.stop();
});

for (const skin of allSkins) {
  test.describe(`${skin} skin`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test("healthy empty: the observed zero claims 'no missions' and nothing else", async ({
      page,
    }) => {
      await page.goto(`${emptyServer.baseUrl}/ui/`);
      await expectListPageState(page, { empty: true });
    });

    test("degraded 200: the warning renders and every other claim stays silent", async ({
      page,
    }) => {
      await page.goto(`${degradedServer.baseUrl}/ui/`);
      await expect(page.getByTestId("list-warning")).toContainText("failed");
      // Degradation arrives as HTTP 200 — the failure line is for a dead
      // wire, not a degraded source; the empty claim would be a lie.
      await expectListPageState(page, { warning: true });
    });
  });
}
