import { describe, expect, it } from "vitest";
import type { Story } from "../types";
import { headlineSlice, sheetHeadlineSlice } from "./story-layout";

describe("story headline layout", () => {
  it("caps collapsed headlines at five and reports the remainder", () => {
    const story = {
      story_id: "story",
      source_count: 9,
      order_key: 1,
      size: "L",
      items: Array.from({ length: 9 }, (_, index) => ({
        item_id: String(index),
      })),
    } as Story;
    expect(headlineSlice(story)).toMatchObject({ remaining: 3 });
    expect(headlineSlice(story).items).toHaveLength(5);
  });

  it("uses at most six sheet rows and reserves the last one for overflow", () => {
    const story = {
      story_id: "story",
      source_count: 9,
      order_key: 1,
      size: "L",
      items: Array.from({ length: 9 }, (_, index) => ({
        item_id: String(index),
      })),
    } as Story;

    expect(sheetHeadlineSlice(story).items.map((item) => item.item_id)).toEqual(
      ["1", "2", "3", "4", "5"],
    );
    expect(sheetHeadlineSlice(story).remaining).toBe(3);

    const sixHeadlines = {
      ...story,
      items: story.items.slice(0, 7),
    };
    expect(sheetHeadlineSlice(sixHeadlines).items).toHaveLength(6);
    expect(sheetHeadlineSlice(sixHeadlines).remaining).toBe(0);
  });
});
