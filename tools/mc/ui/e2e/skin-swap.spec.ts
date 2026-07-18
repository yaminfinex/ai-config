import { expect, type Page, test } from "@playwright/test";
import { skins } from "../src/skins/index";
import { allSkins, type McServer, type SkinUnderTest, startMc, useSkin } from "./harness";

// The skin-swap proof (D4, ARCHITECTURE.md §3/§8): two skins over the same
// view-model props, differing in BOTH halves — token values and at least one
// DECLARED behavioural rendering difference — while behaviour itself cannot
// vary. The declaration on the Skin object drives the assertions: the test
// derives what to expect from `activeThreadRendering` instead of hardcoding
// per-skin DOM knowledge, so an undeclared divergence has nowhere to hide.

let server: McServer;

test.beforeAll(async () => {
  server = await startMc(9350);
});

test.afterAll(async () => {
  await server.stop();
});

function otherSkin(skin: SkinUnderTest): SkinUnderTest {
  return skin === "minimal" ? "terminal" : "minimal";
}

// The declared difference: where the active thread renders. In-row means
// inside the row that opened it; panel means inside the dedicated panel and
// NOT inside any row.
async function expectActiveThreadPerDeclaration(page: Page, skin: SkinUnderTest): Promise<void> {
  const rendering = skins[skin].activeThreadRendering;
  const inRow = page.locator("[data-thread-id]").getByTestId("active-thread");
  const inPanel = page.getByTestId("active-thread-panel").getByTestId("active-thread");
  if (rendering === "in-row") {
    await expect(inRow).toHaveCount(1);
    await expect(inPanel).toHaveCount(0);
  } else {
    await expect(inPanel).toHaveCount(1);
    await expect(inRow).toHaveCount(0);
  }
  // Same view-model content under every rendering: the real last message.
  await expect(page.getByTestId("active-thread-text")).toContainText("which shape wins?");
}

async function computedLook(page: Page): Promise<{ background: string; speechFace: string }> {
  return {
    background: await page.evaluate(() => getComputedStyle(document.body).backgroundColor),
    speechFace: await page
      .getByTestId("active-thread-text")
      .evaluate((el) => getComputedStyle(el).fontFamily),
  };
}

test("the two skins declare different behavioural renderings — the proof has a subject", () => {
  const declared = new Set(Object.values(skins).map((skin) => skin.activeThreadRendering));
  expect(declared.size).toBeGreaterThan(1);
});

for (const skin of allSkins) {
  test.describe(`starting from ${skin}`, () => {
    test.beforeEach(async ({ page }) => {
      await useSkin(page, skin);
    });

    test("the toggle swaps both halves over the SAME engaged state", async ({ page }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await page.locator('[data-thread-id="task-x"]').getByTestId("thread-row").click();
      await expectActiveThreadPerDeclaration(page, skin);
      await expect(page.locator("html")).toHaveAttribute("data-theme", skin);
      const before = await computedLook(page);

      await page.getByTestId("skin-toggle").click();

      // Visual half: data-theme flipped and computed styles actually changed.
      const flipped = otherSkin(skin);
      await expect(page.locator("html")).toHaveAttribute("data-theme", flipped);
      await expectActiveThreadPerDeclaration(page, flipped);
      const after = await computedLook(page);
      expect(after.background).not.toBe(before.background);
      expect(after.speechFace).not.toBe(before.speechFace);
    });

    test("behaviour is identical: one active thread, replace by default, toggle closes", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await expect(page.getByTestId("active-thread")).toHaveCount(0);

      await page.locator('[data-thread-id="task-x"]').getByTestId("thread-row").click();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);
      await expect(page.getByTestId("active-thread-text")).toContainText("which shape wins?");

      // Replace, never multiply.
      await page.locator('[data-thread-id="task-y"]').getByTestId("thread-row").click();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);
      await expect(page.getByTestId("active-thread-text")).toContainText("no messages");

      // Toggling the engaged thread closes it.
      await page.locator('[data-thread-id="task-y"]').getByTestId("thread-row").click();
      await expect(page.getByTestId("active-thread")).toHaveCount(0);
    });

    test("refresh loses nothing: the engaged thread and the skin choice survive", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await page.locator('[data-thread-id="task-x"]').getByTestId("thread-row").click();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);

      await page.reload();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);
      await expect(page.locator("html")).toHaveAttribute("data-theme", skin);
      await expectActiveThreadPerDeclaration(page, skin);
    });

    test("navigation never clears the layout: the thread slot survives a round trip", async ({
      page,
    }) => {
      await page.goto(`${server.baseUrl}/ui/mission/mission-one`);
      await page.locator('[data-thread-id="task-x"]').getByTestId("thread-row").click();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);

      await page.getByTestId("mc-home").click();
      await expect(page.getByTestId("mission-row")).toHaveCount(2);
      await page.getByTestId("mission-row").nth(1).click();
      await expect(page.getByTestId("active-thread")).toHaveCount(1);
    });
  });
}
