import { expect, type Page } from "@playwright/test";

// Chromium has no status bar or standalone mode: override the real env() inset
// and switch on the production standalone rules through CSSOM.
export async function simulateStatusBar(page: Page, top: number, bottom = 0) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setSafeAreaInsetsOverride", {
    insets: { top, bottom },
  });
}

export async function activateStandalone(page: Page) {
  const activated = await page.evaluate(() => {
    let count = 0;
    const readable = Array.from(document.styleSheets).filter((sheet) => {
      try {
        return Boolean(sheet.cssRules);
      } catch {
        return false; // Cross-origin, e.g. Google Fonts.
      }
    });
    for (const sheet of readable)
      for (const rule of Array.from(sheet.cssRules))
        if (
          rule instanceof CSSMediaRule &&
          rule.media.mediaText.includes("display-mode: standalone")
        ) {
          rule.media.mediaText = rule.media.mediaText.replace(
            "(display-mode: standalone)",
            "all",
          );
          count++;
        }
    return count;
  });
  expect(activated).toBeGreaterThan(0);
}

// iOS tints the status bar from the root background.
export const rootTint = (page: Page) =>
  page.evaluate(() => [
    getComputedStyle(document.documentElement).backgroundColor,
    getComputedStyle(document.body).backgroundColor,
  ]);
