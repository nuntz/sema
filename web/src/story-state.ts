import type { Item, Story } from "./types";

export function excludeRenderedStoryItems(
  items: Item[],
  stories: Story[],
): Item[] {
  if (items.length === 0 || stories.length === 0) return items;
  const hidden = new Set(
    stories.flatMap((story) => story.items.map((item) => item.item_id)),
  );
  return items.filter((item) => !hidden.has(item.item_id));
}

export function updateStoriesRead(
  stories: Story[],
  ids: Iterable<string>,
  read: boolean,
): Story[] {
  const changed = new Set(ids);
  if (changed.size === 0) return stories;
  let storiesChanged = false;
  const updated = stories.map((story) => {
    let storyChanged = false;
    const items = story.items.map((item) => {
      if (!changed.has(item.item_id) || item.read === read) return item;
      storyChanged = true;
      return { ...item, read };
    });
    if (!storyChanged) return story;
    storiesChanged = true;
    return { ...story, items };
  });
  return storiesChanged ? updated : stories;
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
