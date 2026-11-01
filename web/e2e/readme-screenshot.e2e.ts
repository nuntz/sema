import { fileURLToPath } from "node:url";
import { expect, test } from "@playwright/test";
import { openShowcase } from "./showcase-fixture";

// Regenerates docs/screenshot.png for the README: `make screenshot`. It writes into the
// repository, so ordinary e2e runs skip it.
const output = fileURLToPath(
  new URL("../../docs/screenshot.png", import.meta.url),
);

test("README screenshot", async ({ page }) => {
  test.skip(!process.env.SEMA_SCREENSHOT, "run with make screenshot");
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await openShowcase(page);
  const images = page.locator("[data-item-id] img");
  await expect(images.first()).toBeVisible();
  await page.waitForLoadState("networkidle");
  await page.evaluate(() =>
    Promise.all([...document.images].map((image) => image.decode())),
  );
  await page.screenshot({ path: output });
});
