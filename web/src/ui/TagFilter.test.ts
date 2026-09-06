import { describe, expect, it } from "vitest";
import type { Feed } from "../types";
import {
  feedScopeChip,
  feedScopeOptions,
  feedTagOptions,
  scopeFilterOptions,
  scopeForClosedEscape,
  scopeForEnter,
} from "./tag-options";

describe("grid tag filter", () => {
  it("counts retained or unread items for each tag and excludes muted feeds", () => {
    const feeds = [
      { feed_id: "one", tags: ["dev", "longform"], item_count: 600 },
      { feed_id: "two", tags: ["dev"], item_count: 300 },
      { feed_id: "plain", tags: [], item_count: 500 },
      { feed_id: "muted", tags: ["dev"], item_count: 100, muted: true },
    ] as Feed[];
    const counts = {
      one: { all: 8, unread: 2 },
      two: { all: 3, unread: 1 },
      plain: { all: 5, unread: 4 },
      muted: { all: 100, unread: 100 },
    };

    expect(feedTagOptions(feeds, counts, false)).toEqual([
      { tag: "dev", count: 11 },
      { tag: "longform", count: 8 },
      { tag: "untagged", count: 5 },
    ]);
    expect(feedTagOptions(feeds, counts, true)).toEqual([
      { tag: "dev", count: 3 },
      { tag: "longform", count: 2 },
      { tag: "untagged", count: 4 },
    ]);
  });

  it("lists feed rows after tag rows", () => {
    const feeds = [
      {
        feed_id: "daily",
        url: "https://daily.example.com/",
        title: "Daily Example",
        connector: "rss",
        tags: ["news"],
        item_count: 4,
      },
    ] as Feed[];

    expect(scopeFilterOptions(feeds).map(({ kind }) => kind)).toEqual([
      "tag",
      "tag",
      "feed",
    ]);
  });

  it("matches feed titles case-insensitively", () => {
    const feeds = [
      {
        feed_id: "daily",
        url: "https://example.com/",
        title: "The Daily Brief",
        connector: "rss",
        tags: ["news"],
      },
      {
        feed_id: "weekly",
        url: "https://weekly.example.com/",
        title: "Weekly Review",
        connector: "rss",
        tags: [],
      },
    ] as Feed[];

    expect(scopeFilterOptions(feeds, "DAILY")).toMatchObject([
      { kind: "feed", value: "daily", label: "The Daily Brief" },
    ]);
  });

  it("applies the highlighted feed on Enter", () => {
    const options = scopeFilterOptions([
      {
        feed_id: "daily",
        url: "https://example.com/",
        title: "Daily Brief",
        connector: "rss",
        tags: ["news"],
      } as unknown as Feed,
    ]);
    const feedIndex = options.findIndex((option) => option.kind === "feed");

    expect(scopeForEnter(options, feedIndex)).toEqual({
      kind: "feed",
      value: "daily",
    });
  });

  it("renders the active feed title in the chip", () => {
    const feeds = [
      {
        feed_id: "daily",
        url: "https://example.com/",
        title: "Daily Brief",
        custom_title: "My Daily",
        connector: "rss",
        tags: [],
      } as unknown as Feed,
    ];
    const chip = feedScopeChip({ kind: "feed", value: "daily" }, feeds);

    expect(chip).toMatchObject({
      title: "My Daily",
      ariaLabel: "Clear feed filter: My Daily",
    });
  });

  it("clears an active feed scope on Escape", () => {
    expect(scopeForClosedEscape({ kind: "feed", value: "daily" })).toBeNull();
  });
});

describe("feed scope options", () => {
  it("excludes muted feeds, sorts by display title, and prefers custom titles", () => {
    const feeds = [
      {
        feed_id: "zeta",
        url: "https://zeta.example.com/",
        title: "Zeta",
        connector: "rss",
        item_count: 3,
      },
      {
        feed_id: "alpha",
        url: "https://alpha.example.com/",
        title: "Alpha",
        custom_title: "Aardvark",
        connector: "rss",
        item_count: 5,
      },
      {
        feed_id: "muted",
        url: "https://muted.example.com/",
        title: "Muted",
        connector: "rss",
        muted: true,
        item_count: 100,
      },
    ] as Feed[];

    expect(feedScopeOptions(feeds)).toMatchObject([
      { feedID: "alpha", title: "Aardvark", count: 5 },
      { feedID: "zeta", title: "Zeta", count: 3 },
    ]);
  });
});
