import { HOUR, hoursLeft, LIFETIME } from "./expiry";
import type { FetchWindow } from "./item-view";
import type { Item } from "./types";

export function nextMidnight(now: number): Date {
  const date = new Date(now);
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + 1);
}
export function expiringWindow(now: number): FetchWindow {
  return {
    from: new Date(now - LIFETIME).toISOString(),
    before: new Date(now - 120 * HOUR).toISOString(),
    basis: "published",
  };
}
export function deadlineGroup(
  published: string,
  now: number,
): "tonight" | "tomorrow" | "later" {
  const deadline = Date.parse(published) + LIFETIME;
  const midnight = nextMidnight(now);
  if (deadline < midnight.getTime()) return "tonight";
  midnight.setDate(midnight.getDate() + 1);
  return deadline < midnight.getTime() ? "tomorrow" : "later";
}
export function deadlineTime(published: string): string {
  return new Date(Date.parse(published) + LIFETIME).toLocaleTimeString(
    undefined,
    { hour: "2-digit", minute: "2-digit", hour12: false },
  );
}
export function expiringItems(items: Item[], now: number): Item[] {
  return items
    .filter(
      (item) =>
        !item.hearted &&
        !item.archived &&
        hoursLeft(item.published_ts, now) > 0 &&
        hoursLeft(item.published_ts, now) <= 48,
    )
    .sort(
      (a, b) =>
        Date.parse(a.published_ts) - Date.parse(b.published_ts) ||
        a.item_id.localeCompare(b.item_id),
    );
}
export function expiryGroups(items: Item[], now: number) {
  const sorted = expiringItems(items, now);
  return (["tonight", "tomorrow", "later"] as const).flatMap((key) => {
    const members = sorted.filter(
      (item) => deadlineGroup(item.published_ts, now) === key,
    );
    return members.length
      ? [
          {
            key,
            items: members,
            label:
              key === "tonight"
                ? "GOES TONIGHT"
                : key === "tomorrow"
                  ? "GOES TOMORROW"
                  : "LATER THIS WEEK",
            note:
              key === "tonight"
                ? `after ${deadlineTime(members[0].published_ts)}`
                : key === "later"
                  ? "two days or less"
                  : "",
          },
        ]
      : [];
  });
}
