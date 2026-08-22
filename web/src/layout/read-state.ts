import type { LayoutRow } from "./justified";

export function fullyPassedRows(
  rows: LayoutRow[],
  previousIndex: number,
  scrollTop: number,
): { rows: LayoutRow[]; lastIndex: number } {
  let lastIndex = previousIndex;
  while (
    lastIndex + 1 < rows.length &&
    rows[lastIndex + 1].top + rows[lastIndex + 1].height < scrollTop
  )
    lastIndex++;
  return { rows: rows.slice(previousIndex + 1, lastIndex + 1), lastIndex };
}

export function shouldMarkAtBottom(
  userInitiated: boolean,
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
): boolean {
  return (
    userInitiated &&
    clientHeight > 0 &&
    scrollHeight > clientHeight &&
    scrollTop + clientHeight >= scrollHeight - 1
  );
}

export function shouldLoadNextPage(
  hasMore: boolean,
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
): boolean {
  return hasMore && scrollHeight - scrollTop - clientHeight < clientHeight * 2;
}

export function shouldShowEndCard(hasMore: boolean): boolean {
  return !hasMore;
}

export function intersectingRowIDs(
  rows: LayoutRow[],
  scrollTop: number,
  viewportHeight: number,
  rowOffset = 0,
): string[] {
  const bottom = scrollTop + viewportHeight;
  return rows.flatMap((row) => {
    const top = row.top + rowOffset;
    if (top + row.height < scrollTop || top > bottom) return [];
    return row.cells.map((cell) => cell.item.item_id);
  });
}
