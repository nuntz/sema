import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";
import { nearestCell } from "./navigation";

const item = (index: number, size: Item["size"], ratio = 1.5): Item => ({
  item_id: String(index),
  feed_id: "f",
  url: "https://example.com",
  title: `Item ${index}`,
  summary_source: "",
  published_ts: "2026-08-24T00:00:00Z",
  fetched_ts: "2026-08-24T00:00:00Z",
  has_body: true,
  extract_quality: 1,
  score: 0.5,
  size,
  read: false,
  signal: 0,
  hearted: false,
  media_url: `https://example.com/${index}.jpg`,
  media_w: ratio * 100,
  media_h: 100,
});

describe("geometric grid navigation", () => {
  it("moves down from a spanning hero into its lower adjacent sub-row", () => {
    const entries = [
      item(0, "L", 1),
      ...Array.from({ length: 6 }, (_, index) => item(index + 1, "S")),
      item(7, "M"),
    ];
    const rows = justify(entries, 1248);

    expect(nearestCell(rows, "0", "down")).toBe("4");
    expect(nearestCell(rows, "1", "down")).toBe("4");
    expect(nearestCell(rows, "4", "up")).toBe("1");
    expect(
      rows.flatMap((row) => row.cells.map((cell) => cell.item.item_id)),
    ).toEqual(entries.map((entry) => entry.item_id));
  });

  it("keeps left/right on the visual line and crosses bands vertically", () => {
    const entries = [
      item(0, "L", 1),
      ...Array.from({ length: 6 }, (_, index) => item(index + 1, "S")),
      item(7, "M"),
      item(8, "S"),
    ];
    const rows = justify(entries, 1248);

    expect(nearestCell(rows, "1", "right")).toBe("2");
    expect(nearestCell(rows, "3", "right")).toBe("3");
    expect(nearestCell(rows, "5", "left")).toBe("4");
    expect(nearestCell(rows, "4", "down")).toBe("8");
  });

  it("moves geometrically across full-width mobile L and pair bands", () => {
    const rows = justify(
      [item(0, "L", 1), item(1, "M"), item(2, "S"), item(3, "M")],
      390,
    );
    expect(rows.every((row) => row.kind !== "span")).toBe(true);
    expect(nearestCell(rows, "0", "down")).toBe("1");
    expect(nearestCell(rows, "1", "down")).toBe("3");
    expect(nearestCell(rows, "2", "down")).toBe("3");
  });

  it("traverses hero rows left-to-right and crosses into a mosaic band", () => {
    const rows = justify(
      [
        item(0, "L", 1.5),
        item(1, "L", 1.5),
        item(2, "L", 1.5),
        item(3, "L", 1),
        ...Array.from({ length: 5 }, (_, index) => item(index + 4, "M")),
      ],
      1248,
    );
    expect(rows.map((row) => row.kind)).toEqual(["hero", "span"]);
    expect(nearestCell(rows, "0", "right")).toBe("1");
    expect(nearestCell(rows, "1", "right")).toBe("2");
    expect(nearestCell(rows, "2", "down")).toBe("6");
    expect(rows[1].cells.map((cell) => cell.item.item_id)).toContain(
      nearestCell(rows, "1", "down"),
    );
  });
});
