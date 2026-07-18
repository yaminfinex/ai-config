import { defineConfig } from "@playwright/test";

// The flow suite (ARCHITECTURE.md §8): the REAL Go server with fake
// mish/hcom/herder shell scripts and a seeded journal fixture — no frontend
// mocks, no request interception. Every flow runs under BOTH skins; the
// skin-swap proof asserts the declared behavioural rendering difference via
// the Skin declaration itself.
export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  timeout: 60_000,
  // Live-update assertions ride the real 5s version poll (plus retries), so
  // expectations wait generously; nothing here is animation-timing.
  expect: { timeout: 15_000 },
  fullyParallel: false,
  reporter: [["list"]],
  use: { trace: "retain-on-failure" },
});
