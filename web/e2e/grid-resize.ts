import { expect, type Page } from "./test";

// Resizes the page and waits until the grid has laid out for its new size.
// The grid relays out from a ResizeObserver a frame or more after the resize,
// replacing its cells, so a read before then sees cells about to be detached
// (#57). Its canvas also ends a viewport below the last row, so a height-only
// resize moves the end of the grid (#58). Resizing before the grid renders
// needs no wait.
export async function resizeGrid(page: Page, width: number, height: number) {
  await page.setViewportSize({ width, height });
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((grid: HTMLElement) => {
        const laidOut = `${grid.dataset.layoutWidth}x${grid.dataset.layoutHeight}`;
        const now = `${grid.clientWidth}x${grid.clientHeight}`;
        return laidOut === now
          ? "laid out"
          : `laid out for ${laidOut}px, now ${now}px`;
      }),
    )
    .toBe("laid out");
}

// Waits until the grid's scroll extent (scrollHeight - clientHeight) has held
// for half a second and returns it. Heights the grid measures with a
// ResizeObserver, such as the top slot's or a Story Lead's, can still move the
// extent after resizeGrid returns, and the headline heights carry no marker to
// wait for, so a test that asserts against the extent reads it only once it
// holds; #83 read it once and missed the bottom moving up 2px. It settles the
// rows mounted now: rows that mount only after a scroll can move it again. On
// its cap the poll fails with every reading it took.
export async function settledScrollExtent(page: Page) {
  const holdMs = 500;
  const grid = page.locator(".grid-scroll");
  const readings: { at: number; extent: number }[] = [];
  await expect
    .poll(
      async () => {
        const extent = await grid.evaluate(
          (element: HTMLElement) => element.scrollHeight - element.clientHeight,
        );
        const at = Date.now();
        readings.push({ at, extent });
        let runStart = readings.length - 1;
        while (runStart > 0 && readings[runStart - 1].extent === extent)
          runStart--;
        const held = at - readings[runStart].at;
        if (held >= holdMs) return "held";
        const start = readings[0].at;
        return `held ${held}ms of ${holdMs}ms; readings: ${readings
          .map((reading) => `${reading.extent} at ${reading.at - start}ms`)
          .join(", ")}`;
      },
      { intervals: [100] },
    )
    .toBe("held");
  return readings[readings.length - 1].extent;
}
