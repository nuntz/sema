import { describe, expect, it } from "vitest";
import type { Story } from "../types";
import { blockRows, headlineSlice } from "./story-layout";

const stories = Array.from({ length: 7 }, (_, index) => ({
  story_id: String(index),
  source_count: 2,
  items: [],
})) as Story[];

describe("story block layout", () => {
  it("uses the asymmetric desktop first row and three-column following rows", () => {
    expect(
      blockRows(stories, 1200).map((row) => [row.stories.length, row.template]),
    ).toEqual([
      [2, "1.55fr 1fr"],
      [3, "repeat(3, 1fr)"],
      [2, "repeat(2, 1fr)"],
    ]);
  });

  it("uses two tablet columns and one mobile column", () => {
    expect(
      blockRows(stories.slice(0, 3), 800).map((row) => row.stories.length),
    ).toEqual([2, 1]);
    expect(
      blockRows(stories.slice(0, 3), 390).map((row) => row.stories.length),
    ).toEqual([1, 1, 1]);
    expect(blockRows(stories.slice(0, 1), 1400)[0].template).toBe("1fr");
  });

  it("caps collapsed headlines at five and reports the remainder", () => {
    const story = {
      ...stories[0],
      items: Array.from({ length: 9 }, (_, index) => ({
        item_id: String(index),
      })),
    } as Story;
    expect(headlineSlice(story)).toMatchObject({ remaining: 3 });
    expect(headlineSlice(story).items).toHaveLength(5);
  });
});
