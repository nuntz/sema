import type { Item, Story } from "./types";

// A Cursor ID names what the Cursor can rest on: an Item by its Item ID, a
// Story by its Story ID behind a `story:` prefix. Since #82 the grid also
// mounts its cells by these IDs, so they're built and read only here; `bun
// run lint` rejects a hand-built one (biome-plugins/no-inline-story-cursor-id).

export function storyCursorID(storyID: string): string {
  // biome-ignore lint/plugin: this is the one place a Story's Cursor ID is built.
  return `story:${storyID}`;
}

/** The Cursor ID of a cell showing `item`, or `story` when it shows one. */
export function cursorID(item: Item, story?: Story): string {
  return story ? storyCursorID(story.story_id) : item.item_id;
}

export function isStoryCursorID(id: string): boolean {
  return id.startsWith(storyCursorID(""));
}
