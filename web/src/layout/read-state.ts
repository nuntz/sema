import type { Item, Order, ReadAnchor } from "../types";
import type { LayoutRow } from "./justified";

export type ReadStateContext = "unread" | "all-items" | "search" | "archive";

export interface ReadVisualState {
  dimmed: boolean;
  unreadDot: boolean;
}

export interface CaughtUpBoundary {
  count: number;
  beforeItemID?: string;
}

export function gridReadStateContext(
  archive: boolean,
  unreadOnly: boolean,
): ReadStateContext {
  if (archive) return "archive";
  return unreadOnly ? "unread" : "all-items";
}

export function readVisualState(
  context: ReadStateContext,
  read: boolean,
): ReadVisualState {
  return {
    dimmed: context === "unread" && read,
    unreadDot: context === "all-items" && !read,
  };
}

export function automaticReadEnabled(context: ReadStateContext): boolean {
  return context === "unread";
}

export function endMarkActionEnabled(context: ReadStateContext): boolean {
  return context === "unread";
}

export function caughtUpBoundary(
  context: ReadStateContext,
  order: Order,
  loadedItems: Item[],
  visibleItems: Item[],
  readAnchor?: ReadAnchor,
): CaughtUpBoundary | undefined {
  if (order !== "chrono" || (context !== "unread" && context !== "all-items"))
    return undefined;

  const newestLoadedReadIndex = loadedItems.findIndex((item) => item.read);
  if (context === "all-items") {
    if (newestLoadedReadIndex <= 0) return undefined;

    const visible = new Set(visibleItems.map((item) => item.item_id));
    const beforeItemID = loadedItems
      .slice(newestLoadedReadIndex)
      .find((item) => visible.has(item.item_id))?.item_id;
    return { count: newestLoadedReadIndex, beforeItemID };
  }

  const newestLoadedRead = loadedItems[newestLoadedReadIndex];
  const anchor =
    newestLoadedRead &&
    (!readAnchor || newestLoadedRead.published_ts >= readAnchor.published_ts)
      ? newestLoadedRead
      : readAnchor;
  if (!anchor) return undefined;

  const beforeIndex = visibleItems.findIndex(
    (item) => item.published_ts <= anchor.published_ts,
  );
  const count = beforeIndex < 0 ? visibleItems.length : beforeIndex;
  if (count === 0) return undefined;
  return {
    count,
    beforeItemID:
      beforeIndex < 0 ? undefined : visibleItems[beforeIndex].item_id,
  };
}

export function caughtUpLabel(count: number): string {
  return `New since you last caught up · ${count}`;
}

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

export function shouldLoadToFillViewport(
  hasMore: boolean,
  contentHeight: number,
  viewportHeight: number,
): boolean {
  return hasMore && viewportHeight > 0 && contentHeight <= viewportHeight;
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

export function scrollReadCandidates(
  context: ReadStateContext,
  rows: LayoutRow[],
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  userInitiated: boolean,
  alreadyPassed: ReadonlySet<string>,
  alreadyRead: ReadonlySet<string>,
): string[] {
  if (!automaticReadEnabled(context) || !userInitiated) return [];

  const ids = new Set<string>();
  const addUnread = (id: string) => {
    if (!alreadyPassed.has(id) && !alreadyRead.has(id)) ids.add(id);
  };
  for (const row of fullyPassedRows(rows, -1, scrollTop).rows) {
    for (const cell of row.cells) addUnread(cell.item.item_id);
  }
  if (shouldMarkAtBottom(true, scrollTop, clientHeight, scrollHeight)) {
    for (const id of intersectingRowIDs(rows, scrollTop, clientHeight, 14)) {
      addUnread(id);
    }
  }
  return [...ids];
}
