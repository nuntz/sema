import { describe, expect, it } from "vitest";
import { resolveReaderItem } from "./reader-item";
import type { Item } from "./types";

const item = (itemID: string): Item => ({
  item_id: itemID,
  feed_id: "feed",
  url: `https://example.com/${itemID}`,
  title: itemID,
  summary_source: "",
  published_ts: "2026-08-30T00:00:00Z",
  fetched_ts: "2026-08-30T00:00:00Z",
  has_body: false,
  extract_quality: 0,
  score: 0,
  size: "M",
  read: false,
  signal: 0,
  hearted: false,
});

describe("reader item resolution", () => {
  it("retains an off-grid related item after panel results are cleared", () => {
    const related = item("related");

    expect(resolveReaderItem("related", [[], [], []], related)).toBe(related);
  });

  it("prefers a current collection item over the retained snapshot", () => {
    const retained = item("shared");
    const current = { ...retained, read: true };

    expect(resolveReaderItem("shared", [[current]], retained)).toBe(current);
  });

  it("does not resolve a retained item for another reader ID", () => {
    expect(resolveReaderItem("current", [[], [], []], item("stale"))).toBe(
      undefined,
    );
  });
});
