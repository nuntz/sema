export type ItemWindow = "today" | "yesterday" | "all";

export const ITEM_WINDOWS: { value: ItemWindow; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "yesterday", label: "Yesterday" },
  { value: "all", label: "All" },
];

export interface FetchWindow {
  from: string;
  before: string;
}

export function windowRange(
  view: ItemWindow,
  now = new Date(),
): FetchWindow | undefined {
  if (view !== "today" && view !== "yesterday") return undefined;
  const from = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  if (view === "yesterday") from.setDate(from.getDate() - 1);
  const before = new Date(from);
  before.setDate(before.getDate() + 1);
  return { from: from.toISOString(), before: before.toISOString() };
}

export function scopeSummary(window: ItemWindow, unreadOnly: boolean): string {
  const label =
    ITEM_WINDOWS.find((option) => option.value === window)?.label ?? window;
  return unreadOnly ? `${label} · Unread` : label;
}
