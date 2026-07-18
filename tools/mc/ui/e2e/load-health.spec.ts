import { expect, test } from "@playwright/test";
import { allSkins, startDeadShell, startMc, useSkin } from "./harness";

// The two-situation load-health law (ARCHITECTURE.md §6) on screen, plus the
// render precedence: failure > loading > empty claim > data.

for (const skin of allSkins) {
  test.describe(`${skin} skin`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test("cold dead server: the list page renders the failure, not a healthy blank", async ({
      page,
    }) => {
      const shell = startDeadShell(9370);
      try {
        await page.goto(`${shell.baseUrl}/ui/`);
        await expect(page.getByTestId("load-failure")).toBeVisible({ timeout: 30_000 });
        await expect(page.getByTestId("load-failure")).toContainText("503");
        // Precedence on screen: the failure claim stands alone.
        await expect(page.getByTestId("loading")).toHaveCount(0);
        await expect(page.getByTestId("missions-empty")).toHaveCount(0);
        await expect(page.getByTestId("mission-row")).toHaveCount(0);
      } finally {
        shell.stop();
      }
    });

    test("cold dead server: the mission page renders the failure, not eternal loading", async ({
      page,
    }) => {
      const shell = startDeadShell(9371);
      try {
        await page.goto(`${shell.baseUrl}/ui/mission/mission-one`);
        await expect(page.getByTestId("load-failure")).toBeVisible({ timeout: 30_000 });
        await expect(page.getByTestId("loading")).toHaveCount(0);
        await expect(page.getByTestId("mission-facts")).toHaveCount(0);
      } finally {
        shell.stop();
      }
    });

    test("loading precedes data and never stands in for it", async ({ page }) => {
      const server = await startMc(9372, "slow");
      try {
        await page.goto(`${server.baseUrl}/ui/`);
        // The slow source holds the page in its honest loading state...
        await expect(page.getByTestId("loading")).toBeVisible();
        await expect(page.getByTestId("mission-row")).toHaveCount(0);
        // ...and real data replaces it.
        await expect(page.getByTestId("mission-row")).toHaveCount(2, { timeout: 20_000 });
        await expect(page.getByTestId("loading")).toHaveCount(0);
      } finally {
        server.stop();
      }
    });

    test("warm cache, then the server dies: cached data stays, marked stale", async ({ page }) => {
      const server = await startMc(9373);
      await page.goto(`${server.baseUrl}/ui/`);
      await expect(page.getByTestId("mission-row")).toHaveCount(2);
      await expect(page.getByTestId("stale-warning")).toHaveCount(0);
      // The backend dies out from under a loaded page.
      server.stop();
      // Cached truth keeps rendering — with the staleness line carrying the
      // payload's own observedAt; the failure line is NOT for this state.
      await expect(page.getByTestId("stale-warning")).toBeVisible({ timeout: 30_000 });
      await expect(page.getByTestId("stale-warning")).toContainText("last observed");
      await expect(page.getByTestId("mission-row")).toHaveCount(2);
      await expect(page.getByTestId("load-failure")).toHaveCount(0);
    });
  });
}
