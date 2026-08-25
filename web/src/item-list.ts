import type { Item, Order } from "./types";

export function includeReadForGrid(order: Order, unreadOnly: boolean): boolean {
  return !unreadOnly || order === "chrono";
}

export function mergeNewItems(current: Item[], incoming: Item[]): Item[] {
  const seen = new Set(current.map((item) => item.item_id));
  const added = incoming.filter((item) => {
    if (seen.has(item.item_id)) return false;
    seen.add(item.item_id);
    return true;
  });
  return added.length > 0 ? [...added, ...current] : current;
}

export function pollCandidates(
  loaded: Item[],
  pending: Item[],
  incoming: Item[],
  unreadOnly: boolean,
): Item[] {
  const known = new Set([...loaded, ...pending].map((item) => item.item_id));
  const newestFetch = loaded.reduce(
    (newest, item) => (item.fetched_ts > newest ? item.fetched_ts : newest),
    "",
  );
  return incoming.filter(
    (item) =>
      !known.has(item.item_id) &&
      item.fetched_ts > newestFetch &&
      (!unreadOnly || !item.read),
  );
}

export function visibleItemIDs(items: Item[], unreadOnly: boolean): string[] {
  return items
    .filter((item) => !unreadOnly || !item.read)
    .map((item) => item.item_id);
}

export function updateRead(
  items: Item[],
  ids: Iterable<string>,
  read: boolean,
): Item[] {
  const selected = new Set(ids);
  let changed = false;
  const updated = items.map((item) => {
    if (!selected.has(item.item_id) || item.read === read) return item;
    changed = true;
    return { ...item, read };
  });
  return changed ? updated : items;
}

export function unreadIDsAfter(items: Item[], focusedID: string): string[] {
  const focused = items.findIndex((item) => item.item_id === focusedID);
  if (focused < 0) return [];
  return items
    .slice(focused + 1)
    .filter((item) => !item.read)
    .map((item) => item.item_id);
}
