import type { Item, Story } from "./types";

export function updateStoriesRead(
  stories: Story[],
  ids: Iterable<string>,
  read: boolean,
): Story[] {
  const changed = new Set(ids);
  if (changed.size === 0) return stories;
  return stories.map((story) => ({
    ...story,
    items: story.items.map((item) =>
      changed.has(item.item_id) ? { ...item, read } : item,
    ),
  }));
}

export function updateStoryItem(
  stories: Story[],
  itemID: string,
  patch: Partial<Item>,
): Story[] {
  return stories.map((story) => ({
    ...story,
    items: story.items.map((item) =>
      item.item_id === itemID ? { ...item, ...patch } : item,
    ),
  }));
}
