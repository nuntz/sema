import { describe, expect, it } from "vitest";
import type { Story } from "../types";
import { headlineSlice } from "./story-layout";

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
});
