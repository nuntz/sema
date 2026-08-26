import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: "*.e2e.ts",
  outputDir: "/tmp/sema-playwright-results",
  fullyParallel: true,
  reporter: "line",
  use: {
    baseURL: "http://127.0.0.1:4173",
    ...devices["Desktop Chrome"],
  },
  webServer: {
    command: "bun run dev --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173/e2e/header-fixture.html",
    reuseExistingServer: true,
  },
});
