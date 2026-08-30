import type { Item } from "./types";

export function resolveReaderItem(
  itemID: string,
  collections: readonly (readonly Item[])[],
  retained?: Item,
): Item | undefined {
  for (const items of collections) {
    const item = items.find((candidate) => candidate.item_id === itemID);
    if (item) return item;
  }
  return retained?.item_id === itemID ? retained : undefined;
}
