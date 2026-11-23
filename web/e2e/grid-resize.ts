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
