import type { Item, Story } from "../types";
import { blockRows, headlineSlice } from "./story-layout";

export type FrontPageFocusKind = "story" | "headline" | "grid";

export interface FrontPageFocus {
  id: string;
  item: Item;
  kind: FrontPageFocusKind;
  storyID?: string;
}

export function frontPageSequence(
  stories: Story[],
  gridItems: Item[],
  expandedStoryIDs: ReadonlySet<string> = new Set(),
): FrontPageFocus[] {
  const sequence: FrontPageFocus[] = [];
  for (const story of stories) {
    const lead = story.items[0];
    if (!lead) continue;
    sequence.push({
      id: `story:${story.story_id}`,
      item: lead,
      kind: "story",
      storyID: story.story_id,
    });
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
  for (const item of gridItems)
    sequence.push({ id: item.item_id, item, kind: "grid" });
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

export function horizontalStoryFocus(
  stories: Story[],
  width: number,
  focusedID: string,
  delta: -1 | 1,
): string | undefined {
  const storyID = focusedID.startsWith("story:")
    ? focusedID.slice("story:".length)
    : stories.find(({ items }) => items[0]?.item_id === focusedID)?.story_id;
  if (!storyID) return undefined;
  for (const row of blockRows(stories, width)) {
    const index = row.stories.findIndex((story) => story.story_id === storyID);
    if (index < 0) continue;
    const target = row.stories[index + delta];
    return target ? `story:${target.story_id}` : undefined;
  }
  return undefined;
}
