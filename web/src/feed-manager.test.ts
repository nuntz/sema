import { describe, expect, it } from "vitest";
import { feedFilterCounts, filterFeeds } from "./feed-manager";
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
    tags: { tech: 2, news: 1 },
  });
  expect(feedFilterCounts([])).toEqual({
    all: 0,
    attention: 0,
    muted: 0,
    never: 0,
    tags: {},
  });
});
