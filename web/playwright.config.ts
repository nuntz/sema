import { defineConfig, devices } from "@playwright/test";

// Each run starts its own Vite on its own port, so a run never tests another
// worktree's server. Give parallel runs distinct SEMA_E2E_PORTs.
const portOverride = process.env.SEMA_E2E_PORT;
const port = Number(portOverride ?? 4173);
const origin = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  testMatch: "*.e2e.ts",
  outputDir: `/tmp/sema-playwright-results${portOverride ? `-${port}` : ""}`,
  fullyParallel: true,
  reporter: "line",
  use: {
    baseURL: origin,
    ...devices["Desktop Chrome"],
    // Tests stub every outside request; an unstubbed one fails here rather
    // than depending on what the real site serves CI runners.
    launchOptions: {
      args: ["--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"],
    },
  },
  webServer: {
    command: `bun run dev --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${origin}/e2e/header-fixture.html`,
    reuseExistingServer: false,
  },
});
