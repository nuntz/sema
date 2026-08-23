import { describe, expect, it } from "vitest";
import { shouldConfirmArchiveRemoval, updateHeartState } from "./archive";
import type { Item } from "./types";

const item = (hearted: boolean): Item => ({
  item_id: "item",
  feed_id: "feed",
  url: "https://example.com",
  title: "Item",
  summary_source: "",
  published_ts: "2026-08-22T00:00:00Z",
  fetched_ts: "2026-08-22T00:00:00Z",
  has_body: true,
  extract_quality: 1,
  score: 0.5,
  size: "M",
  read: false,
  signal: 0,
  hearted,
});

describe("archive heart behavior", () => {
  it("supports optimistic toggling and failure rollback", () => {
    const original = [item(false)];
    const optimistic = updateHeartState(original, "item", true);
    expect(optimistic[0].hearted).toBe(true);
    expect(updateHeartState(optimistic, "item", false)[0]).toEqual(original[0]);
  });

  it("confirms removal only from the archive", () => {
    expect(shouldConfirmArchiveRemoval(true, true)).toBe(true);
    expect(shouldConfirmArchiveRemoval(false, true)).toBe(false);
    expect(shouldConfirmArchiveRemoval(true, false)).toBe(false);
  });
});
