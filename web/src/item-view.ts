import { expiringWindow } from "./expiring-view";
export type ItemView = "unread" | "expiring" | "today" | "yesterday" | "all";

export const ITEM_VIEWS: { value: ItemView; label: string }[] = [
  { value: "unread", label: "Unread" },
  { value: "expiring", label: "Expiring" },
  { value: "today", label: "Today" },
  { value: "yesterday", label: "Yesterday" },
  { value: "all", label: "All" },
];

export interface FetchWindow {
  basis?: "published";
  from: string;
  before: string;
}

export function itemViewWindow(
  view: ItemView,
  now = new Date(),
): FetchWindow | undefined {
  if (view === "expiring") return expiringWindow(now.getTime());
  if (view !== "today" && view !== "yesterday") return undefined;
  const from = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  if (view === "yesterday") from.setDate(from.getDate() - 1);
  const before = new Date(from);
  before.setDate(before.getDate() + 1);
  return { from: from.toISOString(), before: before.toISOString() };
}
