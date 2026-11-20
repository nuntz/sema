import { test as base, type Page } from "@playwright/test";

export * from "@playwright/test";

// SEMA_E2E_STRESS=resize delays every ResizeObserver callback, in order, so a
// test that reads layout before the app has reacted to a resize fails on
// demand rather than on one CI run in thirty (#57). CI runs it as e2e-stress.
// SEMA_E2E_STRESS=animation plays every animation at a tenth of its speed, so
// a test that measures before one finishes fails on demand (#62); wait with
// animationsFinished (./animations). CI doesn't run it yet (#63).
const stress = process.env.SEMA_E2E_STRESS;
if (stress && stress !== "resize" && stress !== "animation")
  throw new Error(
    `Unknown SEMA_E2E_STRESS "${stress}"; use "resize" or "animation".`,
  );

// SEMA_E2E_CONSOLE=1 prints every page's console output beside the test's, so
// a console.log added to app code while diagnosing shows up without wiring
// page.on("console") into the spec (#58).
const forwardConsole = process.env.SEMA_E2E_CONSOLE === "1";

function delayResizeObservers(ms: number) {
  const Native = window.ResizeObserver;
  window.ResizeObserver = class extends Native {
    constructor(callback: ResizeObserverCallback) {
      super((entries, observer) =>
        setTimeout(() => callback(entries, observer), ms),
      );
    }
  };
}

// Chromium only (playwright.config.ts runs Desktop Chrome); the rate holds
// across navigations. A page may close before its session opens.
async function slowAnimations(page: Page) {
  try {
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Animation.enable");
    await cdp.send("Animation.setPlaybackRate", { playbackRate: 0.1 });
  } catch (error) {
    if (!page.isClosed()) throw error;
  }
}

// Specs import `test` from here, so every page, popups included, runs under
// the stress mode and console forwarding when they are set.
export const test = base.extend<{ stressMode: undefined }>({
  stressMode: [
    async ({ context }, use, testInfo) => {
      if (stress === "resize")
        await context.addInitScript(delayResizeObservers, 100);
      if (stress === "animation") {
        await Promise.all(context.pages().map(slowAnimations));
        context.on("page", (page) => void slowAnimations(page));
      }
      if (forwardConsole)
        context.on("console", (message) =>
          console.log(
            `[page ${message.type()}] ${testInfo.titlePath.at(-1)}: ${message.text()}`,
          ),
        );
      await use(undefined);
    },
    { auto: true },
  ],
});
