import { expect, type Page } from "./test";

// Resizes the page and waits until the grid has laid out for its new width.
// The grid relays out from a ResizeObserver a frame or more after the resize,
// replacing its cells, so a read before then sees cells about to be detached
// (#57). Resizing before the grid renders needs no wait.
export async function resizeGrid(page: Page, width: number, height: number) {
  await page.setViewportSize({ width, height });
  await expect
    .poll(() =>
      page
        .locator(".grid-scroll")
        .evaluate((grid: HTMLElement) =>
          grid.dataset.layoutWidth === String(grid.clientWidth)
            ? "laid out"
            : `laid out for ${grid.dataset.layoutWidth}px, now ${grid.clientWidth}px`,
        ),
    )
    .toBe("laid out");
}
