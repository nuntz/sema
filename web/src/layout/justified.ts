import type { Item } from "../types";

export interface LayoutCell {
  item: Item;
  width: number;
  effectiveSize: "S" | "M" | "L";
}

export interface LayoutRow {
  cells: LayoutCell[];
  height: number;
  top: number;
  gap: number;
}

type DraftRow = { cells: LayoutCell[]; targetWidth: number };

const desktopGap = 10;
const mobileGap = 8;
const targetHeight = 172;
const sizeFactor = { S: 0.8, M: 1.05, L: 1.6 } as const;

function aspect(item: Item): number {
  if (!item.media_w || !item.media_h) return 1.4;
  return Math.max(0.75, Math.min(2.1, item.media_w / item.media_h));
}

function naturalWidth(item: Item, baseUnit: number): number {
  return baseUnit * sizeFactor[item.size] * Math.sqrt(aspect(item));
}

function topAfter(rows: LayoutRow[], gap: number): number {
  const last = rows.at(-1);
  return last ? last.top + last.height + gap : 0;
}

function clamp(value: number, low: number, high: number): number {
  return Math.max(low, Math.min(high, value));
}

export function justify(items: Item[], containerWidth: number): LayoutRow[] {
  if (containerWidth <= 0 || items.length === 0) return [];
  if (containerWidth < 700) return mobileRows(items, containerWidth);

  const baseUnit = clamp(containerWidth / 8, 92, 150);
  const smallRowLimit = Math.max(
    1,
    Math.min(6, Math.floor((containerWidth + desktopGap) / (112 + desktopGap))),
  );
  const drafts: DraftRow[] = [];
  let cells: LayoutCell[] = [];
  let targetWidth = 0;

  const flush = () => {
    if (cells.length === 0) return;
    drafts.push({ cells, targetWidth });
    cells = [];
    targetWidth = 0;
  };

  for (const item of items) {
    const width = naturalWidth(item, baseUnit);
    const hasLarge = cells.some((cell) => cell.effectiveSize === "L");
    const allSmall = cells.every((cell) => cell.effectiveSize === "S");
    let trailingSmall = 0;
    for (let index = cells.length - 1; index >= 0; index--) {
      if (cells[index].effectiveSize !== "S") break;
      trailingSmall++;
    }
    if (
      (item.size === "L" && hasLarge) ||
      (item.size === "S" && allSmall && cells.length === smallRowLimit) ||
      (item.size === "S" && !allSmall && trailingSmall === 4) ||
      (item.size !== "S" && allSmall && cells.length > 4)
    )
      flush();

    const projected = targetWidth + width + desktopGap * cells.length;
    if (cells.length >= 2 && projected > containerWidth) flush();

    cells.push({ item, width, effectiveSize: item.size });
    targetWidth += width;
  }
  flush();

  const rows: LayoutRow[] = [];
  for (let index = 0; index < drafts.length; index++) {
    const draft = drafts[index];
    const lastRow = index === drafts.length - 1;
    const allSmall = draft.cells.every((cell) => cell.effectiveSize === "S");
    let height: number;
    let widths: number[];

    if (allSmall) {
      const desiredWidth = clamp(
        (containerWidth - desktopGap * 5) / 6,
        112,
        132,
      );
      const fittingWidth =
        (containerWidth - desktopGap * (draft.cells.length - 1)) /
        draft.cells.length;
      const smallWidth = Math.min(desiredWidth, fittingWidth);
      widths = draft.cells.map(() => smallWidth);
      height = clamp(smallWidth, 112, 132);
    } else if (lastRow) {
      height = rows.at(-1)?.height ?? targetHeight;
      const available = containerWidth - desktopGap * (draft.cells.length - 1);
      const heightScale = height / targetHeight;
      const fitScale = available / draft.targetWidth;
      const scale = Math.min(heightScale, fitScale);
      widths = draft.cells.map((cell) => cell.width * scale);
    } else {
      const available = containerWidth - desktopGap * (draft.cells.length - 1);
      const scale = available / draft.targetWidth;
      widths = draft.cells.map((cell) => cell.width * scale);
      height = clamp(targetHeight * scale, 128, 192);
    }

    rows.push({
      cells: draft.cells.map((cell, cellIndex) => ({
        ...cell,
        width: widths[cellIndex],
      })),
      height,
      top: topAfter(rows, desktopGap),
      gap: desktopGap,
    });
  }
  return rows;
}

