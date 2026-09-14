import { expect, it } from "vitest";
import type { ItemWindow } from "./item-view";
import {
  formatScopeMeta,
  scopeCellModel,
  windowScopeCounts,
} from "./scope-cell";
import type { Feed, GridScope } from "./types";

const tag: GridScope = { kind: "tag", value: "design" };
it.each<[GridScope, ItemWindow | "unread", number, number, string, string]>([
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
  [null, "unread", 42, 9, "All", "42 unread items · 9 feeds"],
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
  expect(
    scopeCellModel(
      scope,
      view === "unread" ? "all" : view,
      view === "unread",
      feeds,
      counts,
      0,
      false,
    ),
  ).toMatchObject({
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
  expect(scopeCellModel(tag, "all", true, feeds, counts, 0, false).text).toBe(
    "24 unread items · 1 feed",
  );
  expect(scopeCellModel(tag, "all", true, feeds, counts, 30, false).text).toBe(
    "0 unread items · 1 feed",
  );
  expect(scopeCellModel(null, "all", true, feeds, counts, 30, false).text).toBe(
    "0 unread items",
  );
  expect(scopeCellModel(null, "today", false, feeds, {}, 0, false).text).toBe(
    "0 items today",
  );
  expect(
    scopeCellModel(tag, "all", true, feeds, undefined, 0, false).count,
  ).toBeUndefined();
  expect(
    scopeCellModel(
      { kind: "tag", value: "untagged" },
      "all",
      true,
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
      false,
      feeds,
      counts,
      0,
      false,
    ).text,
  ).toBe("3 items today");
});

it("aggregates independent window buckets across scoped, unmuted feeds", () => {
  const feeds = [
    { feed_id: "a", tags: ["design"] },
    { feed_id: "b", tags: ["design"] },
    { feed_id: "muted", tags: ["design"], muted: true },
    { feed_id: "other", tags: [] },
  ] as Feed[];
  const counts = {
    today: {
      a: { all: 10, unread: 2 },
      b: { all: 5, unread: 3 },
      muted: { all: 99, unread: 99 },
    },
    all: { a: { all: 30, unread: 4 }, other: { all: 50, unread: 20 } },
  };
  expect(windowScopeCounts(tag, true, feeds, counts)).toEqual({
    today: 5,
    yesterday: undefined,
    all: 4,
  });
  expect(windowScopeCounts(tag, false, feeds, counts)).toEqual({
    today: 15,
    yesterday: undefined,
    all: 30,
  });
  expect(
    windowScopeCounts({ kind: "feed", value: "b" }, true, feeds, counts).today,
  ).toBe(3);
  expect(
    scopeCellModel(null, "today", true, feeds, counts.today, 1, false),
  ).toMatchObject({ title: "Today", count: 4, qualifier: "unread" });
});
