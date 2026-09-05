import { describe, expect, it } from "vitest";
import {
  excludeRenderedStoryItems,
  updateStoriesRead,
  updateStoryItem,
} from "./story-state";
import type { Item, Story } from "./types";

const item = (item_id: string, read = false): Item =>
  ({ item_id, read }) as Item;

describe("story read state", () => {
  it("excludes only members of stories rendered for this request", () => {
    const stories: Story[] = [
      { story_id: "story", source_count: 2, items: [item("a"), item("b")] },
    ];
    const grid = [item("a"), item("singleton"), item("demoted")];

    expect(
      excludeRenderedStoryItems(grid, stories).map(({ item_id }) => item_id),
    ).toEqual(["singleton", "demoted"]);
  });

  it("updates matching members without changing unrelated members", () => {
    const stories: Story[] = [
      { story_id: "story", source_count: 2, items: [item("a"), item("b")] },
    ];
    const updated = updateStoriesRead(stories, ["a"], true);
    expect(updated[0].items.map(({ read }) => read)).toEqual([true, false]);
    expect(stories[0].items[0].read).toBe(false);
  });

  it("applies item patches to story copies", () => {
    const stories: Story[] = [
      { story_id: "story", source_count: 2, items: [item("a"), item("b")] },
    ];
    expect(
      updateStoryItem(stories, "b", { hearted: true })[0].items[1],
    ).toMatchObject({ item_id: "b", hearted: true });
  });
});
