import { describe, expect, it } from "vitest";
import {
  brokenSince,
  cadenceLabel,
  cadencePin,
  compareFeeds,
  feedCount,
  feedFilterCounts,
  filterFeeds,
} from "./feed-manager";
import type { Feed } from "./types";

const feed = (id: string, extra: Partial<Feed> = {}): Feed => ({
  feed_id: id,
  title: id,
  url: `https://${id}.example/feed`,
  connector: "rss",
  tags: [],
  muted: false,
  hide_shorts: false,
  always_generate: false,
  fetch_interval_h: 1,
  error_count: 0,
  next_fetch_at: "",
  prior: 0,
  prior_signals: 0,
  status: "ok",
  item_count: 0,
  extraction_sample: 0,
  last_fetch_at: "2026-09-01T00:00:00Z",
  ...extra,
});
const feeds = [
  feed("Alpha", { tags: ["tech", "tech"], status: "broken" }),
  feed("Beta", { tags: ["tech", "news"], status: "slowed" }),
  feed("Gamma", { muted: true, status: "muted" }),
  feed("Delta", { last_fetch_at: undefined }),
];
describe("filterFeeds", () => {
  it("filters attention, muted, never fetched and exact tags", () => {
    expect(filterFeeds(feeds, "all")).toEqual(feeds);
    expect(filterFeeds(feeds, "attention")).toEqual(feeds.slice(0, 2));
    expect(filterFeeds(feeds, "muted")).toEqual([feeds[2]]);
    expect(filterFeeds(feeds, "never")).toEqual([feeds[3]]);
    expect(filterFeeds(feeds, "tag:tech")).toEqual(feeds.slice(0, 2));
    expect(filterFeeds(feeds, "tag:te")).toEqual([]);
  });
  it("composes trimmed case-insensitive title, URL and tag searches", () => {
    expect(filterFeeds(feeds, "attention", " ALPHA ")).toEqual([feeds[0]]);
    expect(filterFeeds(feeds, "all", "beta.example")).toEqual([feeds[1]]);
    expect(filterFeeds(feeds, "tag:tech", "NEWS")).toEqual([feeds[1]]);
    expect(filterFeeds(feeds, "muted", "news")).toEqual([]);
    expect(
      filterFeeds([feed("x", { custom_title: "Custom" })], "all", "custom"),
    ).toHaveLength(1);
  });
});
it("counts independently of searches, counting each tag once per feed", () => {
  expect(feedFilterCounts(feeds)).toEqual({
    all: 4,
    attention: 2,
    muted: 1,
    never: 1,
    quiet: 0,
    tags: { tech: 2, news: 1 },
  });
  expect(feedFilterCounts([])).toEqual({
    all: 0,
    attention: 0,
    muted: 0,
    never: 0,
    quiet: 0,
    tags: {},
  });
});

