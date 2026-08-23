import { describe, expect, it } from "vitest";
import type { Feed } from "../types";
import { feedTagOptions } from "./tag-options";

describe("grid tag filter", () => {
  it("counts current-window items and excludes muted feeds", () => {
    const feeds = [
      { tags: ["dev", "longform"], item_count: 8 },
      { tags: ["dev"], item_count: 3 },
      { tags: [], item_count: 5 },
      { tags: ["dev"], item_count: 100, muted: true },
    ] as Feed[];
    expect(feedTagOptions(feeds)).toEqual([
      { tag: "dev", count: 11 },
      { tag: "longform", count: 8 },
      { tag: "untagged", count: 5 },
    ]);
  });
});
