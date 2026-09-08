import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify, type LayoutRow } from "./justified";
import {
  cellRects,
  type LayoutRect,
  nearestCell,
  nearestPageCell,
  nextGridPageTop,
} from "./navigation";

describe("grid page scroll target", () => {
  const rows: LayoutRow[] = [0, 210, 420, 630].map((top) => ({
    top,
    height: 200,
    gap: 10,
    kind: "standard",
    cells: [],
  }));

  it("shows the partially visible bottom row from its top on successive pages", () => {
    expect(nextGridPageTop(rows, 0, 500)).toBe(434);
    expect(nextGridPageTop(rows, 224, 500)).toBe(644);
  });

  it("keeps a full viewport step when the bottom is in a gap or at a row edge", () => {
    for (const height of [214, 220, 224]) {
      expect(nextGridPageTop(rows, 0, height)).toBe(height);
    }
    expect(nextGridPageTop([], 0, 500)).toBe(500);
  });

  it("continues through rows taller than the viewport", () => {
    const tallRows = [{ ...rows[0], height: 1000 }];
    expect(nextGridPageTop(tallRows, 0, 500)).toBe(14);
    expect(nextGridPageTop(tallRows, 14, 500)).toBe(514);
    expect(nextGridPageTop(tallRows, 300, 500)).toBe(800);
  });
});

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
  it("navigates only displayed story headlines and crosses into the next row", () => {
    const lead = item(0, "L");
    const rows: LayoutRow[] = [
      {
        top: 0,
        height: 300,
        gap: 10,
        kind: "standard",
        cells: [
          {
            item: lead,
            story: {
              story_id: "story",
              source_count: 4,
              order_key: 1,
              size: "L",
              items: [lead, item(1, "S"), item(2, "S"), item(3, "S")],
            },
            left: 0,
            width: 200,
            effectiveSize: "L",
            headlineHeight: 104,
            headlineItemCount: 2,
          },
        ],
      },
      {
        top: 310,
        height: 100,
        gap: 10,
        kind: "standard",
        cells: [
          { item: item(4, "S"), left: 0, width: 200, effectiveSize: "S" },
        ],
      },
    ];
    expect(cellRects(rows).map(({ id }) => id)).toEqual([
      "story:story",
      "1",
      "2",
      "4",
    ]);
    expect(nearestCell(rows, "story:story", "down")).toBe("1");
    expect(nearestCell(rows, "1", "down")).toBe("2");
    expect(nearestCell(rows, "2", "down")).toBe("4");
    expect(nearestCell(rows, "4", "up")).toBe("2");
    expect(nearestCell(rows, "1", "up")).toBe("story:story");
  });

  it("does not move vertically into a same-row neighbor with a different height", () => {
    const rows: LayoutRow[] = [
      {
        top: 0,
        height: 300,
        gap: 10,
        kind: "standard",
        cells: [
          {
            item: item(0, "S"),
            left: 0,
            width: 100,
            height: 100,
            effectiveSize: "S",
          },
          {
            item: item(1, "L"),
            left: 110,
            width: 100,
            height: 300,
            effectiveSize: "L",
          },
        ],
      },
    ];
    expect(nearestCell(rows, "0", "down")).toBe("0");
    expect(nearestCell(rows, "1", "up")).toBe("1");
    expect(nearestCell(rows, "0", "right")).toBe("1");
  });
  const rect = (
    id: string,
    left: number,
    top: number,
    width = 100,
    height = 100,
  ): LayoutRect => ({
    id,
    left,
    top,
    right: left + width,
    bottom: top + height,
    centerX: left + width / 2,
    centerY: top + height / 2,
  });

  it("pages to the nearest visible position across unequal cells and columns", () => {
    const rects = [
      rect("old", 200, -200),
      rect("left", 0, 100),
      rect("right", 200, 100),
      rect("lower", 200, 300),
    ];
    expect(nearestPageCell(rects, 250, 170, 0, 500)).toBe("right");
    expect(nearestPageCell(rects, 50, 170, 0, 500)).toBe("left");
    expect(nearestPageCell(rects, 250, 380, 0, 500)).toBe("lower");
  });

  it("prefers a fully visible cell over a clipped edge cell", () => {
    expect(
      nearestPageCell(
        [rect("clipped", 0, -50), rect("visible", 0, 60)],
        50,
        20,
        0,
        500,
      ),
    ).toBe("visible");
  });

  it("supports cells taller than the viewport and an end card with no cells", () => {
    expect(
      nearestPageCell([rect("tall", 0, -100, 100, 800)], 50, 250, 0, 500),
    ).toBe("tall");
    expect(
      nearestPageCell([rect("above", 0, -100)], 50, 250, 0, 500),
    ).toBeUndefined();
  });
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
