import type { FrontPageEntry, Item, Order, Story } from "../types";
import { headlineSlice } from "./story-layout";

export type FrontPageFocusKind = "story" | "headline" | "grid";

export interface FrontPageFocus {
  id: string;
  item: Item;
  kind: FrontPageFocusKind;
  storyID?: string;
}

export function mergeFrontPage(
  stories: Story[],
  pages: Item[],
  hasMore: boolean,
): FrontPageEntry[] {
  const orderedStories = [...stories].sort((left, right) => {
    if (left.order_key !== right.order_key)
      return right.order_key - left.order_key;
    const published = (right.items[0]?.published_ts ?? "").localeCompare(
      left.items[0]?.published_ts ?? "",
    );
    return published || left.story_id.localeCompare(right.story_id);
  });
  const entries: FrontPageEntry[] = [];
  let storyIndex = 0;
  for (const item of pages) {
    while (
      storyIndex < orderedStories.length &&
      orderedStories[storyIndex].order_key >= item.score
    ) {
      entries.push({ kind: "story", story: orderedStories[storyIndex] });
      storyIndex++;
    }
    entries.push({ kind: "item", item });
  }
  if (!hasMore) {
    for (; storyIndex < orderedStories.length; storyIndex++)
      entries.push({ kind: "story", story: orderedStories[storyIndex] });
  }
  return entries;
}

export function frontPageEntriesForState(
  stories: Story[],
  items: Item[],
  mode: "live" | "archive",
  order: Order,
  cursor: string,
): FrontPageEntry[] {
  if (mode === "live" && order === "interest")
    return mergeFrontPage(stories, items, cursor !== "");
  return items.map((item) => ({ kind: "item", item }));
}

export function frontPageEntryItem(entry: FrontPageEntry): Item | undefined {
  return entry.kind === "item" ? entry.item : entry.story.items[0];
}

export function frontPageSequence(
  entries: FrontPageEntry[],
  expandedStoryIDs: ReadonlySet<string> = new Set(),
): FrontPageFocus[] {
  const sequence: FrontPageFocus[] = [];
  for (const entry of entries) {
    if (entry.kind === "item") {
      sequence.push({ id: entry.item.item_id, item: entry.item, kind: "grid" });
      continue;
    }
    const { story } = entry;
    const lead = story.items[0];
    if (!lead) continue;
    sequence.push({
      id: `story:${story.story_id}`,
      item: lead,
      kind: "story",
      storyID: story.story_id,
    });
    if (story.size !== "L") continue;
    const headlines = expandedStoryIDs.has(story.story_id)
      ? story.items.slice(1)
      : headlineSlice(story).items;
    for (const item of headlines) {
      sequence.push({
        id: item.item_id,
        item,
        kind: "headline",
        storyID: story.story_id,
      });
    }
  }
  return sequence;
}

export function moveFrontPageFocus(
  sequence: FrontPageFocus[],
  focusedID: string,
  delta: -1 | 1,
): FrontPageFocus | undefined {
  if (sequence.length === 0) return undefined;
  const index = sequence.findIndex(({ id }) => id === focusedID);
  return sequence[
    Math.max(0, Math.min(sequence.length - 1, (index < 0 ? 0 : index) + delta))
  ];
}

export function frontPageUnreadIDsAfter(
  entries: FrontPageEntry[],
  focusedID: string,
): string[] {
  const index = entries.findIndex((entry) =>
    entry.kind === "item"
      ? entry.item.item_id === focusedID
      : focusedID === `story:${entry.story.story_id}` ||
        entry.story.items.some((item) => item.item_id === focusedID),
  );
  if (index < 0) return [];
  const ids = new Set<string>();
  for (const entry of entries.slice(index)) {
    const items = entry.kind === "item" ? [entry.item] : entry.story.items;
    for (const item of items) if (!item.read) ids.add(item.item_id);
  }
  return [...ids];
}
