import { expect, test } from "@playwright/test";
import type { MissionsPayload } from "../src/entities/types";
import {
  allSkins,
  expectDetailPageState,
  expectListPageState,
  startDeadShell,
  startMc,
  useSkin,
} from "./harness";

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
      const shell = await startDeadShell();
      try {
        await page.goto(`${shell.baseUrl}/ui/`);
        await expect(page.getByTestId("load-failure")).toBeVisible({ timeout: 30_000 });
        await expect(page.getByTestId("load-failure")).toContainText("503");
        // Precedence on screen: the failure claim stands alone.
        await expectListPageState(page, { failure: true });
      } finally {
        await shell.stop();
      }
    });

    test("cold dead server: the mission page renders the failure, not eternal loading", async ({
      page,
    }) => {
      const shell = await startDeadShell();
      try {
        await page.goto(`${shell.baseUrl}/ui/mission/mission-one`);
        await expect(page.getByTestId("load-failure")).toBeVisible({ timeout: 30_000 });
        await expectDetailPageState(page, { failure: true });
      } finally {
        await shell.stop();
      }
    });

    test("loading precedes data and never stands in for it", async ({ page }) => {
      // Phase 1 — a source that never answers holds the page in a STABLE
      // loading state, so the state's full claim set is assertable without
      // racing data arrival.
      const hung = await startMc("hang");
      try {
        await page.goto(`${hung.baseUrl}/ui/`);
        await expectListPageState(page, { loading: true });
      } finally {
        await hung.stop();
      }
      // Phase 2 — a slow source proves the transition: loading, then data,
      // each alone.
      const server = await startMc("slow");
      try {
        await page.goto(`${server.baseUrl}/ui/`);
        await expect(page.getByTestId("loading")).toBeVisible();
        await expect(page.getByTestId("mission-row")).toHaveCount(2, { timeout: 20_000 });
        await expectListPageState(page, { rows: 2 });
      } finally {
        await server.stop();
      }
    });

    test("warm cache, then the server dies: cached data stays, marked stale", async ({ page }) => {
      const server = await startMc();
      await page.goto(`${server.baseUrl}/ui/`);
      await expect(page.getByTestId("mission-row")).toHaveCount(2);
      await expectListPageState(page, { rows: 2 });
      // Capture the cached truth before killing it: the resolver caches
      // observations, so this fetch returns the same provenance stamp the
      // page's cached payload carries.
      const captured = (await (
        await fetch(`${server.baseUrl}/api/v1/missions`)
      ).json()) as MissionsPayload;
      // The backend dies out from under a loaded page.
      await server.stop();
      // Cached truth keeps rendering — with the staleness line carrying the
      // EXACT observedAt of the payload it qualifies; a fabricated or
      // reformatted stamp fails here, not just a missing line.
      await expect(page.getByTestId("stale-warning")).toBeVisible({ timeout: 30_000 });
      await expect(page.getByTestId("stale-warning")).toContainText(
        `last observed ${captured.provenance.observedAt}`,
      );
      await expectListPageState(page, { rows: 2, stale: true });
    });
  });
}
