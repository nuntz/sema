import {
  type LayoutRow,
  mobileStoryHeadlineHeight,
  storyHeadlineHeight,
} from "./justified";

export type LayoutDirection = "up" | "down" | "left" | "right";

export function nextGridPageTop(
  rows: LayoutRow[],
  scrollTop: number,
  viewportHeight: number,
  paddingTop = 14,
): number {
  const bottom = scrollTop + viewportHeight;
  const partialRow = rows.find(
    (row) =>
      row.top + paddingTop < bottom &&
      row.top + paddingTop + row.height > bottom,
  );
  const top = partialRow ? partialRow.top + paddingTop : bottom;
  // Oversized rows must still allow paging through their remaining content.
  return top >= scrollTop + 1 ? top : bottom;
}

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
    row.cells.flatMap((cell) => {
      const left = cell.left;
      const top = row.top + (cell.offsetY ?? 0);
      const right = left + cell.width;
      const bottom =
        top + (cell.height ?? row.height) - (cell.headlineHeight ?? 0);
      const lead = {
        id: cell.story ? `story:${cell.story.story_id}` : cell.item.item_id,
        left,
        right,
        top,
        bottom,
        centerX: (left + right) / 2,
        centerY: (top + bottom) / 2,
      };
      const headlineHeight = cell.mobileStoryCard
        ? mobileStoryHeadlineHeight
        : storyHeadlineHeight;
      const headlines =
        cell.story?.items.slice(1, 1 + (cell.headlineItemCount ?? 0)) ?? [];
      return [
        lead,
        ...headlines.map((item, index) => {
          const headlineTop = bottom + index * headlineHeight;
          return {
            ...lead,
            id: item.item_id,
            top: headlineTop,
            bottom: headlineTop + headlineHeight,
            centerY: headlineTop + headlineHeight / 2,
          };
        }),
      ];
    }),
  );
}

function overlaps(startA: number, endA: number, startB: number, endB: number) {
  return startA < endB && startB < endA;
}

export function nearestPageCell(
  rects: LayoutRect[],
  x: number,
  y: number,
  viewportTop: number,
  viewportBottom: number,
): string | undefined {
  const visible = rects.filter((rect) =>
    overlaps(rect.top, rect.bottom, viewportTop, viewportBottom),
  );
  const fullyVisible = visible.filter(
    (rect) => rect.top >= viewportTop && rect.bottom <= viewportBottom,
  );
  const candidates = fullyVisible.length > 0 ? fullyVisible : visible;
  const distance = (rect: LayoutRect) => {
    const centerY =
      (Math.max(rect.top, viewportTop) +
        Math.min(rect.bottom, viewportBottom)) /
      2;
    return Math.hypot(rect.centerX - x, centerY - y);
  };
  return candidates.sort((a, b) => distance(a) - distance(b))[0]?.id;
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
        ? rect.centerY < current.centerY && rect.top < current.top - 1
        : rect.centerY > current.centerY && rect.top > current.top + 1),
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
