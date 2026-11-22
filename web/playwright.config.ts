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
  // CI uploads the JSON report from every e2e leg, and
  // scripts/ci-e2e-report.sh reads a run's failures and slowest tests from it.
  reporter: [
    ["line"],
    [
      "json",
      {
        outputFile: `/tmp/sema-playwright-report${portOverride ? `-${port}` : ""}.json`,
      },
    ],
  ],
  use: {
    baseURL: origin,
    ...devices["Desktop Chrome"],
    // A failed test keeps its trace in outputDir, which CI uploads, so a
    // CI-only failure shows where each action landed. #70's one failure left
    // only error-context.md, and nothing local reproduced it.
    trace: "retain-on-failure",
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
