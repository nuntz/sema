import { describe, expect, it } from "vitest";
import type { Feed } from "../types";
import type { TagOption } from "./tag-options";
import { rankStripTags, tagSetKey, tagStripChips } from "./tag-strip";

const options: TagOption[] = [
  { tag: "ai", count: 1 },
  { tag: "apple", count: 3 },
  { tag: "aviation", count: 20 },
  { tag: "cars", count: 0 },
  { tag: "gaming", count: 3 },
  { tag: "untagged", count: 7 },
];

describe("tagSetKey", () => {
  const feed = (id: string, tags: string[], muted = false): Feed =>
    ({
      feed_id: id,
      title: id,
      url: `https://${id}.example`,
      tags,
      muted,
    }) as Feed;
  it("ignores how many feeds share a tag and which are muted", () => {
    const base = [feed("a", ["ai", "art"]), feed("b", ["art"])];
    expect(tagSetKey([...base, feed("c", ["ai"])])).toBe(tagSetKey(base));
    expect(tagSetKey([...base, feed("c", ["cars"], true)])).toBe(
      tagSetKey(base),
    );
    expect(tagSetKey([...base, feed("c", ["cars"])])).not.toBe(tagSetKey(base));
  });
});

describe("rankStripTags", () => {
  it("ranks by count, ties alphabetical, and never ranks untagged", () => {
    expect(rankStripTags(options)).toEqual([
      "aviation",
      "apple",
      "gaming",
      "ai",
      "cars",
    ]);
  });
});

describe("tagStripChips", () => {
  const order = rankStripTags(options);
  it("follows the frozen order and hides zero-count tags", () => {
    expect(tagStripChips(options, order, null)).toEqual([
      { tag: "aviation", count: 20 },
      { tag: "apple", count: 3 },
      { tag: "gaming", count: 3 },
      { tag: "ai", count: 1 },
    ]);
  });
  it("keeps the frozen order when counts change", () => {
    const later = options.map((option) =>
      option.tag === "ai" ? { ...option, count: 40 } : option,
    );
    expect(tagStripChips(later, order, null).map((chip) => chip.tag)).toEqual([
      "aviation",
      "apple",
      "gaming",
      "ai",
    ]);
  });
  it("lifts the active tag out of the strip and ignores a feed scope", () => {
    expect(
      tagStripChips(options, order, { kind: "tag", value: "apple" }).map(
        (chip) => chip.tag,
      ),
    ).toEqual(["aviation", "gaming", "ai"]);
    expect(
      tagStripChips(options, order, { kind: "feed", value: "daily" }).map(
        (chip) => chip.tag,
      ),
    ).toEqual(["aviation", "apple", "gaming", "ai"]);
  });
  it("appends tags the frozen order has not seen", () => {
    const withNew: TagOption[] = [...options, { tag: "zebra", count: 9 }];
    expect(tagStripChips(withNew, order, null).map((chip) => chip.tag)).toEqual(
      ["aviation", "apple", "gaming", "ai", "zebra"],
    );
  });
});
