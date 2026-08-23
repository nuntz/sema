import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";

const item = (index: number, size: Item["size"], ratio = 1.5): Item => ({
  item_id: String(index),
  feed_id: "f",
  url: "https://example.com",
  title: `Item ${index}`,
  published_ts: new Date().toISOString(),
  fetched_ts: new Date().toISOString(),
  has_body: true,
  score: 0.5,
  size,
  read: false,
  signal: 0,
  hearted: false,
  media_w: ratio * 100,
  media_h: 100,
});

describe("justify", () => {
  it("fills every row exactly", () => {
    const width = 1180;
    const rows = justify(
      [
        item(1, "S"),
        item(2, "M"),
        item(3, "L"),
        item(4, "L"),
        item(5, "S"),
        item(6, "M"),
      ],
      width,
    );
    for (const row of rows) {
      const used =
        row.cells.reduce((sum, cell) => sum + cell.width, 0) +
        (row.cells.length - 1) * 10;
      expect(used).toBeCloseTo(width, 5);
    }
  });

  it("caps large cells at one per row", () => {
    const rows = justify(
      Array.from({ length: 12 }, (_, index) => item(index, "L", 0.8)),
      1180,
    );
    for (const row of rows)
      expect(
        row.cells.filter((cell) => cell.effectiveSize === "L"),
      ).toHaveLength(1);
  });

  it("uses bounded, known row heights", () => {
    const rows = justify([item(1, "S"), item(2, "M"), item(3, "L")], 800);
    expect(rows.every((row) => [148, 176, 208].includes(row.height))).toBe(
      true,
    );
  });
});
