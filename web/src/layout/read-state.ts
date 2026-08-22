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

export function chronoBoundary(row: LayoutRow): string {
  return row.cells.reduce(
    (oldest, cell) =>
      cell.item.published_ts < oldest ? cell.item.published_ts : oldest,
    row.cells[0].item.published_ts,
  );
}

export function chronoRead(published: string, boundary: string): boolean {
  return boundary !== "" && published >= boundary;
}
