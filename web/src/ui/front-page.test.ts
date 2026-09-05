import { describe, expect, it } from "vitest";
import type { FrontPageEntry, Item, Story } from "../types";
import {
  frontPageSequence,
  frontPageUnreadIDsAfter,
  mergeFrontPage,
  moveFrontPageFocus,
} from "./front-page";

const item = (item_id: string, score = 0) => ({ item_id, score }) as Item;
const story = (
  story_id: string,
  order_key: number,
  size: Story["size"] = "L",
): Story => ({
  story_id,
  source_count: 2,
  order_key,
  size,
  items: [
    item(`lead-${story_id}`),
    item(`${story_id}-a`),
    item(`${story_id}-b`),
  ],
});

describe("front-page merging", () => {
  it("places stories by order key among score-ordered items", () => {
    expect(
      mergeFrontPage(
        [story("middle", 0.8), story("first", 1.2)],
        [item("large", 1.4), item("medium", 0.9), item("small", 0.5)],
        false,
      ).map((entry) =>
        entry.kind === "story"
          ? `story:${entry.story.story_id}`
          : entry.item.item_id,
      ),
    ).toEqual(["large", "story:first", "medium", "story:middle", "small"]);
  });

  it("holds stories below the loaded score floor until a later page arrives", () => {
    const stories = [story("held", 0.4)];
    expect(
      mergeFrontPage(stories, [item("loaded", 0.5)], true).map(
        (entry) => entry.kind,
      ),
    ).toEqual(["item"]);
    expect(
      mergeFrontPage(
        stories,
        [item("loaded", 0.5), item("later", 0.3)],
        true,
      ).map((entry) =>
        entry.kind === "story" ? entry.story.story_id : entry.item.item_id,
      ),
    ).toEqual(["loaded", "held", "later"]);
  });

  it("appends remaining stories once the final page is loaded", () => {
    expect(
      mergeFrontPage([story("tail", 0.1)], [item("loaded", 0.5)], false).map(
        (entry) =>
          entry.kind === "story" ? entry.story.story_id : entry.item.item_id,
      ),
    ).toEqual(["loaded", "tail"]);
  });

  it("puts a story before an item when their ordering values tie", () => {
    const entries = mergeFrontPage(
      [story("tie", 0.5)],
      [item("singleton", 0.5)],
      true,
    );
    expect(entries.map((entry) => entry.kind)).toEqual(["story", "item"]);
  });
});

describe("front-page focus", () => {
  const entries: FrontPageEntry[] = [
    { kind: "item", item: item("grid-a") },
    { kind: "story", story: story("one", 0.8) },
    { kind: "story", story: story("two", 0.7, "M") },
    { kind: "item", item: item("grid-b") },
  ];

  it("walks the merged cell order and visible large-story headlines", () => {
    const sequence = frontPageSequence(entries);
    expect(sequence.map(({ id }) => id)).toEqual([
      "grid-a",
      "story:one",
      "one-a",
      "one-b",
      "story:two",
      "grid-b",
    ]);
    expect(moveFrontPageFocus(sequence, "one-b", 1)?.id).toBe("story:two");
    expect(moveFrontPageFocus(sequence, "story:two", -1)?.id).toBe("one-b");
  });

  it("never includes M-story headlines in the cell sequence", () => {
    expect(frontPageSequence(entries).map(({ id }) => id)).not.toContain(
      "two-a",
    );
    expect(
      frontPageSequence(entries, new Set(["two"])).map(({ id }) => id),
    ).not.toContain("two-a");
  });

  it("marks from the focused merged cell and includes every story member", () => {
    expect(frontPageUnreadIDsAfter(entries, "two-a")).toEqual([
      "lead-two",
      "two-a",
      "two-b",
      "grid-b",
    ]);
  });
});
