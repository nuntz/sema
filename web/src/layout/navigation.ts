import type { LayoutRow } from "./justified";

export type LayoutDirection = "up" | "down" | "left" | "right";

export interface LayoutRect {
  id: string;
  left: number;
  right: number;
  top: number;
  bottom: number;
  centerX: number;
  centerY: number;
}

export function cellRects(rows: LayoutRow[]): LayoutRect[] {
  return rows.flatMap((row) =>
    row.cells.map((cell) => {
      const left = cell.left;
      const top = row.top + (cell.offsetY ?? 0);
      const right = left + cell.width;
      const bottom = top + (cell.height ?? row.height);
      return {
        id: cell.story ? `story:${cell.story.story_id}` : cell.item.item_id,
        left,
        right,
        top,
        bottom,
        centerX: (left + right) / 2,
        centerY: (top + bottom) / 2,
      };
    }),
  );
}

function overlaps(startA: number, endA: number, startB: number, endB: number) {
  return startA < endB && startB < endA;
}

export function nearestCell(
  rows: LayoutRow[],
  focusedID: string,
  direction: LayoutDirection,
): string | undefined {
  const rects = cellRects(rows);
  const current = rects.find((rect) => rect.id === focusedID) ?? rects.at(0);
  if (!current) return undefined;

  if (direction === "left" || direction === "right") {
    const candidates = rects.filter(
      (rect) =>
        rect.id !== current.id &&
        (direction === "left"
          ? rect.centerX < current.centerX
          : rect.centerX > current.centerX) &&
        overlaps(rect.top, rect.bottom, current.top, current.bottom),
    );
    candidates.sort((left, right) => {
      const leftPerpendicular = Math.abs(left.centerY - current.centerY);
      const rightPerpendicular = Math.abs(right.centerY - current.centerY);
      if (leftPerpendicular !== rightPerpendicular)
        return leftPerpendicular - rightPerpendicular;
      return (
        Math.abs(left.centerX - current.centerX) -
        Math.abs(right.centerX - current.centerX)
      );
    });
    return candidates.at(0)?.id ?? current.id;
  }

  const candidates = rects.filter(
    (rect) =>
      rect.id !== current.id &&
      (direction === "up"
        ? rect.centerY < current.centerY
        : rect.centerY > current.centerY),
  );
  candidates.sort((left, right) => {
    const leftScore =
      Math.abs(left.centerY - current.centerY) +
      Math.abs(left.centerX - current.centerX) * 0.35;
    const rightScore =
      Math.abs(right.centerY - current.centerY) +
      Math.abs(right.centerX - current.centerX) * 0.35;
    return leftScore - rightScore;
  });
  return candidates.at(0)?.id ?? current.id;
}
