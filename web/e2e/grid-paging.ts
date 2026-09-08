import type { Locator } from "@playwright/test";

export async function nextPageTop(grid: Locator): Promise<number> {
  return grid.evaluate((element) => {
    const bottom = element.scrollTop + element.clientHeight;
    const row = Array.from(
      element.querySelectorAll<HTMLElement>(".grid-row"),
    ).find(
      (row) =>
        row.offsetTop < bottom && row.offsetTop + row.offsetHeight > bottom,
    );
    const top =
      row && row.offsetTop >= element.scrollTop + 1 ? row.offsetTop : bottom;
    return Math.min(top, element.scrollHeight - element.clientHeight);
  });
}
