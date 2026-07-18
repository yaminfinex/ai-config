import { expect, test } from "@playwright/test";
import { allSkins, type McServer, startMc, useSkin } from "./harness";

// The slice read path against the real stack: first mount, mission detail,
// mission-to-mission scope change, unknown slug — identically under both
// skins. Fixture truth: mission-broken (degraded row, board unavailable) and
// mission-one (healthy, owner riley, board 1 to-do; threads task-x decide +
// task-y reply; crew builder-lobo).

let server: McServer;

test.beforeAll(async () => {
  server = await startMc(9310);
});

test.afterAll(() => {
  server.stop();
});

for (const skin of allSkins) {
  test.describe(`${skin} skin`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test("first mount renders the real mission list", async ({ page }) => {
      await page.goto(`${server.baseUrl}/ui/`);
      const rows = page.getByTestId("mission-row");
      await expect(rows).toHaveCount(2);
      // Slug sort law: mission-broken before mission-one.
      await expect(rows.first()).toHaveAttribute("data-slug", "mission-broken");
      await expect(rows.nth(1)).toHaveAttribute("data-slug", "mission-one");
      // The healthy row renders its facts; the degraded row renders its
      // warnings and NO task summary (board unavailable — gap honesty).
      await expect(rows.nth(1)).toContainText("riley");
      await expect(page.getByText("board missing: backlog/config.yml")).toBeVisible();
      await expect(page.getByTestId("task-summary")).toHaveCount(1);
      // Healthy list: no list-level warning, no failure, no empty claim.
      await expect(page.getByTestId("list-warning")).toHaveCount(0);
      await expect(page.getByTestId("load-failure")).toHaveCount(0);
      await expect(page.getByTestId("missions-empty")).toHaveCount(0);
    });

    test("mission page renders all three sections from the wire", async ({ page }) => {
      await page.goto(`${server.baseUrl}/ui/`);
      await page.getByTestId("mission-row").nth(1).click();
      await expect(page).toHaveURL(`${server.baseUrl}/ui/mission/mission-one`);
      // Mission section: facts line + task summary.
      await expect(page.getByTestId("mission-facts")).toContainText("owner riley");
      await expect(page.getByTestId("mission-facts")).toContainText("active");
      await expect(page.getByTestId("task-summary")).toContainText("1 to do");
      // Threads section, sort law on screen: decide before reply.
      const threads = page.getByTestId("thread-row");
      await expect(threads).toHaveCount(2);
      await expect(threads.first()).toContainText("grok A/B");
      await expect(threads.nth(1)).toContainText("quiet reply thread");
      // Crew section from the live roster.
      await expect(page.getByTestId("agent-row")).toContainText("builder-lobo");
      await expect(page.getByTestId("crew-empty")).toHaveCount(0);
    });

    test("a mission page is a shareable URL — deep link renders without the list", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await expect(page.getByTestId("mission-facts")).toContainText("owner riley");
      await expect(page.getByTestId("thread-row")).toHaveCount(2);
    });

    test("mission-to-mission scope change: the next page renders ITS truth and polls ITS scope", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await expect(page.getByTestId("thread-row")).toHaveCount(2);
      const scopedPoll = page.waitForRequest((req) =>
        req.url().includes("/api/v1/version?mission=mission-broken"),
      );
      await page.goto(`${server.baseUrl}/ui/mission/mission-broken`);
      // The degraded mission's own sections: warnings, no facts to fabricate,
      // its single thread, no crew — and because the roster was healthily
      // observed, the empty claim is honest here.
      await expect(page.getByTestId("mission-warning").first()).toBeVisible();
      await expect(page.getByTestId("mission-facts")).toHaveCount(0);
      await expect(page.getByTestId("thread-row")).toHaveCount(1);
      await expect(page.getByTestId("thread-row")).toContainText("other mission thread");
      await expect(page.getByTestId("crew-empty")).toBeVisible();
      // The invalidation scope follows the page (observed, not intercepted).
      await scopedPoll;
    });

    test("unknown slug renders the refusal honestly — no fabrication", async ({ page }) => {
      await page.goto(`${server.baseUrl}/ui/mission/ghost`);
      await expect(page.getByTestId("mission-warning")).toContainText("mission ghost not found");
      await expect(page.getByTestId("mission-facts")).toHaveCount(0);
      await expect(page.getByTestId("task-summary")).toHaveCount(0);
      await expect(page.getByTestId("threads-empty")).toBeVisible();
      await expect(page.getByTestId("crew-empty")).toBeVisible();
      await expect(page.getByTestId("load-failure")).toHaveCount(0);
    });
  });
}
