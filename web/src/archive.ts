import type { Item } from "./types";

export function updateHeartState(
  items: Item[],
  itemID: string,
  hearted: boolean,
): Item[] {
  return items.map((item) =>
    item.item_id === itemID ? { ...item, hearted } : item,
  );
}

export function shouldConfirmArchiveRemoval(
  archive: boolean,
  hearted: boolean,
): boolean {
  return archive && hearted;
}

export function archiveSize(count: number): string {
  const megabytes = count * 0.3;
  return megabytes >= 10
    ? `${Math.round(megabytes)} MB`
    : `${megabytes.toFixed(1)} MB`;
}

export function isOlderThanThirtyDays(
  publishedTS: string,
  now = Date.now(),
): boolean {
  return now - new Date(publishedTS).getTime() > 30 * 24 * 60 * 60 * 1000;
}
