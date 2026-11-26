import type { Page } from "./test";

/** The reader once it is open. It stays mounted, and matches `.reader`, through
 * its 220ms exit, so `.reader` being visible passes on a reader on its way out:
 * a check that the reader stayed open can't fail, and one that a click opened
 * it passes on the previous reader (#69). `bun run lint` sends specs here
 * (no-closing-reader-visible.grit). */
export const openReader = (page: Page) => page.locator(".reader:not(.closing)");

/** Dispatches one touch event at `x` on the reader's scroller, as a finger on
 * the reader would. */
export async function touchReader(page: Page, type: string, x: number) {
  return page.locator(".reader-scroll").evaluate(
    (target, { type, x }) => {
      const point = new Touch({
        identifier: 1,
        target,
        clientX: x,
        clientY: 300,
      });
      return target.dispatchEvent(
        new TouchEvent(type, {
          bubbles: true,
          cancelable: true,
          touches: type === "touchend" || type === "touchcancel" ? [] : [point],
          changedTouches: [point],
        }),
      );
    },
    { type, x },
  );
}
