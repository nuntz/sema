import type { Item } from "./types";

export function whyText(item: Item): string {
  if (item.why?.title) return `Because you liked: ${item.why.title}`;
  if (item.why?.feed_title) return `You often like ${item.why.feed_title}`;
  return "";
}

export function formatPrior(value: number): string {
  if (Math.abs(value) < 0.0005) return "0.00";
  const magnitude = Math.abs(value).toFixed(2);
  return value > 0 ? `+${magnitude}` : `−${magnitude}`;
}
