import { test as base } from "@playwright/test";

export * from "@playwright/test";

// SEMA_E2E_STRESS=resize delays every ResizeObserver callback, in order, so a
// test that reads layout before the app has reacted to a resize fails on
// demand rather than on one CI run in thirty (#57). CI runs it as e2e-stress.
const stress = process.env.SEMA_E2E_STRESS;
if (stress && stress !== "resize")
  throw new Error(`Unknown SEMA_E2E_STRESS "${stress}"; use "resize".`);

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

// Specs import `test` from here, so every page, popups included, runs under
// the stress mode and console forwarding when they are set.
export const test = base.extend<{ stressMode: undefined }>({
  stressMode: [
    async ({ context }, use, testInfo) => {
      if (stress === "resize")
        await context.addInitScript(delayResizeObservers, 100);
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
