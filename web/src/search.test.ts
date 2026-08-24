import { describe, expect, it } from "vitest";
import {
  normalizeSearchResponse,
  SEARCH_DEBOUNCE_MS,
  visibleSearchSections,
} from "./search";
import type { Item, SearchResponse } from "./types";

const item = (id: string): Item => ({
  item_id: id,
  feed_id: "feed",
  url: "https://example.com",
  title: id,
  summary_source: "",
  published_ts: "2026-08-20T00:00:00Z",
  fetched_ts: "2026-08-20T00:00:00Z",
  has_body: false,
  extract_quality: 0,
  score: 0,
  size: "M",
  read: false,
  signal: 0,
  hearted: false,
});

describe("search presentation", () => {
  it("uses the specified debounce", () => {
    expect(SEARCH_DEBOUNCE_MS).toBe(300);
  });

  it("omits empty sections and preserves fixed section order", () => {
    const response: SearchResponse = {
      matches: { window: [item("literal")], archive: [] },
      related: { window: [], archive: [item("meaning")] },
      semantic_available: true,
    };
    expect(
      visibleSearchSections(response).map((section) => section.key),
    ).toEqual(["matches-window", "related-archive"]);
  });

  it("normalizes null collections from older or cached API responses", () => {
    const response = {
      matches: { window: null, archive: null },
      related: { window: null, archive: null },
      semantic_available: false,
    } as unknown as SearchResponse;

    expect(normalizeSearchResponse(response)).toEqual({
      matches: { window: [], archive: [] },
      related: { window: [], archive: [] },
      semantic_available: false,
    });
    expect(visibleSearchSections(response)).toEqual([]);
  });
});
