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
}

const gap = 10;
const sizeFactor = { S: 1, M: 1.6, L: 2.6 } as const;

function aspect(item: Item): number {
  if (!item.media_w || !item.media_h) return 1.4;
  return Math.max(0.75, Math.min(2.1, item.media_w / item.media_h));
}

function rowHeight(cells: LayoutCell[]): number {
  if (cells.some((cell) => cell.effectiveSize === "L")) return 208;
  if (cells.some((cell) => cell.effectiveSize === "M")) return 176;
  return 148;
}

export function justify(items: Item[], containerWidth: number): LayoutRow[] {
  if (containerWidth <= 0 || items.length === 0) return [];
  if (containerWidth < 700) return mobileRows(items, containerWidth);
  const baseUnit = Math.max(92, Math.min(150, containerWidth / 8));
  const rows: LayoutRow[] = [];
  let pending: LayoutCell[] = [];
  let targetWidth = 0;
  let largeUsed = false;

  const flush = () => {
    if (pending.length === 0) return;
    const available = Math.max(1, containerWidth - gap * (pending.length - 1));
    const scale = available / targetWidth;
    const cells = pending.map((cell) => ({
      ...cell,
      width: cell.width * scale,
    }));
    const height = rowHeight(cells);
    const last = rows.at(-1);
    const top = last ? last.top + last.height + gap : 0;
    rows.push({ cells, height, top });
    pending = [];
    targetWidth = 0;
    largeUsed = false;
  };

  for (const item of items) {
    let effectiveSize = item.size;
    if (effectiveSize === "L" && largeUsed) effectiveSize = "M";
    const width =
      baseUnit * sizeFactor[effectiveSize] * Math.sqrt(aspect(item));
    const projected = targetWidth + width + gap * pending.length;
    if (pending.length >= 2 && projected > containerWidth) flush();
    effectiveSize = item.size === "L" && largeUsed ? "M" : item.size;
    const cell = {
      item,
      width: baseUnit * sizeFactor[effectiveSize] * Math.sqrt(aspect(item)),
      effectiveSize,
    };
    pending.push(cell);
    targetWidth += cell.width;
    if (effectiveSize === "L") largeUsed = true;
  }
  flush();
  return rows;
}

function mobileRows(items: Item[], containerWidth: number): LayoutRow[] {
  const rows: LayoutRow[] = [];
  let pending: LayoutCell[] = [];
  const flush = () => {
    if (pending.length === 0) return;
    const last = rows.at(-1);
    const top = last ? last.top + last.height + gap : 0;
    const available = containerWidth - gap * (pending.length - 1);
    const width = available / pending.length;
    const large = pending.some((cell) => cell.effectiveSize === "L");
    rows.push({
      cells: pending.map((cell) => ({ ...cell, width })),
      height: large ? 250 : 210,
      top,
    });
    pending = [];
  };
  for (const item of items) {
    if (item.size === "L") {
      flush();
      pending.push({ item, width: containerWidth, effectiveSize: "L" });
      flush();
      continue;
    }
    pending.push({ item, width: 0, effectiveSize: item.size });
    if (pending.length === 2) flush();
  }
  flush();
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
