import { ITEM_WINDOWS, type ItemWindow } from "../item-view";
import type { Order } from "../types";

function windowLabel(window: ItemWindow): string {
  return (
    ITEM_WINDOWS.find((option) => option.value === window)?.label ?? window
  );
}

/** Header summary: only the settings that differ from the launch defaults. */
export function filterSummaryLabel(
  order: Order,
  window: ItemWindow,
  unreadOnly: boolean,
): string {
  const parts: string[] = [];
  if (order === "chrono") parts.push("Latest");
  if (window !== "all") parts.push(windowLabel(window));
  if (unreadOnly) parts.push("Unread");
  return parts.join(" · ") || "All";
}

/** Full-sentence accessible name for the header summary button. */
export function filterSummaryDescription(
  order: Order,
  window: ItemWindow,
  unreadOnly: boolean,
  count: number | undefined,
): string {
  const items =
    count === undefined
      ? "counting"
      : `${count.toLocaleString("en-US")} ${count === 1 ? "item" : "items"}`;
  return [
    `Filter: ${order === "chrono" ? "Latest" : "Front page"}`,
    window === "all" ? "All dates" : windowLabel(window),
    unreadOnly ? "Unread only" : "All items",
    items,
  ].join(", ");
}
