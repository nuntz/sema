import type { Item } from "./types";

export const HOUR = 3_600_000;
export const LIFETIME = 168 * HOUR;

export function hoursLeft(published: string, now: number): number {
  return 168 - (now - Date.parse(published)) / HOUR;
}

export function expiryState(
  hours: number,
): "none" | "soon" | "today" | "imminent" {
  if (!Number.isFinite(hours) || hours > 48) return "none";
  if (hours >= 24) return "soon";
  if (hours >= 6) return "today";
  return "imminent";
}

export function itemExpiryState(
  item: Pick<Item, "hearted" | "archived" | "published_ts">,
  now: number,
) {
  return item.hearted || item.archived
    ? "none"
    : expiryState(hoursLeft(item.published_ts, now));
}

export function expiryLabel(hours: number, { compact = false } = {}): string {
  if (hours <= 0) return "goes now";
  const amount =
    hours > 24
      ? `${Math.ceil(hours / 24)}d`
      : `${Math.max(1, Math.ceil(hours))}h`;
  return compact && hours >= 6 ? amount : `${amount} left`;
}

export function expiryRingFraction(hours: number): number {
  return Math.min(1, Math.max(0.05, hours / 48));
}

export function expirySentence(published: string, now: number): string {
  const hours = hoursLeft(published, now);
  if (hours <= 0) return "goes now";
  if (hours < 1) return "less than an hour left";
  if (hours < 6) {
    const words = ["zero", "one", "two", "three", "four", "five", "six"];
    const n = Math.ceil(hours);
    return `about ${words[n]} ${n === 1 ? "hour" : "hours"} left`;
  }
  if (hours > 24) return `${Math.ceil(hours / 24)} days left`;
  return `${Math.ceil(hours)} hours left`;
}