function mobileRows(items: Item[], containerWidth: number): LayoutRow[] {
  const drafts: LayoutCell[][] = [];
  for (let index = 0; index < items.length; ) {
    const item = items[index];
    if (item.size === "L") {
      const next = items[index + 1];
      if (next && next.size !== "L") {
        const unit =
          (containerWidth - mobileGap) / (sizeFactor.L + sizeFactor[next.size]);
        if (unit * sizeFactor[next.size] >= 96) {
          drafts.push([
            { item, width: unit * sizeFactor.L, effectiveSize: "L" },
            {
              item: next,
              width: unit * sizeFactor[next.size],
              effectiveSize: next.size,
            },
          ]);
          index += 2;
          continue;
        }
      }
      drafts.push([{ item, width: containerWidth, effectiveSize: item.size }]);
      index++;
      continue;
    }

    if (
      item.size === "S" &&
      items[index + 1]?.size === "S" &&
      items[index + 2]?.size === "S"
    ) {
      const width = (containerWidth - 2 * mobileGap) / 3;
      drafts.push(
        items.slice(index, index + 3).map((small) => ({
          item: small,
          width,
          effectiveSize: "S" as const,
        })),
      );
      index += 3;
      continue;
    }

    const next = items[index + 1];
    if (next && next.size !== "L") {
      const unit =
        (containerWidth - mobileGap) /
        (sizeFactor[item.size] + sizeFactor[next.size]);
      drafts.push([
        {
          item,
          width: unit * sizeFactor[item.size],
          effectiveSize: item.size,
        },
        {
          item: next,
          width: unit * sizeFactor[next.size],
          effectiveSize: next.size,
        },
      ]);
      index += 2;
      continue;
    }

    drafts.push([
      {
        item,
        width: 120 * sizeFactor[item.size],
        effectiveSize: item.size,
      },
    ]);
    index++;
  }

  const rows: LayoutRow[] = [];
  for (let index = 0; index < drafts.length; index++) {
    const draft = drafts[index];
    const lastRow = index === drafts.length - 1;
    const allSmall = draft.every((cell) => cell.effectiveSize === "S");
    const hasLarge = draft.some((cell) => cell.effectiveSize === "L");
    let cells = draft;
    let height = allSmall
      ? clamp(draft[0].width, 86, 132)
      : hasLarge
        ? clamp(containerWidth * 0.43, 118, 192)
        : clamp(containerWidth * 0.35, 96, 160);

    if (lastRow && rows.length > 0) {
      height = rows.at(-1)?.height ?? height;
      const natural = draft.map((cell) => 120 * sizeFactor[cell.effectiveSize]);
      const available = containerWidth - mobileGap * (draft.length - 1);
      const scale = Math.min(1, available / natural.reduce((a, b) => a + b, 0));
      cells = draft.map((cell, cellIndex) => ({
        ...cell,
        width: natural[cellIndex] * scale,
      }));
    }

    rows.push({
      cells,
      height,
      top: topAfter(rows, mobileGap),
      gap: mobileGap,
    });
  }
  return rows;
}

export function totalHeight(rows: LayoutRow[]): number {
  const last = rows.at(-1);
  return last ? last.top + last.height : 0;
}

export function visibleRows(
  rows: LayoutRow[],
  scrollTop: number,
  viewportHeight: number,
): LayoutRow[] {
  const overscan = viewportHeight * 2;
  const start = Math.max(0, scrollTop - overscan);
  const end = scrollTop + viewportHeight + overscan;
  return rows.filter((row) => row.top + row.height >= start && row.top <= end);
}
