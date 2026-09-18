import type { Locator } from "@playwright/test";

// Mirrors `gridCanvasPadding` in src/layout/justified.ts: rows are laid out
// this far below the scroller's top edge.
const canvasPadding = 14;

// A landed page puts the previous row's bottom edge exactly at the top of the
// scroller, so the row gap is the air above the landed row. The virtual window
// keeps the previous row mounted, so it can be measured in the DOM.
export async function nextPageTop(grid: Locator): Promise<number> {
  return grid.evaluate((element, padding) => {
    const rows = Array.from(
      element.querySelectorAll<HTMLElement>(".grid-row"),
    ).sort((a, b) => a.offsetTop - b.offsetTop);
    const bottom = element.scrollTop + element.clientHeight;
    const index = rows.findIndex(
      (row) => row.offsetTop + row.offsetHeight > bottom,
    );
    const row = rows[index];
    const previous = rows[index - 1];
    let top = bottom;
    if (row && row.offsetTop - padding > element.scrollTop) {
      top = previous
        ? previous.offsetTop + previous.offsetHeight
        : row.offsetTop - padding;
    }
    return Math.min(top, element.scrollHeight - element.clientHeight);
  }, canvasPadding);
}

export interface PageUpLanding {
  top: number;
  withinPage: boolean;
}

// The page-up destination is usually outside the virtualized window before the
// key is pressed, so it is checked after landing: the grid moved back, by at
// most one viewport. Exact landing positions are covered by unit tests in
// src/layout/navigation.test.ts. Returns null while the scroll is still moving.
export async function pageUpLanding(
  grid: Locator,
  from: number,
): Promise<PageUpLanding | null> {
  return grid.evaluate(async (element, from) => {
    const top = element.scrollTop;
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
    if (top >= from || element.scrollTop !== top) return null;
    return {
      top,
      withinPage: top >= Math.max(0, from - element.clientHeight),
    };
  }, from);
}