it("distinguishes unavailable counts from a loaded sparse or empty map", () => {
  expect(feedCount(undefined, "Alpha")).toBeUndefined();
  expect(feedCount({}, "Alpha")).toEqual({ all: 0, unread: 0 });
  expect(feedCount({ Beta: { all: 3, unread: 2 } }, "Alpha")).toEqual({
    all: 0,
    unread: 0,
  });
  expect(feedCount({ Beta: { all: 3, unread: 2 } }, "Beta")).toEqual({
    all: 3,
    unread: 2,
  });
});
it("quiet treats omissions as zero only after loading and excludes muted feeds", () => {
  const counts = { Beta: { all: 3, unread: 2 } };
  expect(filterFeeds(feeds, "quiet", "", counts)).toEqual([feeds[0], feeds[3]]);
  expect(feedFilterCounts(feeds, counts).quiet).toBe(2);
  expect(filterFeeds(feeds, "quiet", "", {})).toEqual([
    feeds[0],
    feeds[1],
    feeds[3],
  ]);
  expect(feedFilterCounts(feeds, {}).quiet).toBe(3);
  expect(filterFeeds(feeds, "quiet")).toEqual([]);
  expect(feedFilterCounts(feeds).quiet).toBe(0);
});
describe("compareFeeds", () => {
  const entries = [
    feed("Zulu"),
    feed("Beta"),
    feed("Alpha"),
    feed("Missing"),
    feed("Absent"),
  ];
  const counts = {
    Zulu: { all: 9, unread: 9 },
    Beta: { all: 3, unread: 3 },
    Alpha: { all: 3, unread: 3 },
  };
  it("sorts unread descending with omitted feeds at zero and title ties", () => {
    expect(
      [...entries].sort(compareFeeds("unread", counts)).map((f) => f.feed_id),
    ).toEqual(["Zulu", "Alpha", "Beta", "Absent", "Missing"]);
  });
  it("sorts quietest ascending with omitted feeds first and title ties", () => {
    expect(
      [...entries].sort(compareFeeds("quietest", counts)).map((f) => f.feed_id),
    ).toEqual(["Absent", "Missing", "Alpha", "Beta", "Zulu"]);
  });
  it.each(["unread", "quietest"] as const)(
    "uses title order for %s when unavailable or empty-but-loaded",
    (sort) => {
      for (const map of [undefined, {}])
        expect(
          [...entries].sort(compareFeeds(sort, map)).map((f) => f.feed_id),
        ).toEqual(["Absent", "Alpha", "Beta", "Missing", "Zulu"]);
    },
  );
  it.each(["title", "updated", "errors", "prior", "quality"] as const)(
    "breaks %s ties by title",
    (sort) => {
      expect(
        [...entries].sort(compareFeeds(sort)).map((f) => f.feed_id),
      ).toEqual(["Absent", "Alpha", "Beta", "Missing", "Zulu"]);
    },
  );
  it("preserves update, error, prior and extraction ordering", () => {
    const a = feed("Alpha"),
      b = feed("Beta", {
        status: "broken",
        prior: 1,
        last_fetch_at: "2026-09-02T00:00:00Z",
        extraction_success_rate: 0.1,
      });
    for (const sort of ["updated", "errors", "prior", "quality"] as const)
      expect([a, b].sort(compareFeeds(sort))).toEqual([b, a]);
    expect(
      [
        feed("Alpha", {
          extraction_success_rate: 0.5,
          average_extract_quality: 0.8,
        }),
        feed("Beta", {
          extraction_success_rate: 0.5,
          average_extract_quality: 0.2,
        }),
      ].sort(compareFeeds("quality"))[0].title,
    ).toBe("Beta");
    expect(
      [
        feed("ok"),
        feed("muted", { status: "muted" }),
        feed("slowed", { status: "slowed" }),
      ]
        .sort(compareFeeds("errors"))
        .map((f) => f.title),
    ).toEqual(["slowed", "muted", "ok"]);
  });
});

describe("brokenSince", () => {
  const now = Date.parse("2026-09-13T00:00:00Z");
  it.each([
    [1, 10, 150],
    // The worker caps delays at 24h; it does not impose a 24h minimum.
    [24, 3, 6],
    [1, 1, 0],
    [1, 11, 174],
    [1, 12, 198],
    [24, 8, 102],
    [24, 12, 198],
  ] as const)(
    "estimates interval %sh with %s errors as %sh before the attempt",
    (fetch_interval_h, error_count, hours) => {
      expect(
        brokenSince(
          feed("x", {
            status: "broken",
            error_count,
            fetch_interval_h,
            last_fetch_at: new Date(now).toISOString(),
          }),
          now,
        ),
      ).toBe(new Date(now - hours * 3600000).toISOString());
    },
  );
  it.each([1, 0, -1])("clamps %s errors to the last attempt", (error_count) => {
    expect(brokenSince(feed("x", { status: "broken", error_count }), now)).toBe(
      "2026-09-01T00:00:00.000Z",
    );
  });
  it.each(["ok", "slowed", "muted"] as const)("omits %s feeds", (status) => {
    expect(
      brokenSince(feed("x", { status, error_count: 5 }), now),
    ).toBeUndefined();
  });
  it("does not invent dates for missing, invalid or future attempts", () => {
    for (const last_fetch_at of [undefined, "invalid", "2027-01-01T00:00:00Z"])
      expect(
        brokenSince(feed("x", { status: "broken", last_fetch_at }), now),
      ).toBeUndefined();
  });
});

it("uses the first refusal for broken-since even without a last fetch", () => {
  const refused = "2026-09-01T00:00:00Z";
  expect(
    brokenSince(
      feed("refused", {
        status: "broken",
        refused_since: refused,
        last_fetch_at: undefined,
      }),
      Date.parse("2026-09-03T00:00:00Z"),
    ),
  ).toBe(refused);
});

it("keeps Auto distinct from its effective Cadence", () => {
  const automatic = feed("auto", {
    cadence_pin_h: null,
    effective_cadence_h: 6,
  });
  expect(cadencePin(automatic)).toBeNull();
  expect(cadenceLabel(automatic)).toBe("Auto · every 6h");
  expect(cadencePin(feed("pin", { cadence_pin_h: 1 }))).toBe(1);
  expect(
    cadenceLabel(feed("reddit", { connector: "reddit", fetch_interval_h: 24 })),
  ).toBe("Daily");
});
