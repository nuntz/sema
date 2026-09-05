import { describe, expect, it } from "vitest";
import type { Item, Story } from "../types";
import {
  frontPageSequence,
  horizontalStoryFocus,
  moveFrontPageFocus,
} from "./front-page";

const item = (item_id: string) => ({ item_id }) as Item;
const stories: Story[] = [
  {
    story_id: "one",
    source_count: 2,
    items: [item("lead-one"), item("one-a"), item("one-b")],
  },
  {
    story_id: "two",
    source_count: 2,
    items: [item("lead-two"), item("two-a")],
  },
];

describe("front-page focus", () => {
  it("walks leads, visible headlines, then grid items", () => {
    const sequence = frontPageSequence(stories, [
      item("grid-a"),
      item("grid-b"),
    ]);
    expect(sequence.map(({ id }) => id)).toEqual([
      "story:one",
      "one-a",
      "one-b",
      "story:two",
      "two-a",
      "grid-a",
      "grid-b",
    ]);
    expect(moveFrontPageFocus(sequence, "two-a", 1)?.id).toBe("grid-a");
    expect(moveFrontPageFocus(sequence, "grid-a", -1)?.id).toBe("two-a");
  });

  it("moves horizontally only between blocks in the same row", () => {
    expect(horizontalStoryFocus(stories, 1200, "story:one", 1)).toBe(
      "story:two",
    );
    expect(horizontalStoryFocus(stories, 1200, "story:two", 1)).toBeUndefined();
    expect(horizontalStoryFocus(stories, 390, "story:one", 1)).toBeUndefined();
  });
});
