import { test as base } from "@playwright/test";

export * from "@playwright/test";

// SEMA_E2E_STRESS=resize delays every ResizeObserver callback, in order, so a
// test that reads layout before the app has reacted to a resize fails on
// demand rather than on one CI run in thirty (#57).
const stress = process.env.SEMA_E2E_STRESS;
if (stress && stress !== "resize")
  throw new Error(`Unknown SEMA_E2E_STRESS "${stress}"; use "resize".`);

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
// the stress mode when it is set.
export const test = base.extend<{ stressMode: undefined }>({
  stressMode: [
    async ({ context }, use) => {
      if (stress === "resize")
        await context.addInitScript(delayResizeObservers, 100);
      await use(undefined);
    },
    { auto: true },
  ],
});
