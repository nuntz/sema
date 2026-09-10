import { expect, it } from "vitest";
import type { ItemView } from "./item-view";
import { formatScopeMeta, scopeCellModel } from "./scope-cell";
import type { Feed, GridScope } from "./types";

const tag: GridScope = { kind: "tag", value: "design" };
it.each<[GridScope, ItemView, number, number, string, string]>([
  [tag, "unread", 24, 6, "#design", "24 unread items · 6 feeds"],
  [tag, "today", 18, 6, "#design", "18 items today · 6 feeds"],
  [tag, "yesterday", 31, 6, "#design", "31 items yesterday · 6 feeds"],
  [tag, "all", 1204, 6, "#design", "1,204 items · 6 feeds"],
  [
    { kind: "feed", value: "0" },
    "unread",
    37,
    1,
    "The Verge",
    "37 unread items",
  ],
  [null, "today", 18, 9, "Today", "18 items · 9 feeds"],
  [null, "yesterday", 31, 7, "Yesterday", "31 items · 7 feeds"],
  [null, "all", 1204, 12, "All", "1,204 items · 12 feeds"],
  [null, "unread", 42, 9, "Unread", "42 unread items · 9 feeds"],
])("formats %j %s", (scope, view, total, n, title, text) => {
  const feeds = Array.from(
    { length: n },
    (_, i) =>
      ({ feed_id: `${i}`, title: "The Verge", tags: ["design"] }) as Feed,
  );
  const counts = Object.fromEntries(
    feeds.map((f, i) => [
      f.feed_id,
      { all: i ? 1 : total - n + 1, unread: i ? 1 : total - n + 1 },
    ]),
  );
  expect(scopeCellModel(scope, view, feeds, counts, 0, false)).toMatchObject({
    title,
    text,
  });
});
it.each([
  [undefined, "unread", 6, false, "counting unread items · 6 feeds"],
  [undefined, "", 9, false, "counting items · 9 feeds"],
  [undefined, "today", undefined, false, "counting items today"],
  [1, "unread", 1, false, "1 unread item · 1 feed"],
  [24, "unread", 6, true, "24 unread"],
  [18, "today", 6, true, "18 items today"],
  [0, "unread", 6, true, "0 unread"],
] as const)("formats meta %s %s", (count, q, feeds, phone, text) =>
  expect(formatScopeMeta(count, q, feeds, phone)).toBe(text),
);
it("handles zero, loading, untagged, muted feeds and contributors", () => {
  const feeds = [
    { feed_id: "a", tags: ["design"] },
    { feed_id: "b", tags: ["design"], muted: true },
    { feed_id: "c", tags: [] },
    { feed_id: "d", tags: ["design"] },
  ] as Feed[];
  const counts = {
    a: { all: 24, unread: 24 },
    b: { all: 99, unread: 99 },
    c: { all: 1, unread: 1 },
    d: { all: 3, unread: 0 },
  };
  expect(scopeCellModel(tag, "unread", feeds, counts, 0, false).text).toBe(
    "24 unread items · 1 feed",
  );
  expect(scopeCellModel(tag, "unread", feeds, counts, 30, false).text).toBe(
    "0 unread items · 1 feed",
  );
  expect(scopeCellModel(null, "unread", feeds, counts, 30, false).text).toBe(
    "0 unread items",
  );
  expect(scopeCellModel(null, "today", feeds, {}, 0, false).text).toBe(
    "0 items today",
  );
  expect(
    scopeCellModel(tag, "unread", feeds, undefined, 0, false).count,
  ).toBeUndefined();
  expect(
    scopeCellModel(
      { kind: "tag", value: "untagged" },
      "unread",
      feeds,
      counts,
      0,
      false,
    ).text,
  ).toBe("1 unread item · 1 feed");
  expect(
    scopeCellModel(
      { kind: "feed", value: "d" },
      "today",
      feeds,
      counts,
      0,
      false,
    ).text,
  ).toBe("3 items today");
});
